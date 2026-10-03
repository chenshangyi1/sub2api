package migration

import (
	"strings"
	"testing"
	"time"

	"github.com/bejix/upstream-ops/backend/storage"
)

func TestRequireVerifiedGatesOnlyLegacySchema(t *testing.T) {
	clean := openMigrationTestDB(t)
	if err := RequireVerified(clean, VersionV007LegacyImport); err != nil {
		t.Fatalf("clean schema gate: %v", err)
	}

	legacy := openMigrationTestDB(t)
	if err := legacy.Exec("CREATE TABLE connections (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	if err := RequireVerified(legacy, VersionV007LegacyImport); err == nil || !strings.Contains(err.Error(), "run migration") {
		t.Fatalf("missing metadata gate error = %v", err)
	}

	if err := EnsureMetadata(legacy); err != nil {
		t.Fatal(err)
	}
	fingerprint := testFingerprint(t, "legacy")
	if err := Begin(legacy, VersionV007LegacyImport, fingerprint, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := MarkApplied(legacy, VersionV007LegacyImport, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := RequireVerified(legacy, VersionV007LegacyImport); err == nil || !strings.Contains(err.Error(), MigrationStateApplied) {
		t.Fatalf("applied gate error = %v", err)
	}
	if err := MarkVerified(legacy, VersionV007LegacyImport, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := RequireVerified(legacy, VersionV007LegacyImport); err != nil {
		t.Fatalf("verified gate: %v", err)
	}
}

func TestRequireVerifiedAllowsCanonicalConnectionShadows(t *testing.T) {
	db := openMigrationTestDB(t)
	if err := db.AutoMigrate(&storage.Connection{}); err != nil {
		t.Fatal(err)
	}
	if err := RequireVerified(db, VersionV007LegacyImport); err != nil {
		t.Fatalf("empty canonical shadow table gate: %v", err)
	}

	shadow := storage.Connection{
		ID:          1,
		Name:        "canonical",
		BaseURL:     "https://canonical.example",
		AdminAPIKey: "",
		Enabled:     true,
		SyncMode:    storage.ConnectionSyncModeCanonicalTarget,
	}
	if err := db.Create(&shadow).Error; err != nil {
		t.Fatal(err)
	}
	if err := RequireVerified(db, VersionV007LegacyImport); err != nil {
		t.Fatalf("canonical shadow row gate: %v", err)
	}
}

func TestRequireVerifiedRejectsNonCanonicalConnectionRow(t *testing.T) {
	db := openMigrationTestDB(t)
	if err := db.AutoMigrate(&storage.Connection{}); err != nil {
		t.Fatal(err)
	}
	legacy := storage.Connection{
		ID:          1,
		Name:        "legacy",
		BaseURL:     "https://legacy.example",
		AdminAPIKey: "cipher",
		Enabled:     true,
		SyncMode:    "manual",
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := RequireVerified(db, VersionV007LegacyImport); err == nil || !strings.Contains(err.Error(), "run migration") {
		t.Fatalf("legacy connection row gate error = %v", err)
	}
}
