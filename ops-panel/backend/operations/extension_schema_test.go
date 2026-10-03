package operations

import (
	"testing"

	"github.com/bejix/upstream-ops/backend/migration"
	"github.com/bejix/upstream-ops/backend/storage"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestEnsureSchemaFreshSQLiteSurvivesRepeatedStartup(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open fresh SQLite database: %v", err)
	}
	assertFreshOperationsStartup(t, db)
}

func TestEnsureSchemaFreshPostgresSurvivesRepeatedStartup(t *testing.T) {
	db := openOperationsPostgresSchema(t, "ops_extension_schema_fresh")
	assertFreshOperationsStartup(t, db)

	var arrayType string
	if err := db.Raw(`
		SELECT data_type
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'announcement_rules'
		  AND column_name = 'target_group_ids'
	`).Scan(&arrayType).Error; err != nil {
		t.Fatalf("inspect announcement target group type: %v", err)
	}
	if arrayType != "ARRAY" {
		t.Fatalf("announcement target_group_ids type = %q, want ARRAY", arrayType)
	}

	var jsonType string
	if err := db.Raw(`
		SELECT data_type
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'api_capability_probe_runs'
		  AND column_name = 'results'
	`).Scan(&jsonType).Error; err != nil {
		t.Fatalf("inspect capability results type: %v", err)
	}
	if jsonType != "jsonb" {
		t.Fatalf("api capability results type = %q, want jsonb", jsonType)
	}

	var foreignKeys int64
	if err := db.Raw(`
		SELECT count(*)
		FROM information_schema.table_constraints
		WHERE table_schema = current_schema()
		  AND constraint_type = 'FOREIGN KEY'
		  AND constraint_name LIKE 'fk_ops_%'
	`).Scan(&foreignKeys).Error; err != nil {
		t.Fatalf("inspect extension foreign keys: %v", err)
	}
	if foreignKeys != int64(len(extensionForeignKeys)) {
		t.Fatalf("extension foreign key count = %d, want %d", foreignKeys, len(extensionForeignKeys))
	}
}

func assertFreshOperationsStartup(t *testing.T, db *gorm.DB) {
	t.Helper()
	startup := func(label string) {
		t.Helper()
		if err := migration.RequireVerified(db, migration.VersionV007LegacyImport); err != nil {
			t.Fatalf("%s migration gate: %v", label, err)
		}
		if err := storage.AutoMigrate(db); err != nil {
			t.Fatalf("%s core schema: %v", label, err)
		}
		if err := migration.EnsureMetadata(db); err != nil {
			t.Fatalf("%s migration metadata: %v", label, err)
		}
		if err := EnsureSchema(db); err != nil {
			t.Fatalf("%s extension schema: %v", label, err)
		}
	}

	startup("first startup")
	for _, contract := range extensionSchemaContracts {
		if !db.Migrator().HasTable(contract.table) {
			t.Fatalf("first startup did not create %s", contract.table)
		}
	}
	if !db.Migrator().HasTable((&migration.LegacyImportMap{}).TableName()) {
		t.Fatal("first startup did not create the worker mapping ledger")
	}

	marker := settingRow{Key: "worker_schema_test", Value: "preserved"}
	if err := db.Create(&marker).Error; err != nil {
		t.Fatalf("insert extension setting: %v", err)
	}
	startup("second startup")
	var stored settingRow
	if err := db.Where("key = ?", marker.Key).Take(&stored).Error; err != nil {
		t.Fatalf("read preserved extension setting: %v", err)
	}
	if stored.Value != marker.Value {
		t.Fatalf("extension setting value = %q, want %q", stored.Value, marker.Value)
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
		t.Fatalf("insert canonical connection shadow: %v", err)
	}
	startup("startup with canonical shadow")
}
