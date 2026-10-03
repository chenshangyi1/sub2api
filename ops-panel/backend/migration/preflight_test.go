package migration

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func openMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	return db
}

func createLegacyFixture(t *testing.T, db *gorm.DB) {
	createLegacyFixtureWithout(t, db, "", "")
}

func createLegacyFixtureWithout(t *testing.T, db *gorm.DB, missingTable, missingColumn string) {
	t.Helper()
	tables := make([]string, 0, len(legacyTableColumns))
	for table := range legacyTableColumns {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	for _, table := range tables {
		columns := make([]string, 0, len(legacyTableColumns[table]))
		for _, column := range legacyTableColumns[table] {
			if table == missingTable && column == missingColumn {
				continue
			}
			typeName := "TEXT"
			if column == "id" {
				typeName = "INTEGER"
			}
			columns = append(columns, fmt.Sprintf("%s %s", column, typeName))
		}
		ddl := fmt.Sprintf("CREATE TABLE %s (%s)", table, strings.Join(columns, ", "))
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("create %s: %v", table, err)
		}
	}
}

func TestPreflightAcceptsCompleteLegacySchema(t *testing.T) {
	db := openMigrationTestDB(t)
	createLegacyFixture(t, db)
	report, err := Preflight(context.Background(), db, PreflightOptions{})
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if report.Dialect != "sqlite" {
		t.Fatalf("dialect = %q", report.Dialect)
	}
	if len(report.LegacyRowCounts) != len(legacyTableColumns) {
		t.Fatalf("legacy counts = %d", len(report.LegacyRowCounts))
	}
}

func TestPreflightRejectsMissingLegacyColumn(t *testing.T) {
	db := openMigrationTestDB(t)
	createLegacyFixtureWithout(t, db, "connections", "admin_api_key")
	_, err := Preflight(context.Background(), db, PreflightOptions{})
	if err == nil || !strings.Contains(err.Error(), "connections.admin_api_key") {
		t.Fatalf("error = %v", err)
	}
}

func TestPreflightRejectsUntrackedCanonicalRows(t *testing.T) {
	db := openMigrationTestDB(t)
	createLegacyFixture(t, db)
	if err := db.Exec("CREATE TABLE channels (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO channels (id) VALUES (1)").Error; err != nil {
		t.Fatal(err)
	}
	_, err := Preflight(context.Background(), db, PreflightOptions{})
	if err == nil || !strings.Contains(err.Error(), "untracked rows") {
		t.Fatalf("error = %v", err)
	}
}

func TestPreflightProductionRequiresPostgres(t *testing.T) {
	db := openMigrationTestDB(t)
	_, err := Preflight(context.Background(), db, PreflightOptions{RequirePostgres: true})
	if err == nil || !strings.Contains(err.Error(), "requires postgres") {
		t.Fatalf("error = %v", err)
	}
}

func TestEnsureMetadataAndFingerprint(t *testing.T) {
	db := openMigrationTestDB(t)
	if err := EnsureMetadata(db); err != nil {
		t.Fatalf("ensure metadata: %v", err)
	}
	if !db.Migrator().HasTable(&SchemaMigration{}) || !db.Migrator().HasTable(&LegacyImportMap{}) {
		t.Fatal("metadata tables were not created")
	}

	a, err := Fingerprint(map[string]any{"b": 2, "a": 1})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Fingerprint(map[string]any{"a": 1, "b": 2})
	if err != nil {
		t.Fatal(err)
	}
	if a != b || len(a) != 64 {
		t.Fatalf("fingerprints differ: %q %q", a, b)
	}
}
