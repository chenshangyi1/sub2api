package operations

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

const announcementPublishedAtKeyPrefix = "announcement_rule_last_published_at:"

type announcementRow struct {
	ID              int64      `gorm:"column:id"`
	Name            string     `gorm:"column:name"`
	Enabled         bool       `gorm:"column:enabled"`
	TitleTemplate   string     `gorm:"column:title_template"`
	ContentTemplate string     `gorm:"column:content_template"`
	TargetGroupIDs  string     `gorm:"column:target_group_ids_csv"`
	Status          string     `gorm:"column:status"`
	NotifyMode      string     `gorm:"column:notify_mode"`
	LastPublishedAt *time.Time `gorm:"column:last_published_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at"`
}

func (s *Service) ListAnnouncementRules(ctx context.Context, targetID uint) ([]AnnouncementRule, error) {
	if err := s.validateTarget(ctx, targetID); err != nil {
		return nil, err
	}
	if !s.db.Migrator().HasTable("announcement_rules") {
		return []AnnouncementRule{}, nil
	}
	var rows []announcementRow
	query := `
		SELECT rule.id, rule.name, rule.enabled, rule.title_template, rule.content_template,
		       array_to_string(rule.target_group_ids, ',') AS target_group_ids_csv,
		       rule.status, rule.notify_mode, published.updated_at AS last_published_at,
		       rule.updated_at
		FROM announcement_rules AS rule
		LEFT JOIN settings AS published
		       ON published.key = CAST(? AS text) || rule.id::text
		WHERE rule.connection_id = ?
		ORDER BY rule.id ASC`
	if err := s.db.WithContext(ctx).Raw(query, announcementPublishedAtKeyPrefix, targetID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]AnnouncementRule, 0, len(rows))
	for _, row := range rows {
		items = append(items, AnnouncementRule{
			ID: row.ID, Name: row.Name, Enabled: row.Enabled,
			TitleTemplate: row.TitleTemplate, ContentTemplate: row.ContentTemplate,
			TargetGroupIDs: parseIDList(row.TargetGroupIDs), Status: row.Status,
			NotifyMode: row.NotifyMode, LastPublishedAt: row.LastPublishedAt,
		})
	}
	return items, nil
}

func (s *Service) CreateAnnouncementRule(ctx context.Context, targetID uint, input AnnouncementRule) (*AnnouncementRule, error) {
	if err := s.validateTarget(ctx, targetID); err != nil {
		return nil, err
	}
	if !s.db.Migrator().HasTable("announcement_rules") {
		return nil, fmt.Errorf("%w: announcement rules are not available", ErrInvalid)
	}
	normalized, err := normalizeAnnouncementRule(input)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	var id int64
	err = s.db.WithContext(ctx).Raw(`
		INSERT INTO announcement_rules
			(connection_id, name, enabled, title_template, content_template, target_group_ids, status, notify_mode, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?::bigint[], ?, ?, ?, ?)
		RETURNING id`, targetID, normalized.Name, normalized.Enabled, normalized.TitleTemplate,
		normalized.ContentTemplate, postgresIDArray(normalized.TargetGroupIDs), normalized.Status,
		normalized.NotifyMode, now, now).Scan(&id).Error
	if err != nil {
		return nil, err
	}
	s.recordAction(ctx, "create_announcement_rule", fmt.Sprintf("target:%d/rule:%d", targetID, id), normalized.Name, true)
	return s.findAnnouncementRule(ctx, targetID, id)
}

func (s *Service) UpdateAnnouncementRule(ctx context.Context, targetID uint, ruleID int64, input AnnouncementRule) (*AnnouncementRule, error) {
	if err := s.validateTarget(ctx, targetID); err != nil {
		return nil, err
	}
	if ruleID <= 0 {
		return nil, fmt.Errorf("%w: announcement rule id is required", ErrInvalid)
	}
	normalized, err := normalizeAnnouncementRule(input)
	if err != nil {
		return nil, err
	}
	result := s.db.WithContext(ctx).Exec(`
		UPDATE announcement_rules
		SET name = ?, enabled = ?, title_template = ?, content_template = ?,
		    target_group_ids = ?::bigint[], status = ?, notify_mode = ?, updated_at = ?
		WHERE id = ? AND connection_id = ?`, normalized.Name, normalized.Enabled,
		normalized.TitleTemplate, normalized.ContentTemplate, postgresIDArray(normalized.TargetGroupIDs),
		normalized.Status, normalized.NotifyMode, s.now().UTC(), ruleID, targetID)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, fmt.Errorf("%w: announcement rule %d", ErrNotFound, ruleID)
	}
	s.recordAction(ctx, "update_announcement_rule", fmt.Sprintf("target:%d/rule:%d", targetID, ruleID), normalized.Name, true)
	return s.findAnnouncementRule(ctx, targetID, ruleID)
}

func (s *Service) DeleteAnnouncementRule(ctx context.Context, targetID uint, ruleID int64) error {
	if err := s.validateTarget(ctx, targetID); err != nil {
		return err
	}
	if ruleID <= 0 {
		return fmt.Errorf("%w: announcement rule id is required", ErrInvalid)
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec("DELETE FROM announcement_rules WHERE id = ? AND connection_id = ?", ruleID, targetID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: announcement rule %d", ErrNotFound, ruleID)
		}
		return tx.Exec("DELETE FROM settings WHERE key = ?", announcementPublishedAtKeyPrefix+strconv.FormatInt(ruleID, 10)).Error
	})
	if err != nil {
		return err
	}
	s.recordAction(ctx, "delete_announcement_rule", fmt.Sprintf("target:%d/rule:%d", targetID, ruleID), "", true)
	return nil
}

func (s *Service) findAnnouncementRule(ctx context.Context, targetID uint, ruleID int64) (*AnnouncementRule, error) {
	items, err := s.ListAnnouncementRules(ctx, targetID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].ID == ruleID {
			return &items[i], nil
		}
	}
	return nil, fmt.Errorf("%w: announcement rule %d", ErrNotFound, ruleID)
}

func normalizeAnnouncementRule(input AnnouncementRule) (AnnouncementRule, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.TitleTemplate = strings.TrimSpace(input.TitleTemplate)
	input.ContentTemplate = strings.TrimSpace(input.ContentTemplate)
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	input.NotifyMode = strings.ToLower(strings.TrimSpace(input.NotifyMode))
	if input.Name == "" || input.TitleTemplate == "" || input.ContentTemplate == "" {
		return input, fmt.Errorf("%w: rule name, title template and content template are required", ErrInvalid)
	}
	if len(input.Name) > 200 || len(input.TitleTemplate) > 2000 || len(input.ContentTemplate) > 20_000 {
		return input, fmt.Errorf("%w: announcement rule text is too long", ErrInvalid)
	}
	if input.Status != "draft" && input.Status != "active" && input.Status != "archived" {
		return input, fmt.Errorf("%w: status must be draft, active or archived", ErrInvalid)
	}
	if input.NotifyMode != "silent" && input.NotifyMode != "popup" {
		return input, fmt.Errorf("%w: notify mode must be silent or popup", ErrInvalid)
	}
	seen := map[int64]struct{}{}
	targetGroupIDs := append([]int64(nil), input.TargetGroupIDs...)
	input.TargetGroupIDs = input.TargetGroupIDs[:0]
	for _, id := range targetGroupIDs {
		if id > 0 {
			seen[id] = struct{}{}
		}
	}
	for id := range seen {
		input.TargetGroupIDs = append(input.TargetGroupIDs, id)
	}
	sort.Slice(input.TargetGroupIDs, func(i, j int) bool { return input.TargetGroupIDs[i] < input.TargetGroupIDs[j] })
	return input, nil
}

func postgresIDArray(ids []int64) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			parts = append(parts, strconv.FormatInt(id, 10))
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func parseIDList(raw string) []int64 {
	out := []int64{}
	for _, part := range strings.Split(strings.Trim(raw, "{}"), ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err == nil && id > 0 {
			out = append(out, id)
		}
	}
	return out
}
