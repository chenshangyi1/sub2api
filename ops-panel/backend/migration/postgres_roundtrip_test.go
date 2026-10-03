package migration

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/bejix/upstream-ops/backend/storage"
	"gorm.io/gorm"
)

const postgresRoundTripSchema = "ops_migration_roundtrip"

func TestRunnerPostgresRoundTrip(t *testing.T) {
	rawDSN := os.Getenv("TEST_POSTGRES_DSN")
	if rawDSN == "" {
		t.Skip("TEST_POSTGRES_DSN is not set")
	}

	bootstrap, err := storage.Open(storage.DBConfig{URL: rawDSN, MaxOpenConns: 2, MaxIdleConns: 1})
	if err != nil {
		t.Fatalf("open bootstrap database: %v", err)
	}
	if err := bootstrap.Exec("DROP SCHEMA IF EXISTS " + postgresRoundTripSchema + " CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.Exec("CREATE SCHEMA " + postgresRoundTripSchema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = bootstrap.Exec("DROP SCHEMA IF EXISTS " + postgresRoundTripSchema + " CASCADE").Error
	})

	dsn, err := postgresDSNWithSearchPath(rawDSN, postgresRoundTripSchema)
	if err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(storage.DBConfig{URL: dsn, MaxOpenConns: 4, MaxIdleConns: 2})
	if err != nil {
		t.Fatalf("open scoped database: %v", err)
	}
	createPostgresLegacyFixture(t, db)
	seedRoundTripFixture(t, db)

	runner := newRoundTripRunner(t, db, true)
	ctx := context.Background()
	want := expectedRoundTripCounts()
	migrated, err := runner.Migrate(ctx)
	if err != nil {
		t.Fatalf("postgres migrate: %v", err)
	}
	assertCounts(t, migrated.Imported, want)
	verified, err := runner.Verify(ctx)
	if err != nil {
		t.Fatalf("postgres verify: %v", err)
	}
	assertCounts(t, verified.Verified, want)
	if err := RequireVerified(db, VersionV007LegacyImport); err != nil {
		t.Fatalf("postgres startup gate: %v", err)
	}
	rolledBack, err := runner.Rollback(ctx)
	if err != nil {
		t.Fatalf("postgres rollback: %v", err)
	}
	assertCounts(t, rolledBack.Deleted, want)
	remigrated, err := runner.Migrate(ctx)
	if err != nil {
		t.Fatalf("postgres remigrate: %v", err)
	}
	assertCounts(t, remigrated.Imported, want)
	if _, err := runner.Verify(ctx); err != nil {
		t.Fatalf("postgres reverify: %v", err)
	}
}

func postgresDSNWithSearchPath(rawDSN, schema string) (string, error) {
	u, err := url.Parse(rawDSN)
	if err != nil {
		return "", fmt.Errorf("parse test PostgreSQL DSN: %w", err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func createPostgresLegacyFixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	used := map[string]string{
		"admin_users": `(
			id BIGINT PRIMARY KEY, email TEXT NOT NULL, password_hash TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		"connections": `(
			id BIGINT PRIMARY KEY, name TEXT NOT NULL, base_url TEXT NOT NULL, admin_api_key TEXT NOT NULL,
			enabled BOOLEAN NOT NULL DEFAULT TRUE, sync_mode TEXT NOT NULL DEFAULT '', last_check_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		"bl_collection_sites": `(
			id BIGINT PRIMARY KEY, connection_id BIGINT NOT NULL, name TEXT NOT NULL, base_url TEXT NOT NULL,
			site_type TEXT NOT NULL, email TEXT, password_enc TEXT NOT NULL DEFAULT '', auth_mode TEXT NOT NULL,
			enabled BOOLEAN NOT NULL DEFAULT TRUE, interval_min INTEGER NOT NULL DEFAULT 0,
			recharge_ratio DOUBLE PRECISION NOT NULL, access_token TEXT, refresh_token TEXT, token_expire BIGINT,
			new_api_user_id TEXT, last_run_at TIMESTAMPTZ, last_status TEXT, last_error TEXT,
			consecutive_failures INTEGER NOT NULL DEFAULT 0, last_success_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		"bl_collection_runs": `(
			id BIGINT PRIMARY KEY, connection_id BIGINT NOT NULL, site_id BIGINT NOT NULL, status TEXT NOT NULL
		)`,
		"bl_collected_group_rates": `(
			id BIGINT PRIMARY KEY, connection_id BIGINT NOT NULL, site_id BIGINT NOT NULL, run_id BIGINT NOT NULL,
			group_id TEXT NOT NULL, name TEXT, platform TEXT, rate_multiplier DOUBLE PRECISION,
			user_rate DOUBLE PRECISION, effective_rate DOUBLE PRECISION, collected_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		"bl_collected_changes": `(
			id BIGINT PRIMARY KEY, connection_id BIGINT NOT NULL, site_id BIGINT NOT NULL, run_id BIGINT NOT NULL,
			entity_type TEXT NOT NULL, entity_key TEXT NOT NULL, field TEXT NOT NULL, old_value TEXT,
			new_value TEXT, change_type TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		"bl_source_bindings": `(
			id BIGINT PRIMARY KEY, connection_id BIGINT NOT NULL, target_type TEXT NOT NULL, target_id BIGINT NOT NULL,
			source_site_id BIGINT NOT NULL, source_site_name TEXT, source_group_id TEXT NOT NULL,
			source_group_name TEXT, source_platform TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
	}

	tables := make([]string, 0, len(legacyTableColumns))
	for table := range legacyTableColumns {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	for _, table := range tables {
		definition, ok := used[table]
		if !ok {
			columns := make([]string, 0, len(legacyTableColumns[table]))
			for _, column := range legacyTableColumns[table] {
				typeName := "TEXT"
				if column == "id" {
					typeName = "BIGINT"
				}
				columns = append(columns, fmt.Sprintf(`%q %s`, column, typeName))
			}
			definition = "(" + strings.Join(columns, ", ") + ")"
		}
		if err := db.Exec(fmt.Sprintf(`CREATE TABLE %q %s`, table, definition)).Error; err != nil {
			t.Fatalf("create PostgreSQL fixture table %s: %v", table, err)
		}
	}
}
