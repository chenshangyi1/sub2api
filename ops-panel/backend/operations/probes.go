package operations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bejix/upstream-ops/backend/connector/sub2api"
	"gorm.io/gorm"
)

type probeRuleRow struct {
	ID               int64      `gorm:"column:id"`
	AccountID        int64      `gorm:"column:account_id"`
	AccountName      string     `gorm:"column:account_name"`
	Enabled          bool       `gorm:"column:enabled"`
	CheckInterval    int        `gorm:"column:check_interval_minutes"`
	ModelID          *string    `gorm:"column:model_id"`
	GroupName        *string    `gorm:"column:sub2api_group_name"`
	NativeMonitorID  *int64     `gorm:"column:sub2api_channel_monitor_id"`
	LastStatus       *string    `gorm:"column:last_status"`
	LastMessage      *string    `gorm:"column:last_message"`
	LastLatencyMS    *int       `gorm:"column:last_latency_ms"`
	LastCheckMode    *string    `gorm:"column:last_check_mode"`
	LastFirstTokenMS *int       `gorm:"column:last_first_token_ms"`
	LastStreamTPS    *float64   `gorm:"column:last_stream_tps"`
	LastCheckedAt    *time.Time `gorm:"column:last_checked_at"`
	NextCheckAt      *time.Time `gorm:"column:next_check_at"`
}

type probeAvailabilityRow struct {
	RuleID  int64 `gorm:"column:rule_id"`
	Total   int64 `gorm:"column:total"`
	Success int64 `gorm:"column:success"`
}

type CreateProbeInput struct {
	AccountID            int64  `json:"account_id" binding:"required"`
	ModelID              string `json:"model_id"`
	Prompt               string `json:"prompt"`
	GroupName            string `json:"group_name"`
	Enabled              *bool  `json:"enabled"`
	CheckIntervalMinutes int    `json:"check_interval_minutes"`
	FailureThreshold     int    `json:"failure_threshold"`
	PauseMinutes         int    `json:"pause_minutes"`
}

type probeRuleWriteRow struct {
	ID                   int64      `gorm:"column:id;primaryKey"`
	ConnectionID         uint       `gorm:"column:connection_id"`
	AccountID            int64      `gorm:"column:account_id"`
	AccountName          string     `gorm:"column:account_name"`
	Enabled              bool       `gorm:"column:enabled"`
	CheckIntervalMinutes int        `gorm:"column:check_interval_minutes"`
	FailureThreshold     int        `gorm:"column:failure_threshold"`
	PauseMinutes         int        `gorm:"column:pause_minutes"`
	ModelID              *string    `gorm:"column:model_id"`
	Prompt               *string    `gorm:"column:prompt"`
	Sub2APIGroupName     *string    `gorm:"column:sub2api_group_name"`
	LastStatus           *string    `gorm:"column:last_status"`
	LastMessage          *string    `gorm:"column:last_message"`
	LastCheckMode        *string    `gorm:"column:last_check_mode"`
	LastLatencyMS        *int       `gorm:"column:last_latency_ms"`
	LastFirstTokenMS     *int       `gorm:"column:last_first_token_ms"`
	LastStreamTPS        *float64   `gorm:"column:last_stream_tps"`
	LastCheckedAt        *time.Time `gorm:"column:last_checked_at"`
	NextCheckAt          *time.Time `gorm:"column:next_check_at"`
	CreatedAt            time.Time  `gorm:"column:created_at"`
	UpdatedAt            time.Time  `gorm:"column:updated_at"`
}

func (probeRuleWriteRow) TableName() string { return "upstream_monitor_rules" }

func (row probeRuleWriteRow) probeRule() probeRuleRow {
	return probeRuleRow{
		ID: row.ID, AccountID: row.AccountID, AccountName: row.AccountName, Enabled: row.Enabled,
		CheckInterval: row.CheckIntervalMinutes, ModelID: row.ModelID, GroupName: row.Sub2APIGroupName,
		LastStatus: row.LastStatus, LastMessage: row.LastMessage, LastLatencyMS: row.LastLatencyMS,
		LastCheckMode: row.LastCheckMode, LastFirstTokenMS: row.LastFirstTokenMS, LastStreamTPS: row.LastStreamTPS,
		LastCheckedAt: row.LastCheckedAt, NextCheckAt: row.NextCheckAt,
	}
}

func normalizeProbeSetting(value, fallback, min, max int) int {
	if value == 0 {
		value = fallback
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func (s *Service) CreateProbe(ctx context.Context, targetID uint, input CreateProbeInput) (*Probe, error) {
	if input.AccountID <= 0 {
		return nil, fmt.Errorf("%w: account id is required", ErrInvalid)
	}
	_, target, err := s.target(ctx, targetID)
	if err != nil {
		return nil, err
	}
	if !s.db.Migrator().HasTable("upstream_monitor_rules") {
		return nil, fmt.Errorf("%w: probe rules table is missing", ErrInvalid)
	}
	accounts, err := listAllTargetAccounts(func(page, pageSize int) ([]sub2api.AdminAccount, error) {
		return s.admin.ListAccounts(ctx, target, page, pageSize)
	})
	if err != nil {
		return nil, fmt.Errorf("list target accounts: %w", err)
	}
	var account *sub2api.AdminAccount
	for i := range accounts {
		if accounts[i].ID == input.AccountID {
			account = &accounts[i]
			break
		}
	}
	if account == nil {
		return nil, fmt.Errorf("%w: account %d", ErrNotFound, input.AccountID)
	}

	now := s.now().UTC()
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	interval := normalizeProbeSetting(input.CheckIntervalMinutes, 10, 1, 1440)
	failureThreshold := normalizeProbeSetting(input.FailureThreshold, 3, 1, 100)
	pauseMinutes := normalizeProbeSetting(input.PauseMinutes, 30, 0, 10080)
	modelID := strings.TrimSpace(input.ModelID)
	groupName := strings.TrimSpace(input.GroupName)
	prompt := strings.TrimSpace(input.Prompt)
	row := probeRuleWriteRow{
		ConnectionID: targetID, AccountID: input.AccountID, AccountName: strings.TrimSpace(account.Name),
		Enabled: enabled, CheckIntervalMinutes: interval, FailureThreshold: failureThreshold,
		PauseMinutes: pauseMinutes, CreatedAt: now, UpdatedAt: now,
	}
	if modelID != "" {
		row.ModelID = &modelID
	}
	if prompt != "" {
		row.Prompt = &prompt
	}
	if groupName != "" {
		row.Sub2APIGroupName = &groupName
	}
	if enabled {
		row.NextCheckAt = &now
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, fmt.Errorf("%w: account %d already has a probe rule", ErrInvalid, input.AccountID)
		}
		return nil, err
	}
	s.recordAction(ctx, "create_probe", fmt.Sprintf("target:%d/probe:%d", targetID, row.ID), fmt.Sprintf("account:%d", input.AccountID), true)
	item := mapProbeRule(row.probeRule(), target.BaseURL, strings.ToLower(strings.TrimSpace(account.Platform)), nil)
	return &item, nil
}

func (s *Service) DeleteProbe(ctx context.Context, targetID uint, probeID int64) error {
	if probeID <= 0 {
		return fmt.Errorf("%w: probe id is required", ErrInvalid)
	}
	_, target, err := s.target(ctx, targetID)
	if err != nil {
		return err
	}
	var row struct {
		ID              int64  `gorm:"column:id"`
		NativeMonitorID *int64 `gorm:"column:sub2api_channel_monitor_id"`
	}
	result := s.db.WithContext(ctx).Table("upstream_monitor_rules").
		Select("id, sub2api_channel_monitor_id").
		Where("id = ? AND connection_id = ?", probeID, targetID).First(&row)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: probe %d", ErrNotFound, probeID)
	}
	if result.Error != nil {
		return result.Error
	}
	// Stop scheduling before touching the remote monitor. If the remote call
	// fails, the disabled rule remains available for a retry from the panel.
	now := s.now().UTC()
	if err := s.db.WithContext(ctx).Table("upstream_monitor_rules").
		Where("id = ? AND connection_id = ?", probeID, targetID).
		Updates(map[string]any{"enabled": false, "next_check_at": nil, "updated_at": now}).Error; err != nil {
		return err
	}
	if row.NativeMonitorID != nil && *row.NativeMonitorID > 0 {
		if err := s.admin.DeleteChannelMonitor(ctx, target, *row.NativeMonitorID); err != nil {
			s.recordAction(ctx, "delete_probe", fmt.Sprintf("target:%d/probe:%d", targetID, probeID), err.Error(), false)
			return fmt.Errorf("delete native probe monitor: %w", err)
		}
	}
	deleted := s.db.WithContext(ctx).Table("upstream_monitor_rules").
		Where("id = ? AND connection_id = ?", probeID, targetID).
		Delete(&probeRuleWriteRow{})
	if deleted.Error != nil {
		return deleted.Error
	}
	if deleted.RowsAffected != 1 {
		return fmt.Errorf("%w: probe %d", ErrNotFound, probeID)
	}
	_ = setWorkerRequest(s.db.WithContext(ctx), targetID, "probe", now)
	s.recordAction(ctx, "delete_probe", fmt.Sprintf("target:%d/probe:%d", targetID, probeID), "deleted", true)
	return nil
}

func (s *Service) ListProbes(ctx context.Context, targetID uint) (*ProbePage, error) {
	_, target, err := s.target(ctx, targetID)
	if err != nil {
		return nil, err
	}
	if !s.db.Migrator().HasTable("upstream_monitor_rules") {
		return &ProbePage{Items: []Probe{}}, nil
	}

	var rows []probeRuleRow
	if err := s.db.WithContext(ctx).Table("upstream_monitor_rules").
		Where("connection_id = ?", targetID).Order("id ASC").Scan(&rows).Error; err != nil {
		return nil, err
	}

	availability := map[int64]*float64{}
	if s.db.Migrator().HasTable("upstream_monitor_results") {
		var stats []probeAvailabilityRow
		err := s.db.WithContext(ctx).Raw(`
			SELECT rule_id,
			       COUNT(*) AS total,
			       SUM(CASE WHEN LOWER(status) IN ('success', 'healthy', 'operational', 'ok') THEN 1 ELSE 0 END) AS success
			FROM upstream_monitor_results
			WHERE connection_id = ? AND finished_at >= ?
			GROUP BY rule_id`, targetID, s.now().AddDate(0, 0, -7)).Scan(&stats).Error
		if err == nil {
			for _, row := range stats {
				if row.Total > 0 {
					value := float64(row.Success) * 100 / float64(row.Total)
					availability[row.RuleID] = &value
				}
			}
		}
	}

	providers := map[int64]string{}
	if accounts, listErr := listAllTargetAccounts(func(page, pageSize int) ([]sub2api.AdminAccount, error) {
		return s.admin.ListAccounts(ctx, target, page, pageSize)
	}); listErr == nil {
		for _, account := range accounts {
			providers[account.ID] = strings.ToLower(strings.TrimSpace(account.Platform))
		}
	}

	items := make([]Probe, 0, len(rows))
	summary := ProbeSummary{Total: len(rows)}
	for _, row := range rows {
		item := mapProbeRule(row, target.BaseURL, providers[row.AccountID], availability[row.ID])
		items = append(items, item)
		switch item.Status {
		case StatusHealthy:
			summary.Healthy++
		case StatusWarning:
			summary.Warning++
		case StatusError:
			summary.Error++
		}
	}
	return &ProbePage{Items: items, Summary: summary}, nil
}

func mapProbeRule(row probeRuleRow, endpoint, provider string, availability *float64) Probe {
	name := strings.TrimSpace(row.AccountName)
	if name == "" {
		name = fmt.Sprintf("Account #%d", row.AccountID)
	}
	mode := strings.ToLower(strings.TrimSpace(valueOrEmpty(row.LastCheckMode)))
	if mode != "light" && mode != "heavy" && mode != "capability" {
		mode = "heavy"
	}
	model := strings.TrimSpace(valueOrEmpty(row.ModelID))
	candidates := []string{}
	if model != "" {
		candidates = append(candidates, model)
	}
	return Probe{
		ID: row.ID, Name: name, AccountID: &row.AccountID,
		GroupName: strings.TrimSpace(valueOrEmpty(row.GroupName)), Provider: provider,
		Endpoint: endpoint, Enabled: row.Enabled, Mode: mode, Status: normalizeProbeStatus(valueOrEmpty(row.LastStatus)),
		Model: model, CandidateModels: candidates, LatencyMS: row.LastLatencyMS,
		FirstTokenMS: row.LastFirstTokenMS, TokensPerSecond: row.LastStreamTPS,
		Availability7D: availability, LastCheckedAt: row.LastCheckedAt,
		NextRunAt: row.NextCheckAt, LastError: probeError(row),
	}
}

func normalizeProbeStatus(value string) Status {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "success", "healthy", "operational", "ok", "pass":
		return StatusHealthy
	case "warning", "degraded", "partial", "paused":
		return StatusWarning
	case "error", "failed", "failure", "unhealthy":
		return StatusError
	default:
		return StatusUnknown
	}
}

func probeError(row probeRuleRow) string {
	if normalizeProbeStatus(valueOrEmpty(row.LastStatus)) != StatusError {
		return ""
	}
	return strings.TrimSpace(valueOrEmpty(row.LastMessage))
}

func (s *Service) RunProbe(ctx context.Context, targetID uint, probeID int64) (*Probe, error) {
	if probeID <= 0 {
		return nil, fmt.Errorf("%w: probe id is required", ErrInvalid)
	}
	if err := s.queueProbe(ctx, targetID, probeID); err != nil {
		return nil, err
	}
	page, err := s.ListProbes(ctx, targetID)
	if err != nil {
		return nil, err
	}
	for i := range page.Items {
		if page.Items[i].ID == probeID {
			return &page.Items[i], nil
		}
	}
	return nil, fmt.Errorf("%w: probe %d", ErrNotFound, probeID)
}

func (s *Service) queueProbe(ctx context.Context, targetID uint, probeID int64) error {
	if err := s.validateTarget(ctx, targetID); err != nil {
		return err
	}
	now := s.now().UTC()
	result := s.db.WithContext(ctx).Table("upstream_monitor_rules").
		Where("id = ? AND connection_id = ?", probeID, targetID).
		Updates(map[string]any{"next_check_at": now, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: probe %d", ErrNotFound, probeID)
	}
	if err := setWorkerRequest(s.db.WithContext(ctx), targetID, "probe", now); err != nil {
		return err
	}
	s.recordAction(ctx, "run_probe", fmt.Sprintf("target:%d/probe:%d", targetID, probeID), "queued for extension worker", true)
	return nil
}

func (s *Service) SetProbeEnabled(ctx context.Context, targetID uint, probeID int64, enabled bool) (*Probe, error) {
	if err := s.validateTarget(ctx, targetID); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	updates := map[string]any{"enabled": enabled, "updated_at": now}
	if enabled {
		updates["next_check_at"] = now
	} else {
		updates["next_check_at"] = nil
	}
	result := s.db.WithContext(ctx).Table("upstream_monitor_rules").
		Where("id = ? AND connection_id = ?", probeID, targetID).Updates(updates)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, fmt.Errorf("%w: probe %d", ErrNotFound, probeID)
	}
	if enabled {
		_ = setWorkerRequest(s.db.WithContext(ctx), targetID, "probe", now)
	}
	s.recordAction(ctx, "set_probe_enabled", fmt.Sprintf("target:%d/probe:%d", targetID, probeID), fmt.Sprintf("enabled=%t", enabled), true)
	return s.RunProbeState(ctx, targetID, probeID)
}

func (s *Service) RunProbeState(ctx context.Context, targetID uint, probeID int64) (*Probe, error) {
	page, err := s.ListProbes(ctx, targetID)
	if err != nil {
		return nil, err
	}
	for i := range page.Items {
		if page.Items[i].ID == probeID {
			return &page.Items[i], nil
		}
	}
	return nil, fmt.Errorf("%w: probe %d", ErrNotFound, probeID)
}

func (s *Service) RunProbeBatch(ctx context.Context, targetID uint, mode string, filter ProbeBatchFilter) (int64, error) {
	if err := s.validateTarget(ctx, targetID); err != nil {
		return 0, err
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "all" && mode != "light" && mode != "heavy" {
		return 0, fmt.Errorf("%w: unsupported probe mode", ErrInvalid)
	}
	probeIDs := positiveInt64s(filter.ProbeIDs)
	accountIDs := positiveInt64s(filter.AccountIDs)
	if len(probeIDs) == 0 && len(accountIDs) == 0 {
		return 0, fmt.Errorf("%w: probe or account ids are required", ErrInvalid)
	}
	now := s.now().UTC()
	query := s.db.WithContext(ctx).Table("upstream_monitor_rules").
		Where("connection_id = ? AND enabled = ?", targetID, true)
	if len(probeIDs) > 0 {
		query = query.Where("id IN ?", probeIDs)
	}
	if len(accountIDs) > 0 {
		query = query.Where("account_id IN ?", accountIDs)
	}
	result := query.Updates(map[string]any{"next_check_at": now, "updated_at": now})
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected == 0 {
		return 0, nil
	}
	if err := setWorkerRequest(s.db.WithContext(ctx), targetID, "probe:"+mode, now); err != nil {
		return 0, err
	}
	s.recordAction(ctx, "run_probe_batch", fmt.Sprintf("target:%d", targetID), fmt.Sprintf("mode=%s queued=%d", mode, result.RowsAffected), true)
	return result.RowsAffected, nil
}

func positiveInt64s(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func setWorkerRequest(db *gorm.DB, targetID uint, mode string, now time.Time) error {
	return db.Transaction(func(tx *gorm.DB) error {
		values := map[string]string{
			"worker_run_requested_at":        now.Format(time.RFC3339Nano),
			"worker_run_requested_target_id": fmt.Sprintf("%d", targetID),
			"worker_run_requested_mode":      mode,
		}
		for key, value := range values {
			if err := setSetting(tx, key, value, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func valueOrEmpty[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}
