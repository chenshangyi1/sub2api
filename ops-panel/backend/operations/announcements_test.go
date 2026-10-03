package operations

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/bejix/upstream-ops/backend/storage"
)

func TestAnnouncementRulePostgresCRUD(t *testing.T) {
	db := openOperationsPostgresSchema(t, "ops_announcement_crud")
	if err := db.AutoMigrate(&storage.UpstreamSyncTarget{}, &ActionLog{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE announcement_rules (
		id BIGSERIAL PRIMARY KEY,
		connection_id BIGINT NOT NULL,
		name TEXT NOT NULL,
		enabled BOOLEAN NOT NULL DEFAULT TRUE,
		title_template TEXT NOT NULL,
		content_template TEXT NOT NULL,
		target_group_ids BIGINT[] NOT NULL DEFAULT '{}',
		status TEXT NOT NULL DEFAULT 'active',
		notify_mode TEXT NOT NULL DEFAULT 'silent',
		created_at TIMESTAMPTZ NOT NULL,
		updated_at TIMESTAMPTZ NOT NULL
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TIMESTAMPTZ NOT NULL
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&storage.UpstreamSyncTarget{ID: 1, Name: "target", BaseURL: "https://target.example.test"}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 29, 1, 2, 3, 0, time.UTC)
	service := &Service{db: db, now: func() time.Time { return now }}
	ctx := context.Background()

	created, err := service.CreateAnnouncementRule(ctx, 1, AnnouncementRule{
		Name: " Production rule ", Enabled: true,
		TitleTemplate: " title ", ContentTemplate: " content ",
		TargetGroupIDs: []int64{3, 1, 3, 0}, Status: "active", NotifyMode: "popup",
	})
	if err != nil {
		t.Fatalf("create announcement rule: %v", err)
	}
	if created.ID <= 0 || created.Name != "Production rule" || len(created.TargetGroupIDs) != 2 || created.TargetGroupIDs[0] != 1 || created.TargetGroupIDs[1] != 3 {
		t.Fatalf("created announcement rule = %+v", created)
	}
	publishedAt := now.Add(-time.Hour)
	publishedKey := announcementPublishedAtKeyPrefix + strconv.FormatInt(created.ID, 10)
	if err := db.Exec(
		"INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)",
		publishedKey, publishedAt.Format(time.RFC3339Nano), publishedAt,
	).Error; err != nil {
		t.Fatal(err)
	}
	listed, err := service.ListAnnouncementRules(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].LastPublishedAt == nil || !listed[0].LastPublishedAt.Equal(publishedAt) {
		t.Fatalf("published timestamp = %+v", listed)
	}

	updated, err := service.UpdateAnnouncementRule(ctx, 1, created.ID, AnnouncementRule{
		Name: "Updated", Enabled: false, TitleTemplate: "updated title", ContentTemplate: "updated content",
		TargetGroupIDs: []int64{}, Status: "draft", NotifyMode: "silent",
	})
	if err != nil {
		t.Fatalf("update announcement rule: %v", err)
	}
	if updated.Enabled || updated.Status != "draft" || len(updated.TargetGroupIDs) != 0 {
		t.Fatalf("updated announcement rule = %+v", updated)
	}

	if err := service.DeleteAnnouncementRule(ctx, 1, created.ID); err != nil {
		t.Fatalf("delete announcement rule: %v", err)
	}
	items, err := service.ListAnnouncementRules(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("announcement rules after delete = %d", len(items))
	}
	var markerCount int64
	if err := db.Table("settings").Where("key = ?", publishedKey).Count(&markerCount).Error; err != nil {
		t.Fatal(err)
	}
	if markerCount != 0 {
		t.Fatalf("announcement publication marker count after delete = %d", markerCount)
	}
}
