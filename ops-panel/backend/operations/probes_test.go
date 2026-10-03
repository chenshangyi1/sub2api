package operations

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func openProbeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open probe test database: %v", err)
	}
	statements := []string{
		`CREATE TABLE upstream_sync_targets (id integer primary key)`,
		`CREATE TABLE upstream_monitor_rules (id integer primary key, connection_id integer, account_id integer, enabled boolean, next_check_at datetime, updated_at datetime)`,
		`CREATE TABLE settings (key text primary key, value text, updated_at datetime)`,
		`CREATE TABLE operations_action_logs (id integer primary key autoincrement, action text not null, target text not null, success boolean not null, message text, created_at datetime not null)`,
		`INSERT INTO upstream_sync_targets (id) VALUES (1), (2)`,
		`INSERT INTO upstream_monitor_rules (id, connection_id, account_id, enabled) VALUES (1, 1, 10, true), (2, 1, 20, true), (3, 1, 30, false), (4, 2, 20, true)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("execute probe test statement %q: %v", statement, err)
		}
	}
	return db
}

func TestMapProbeRulePreservesZeroAvailability(t *testing.T) {
	zero := 0.0
	probe := mapProbeRule(probeRuleRow{ID: 1, AccountID: 10, Enabled: true}, "https://target.example", "openai", &zero)
	if probe.Availability7D == nil || *probe.Availability7D != 0 {
		t.Fatalf("zero-percent availability was lost: %+v", probe.Availability7D)
	}
	payload, err := json.Marshal(probe)
	if err != nil {
		t.Fatalf("marshal probe: %v", err)
	}
	if !strings.Contains(string(payload), `"availability_7d":0`) {
		t.Fatalf("zero-percent availability missing from JSON: %s", payload)
	}

	unknown := mapProbeRule(probeRuleRow{ID: 2, AccountID: 20}, "https://target.example", "openai", nil)
	payload, err = json.Marshal(unknown)
	if err != nil {
		t.Fatalf("marshal unknown probe: %v", err)
	}
	if strings.Contains(string(payload), `"availability_7d"`) {
		t.Fatalf("unknown availability should be omitted: %s", payload)
	}
}

func TestRunProbeBatchRestrictsEnabledRequestedRules(t *testing.T) {
	db := openProbeTestDB(t)
	now := time.Date(2026, 7, 29, 7, 0, 0, 0, time.UTC)
	service := &Service{
		db:  db,
		now: func() time.Time { return now },
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	queued, err := service.RunProbeBatch(context.Background(), 1, "heavy", ProbeBatchFilter{
		ProbeIDs:   []int64{2, 3, 4},
		AccountIDs: []int64{20, 30},
	})
	if err != nil {
		t.Fatalf("run filtered probe batch: %v", err)
	}
	if queued != 1 {
		t.Fatalf("queued %d probes, want 1", queued)
	}

	var queuedIDs []int64
	if err := db.Table("upstream_monitor_rules").Where("next_check_at IS NOT NULL").Order("id").Pluck("id", &queuedIDs).Error; err != nil {
		t.Fatalf("load queued probe ids: %v", err)
	}
	if len(queuedIDs) != 1 || queuedIDs[0] != 2 {
		t.Fatalf("queued probe ids %v, want [2]", queuedIDs)
	}

	settings := map[string]string{}
	var rows []settingRow
	if err := db.Find(&rows).Error; err != nil {
		t.Fatalf("load worker request settings: %v", err)
	}
	for _, row := range rows {
		settings[row.Key] = row.Value
	}
	if settings["worker_run_requested_target_id"] != "1" || settings["worker_run_requested_mode"] != "probe:heavy" {
		t.Fatalf("unexpected worker request settings: %+v", settings)
	}
}

func TestRunProbeBatchRejectsEmptyRangeAndCapabilityMode(t *testing.T) {
	db := openProbeTestDB(t)
	service := &Service{
		db:  db,
		now: time.Now,
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	if _, err := service.RunProbeBatch(context.Background(), 1, "heavy", ProbeBatchFilter{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty range error = %v, want ErrInvalid", err)
	}
	if _, err := service.RunProbeBatch(context.Background(), 1, "capability", ProbeBatchFilter{ProbeIDs: []int64{1}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("capability mode error = %v, want ErrInvalid", err)
	}
}
