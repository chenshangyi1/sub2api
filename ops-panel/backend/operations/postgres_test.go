package operations

import (
	"net/url"
	"os"
	"regexp"
	"testing"

	"github.com/bejix/upstream-ops/backend/storage"
	"gorm.io/gorm"
)

var postgresSchemaName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func openOperationsPostgresSchema(t *testing.T, schema string) *gorm.DB {
	t.Helper()
	rawDSN := os.Getenv("TEST_POSTGRES_DSN")
	if rawDSN == "" {
		t.Skip("TEST_POSTGRES_DSN is not set")
	}
	if !postgresSchemaName.MatchString(schema) {
		t.Fatalf("invalid PostgreSQL test schema %q", schema)
	}
	bootstrap, err := storage.Open(storage.DBConfig{URL: rawDSN, MaxOpenConns: 2, MaxIdleConns: 1})
	if err != nil {
		t.Fatalf("open PostgreSQL bootstrap database: %v", err)
	}
	if err := bootstrap.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = bootstrap.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE").Error
		if sqlDB, dbErr := bootstrap.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	parsed, err := url.Parse(rawDSN)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := storage.Open(storage.DBConfig{URL: parsed.String(), MaxOpenConns: 4, MaxIdleConns: 2})
	if err != nil {
		t.Fatalf("open PostgreSQL test schema: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}
