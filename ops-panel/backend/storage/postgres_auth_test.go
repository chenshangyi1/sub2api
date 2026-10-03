package storage

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func migrateAdminUsersForTest(t *testing.T) *AdminUsers {
	t.Helper()
	db := openTestDB(t)
	if err := db.AutoMigrate(&AdminUser{}); err != nil {
		t.Fatalf("migrate admin users: %v", err)
	}
	return NewAdminUsers(db)
}

func TestAdminUsersSeedAndAuthenticate(t *testing.T) {
	admins := migrateAdminUsersForTest(t)
	ctx := context.Background()

	seeded, err := admins.SeedIfEmpty(ctx, "admin@example.com", " seed-secret ")
	if err != nil {
		t.Fatalf("SeedIfEmpty: %v", err)
	}
	if !seeded {
		t.Fatal("SeedIfEmpty did not create the first administrator")
	}

	subject, ok, err := admins.Authenticate(ctx, "admin@example.com", " seed-secret ")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if !ok || subject != "admin@example.com" {
		t.Fatalf("Authenticate = (%q, %v)", subject, ok)
	}
	if _, ok, err := admins.Authenticate(ctx, "admin@example.com", "seed-secret"); err != nil || ok {
		t.Fatalf("trimmed password = (%v, %v)", ok, err)
	}
	if _, ok, err := admins.Authenticate(ctx, "admin@example.com", "wrong"); err != nil || ok {
		t.Fatalf("wrong password = (%v, %v)", ok, err)
	}
}

func TestAdminUsersExistingRowWinsOverSeed(t *testing.T) {
	admins := migrateAdminUsersForTest(t)
	ctx := context.Background()
	hash, err := bcrypt.GenerateFromPassword([]byte("existing-secret"), 4)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	legacyHash := strings.Replace(string(hash), "$2a$", "$2b$", 1)
	existing := &AdminUser{Email: "existing@example.com", PasswordHash: legacyHash}
	if err := admins.db.Create(existing).Error; err != nil {
		t.Fatalf("create existing admin: %v", err)
	}

	seeded, err := admins.SeedIfEmpty(ctx, "seed@example.com", "seed-secret")
	if err != nil {
		t.Fatalf("SeedIfEmpty: %v", err)
	}
	if seeded {
		t.Fatal("SeedIfEmpty replaced an existing database account")
	}
	if exists, err := admins.SubjectExists(ctx, "seed@example.com"); err != nil || exists {
		t.Fatalf("seed subject exists = (%v, %v)", exists, err)
	}
	if exists, err := admins.SubjectExists(ctx, existing.Email); err != nil || !exists {
		t.Fatalf("existing subject exists = (%v, %v)", exists, err)
	}
	if _, ok, err := admins.Authenticate(ctx, existing.Email, "existing-secret"); err != nil || !ok {
		t.Fatalf("bcryptjs-compatible password hash = (%v, %v)", ok, err)
	}
}

func TestPostgresDatabaseURLSelectsDriverAndNormalizesSchema(t *testing.T) {
	cfg := DBConfig{
		Driver: DBDriverSQLite,
		URL:    "postgresql://user:p%40ss@db.example:5432/ops?schema=tenant_a&sslmode=require",
	}
	if got := cfg.EffectiveDriver(); got != DBDriverPostgres {
		t.Fatalf("EffectiveDriver = %q", got)
	}
	dsn, err := cfg.PostgresDSN()
	if err != nil {
		t.Fatalf("PostgresDSN: %v", err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse normalized DSN: %v", err)
	}
	if got := parsed.Query().Get("schema"); got != "" {
		t.Fatalf("schema query = %q", got)
	}
	if got := parsed.Query().Get("search_path"); got != "tenant_a" {
		t.Fatalf("search_path = %q", got)
	}
	if got := parsed.Query().Get("sslmode"); got != "require" {
		t.Fatalf("sslmode = %q", got)
	}
	dialector, driver, err := dialectorForConfig(cfg)
	if err != nil {
		t.Fatalf("dialectorForConfig: %v", err)
	}
	if driver != DBDriverPostgres || dialector.Name() != "postgres" {
		t.Fatalf("dialector = (%q, %q)", driver, dialector.Name())
	}
}

func TestPostgresDatabaseURLRejectsOtherSchemes(t *testing.T) {
	_, err := (DBConfig{URL: "mysql://user:secret@db.example/ops"}).PostgresDSN()
	if err == nil {
		t.Fatal("PostgresDSN accepted a non-PostgreSQL URL")
	}
}

func TestPostgresOpenIntegration(t *testing.T) {
	rawURL := os.Getenv("TEST_POSTGRES_URL")
	if rawURL == "" {
		t.Skip("TEST_POSTGRES_URL is not set")
	}
	db, err := Open(DBConfig{URL: rawURL, MaxOpenConns: 2, MaxIdleConns: 1})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	var searchPath string
	if err := db.Raw("SHOW search_path").Scan(&searchPath).Error; err != nil {
		t.Fatalf("SHOW search_path: %v", err)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse TEST_POSTGRES_URL: %v", err)
	}
	if schema := parsed.Query().Get("schema"); schema != "" && !strings.Contains(searchPath, schema) {
		t.Fatalf("search_path = %q, want schema %q", searchPath, schema)
	}
}
