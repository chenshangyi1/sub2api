package operations

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bejix/upstream-ops/backend/connector/sub2api"
	"gorm.io/gorm"
)

const (
	RatePolicyTargetGroup   = "group"
	RatePolicyTargetAccount = "account"
)

var ratePolicyModes = map[string]struct{}{
	"first": {}, "average": {}, "min": {}, "max": {}, "custom": {},
	"locked": {}, "manual_source": {},
}

type RatePolicySource struct {
	ChannelID          uint       `json:"channel_id"`
	ChannelName        string     `json:"channel_name"`
	ChannelType        string     `json:"channel_type"`
	GroupID            string     `json:"group_id"`
	GroupName          string     `json:"group_name"`
	Rate               float64    `json:"rate"`
	Fresh              bool       `json:"fresh"`
	Enabled            bool       `json:"enabled"`
	LastStatus         string     `json:"last_status"`
	LastError          string     `json:"last_error,omitempty"`
	LastRateScanAt     *time.Time `json:"last_rate_scan_at,omitempty"`
	LastSeenAt         time.Time  `json:"last_seen_at"`
	RateIntervalMinute int        `json:"rate_interval_minutes"`
}

type RatePolicyBinding struct {
	ID             int64  `json:"id"`
	ChannelID      uint   `json:"channel_id"`
	ChannelName    string `json:"channel_name"`
	GroupID        string `json:"group_id"`
	GroupName      string `json:"group_name"`
	SourcePlatform string `json:"source_platform,omitempty"`
}

type RatePolicyRule struct {
	Enabled    bool    `json:"enabled"`
	Mode       string  `json:"mode"`
	Offset     float64 `json:"offset"`
	Expression string  `json:"expression,omitempty"`
}

type RatePolicyExclusion struct {
	TargetType string `json:"target_type"`
	TargetID   int64  `json:"target_id"`
	ChannelID  uint   `json:"channel_id"`
	GroupID    string `json:"group_id"`
}

type RatePolicyTarget struct {
	TargetType  string              `json:"target_type"`
	ID          int64               `json:"id"`
	Name        string              `json:"name"`
	CurrentRate float64             `json:"current_rate"`
	Bindings    []RatePolicyBinding `json:"bindings"`
	Rule        RatePolicyRule      `json:"rule"`
}

type RatePolicyWorkspace struct {
	Sources    []RatePolicySource    `json:"sources"`
	Groups     []RatePolicyTarget    `json:"groups"`
	Accounts   []RatePolicyTarget    `json:"accounts"`
	Exclusions []RatePolicyExclusion `json:"exclusions"`
}

type RatePolicyBindingInput struct {
	ChannelID uint   `json:"channel_id" binding:"required"`
	GroupID   string `json:"group_id" binding:"required"`
}

type RatePolicyInput struct {
	Enabled    bool                     `json:"enabled"`
	Mode       string                   `json:"mode" binding:"required"`
	Offset     float64                  `json:"offset"`
	Expression string                   `json:"expression"`
	Bindings   []RatePolicyBindingInput `json:"bindings"`
}

type RatePolicyRunRequest struct {
	Queued       bool   `json:"queued"`
	ConnectionID int64  `json:"connection_id"`
	Mode         string `json:"mode"`
}

type canonicalRateSourceRow struct {
	ChannelID          uint       `gorm:"column:channel_id"`
	ChannelName        string     `gorm:"column:channel_name"`
	ChannelType        string     `gorm:"column:channel_type"`
	MonitorEnabled     bool       `gorm:"column:monitor_enabled"`
	RateIntervalMinute int        `gorm:"column:rate_interval_minutes"`
	LastRateScanAt     *time.Time `gorm:"column:last_rate_scan_at"`
	LastError          string     `gorm:"column:last_error"`
	RemoteGroupID      *int64     `gorm:"column:remote_group_id"`
	ModelName          string     `gorm:"column:model_name"`
	Ratio              float64    `gorm:"column:ratio"`
	LastSeenAt         time.Time  `gorm:"column:last_seen_at"`
}

type ratePolicyBindingRow struct {
	ID              int64  `gorm:"column:id"`
	ConnectionID    int64  `gorm:"column:connection_id"`
	TargetType      string `gorm:"column:target_type"`
	TargetID        int64  `gorm:"column:target_id"`
	SourceSiteID    uint   `gorm:"column:source_site_id"`
	SourceSiteName  string `gorm:"column:source_site_name"`
	SourceGroupID   string `gorm:"column:source_group_id"`
	SourceGroupName string `gorm:"column:source_group_name"`
	SourcePlatform  string `gorm:"column:source_platform"`
}

type ratePolicyRuleRow struct {
	TargetID   int64   `gorm:"column:target_id"`
	Enabled    bool    `gorm:"column:enabled"`
	Mode       string  `gorm:"column:mode"`
	Offset     float64 `gorm:"column:offset"`
	Expression string  `gorm:"column:expression"`
}

type ratePolicyExclusionRow struct {
	AccountID     int64  `gorm:"column:account_id"`
	TargetGroupID int64  `gorm:"column:group_id"`
	SourceSiteID  uint   `gorm:"column:source_site_id"`
	SourceGroupID string `gorm:"column:source_group_id"`
}

func (s *Service) GetRatePolicies(ctx context.Context, targetID uint) (*RatePolicyWorkspace, error) {
	_, target, err := s.target(ctx, targetID)
	if err != nil {
		return nil, err
	}
	connectionID, err := s.extensionConnectionID(ctx, targetID)
	if err != nil {
		return nil, err
	}

	groups, err := s.admin.ListGroups(ctx, target, true)
	if err != nil {
		return nil, fmt.Errorf("list target groups: %w", err)
	}
	accounts, err := listAllTargetAccounts(func(page, pageSize int) ([]sub2api.AdminAccount, error) {
		return s.admin.ListAccounts(ctx, target, page, pageSize)
	})
	if err != nil {
		return nil, fmt.Errorf("list target accounts: %w", err)
	}

	sources, err := s.listCanonicalRateSources(ctx)
	if err != nil {
		return nil, err
	}
	bindings, err := s.listRatePolicyBindings(ctx, connectionID)
	if err != nil {
		return nil, err
	}
	exclusions, err := s.listRatePolicyExclusions(ctx, connectionID)
	if err != nil {
		return nil, err
	}
	groupRules, err := s.listRatePolicyRules(ctx, connectionID, RatePolicyTargetGroup)
	if err != nil {
		return nil, err
	}
	accountRules, err := s.listRatePolicyRules(ctx, connectionID, RatePolicyTargetAccount)
	if err != nil {
		return nil, err
	}

	workspace := &RatePolicyWorkspace{
		Sources: sources, Groups: []RatePolicyTarget{}, Accounts: []RatePolicyTarget{}, Exclusions: exclusions,
	}
	for _, group := range groups {
		workspace.Groups = append(workspace.Groups, RatePolicyTarget{
			TargetType: RatePolicyTargetGroup, ID: group.ID, Name: group.Name,
			CurrentRate: group.Ratio, Bindings: enabledRatePolicyBindings(
				ratePolicyBindingsOrEmpty(bindings, ratePolicyTargetKey(RatePolicyTargetGroup, group.ID)), sources,
			),
			Rule: ruleOrDefault(groupRules[group.ID]),
		})
	}
	for _, account := range accounts {
		workspace.Accounts = append(workspace.Accounts, RatePolicyTarget{
			TargetType: RatePolicyTargetAccount, ID: account.ID, Name: account.Name,
			CurrentRate: account.RateMultiplier, Bindings: enabledRatePolicyBindings(
				ratePolicyBindingsOrEmpty(bindings, ratePolicyTargetKey(RatePolicyTargetAccount, account.ID)), sources,
			),
			Rule: ruleOrDefault(accountRules[account.ID]),
		})
	}
	sort.Slice(workspace.Groups, func(i, j int) bool { return workspace.Groups[i].ID < workspace.Groups[j].ID })
	sort.Slice(workspace.Accounts, func(i, j int) bool { return workspace.Accounts[i].ID < workspace.Accounts[j].ID })
	return workspace, nil
}

func (s *Service) SaveRatePolicy(ctx context.Context, targetID uint, targetType string, objectID int64, input RatePolicyInput) (*RatePolicyTarget, error) {
	targetType, err := normalizeRatePolicyTargetType(targetType)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeRatePolicyInput(input)
	if err != nil {
		return nil, err
	}
	if err := validateRatePolicyBindingCount(targetType, normalized.Bindings); err != nil {
		return nil, err
	}
	workspace, err := s.GetRatePolicies(ctx, targetID)
	if err != nil {
		return nil, err
	}
	targets := workspace.Groups
	if targetType == RatePolicyTargetAccount {
		targets = workspace.Accounts
	}
	var targetExists bool
	for _, item := range targets {
		if item.ID == objectID {
			targetExists = true
			break
		}
	}
	if !targetExists {
		return nil, fmt.Errorf("%w: target %s %d", ErrNotFound, targetType, objectID)
	}

	sourceIndex := make(map[string]RatePolicySource, len(workspace.Sources))
	for _, source := range workspace.Sources {
		sourceIndex[ratePolicySourceKey(source.ChannelID, source.GroupID)] = source
	}
	resolvedBindings, err := resolveRatePolicyBindings(sourceIndex, normalized.Bindings)
	if err != nil {
		return nil, err
	}
	if normalized.Enabled && ratePolicyNeedsSource(normalized.Mode) && len(resolvedBindings) == 0 {
		return nil, fmt.Errorf("%w: enabled %s rule requires at least one canonical rate source", ErrInvalid, normalized.Mode)
	}

	connectionID, err := s.extensionConnectionID(ctx, targetID)
	if err != nil {
		return nil, err
	}
	if !s.db.Migrator().HasTable("bl_source_bindings") || !s.db.Migrator().HasTable(ratePolicyRuleTable(targetType)) {
		return nil, fmt.Errorf("%w: rate policy extension tables are not available", ErrInvalid)
	}
	now := s.now().UTC()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("bl_source_bindings").
			Where("connection_id = ? AND target_type = ? AND target_id = ?", connectionID, targetType, objectID).
			Delete(nil).Error; err != nil {
			return err
		}
		for _, binding := range resolvedBindings {
			row := map[string]any{
				"connection_id": connectionID, "target_type": targetType, "target_id": objectID,
				"source_site_id": binding.ChannelID, "source_site_name": binding.ChannelName,
				"source_group_id": binding.GroupID, "source_group_name": binding.GroupName,
				"source_platform": binding.SourcePlatform, "created_at": now, "updated_at": now,
			}
			if err := tx.Table("bl_source_bindings").Create(row).Error; err != nil {
				return err
			}
		}
		table := ratePolicyRuleTable(targetType)
		return upsertRatePolicyRule(tx, table, ratePolicyRuleIDColumn(targetType), connectionID, objectID, normalized, now)
	})
	if err != nil {
		return nil, err
	}
	s.recordAction(ctx, "save_rate_policy", fmt.Sprintf("target:%d/%s:%d", targetID, targetType, objectID), normalized.Mode, true)

	updated, err := s.GetRatePolicies(ctx, targetID)
	if err != nil {
		return nil, err
	}
	items := updated.Groups
	if targetType == RatePolicyTargetAccount {
		items = updated.Accounts
	}
	for i := range items {
		if items[i].ID == objectID {
			return &items[i], nil
		}
	}
	return nil, fmt.Errorf("%w: saved target %s %d", ErrNotFound, targetType, objectID)
}

func resolveRatePolicyBindings(
	sources map[string]RatePolicySource,
	bindings []RatePolicyBindingInput,
) ([]RatePolicyBinding, error) {
	resolved := make([]RatePolicyBinding, 0, len(bindings))
	seen := map[string]struct{}{}
	for _, binding := range bindings {
		key := ratePolicySourceKey(binding.ChannelID, binding.GroupID)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		source, ok := sources[key]
		if !ok {
			return nil, fmt.Errorf("%w: canonical rate source %s is missing", ErrInvalid, key)
		}
		if !source.Enabled {
			continue
		}
		seen[key] = struct{}{}
		resolved = append(resolved, RatePolicyBinding{
			ChannelID: source.ChannelID, ChannelName: source.ChannelName,
			GroupID: source.GroupID, GroupName: source.GroupName, SourcePlatform: source.ChannelType,
		})
	}
	return resolved, nil
}

func (s *Service) QueueRatePolicies(ctx context.Context, targetID uint) (*RatePolicyRunRequest, error) {
	if err := s.validateTarget(ctx, targetID); err != nil {
		return nil, err
	}
	connectionID, err := s.extensionConnectionID(ctx, targetID)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		values := map[string]string{
			"worker_run_requested_at":        now.Format(time.RFC3339Nano),
			"worker_run_requested_target_id": strconv.FormatInt(connectionID, 10),
			"worker_run_requested_mode":      "rate-rules",
		}
		for key, value := range values {
			if err := setSetting(tx, key, value, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.recordAction(ctx, "apply_rate_policies", fmt.Sprintf("target:%d", targetID), "queued for extension worker", true)
	return &RatePolicyRunRequest{Queued: true, ConnectionID: connectionID, Mode: "rate-rules"}, nil
}

func (s *Service) listCanonicalRateSources(ctx context.Context) ([]RatePolicySource, error) {
	if !s.db.Migrator().HasTable("channels") || !s.db.Migrator().HasTable("rate_snapshots") {
		return []RatePolicySource{}, nil
	}
	var rows []canonicalRateSourceRow
	err := s.db.WithContext(ctx).Raw(`
		SELECT rs.channel_id, c.name AS channel_name, c.type AS channel_type,
		       c.monitor_enabled, c.rate_interval_minutes, c.last_rate_scan_at, c.last_error,
		       rs.remote_group_id, rs.model_name, rs.ratio, rs.last_seen_at
		FROM rate_snapshots rs
		JOIN channels c ON c.id = rs.channel_id
		ORDER BY c.sort_order ASC, c.id ASC, rs.model_name ASC`).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	items := make([]RatePolicySource, 0, len(rows))
	for _, row := range rows {
		groupID := row.ModelName
		if row.RemoteGroupID != nil {
			groupID = strconv.FormatInt(*row.RemoteGroupID, 10)
		}
		lastStatus := "offline"
		fresh := canonicalRateSourceFresh(row, now)
		if fresh {
			lastStatus = "online"
		} else if row.MonitorEnabled && strings.TrimSpace(row.LastError) == "" {
			lastStatus = "stale"
		}
		items = append(items, RatePolicySource{
			ChannelID: row.ChannelID, ChannelName: row.ChannelName, ChannelType: row.ChannelType,
			GroupID: groupID, GroupName: row.ModelName, Rate: row.Ratio, Fresh: fresh,
			Enabled: row.MonitorEnabled, LastStatus: lastStatus, LastError: row.LastError,
			LastRateScanAt: row.LastRateScanAt, LastSeenAt: row.LastSeenAt,
			RateIntervalMinute: row.RateIntervalMinute,
		})
	}
	return items, nil
}

func (s *Service) listRatePolicyBindings(ctx context.Context, connectionID int64) (map[string][]RatePolicyBinding, error) {
	result := map[string][]RatePolicyBinding{}
	if !s.db.Migrator().HasTable("bl_source_bindings") {
		return result, nil
	}
	var rows []ratePolicyBindingRow
	if err := s.db.WithContext(ctx).Table("bl_source_bindings").
		Where("connection_id = ?", connectionID).
		Order("target_type ASC, target_id ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		key := ratePolicyTargetKey(row.TargetType, row.TargetID)
		result[key] = append(result[key], RatePolicyBinding{
			ID: row.ID, ChannelID: row.SourceSiteID, ChannelName: row.SourceSiteName,
			GroupID: row.SourceGroupID, GroupName: row.SourceGroupName, SourcePlatform: row.SourcePlatform,
		})
	}
	return result, nil
}

func (s *Service) listRatePolicyExclusions(ctx context.Context, connectionID int64) ([]RatePolicyExclusion, error) {
	items := []RatePolicyExclusion{}
	if !s.db.Migrator().HasTable("upstream_monitor_rate_exclusions") {
		return items, nil
	}
	var rows []ratePolicyExclusionRow
	if err := s.db.WithContext(ctx).Table("upstream_monitor_rate_exclusions").
		Select("account_id, group_id, source_site_id, source_group_id").
		Where("connection_id = ? AND active = ?", connectionID, true).
		Order("group_id ASC, account_id ASC, source_site_id ASC, source_group_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	appendItem := func(targetType string, targetID int64, row ratePolicyExclusionRow) {
		if targetID <= 0 {
			return
		}
		key := ratePolicyTargetKey(targetType, targetID) + ":" + ratePolicySourceKey(row.SourceSiteID, row.SourceGroupID)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		items = append(items, RatePolicyExclusion{
			TargetType: targetType, TargetID: targetID,
			ChannelID: row.SourceSiteID, GroupID: row.SourceGroupID,
		})
	}
	for _, row := range rows {
		appendItem(RatePolicyTargetGroup, row.TargetGroupID, row)
		appendItem(RatePolicyTargetAccount, row.AccountID, row)
	}
	return items, nil
}

func (s *Service) listRatePolicyRules(ctx context.Context, connectionID int64, targetType string) (map[int64]RatePolicyRule, error) {
	result := map[int64]RatePolicyRule{}
	table := ratePolicyRuleTable(targetType)
	if !s.db.Migrator().HasTable(table) {
		return result, nil
	}
	idColumn := ratePolicyRuleIDColumn(targetType)
	var rows []ratePolicyRuleRow
	if err := s.db.WithContext(ctx).Table(table).
		Select(idColumn+` AS target_id, enabled, mode, "offset" AS "offset", expression`).
		Where("connection_id = ?", connectionID).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.TargetID] = RatePolicyRule{
			Enabled: row.Enabled, Mode: row.Mode, Offset: row.Offset, Expression: row.Expression,
		}
	}
	return result, nil
}

func upsertRatePolicyRule(
	db *gorm.DB,
	table string,
	idColumn string,
	connectionID int64,
	objectID int64,
	input RatePolicyInput,
	now time.Time,
) error {
	statement := fmt.Sprintf(`INSERT INTO %s (connection_id, %s, enabled, mode, "offset", expression, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (connection_id, %s) DO UPDATE SET
		enabled = excluded.enabled, mode = excluded.mode, "offset" = excluded."offset",
		expression = excluded.expression, updated_at = excluded.updated_at`, table, idColumn, idColumn)
	return db.Exec(statement, connectionID, objectID, input.Enabled, input.Mode,
		input.Offset, nullableString(input.Expression), now, now).Error
}

func (s *Service) extensionConnectionID(ctx context.Context, targetID uint) (int64, error) {
	if !s.db.Migrator().HasTable("legacy_import_maps") {
		return int64(targetID), nil
	}
	var row struct {
		LegacyID string `gorm:"column:legacy_id"`
	}
	err := s.db.WithContext(ctx).Table("legacy_import_maps").
		Select("legacy_id").
		Where("migration_version = ? AND legacy_table = ? AND canonical_table = ? AND canonical_id = ? AND rolled_back_at IS NULL",
			"20260729_upstream_ops_v007", "connections", "upstream_sync_targets", strconv.FormatUint(uint64(targetID), 10)).
		Order("id DESC").Take(&row).Error
	if err == nil {
		id, parseErr := strconv.ParseInt(strings.TrimSpace(row.LegacyID), 10, 64)
		if parseErr != nil || id <= 0 {
			return 0, fmt.Errorf("%w: invalid extension connection mapping for target %d", ErrInvalid, targetID)
		}
		return id, nil
	}
	if err == gorm.ErrRecordNotFound {
		return int64(targetID), nil
	}
	return 0, err
}

func normalizeRatePolicyTargetType(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case RatePolicyTargetGroup:
		return RatePolicyTargetGroup, nil
	case RatePolicyTargetAccount:
		return RatePolicyTargetAccount, nil
	default:
		return "", fmt.Errorf("%w: rate policy target type must be group or account", ErrInvalid)
	}
}

func normalizeRatePolicyInput(input RatePolicyInput) (RatePolicyInput, error) {
	input.Mode = strings.ToLower(strings.TrimSpace(input.Mode))
	input.Expression = strings.TrimSpace(input.Expression)
	if _, ok := ratePolicyModes[input.Mode]; !ok {
		return input, fmt.Errorf("%w: unsupported rate policy mode", ErrInvalid)
	}
	if math.IsNaN(input.Offset) || math.IsInf(input.Offset, 0) || input.Offset < -100_000 || input.Offset > 100_000 {
		return input, fmt.Errorf("%w: rate policy offset is outside the supported range", ErrInvalid)
	}
	if len(input.Expression) > 500 {
		return input, fmt.Errorf("%w: rate policy expression is too long", ErrInvalid)
	}
	if (input.Mode == "locked" || input.Mode == "manual_source") && input.Enabled && input.Offset <= 0 {
		return input, fmt.Errorf("%w: enabled %s rule requires a positive rate", ErrInvalid, input.Mode)
	}
	for i := range input.Bindings {
		input.Bindings[i].GroupID = strings.TrimSpace(input.Bindings[i].GroupID)
		if input.Bindings[i].ChannelID == 0 || input.Bindings[i].GroupID == "" {
			return input, fmt.Errorf("%w: rate source channel and group are required", ErrInvalid)
		}
	}
	return input, nil
}

func validateRatePolicyBindingCount(targetType string, bindings []RatePolicyBindingInput) error {
	if targetType == RatePolicyTargetAccount && len(bindings) > 1 {
		return fmt.Errorf("%w: account rate policies support exactly one source binding", ErrInvalid)
	}
	return nil
}

func canonicalRateSourceFresh(row canonicalRateSourceRow, now time.Time) bool {
	if !row.MonitorEnabled || strings.TrimSpace(row.LastError) != "" || row.LastRateScanAt == nil {
		return false
	}
	minutes := row.RateIntervalMinute * 2
	if minutes < 60 {
		minutes = 60
	}
	latest := row.LastSeenAt
	if row.LastRateScanAt.After(latest) {
		latest = *row.LastRateScanAt
	}
	age := now.Sub(latest)
	return age >= -time.Minute && age <= time.Duration(minutes)*time.Minute
}

func ratePolicyNeedsSource(mode string) bool {
	return mode != "locked" && mode != "manual_source"
}

func ratePolicySourceKey(channelID uint, groupID string) string {
	return strconv.FormatUint(uint64(channelID), 10) + ":" + strings.TrimSpace(groupID)
}

func ratePolicyTargetKey(targetType string, targetID int64) string {
	return targetType + ":" + strconv.FormatInt(targetID, 10)
}

func ratePolicyRuleTable(targetType string) string {
	if targetType == RatePolicyTargetAccount {
		return "bl_account_rate_rules"
	}
	return "bl_group_rate_rules"
}

func ratePolicyRuleIDColumn(targetType string) string {
	if targetType == RatePolicyTargetAccount {
		return "account_id"
	}
	return "group_id"
}

func ruleOrDefault(rule RatePolicyRule) RatePolicyRule {
	if strings.TrimSpace(rule.Mode) == "" {
		rule.Mode = "first"
	}
	return rule
}

func ratePolicyBindingsOrEmpty(bindings map[string][]RatePolicyBinding, key string) []RatePolicyBinding {
	value := bindings[key]
	if value == nil {
		return []RatePolicyBinding{}
	}
	return value
}

func enabledRatePolicyBindings(bindings []RatePolicyBinding, sources []RatePolicySource) []RatePolicyBinding {
	enabled := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if source.Enabled {
			enabled[ratePolicySourceKey(source.ChannelID, source.GroupID)] = struct{}{}
		}
	}
	filtered := make([]RatePolicyBinding, 0, len(bindings))
	for _, binding := range bindings {
		if _, ok := enabled[ratePolicySourceKey(binding.ChannelID, binding.GroupID)]; ok {
			filtered = append(filtered, binding)
		}
	}
	return filtered
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}
