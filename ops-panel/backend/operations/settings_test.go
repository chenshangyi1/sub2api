package operations

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestOperationsSettingsOwnHeavyProbeIntervalGlobally(t *testing.T) {
	db := openProbeTestDB(t)
	now := time.Date(2026, 7, 29, 7, 0, 0, 0, time.UTC)
	service := &Service{
		db:  db,
		now: func() time.Time { return now },
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	saved, err := service.SaveSettings(context.Background(), OperationsSettings{
		HeavyProbeIntervalMinutes: 180,
	})
	if err != nil {
		t.Fatalf("save global operations settings: %v", err)
	}
	if saved.HeavyProbeIntervalMinutes != 180 {
		t.Fatalf("saved heavy interval = %d, want 180", saved.HeavyProbeIntervalMinutes)
	}

	_, err = service.SaveTargetSettings(context.Background(), 1, TargetSettings{
		AccountBalanceCooldownMinutes: 60,
		SuppressNativeMonitors:        true,
	})
	if err != nil {
		t.Fatalf("save target settings: %v", err)
	}
	global, err := service.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("load global operations settings: %v", err)
	}
	if global.HeavyProbeIntervalMinutes != 180 {
		t.Fatalf("target save changed global heavy interval to %d", global.HeavyProbeIntervalMinutes)
	}
}

func TestOperationsSettingsClampHeavyProbeInterval(t *testing.T) {
	db := openProbeTestDB(t)
	service := &Service{
		db:  db,
		now: time.Now,
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	saved, err := service.SaveSettings(context.Background(), OperationsSettings{})
	if err != nil {
		t.Fatalf("save clamped operations settings: %v", err)
	}
	if saved.HeavyProbeIntervalMinutes != 5 {
		t.Fatalf("clamped heavy interval = %d, want 5", saved.HeavyProbeIntervalMinutes)
	}
}

func TestOperationsSettingsReadLegacyNullUpdatedAt(t *testing.T) {
	db := openProbeTestDB(t)
	if err := db.Exec(
		`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, NULL)`,
		"upstream_monitor_heavy_interval_minutes",
		"240",
	).Error; err != nil {
		t.Fatalf("insert legacy setting: %v", err)
	}
	service := &Service{
		db:  db,
		now: time.Now,
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	settings, err := service.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("read legacy setting with null updated_at: %v", err)
	}
	if settings.HeavyProbeIntervalMinutes != 240 {
		t.Fatalf("heavy interval = %d, want 240", settings.HeavyProbeIntervalMinutes)
	}
}
