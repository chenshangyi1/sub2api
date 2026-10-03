package operations

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newDiagnosticsTestService(db *gorm.DB, loader targetReferenceLoader) *Service {
	return &Service{
		db: db, targetReferenceLoader: loader, now: time.Now,
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func openDiagnosticsTestDB(t *testing.T, name string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+name+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open diagnostics test database: %v", err)
	}
	return db
}

func execDiagnosticsSQL(t *testing.T, db *gorm.DB, statements ...string) {
	t.Helper()
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("execute diagnostics schema statement %q: %v", statement, err)
		}
	}
}

func TestCleanupInvalidDataDeletesOnlyProvenOrphans(t *testing.T) {
	ctx := context.Background()
	db := openDiagnosticsTestDB(t, "cleanup-operations")
	mainDB := openDiagnosticsTestDB(t, "cleanup-main")
	execDiagnosticsSQL(t, db,
		`CREATE TABLE operations_action_logs (id integer primary key autoincrement, action text not null, target text not null, success boolean not null, message text, created_at datetime not null)`,
		`CREATE TABLE upstream_sync_targets (id integer primary key)`,
		`CREATE TABLE bl_collection_sites (id integer primary key)`,
		`CREATE TABLE bl_source_bindings (id integer primary key, connection_id integer, target_type text, target_id integer, source_site_id integer)`,
		`CREATE TABLE upstream_sync_groups (id integer primary key)`,
		`CREATE TABLE upstream_sync_accounts (id integer primary key)`,
		`CREATE TABLE upstream_sync_managed_accounts (id integer primary key, sync_group_id integer, sync_account_id integer)`,
		`CREATE TABLE upstream_monitor_rules (id integer primary key, connection_id integer, account_id integer)`,
		`INSERT INTO upstream_sync_targets (id) VALUES (1)`,
		`INSERT INTO bl_collection_sites (id) VALUES (101)`,
		`INSERT INTO upstream_sync_groups (id) VALUES (201)`,
		`INSERT INTO upstream_sync_accounts (id) VALUES (301)`,
		`INSERT INTO bl_source_bindings (id, connection_id, target_type, target_id, source_site_id) VALUES (1, 1, 'account', 11, 101), (2, 1, 'account', 99, 101), (3, 1, 'group', 21, 999)`,
		`INSERT INTO upstream_sync_managed_accounts (id, sync_group_id, sync_account_id) VALUES (1, 201, 301), (2, 999, 301), (3, 201, 999)`,
		`INSERT INTO upstream_monitor_rules (id, connection_id, account_id) VALUES (1, 1, 11), (2, 9, 11), (3, 1, 99), (4, 1, 0)`,
	)
	execDiagnosticsSQL(t, mainDB,
		`CREATE TABLE accounts (id integer primary key)`,
		`CREATE TABLE groups (id integer primary key)`,
		`INSERT INTO accounts (id) VALUES (11)`,
		`INSERT INTO groups (id) VALUES (21)`,
	)
	now := time.Date(2026, 7, 29, 5, 30, 0, 0, time.UTC)
	service := &Service{
		db: db, mainDB: mainDB, now: func() time.Time { return now }, log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		targetReferenceLoader: func(_ context.Context, targetID int64) (targetReferenceIDs, error) {
			if targetID != 1 {
				return targetReferenceIDs{}, nil
			}
			return targetReferenceIDs{
				Accounts: map[int64]bool{11: true}, Groups: map[int64]bool{21: true},
				AccountsChecked: true, GroupsChecked: true,
			}, nil
		},
	}

	diagnostics, err := service.GetDiagnostics(ctx)
	if err != nil {
		t.Fatalf("get diagnostics before cleanup: %v", err)
	}
	if diagnostics.InvalidData != (InvalidData{Bindings: 2, ManagedAccounts: 2, ProbeRules: 3}) {
		t.Fatalf("unexpected invalid data counts: %+v", diagnostics.InvalidData)
	}

	deleted, err := service.CleanupInvalidData(ctx)
	if err != nil {
		t.Fatalf("cleanup invalid data: %v", err)
	}
	if *deleted != diagnostics.InvalidData {
		t.Fatalf("cleanup counts %+v do not match diagnostics %+v", *deleted, diagnostics.InvalidData)
	}

	for table, want := range map[string]int64{
		"bl_source_bindings":             1,
		"upstream_sync_managed_accounts": 1,
		"upstream_monitor_rules":         1,
	} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil {
			t.Fatalf("count retained rows in %s: %v", table, err)
		}
		if count != want {
			t.Fatalf("retained %d rows in %s, want %d", count, table, want)
		}
	}
	var action ActionLog
	if err := db.Where("action = ?", "cleanup_invalid_data").Take(&action).Error; err != nil {
		t.Fatalf("load cleanup action: %v", err)
	}
	if !action.Success || action.Message != "deleted bindings=2 managed_accounts=2 probe_rules=3" {
		t.Fatalf("unexpected cleanup action: %+v", action)
	}
}

func TestCleanupInvalidDataScopesReferencesByTarget(t *testing.T) {
	db := openDiagnosticsTestDB(t, "cleanup-target-scope")
	execDiagnosticsSQL(t, db,
		`CREATE TABLE upstream_sync_targets (id integer primary key)`,
		`CREATE TABLE bl_collection_sites (id integer primary key)`,
		`CREATE TABLE bl_source_bindings (id integer primary key, connection_id integer, target_type text, target_id integer, source_site_id integer)`,
		`INSERT INTO upstream_sync_targets (id) VALUES (1), (2)`,
		`INSERT INTO bl_collection_sites (id) VALUES (101)`,
		`INSERT INTO bl_source_bindings (id, connection_id, target_type, target_id, source_site_id) VALUES
			(1, 1, 'account', 11, 101), (2, 1, 'account', 22, 101),
			(3, 2, 'account', 22, 101), (4, 2, 'account', 11, 101)`,
	)
	service := newDiagnosticsTestService(db, func(_ context.Context, targetID int64) (targetReferenceIDs, error) {
		accounts := map[int64]bool{}
		if targetID == 1 {
			accounts[11] = true
		}
		if targetID == 2 {
			accounts[22] = true
		}
		return targetReferenceIDs{
			Accounts: accounts, Groups: map[int64]bool{}, AccountsChecked: true, GroupsChecked: true,
		}, nil
	})

	deleted, err := service.CleanupInvalidData(context.Background())
	if err != nil {
		t.Fatalf("cleanup target-scoped references: %v", err)
	}
	if *deleted != (InvalidData{Bindings: 2}) {
		t.Fatalf("deleted target-scoped references = %+v", *deleted)
	}
	var retained []int64
	if err := db.Table("bl_source_bindings").Order("id").Pluck("id", &retained).Error; err != nil {
		t.Fatal(err)
	}
	if len(retained) != 2 || retained[0] != 1 || retained[1] != 3 {
		t.Fatalf("retained binding ids = %v, want [1 3]", retained)
	}
}

func TestCleanupInvalidDataSkipsUnreachableTarget(t *testing.T) {
	db := openDiagnosticsTestDB(t, "cleanup-unreachable-target")
	execDiagnosticsSQL(t, db,
		`CREATE TABLE upstream_sync_targets (id integer primary key)`,
		`CREATE TABLE bl_collection_sites (id integer primary key)`,
		`CREATE TABLE bl_source_bindings (id integer primary key, connection_id integer, target_type text, target_id integer, source_site_id integer)`,
		`INSERT INTO upstream_sync_targets (id) VALUES (1)`,
		`INSERT INTO bl_collection_sites (id) VALUES (101)`,
		`INSERT INTO bl_source_bindings (id, connection_id, target_type, target_id, source_site_id) VALUES (1, 1, 'account', 999, 101)`,
	)
	service := newDiagnosticsTestService(db, func(context.Context, int64) (targetReferenceIDs, error) {
		return targetReferenceIDs{}, errors.New("target unavailable")
	})

	deleted, err := service.CleanupInvalidData(context.Background())
	if err != nil {
		t.Fatalf("cleanup unreachable target: %v", err)
	}
	if *deleted != (InvalidData{}) {
		t.Fatalf("deleted references for unreachable target = %+v", *deleted)
	}
	var count int64
	if err := db.Table("bl_source_bindings").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("retained bindings = %d, want 1", count)
	}
}

func TestGetDiagnosticsMapsWorkerTasksAndLogs(t *testing.T) {
	ctx := context.Background()
	db := openDiagnosticsTestDB(t, "diagnostics-mapping")
	execDiagnosticsSQL(t, db,
		`CREATE TABLE settings (key text primary key, value text, updated_at datetime)`,
		`CREATE TABLE operations_action_logs (id integer primary key autoincrement, action text not null, target text not null, success boolean not null, message text, created_at datetime not null)`,
		`CREATE TABLE upstream_sync_logs (id integer primary key, target_id integer, action text, success boolean, message text, detail text, created_at datetime)`,
		`INSERT INTO settings (key, value) VALUES
			('worker_heartbeat_at', '2026-07-29T05:29:30Z'),
			('worker_last_run_finished_at', '2026-07-29T05:29:20Z'),
			('worker_last_run_status', 'success'),
			('worker_last_run_message', 'cycle complete'),
			('worker_interval_seconds', '600')`,
		`INSERT INTO operations_action_logs (id, action, target, success, message, created_at) VALUES (7, 'save_settings', 'target:1', true, 'saved', '2026-07-29T05:29:10Z')`,
		`INSERT INTO upstream_sync_logs (id, target_id, action, success, message, detail, created_at) VALUES (9, 1, 'apply_group', false, 'upstream rejected', '', '2026-07-29T05:29:40Z')`,
	)
	now := time.Date(2026, 7, 29, 5, 30, 0, 0, time.UTC)
	service := &Service{db: db, now: func() time.Time { return now }, log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	result, err := service.GetDiagnostics(ctx)
	if err != nil {
		t.Fatalf("get diagnostics: %v", err)
	}
	if result.Worker.Status != StatusHealthy || result.Worker.LastRunStatus != "success" || result.Worker.LastRunMessage != "cycle complete" {
		t.Fatalf("unexpected worker diagnostics: %+v", result.Worker)
	}
	if len(result.Tasks) != 1 || result.Tasks[0].Status != "failed" || result.Tasks[0].Name != "apply_group" {
		t.Fatalf("unexpected task diagnostics: %+v", result.Tasks)
	}
	if len(result.RecentLogs) != 2 || result.RecentLogs[0].Action != "apply_group" || result.RecentLogs[1].Action != "save_settings" {
		t.Fatalf("unexpected recent diagnostics logs: %+v", result.RecentLogs)
	}
	if result.Services[0].Status != StatusHealthy || result.Services[1].Status != StatusUnknown {
		t.Fatalf("unexpected service diagnostics: %+v", result.Services)
	}
}

func TestDiagnosticsMissingExtensionTablesReturnsZero(t *testing.T) {
	db := openDiagnosticsTestDB(t, "diagnostics-missing-tables")
	service := &Service{db: db, now: time.Now, log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	result, err := service.GetDiagnostics(context.Background())
	if err != nil {
		t.Fatalf("get diagnostics with missing extension tables: %v", err)
	}
	if result.InvalidData != (InvalidData{}) {
		t.Fatalf("missing extension tables should return zero counts: %+v", result.InvalidData)
	}
	if len(result.Tasks) != 0 || len(result.RecentLogs) != 0 {
		t.Fatalf("missing log tables should return empty diagnostics: tasks=%+v logs=%+v", result.Tasks, result.RecentLogs)
	}
	deleted, err := service.CleanupInvalidData(context.Background())
	if err != nil {
		t.Fatalf("cleanup with missing extension tables: %v", err)
	}
	if *deleted != (InvalidData{}) {
		t.Fatalf("cleanup with missing extension tables should return zero counts: %+v", *deleted)
	}
}
