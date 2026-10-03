package migration

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func testFingerprint(t *testing.T, value string) string {
	t.Helper()
	fingerprint, err := Fingerprint(value)
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint
}

func TestRecordImportIsIdempotentAndDetectsDrift(t *testing.T) {
	db := openMigrationTestDB(t)
	if err := EnsureMetadata(db); err != nil {
		t.Fatal(err)
	}
	fingerprint := testFingerprint(t, "source-a")
	record := ImportRecord{
		MigrationVersion:  VersionV007LegacyImport,
		LegacyTable:       "connections",
		LegacyID:          "7",
		CanonicalTable:    "upstream_sync_targets",
		CanonicalID:       "11",
		SourceFingerprint: fingerprint,
	}
	row, created, err := RecordImport(db, record)
	if err != nil {
		t.Fatalf("record import: %v", err)
	}
	if !created || row.ID == 0 {
		t.Fatalf("first record created=%v row=%+v", created, row)
	}

	second, created, err := RecordImport(db, record)
	if err != nil {
		t.Fatalf("repeat import: %v", err)
	}
	if created || second.ID != row.ID {
		t.Fatalf("repeat created=%v row=%+v", created, second)
	}

	drifted := record
	drifted.SourceFingerprint = testFingerprint(t, "source-b")
	if _, _, err := RecordImport(db, drifted); !errors.Is(err, ErrSourceFingerprintChanged) {
		t.Fatalf("source drift error = %v", err)
	}
	remapped := record
	remapped.CanonicalID = "12"
	if _, _, err := RecordImport(db, remapped); !errors.Is(err, ErrCanonicalMappingChanged) {
		t.Fatalf("mapping drift error = %v", err)
	}
}

func TestRecordImportCanReactivateRolledBackMapping(t *testing.T) {
	db := openMigrationTestDB(t)
	if err := EnsureMetadata(db); err != nil {
		t.Fatal(err)
	}
	record := ImportRecord{
		MigrationVersion:  VersionV007LegacyImport,
		LegacyTable:       "bl_collection_sites",
		LegacyID:          "3",
		CanonicalTable:    "channels",
		CanonicalID:       "5",
		SourceFingerprint: testFingerprint(t, "old"),
	}
	row, _, err := RecordImport(db, record)
	if err != nil {
		t.Fatal(err)
	}
	if err := MarkImportRolledBack(db, row.ID, time.Now()); err != nil {
		t.Fatal(err)
	}

	record.CanonicalID = "9"
	record.SourceFingerprint = testFingerprint(t, "new")
	reactivated, created, err := RecordImport(db, record)
	if err != nil {
		t.Fatalf("reactivate: %v", err)
	}
	if !created || reactivated.ID != row.ID || reactivated.CanonicalID != "9" || reactivated.RolledBackAt != nil {
		t.Fatalf("reactivated row = %+v created=%v", reactivated, created)
	}
	rows, err := ActiveImports(db, VersionV007LegacyImport)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].CanonicalID != "9" {
		t.Fatalf("active rows = %+v", rows)
	}
}

func TestMigrationLifecycle(t *testing.T) {
	db := openMigrationTestDB(t)
	if err := EnsureMetadata(db); err != nil {
		t.Fatal(err)
	}
	version := VersionV007LegacyImport
	fingerprint := testFingerprint(t, "database snapshot")
	startedAt := time.Date(2026, 7, 29, 1, 2, 3, 0, time.UTC)
	if err := Begin(db, version, fingerprint, startedAt); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := MarkApplied(db, version, startedAt.Add(time.Minute)); err != nil {
		t.Fatalf("applied: %v", err)
	}
	if err := MarkVerified(db, version, startedAt.Add(2*time.Minute)); err != nil {
		t.Fatalf("verified: %v", err)
	}
	if err := Begin(db, version, fingerprint, time.Time{}); err != nil {
		t.Fatalf("idempotent begin: %v", err)
	}
	if err := Begin(db, version, testFingerprint(t, "other snapshot"), time.Time{}); !errors.Is(err, ErrSourceFingerprintChanged) {
		t.Fatalf("changed source error = %v", err)
	}
	if err := MarkApplied(db, version, time.Time{}); err == nil || !strings.Contains(err.Error(), "not eligible") {
		t.Fatalf("invalid transition error = %v", err)
	}
	if err := MarkRolledBack(db, version, startedAt.Add(3*time.Minute)); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if err := Begin(db, version, testFingerprint(t, "other snapshot"), startedAt.Add(4*time.Minute)); err != nil {
		t.Fatalf("restart after rollback: %v", err)
	}
}

func TestMarkFailedStoresState(t *testing.T) {
	db := openMigrationTestDB(t)
	if err := EnsureMetadata(db); err != nil {
		t.Fatal(err)
	}
	fingerprint := testFingerprint(t, "snapshot")
	if err := Begin(db, VersionV007LegacyImport, fingerprint, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := MarkFailed(db, VersionV007LegacyImport, time.Time{}, errors.New("fixture failure")); err != nil {
		t.Fatal(err)
	}
	var row SchemaMigration
	if err := db.Take(&row, "version = ?", VersionV007LegacyImport).Error; err != nil {
		t.Fatal(err)
	}
	if row.State != MigrationStateFailed || row.ErrorMessage != "fixture failure" {
		t.Fatalf("migration row = %+v", row)
	}
}
