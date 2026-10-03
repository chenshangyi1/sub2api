package migration

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	roundTripAppSecret      = "round-trip-app-secret"
	roundTripAdminAPIKey    = "legacy-admin-api-key"
	roundTripPassword       = "legacy-channel-password"
	roundTripSessionCookie  = "legacy-session-cookie"
	roundTripMigrationClock = "2026-07-29T08:00:00Z"
)

func openRoundTripFixture(t *testing.T) (*gorm.DB, *Runner) {
	t.Helper()
	db := openMigrationTestDB(t)
	createLegacyFixture(t, db)
	addRoundTripFixtureColumns(t, db)
	seedRoundTripFixture(t, db)

	runner := newRoundTripRunner(t, db, false)
	return db, runner
}

func newRoundTripRunner(t *testing.T, db *gorm.DB, requirePostgres bool) *Runner {
	t.Helper()
	fixedNow, err := time.Parse(time.RFC3339, roundTripMigrationClock)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewRunner(db, RunnerOptions{
		LegacyEncryptionKey: base64.StdEncoding.EncodeToString(roundTripLegacyKey()),
		AppSecret:           roundTripAppSecret,
		RequirePostgres:     requirePostgres,
		Now:                 func() time.Time { return fixedNow },
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	return runner
}

func roundTripLegacyKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}

func addRoundTripFixtureColumns(t *testing.T, db *gorm.DB) {
	t.Helper()
	columns := map[string][]string{
		"admin_users": {
			"created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
			"updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
		},
		"bl_collection_sites": {
			"email TEXT",
			"enabled INTEGER",
			"interval_min INTEGER",
			"access_token TEXT",
			"refresh_token TEXT",
			"token_expire INTEGER",
			"new_api_user_id TEXT",
		},
		"bl_collected_group_rates": {
			"name TEXT",
			"platform TEXT",
			"rate_multiplier REAL",
			"user_rate REAL",
		},
		"bl_collected_changes": {
			"old_value TEXT",
			"new_value TEXT",
			"change_type TEXT",
		},
		"bl_source_bindings": {
			"source_site_name TEXT",
			"source_group_name TEXT",
			"source_platform TEXT",
		},
	}
	for table, definitions := range columns {
		for _, definition := range definitions {
			if err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", table, definition)).Error; err != nil {
				t.Fatalf("add %s.%s: %v", table, definition, err)
			}
		}
	}
}

func seedRoundTripFixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("admin-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	legacyKey := roundTripLegacyKey()
	passwordCipher := encodeLegacyTestValue(
		t,
		legacyKey,
		[]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12},
		[]byte(roundTripPassword),
	)
	adminAPIKeyCipher := encodeLegacyTestValue(
		t,
		legacyKey,
		[]byte{12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1},
		[]byte(roundTripAdminAPIKey),
	)

	statements := []struct {
		query string
		args  []any
	}{
		{
			"INSERT INTO admin_users (id, email, password_hash) VALUES (?, ?, ?)",
			[]any{1, "admin@example.test", string(passwordHash)},
		},
		{
			"INSERT INTO connections (id, name, base_url, admin_api_key, enabled, last_check_at) VALUES (?, ?, ?, ?, ?, NULL)",
			[]any{10, "Primary target", "https://target.example.test/", adminAPIKeyCipher, true},
		},
		{
			"INSERT INTO bl_collection_sites (id, connection_id, name, base_url, site_type, email, password_enc, auth_mode, enabled, interval_min, recharge_ratio) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			[]any{20, 10, "Password channel", "https://password.example.test/", "sub2api", "password@example.test", passwordCipher, "password", true, 15, 2.0},
		},
		{
			"INSERT INTO bl_collection_sites (id, connection_id, name, base_url, site_type, email, password_enc, auth_mode, enabled, interval_min, recharge_ratio, access_token, token_expire, new_api_user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			[]any{21, 10, "Token channel", "https://token.example.test/", "new_api", "token@example.test", "", "manual_token", true, 30, 4.0, "session:" + roundTripSessionCookie + "::42", int64(1_800_000_000), "42"},
		},
		{
			"INSERT INTO bl_collection_runs (id, connection_id, site_id, status) VALUES (?, ?, ?, ?)",
			[]any{30, 10, 20, "success"},
		},
		{
			"INSERT INTO bl_collection_runs (id, connection_id, site_id, status) VALUES (?, ?, ?, ?)",
			[]any{31, 10, 21, "success"},
		},
		{
			"INSERT INTO bl_collected_group_rates (id, connection_id, site_id, run_id, group_id, name, platform, effective_rate) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			[]any{40, 10, 20, 30, "1001", "Standard", "openai", 6.0},
		},
		{
			"INSERT INTO bl_collected_group_rates (id, connection_id, site_id, run_id, group_id, name, platform, effective_rate) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			[]any{41, 10, 21, 31, "1002", "Premium", "openai", 8.0},
		},
		{
			"INSERT INTO bl_collected_changes (id, connection_id, site_id, run_id, entity_type, entity_key, field, old_value, new_value, change_type) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			[]any{50, 10, 20, 30, "group", "1001", "rateMultiplier", "4", "6", "updated"},
		},
		{
			"INSERT INTO bl_source_bindings (id, connection_id, target_type, target_id, source_site_id, source_site_name, source_group_id, source_group_name, source_platform) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			[]any{60, 10, "group", 7001, 20, "Password channel", "1001", "Standard", "openai"},
		},
		{
			"INSERT INTO bl_source_bindings (id, connection_id, target_type, target_id, source_site_id, source_site_name, source_group_id, source_group_name, source_platform) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			[]any{61, 10, "account", 8001, 21, "Token channel", "1002", "Premium", "openai"},
		},
	}
	for _, statement := range statements {
		if err := db.Exec(statement.query, statement.args...).Error; err != nil {
			t.Fatalf("seed round-trip fixture with %q: %v", statement.query, err)
		}
	}
}

func expectedRoundTripCounts() map[string]int {
	return map[string]int{
		"upstream_sync_targets":  1,
		"channels":               2,
		"auth_sessions":          1,
		"rate_snapshots":         2,
		"rate_change_logs":       1,
		"upstream_sync_groups":   1,
		"upstream_sync_accounts": 1,
	}
}

func assertCounts(t *testing.T, got map[string]int, want map[string]int) {
	t.Helper()
	for table, count := range want {
		if got[table] != count {
			t.Errorf("%s count = %d, want %d (all counts: %+v)", table, got[table], count, got)
		}
	}
}

func assertDatabaseTableCounts(t *testing.T, db *gorm.DB, want map[string]int) {
	t.Helper()
	for table, count := range want {
		var got int64
		if err := db.Table(table).Count(&got).Error; err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != int64(count) {
			t.Errorf("%s rows = %d, want %d", table, got, count)
		}
	}
}

func sumCounts(counts map[string]int) int {
	total := 0
	for _, count := range counts {
		total += count
	}
	return total
}

func TestRunnerSQLiteRoundTripIsIdempotent(t *testing.T) {
	db, runner := openRoundTripFixture(t)
	ctx := context.Background()
	want := expectedRoundTripCounts()
	wantLedger := int64(sumCounts(want))

	first, err := runner.Migrate(ctx)
	if err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if first.State != MigrationStateApplied {
		t.Fatalf("first migration state = %q", first.State)
	}
	assertCounts(t, first.Imported, want)
	assertDatabaseTableCounts(t, db, want)

	var target struct{ AdminAPIKeyCipher string }
	if err := db.Table("upstream_sync_targets").Select("admin_api_key_cipher").Where("id = ?", 10).Take(&target).Error; err != nil {
		t.Fatal(err)
	}
	if target.AdminAPIKeyCipher == roundTripAdminAPIKey {
		t.Fatal("target API key was stored as plaintext")
	}
	plain, err := runner.appCipher.Decrypt(target.AdminAPIKeyCipher)
	if err != nil || plain != roundTripAdminAPIKey {
		t.Fatalf("target API key decrypt = %q, %v", plain, err)
	}

	var channel struct{ PasswordCipher string }
	if err := db.Table("channels").Select("password_cipher").Where("id = ?", 20).Take(&channel).Error; err != nil {
		t.Fatal(err)
	}
	plain, err = runner.appCipher.Decrypt(channel.PasswordCipher)
	if err != nil || plain != roundTripPassword {
		t.Fatalf("channel password decrypt = %q, %v", plain, err)
	}

	var session struct{ CookieCipher string }
	if err := db.Table("auth_sessions").Select("cookie_cipher").Where("channel_id = ?", 21).Take(&session).Error; err != nil {
		t.Fatal(err)
	}
	plain, err = runner.appCipher.Decrypt(session.CookieCipher)
	if err != nil || plain != roundTripSessionCookie {
		t.Fatalf("session cookie decrypt = %q, %v", plain, err)
	}

	active, err := ActiveImports(db, VersionV007LegacyImport)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(active)) != wantLedger {
		t.Fatalf("active ledger rows = %d, want %d", len(active), wantLedger)
	}

	second, err := runner.Migrate(ctx)
	if err != nil {
		t.Fatalf("idempotent migrate: %v", err)
	}
	if second.State != MigrationStateApplied {
		t.Fatalf("idempotent migration state = %q", second.State)
	}
	assertCounts(t, second.Imported, want)
	assertDatabaseTableCounts(t, db, want)

	verified, err := runner.Verify(ctx)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verified.State != MigrationStateVerified {
		t.Fatalf("verified state = %q", verified.State)
	}
	assertCounts(t, verified.Verified, want)

	rolledBack, err := runner.Rollback(ctx)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if rolledBack.State != MigrationStateRolledBack {
		t.Fatalf("rollback state = %q", rolledBack.State)
	}
	assertCounts(t, rolledBack.Deleted, want)
	assertDatabaseTableCounts(t, db, zeroCountsFor(want))

	active, err = ActiveImports(db, VersionV007LegacyImport)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("active ledger rows after rollback = %d", len(active))
	}
	var ledgerRows, rolledBackRows int64
	if err := db.Model(&LegacyImportMap{}).Where("migration_version = ?", VersionV007LegacyImport).Count(&ledgerRows).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&LegacyImportMap{}).Where("migration_version = ? AND rolled_back_at IS NOT NULL", VersionV007LegacyImport).Count(&rolledBackRows).Error; err != nil {
		t.Fatal(err)
	}
	if ledgerRows != wantLedger || rolledBackRows != wantLedger {
		t.Fatalf("ledger after rollback: total=%d rolled_back=%d, want %d", ledgerRows, rolledBackRows, wantLedger)
	}
	var legacySites int64
	if err := db.Table("bl_collection_sites").Count(&legacySites).Error; err != nil {
		t.Fatal(err)
	}
	if legacySites != 2 {
		t.Fatalf("legacy sites after rollback = %d, want 2", legacySites)
	}

	reimported, err := runner.Migrate(ctx)
	if err != nil {
		t.Fatalf("migrate after rollback: %v", err)
	}
	if reimported.State != MigrationStateApplied {
		t.Fatalf("reimport state = %q", reimported.State)
	}
	assertCounts(t, reimported.Imported, want)
	assertDatabaseTableCounts(t, db, want)
	var finalLedgerRows int64
	if err := db.Model(&LegacyImportMap{}).Where("migration_version = ?", VersionV007LegacyImport).Count(&finalLedgerRows).Error; err != nil {
		t.Fatal(err)
	}
	if finalLedgerRows != wantLedger {
		t.Fatalf("ledger rows after reimport = %d, want reused %d", finalLedgerRows, wantLedger)
	}
}

func zeroCountsFor(source map[string]int) map[string]int {
	zero := make(map[string]int, len(source))
	for table := range source {
		zero[table] = 0
	}
	return zero
}

func TestRunnerVerifyRejectsLegacySourceDrift(t *testing.T) {
	db, runner := openRoundTripFixture(t)
	if _, err := runner.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	changedCipher := encodeLegacyTestValue(
		t,
		roundTripLegacyKey(),
		[]byte{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13},
		[]byte("changed-source-key"),
	)
	if err := db.Table("connections").Where("id = ?", 10).Update("admin_api_key", changedCipher).Error; err != nil {
		t.Fatal(err)
	}
	_, err := runner.Verify(context.Background())
	if !errors.Is(err, ErrSourceFingerprintChanged) {
		t.Fatalf("verify source drift error = %v", err)
	}
}

func TestRunnerVerifyRejectsCanonicalCipherTampering(t *testing.T) {
	db, runner := openRoundTripFixture(t)
	if _, err := runner.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	tamperedCipher, err := runner.appCipher.Encrypt("different-admin-api-key")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Table("upstream_sync_targets").Where("id = ?", 10).Update("admin_api_key_cipher", tamperedCipher).Error; err != nil {
		t.Fatal(err)
	}
	_, err = runner.Verify(context.Background())
	if err == nil {
		t.Fatalf("verify tampered target error = %v", err)
	}

	var state SchemaMigration
	if loadErr := db.Where("version = ?", VersionV007LegacyImport).Take(&state).Error; loadErr != nil {
		t.Fatal(loadErr)
	}
	if state.State != MigrationStateApplied {
		t.Fatalf("state after failed verify = %q, want %q", state.State, MigrationStateApplied)
	}
}

func TestRunnerRollbackRejectsCanonicalDrift(t *testing.T) {
	db, runner := openRoundTripFixture(t)
	if _, err := runner.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Table("upstream_sync_targets").Where("id = ?", 10).Update("name", "edited after import").Error; err != nil {
		t.Fatal(err)
	}

	_, err := runner.Rollback(context.Background())
	if !errors.Is(err, ErrRollbackDrift) {
		t.Fatalf("rollback drift error = %v", err)
	}
	var count int64
	if err := db.Table("upstream_sync_targets").Where("id = ?", 10).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("drifted canonical target count = %d, want 1", count)
	}
	active, err := ActiveImports(db, VersionV007LegacyImport)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) == 0 {
		t.Fatal("rollback marked imports rolled back after drift")
	}
}
