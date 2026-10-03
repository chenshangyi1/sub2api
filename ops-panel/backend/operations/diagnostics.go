package operations

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bejix/upstream-ops/backend/connector/sub2api"
	"github.com/bejix/upstream-ops/backend/storage"
	"gorm.io/gorm"
)

const defaultWorkerInterval = 10 * time.Minute

type diagnosticSetting struct {
	Key   string `gorm:"column:key"`
	Value string `gorm:"column:value"`
}

type diagnosticSyncLog struct {
	ID        int64     `gorm:"column:id"`
	TargetID  uint      `gorm:"column:target_id"`
	Action    string    `gorm:"column:action"`
	Success   bool      `gorm:"column:success"`
	Message   string    `gorm:"column:message"`
	Detail    string    `gorm:"column:detail"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

type sourceBindingReference struct {
	ID           int64  `gorm:"column:id"`
	ConnectionID int64  `gorm:"column:connection_id"`
	TargetType   string `gorm:"column:target_type"`
	TargetID     int64  `gorm:"column:target_id"`
	SourceSiteID int64  `gorm:"column:source_site_id"`
}

type managedAccountReference struct {
	ID            int64 `gorm:"column:id"`
	SyncGroupID   int64 `gorm:"column:sync_group_id"`
	SyncAccountID int64 `gorm:"column:sync_account_id"`
}

type monitorRuleReference struct {
	ID           int64 `gorm:"column:id"`
	ConnectionID int64 `gorm:"column:connection_id"`
	AccountID    int64 `gorm:"column:account_id"`
}

type targetReferenceIDs struct {
	Accounts        map[int64]bool
	Groups          map[int64]bool
	AccountsChecked bool
	GroupsChecked   bool
}

type targetReferenceLoader func(context.Context, int64) (targetReferenceIDs, error)

type targetReferenceCache struct {
	service *Service
	values  map[int64]targetReferenceIDs
}

func newTargetReferenceCache(service *Service) *targetReferenceCache {
	return &targetReferenceCache{service: service, values: map[int64]targetReferenceIDs{}}
}

func (c *targetReferenceCache) get(ctx context.Context, targetID int64) targetReferenceIDs {
	if value, ok := c.values[targetID]; ok {
		return value
	}
	loader := c.service.targetReferenceLoader
	if loader == nil {
		loader = c.service.fetchTargetReferenceIDs
	}
	value, err := loader(ctx, targetID)
	if err != nil {
		if c.service.log != nil {
			c.service.log.Warn("target reference validation skipped", "target_id", targetID, "err", err)
		}
	}
	c.values[targetID] = value
	return value
}

func (s *Service) fetchTargetReferenceIDs(ctx context.Context, targetID int64) (targetReferenceIDs, error) {
	result := targetReferenceIDs{Accounts: map[int64]bool{}, Groups: map[int64]bool{}}
	_, target, err := s.target(ctx, uint(targetID))
	if err != nil {
		return result, err
	}
	accounts, err := listAllTargetAccounts(func(page, pageSize int) ([]sub2api.AdminAccount, error) {
		return s.admin.ListAccounts(ctx, target, page, pageSize)
	})
	if err != nil {
		return targetReferenceIDs{}, err
	}
	for _, account := range accounts {
		result.Accounts[account.ID] = true
	}
	groups, err := s.admin.ListGroups(ctx, target, true)
	if err != nil {
		return targetReferenceIDs{}, fmt.Errorf("load target groups: %w", err)
	}
	for _, group := range groups {
		if group.ID > 0 {
			result.Groups[group.ID] = true
		}
	}
	result.AccountsChecked = true
	result.GroupsChecked = true
	return result, nil
}

func (s *Service) GetDiagnostics(ctx context.Context) (*Diagnostics, error) {
	now := s.now().UTC()
	result := &Diagnostics{
		Services:   make([]ServiceStatus, 0, 2),
		Tasks:      []TaskStatus{},
		RecentLogs: []DiagnosticLog{},
	}

	result.Services = append(result.Services, databaseServiceStatus(ctx, s.db, "operations", "Operations database", now, false))
	result.Services = append(result.Services, databaseServiceStatus(ctx, s.mainDB, "sub2api", "Sub2API database", now, true))
	result.Connections = s.connectionStatus(ctx)

	settings, err := loadDiagnosticSettings(ctx, s.db)
	if err != nil {
		return nil, err
	}
	populateWorkerDiagnostics(result, settings, now)

	syncLogs, err := loadRecentSyncLogs(ctx, s.db, 20)
	if err != nil {
		return nil, err
	}
	for _, row := range syncLogs {
		finishedAt := row.CreatedAt.UTC()
		status := "failed"
		if row.Success {
			status = "success"
		}
		message := strings.TrimSpace(row.Message)
		if message == "" {
			message = strings.TrimSpace(row.Detail)
		}
		result.Tasks = append(result.Tasks, TaskStatus{
			ID:         fmt.Sprintf("sync-log:%d", row.ID),
			Name:       row.Action,
			Status:     status,
			FinishedAt: &finishedAt,
			Message:    message,
		})
	}

	actionLogs, err := loadRecentActionLogs(ctx, s.db, 30)
	if err != nil {
		return nil, err
	}
	for _, row := range actionLogs {
		status := "failed"
		if row.Success {
			status = "success"
		}
		result.RecentLogs = append(result.RecentLogs, DiagnosticLog{
			ID:        int64(row.ID),
			CreatedAt: row.CreatedAt.UTC(),
			Action:    row.Action,
			Target:    row.Target,
			Status:    status,
			Message:   row.Message,
		})
	}
	for _, row := range syncLogs {
		status := "failed"
		if row.Success {
			status = "success"
		}
		message := strings.TrimSpace(row.Message)
		if message == "" {
			message = strings.TrimSpace(row.Detail)
		}
		result.RecentLogs = append(result.RecentLogs, DiagnosticLog{
			ID:        -row.ID,
			CreatedAt: row.CreatedAt.UTC(),
			Action:    row.Action,
			Target:    fmt.Sprintf("target:%d", row.TargetID),
			Status:    status,
			Message:   message,
		})
	}
	sort.SliceStable(result.RecentLogs, func(i, j int) bool {
		return result.RecentLogs[i].CreatedAt.After(result.RecentLogs[j].CreatedAt)
	})
	if len(result.RecentLogs) > 30 {
		result.RecentLogs = result.RecentLogs[:30]
	}

	invalid, _, err := s.invalidDataReferences(ctx)
	if err != nil {
		return nil, err
	}
	result.InvalidData = invalid
	return result, nil
}

// connectionStatus 自检面板与 sub2api 的接入面：
//   - panel_db：面板自身存储
//   - sub2api_db：只读主库（SUB2API_DATABASE_URL），驱动目标分析与健康回放
//   - admin_api：默认上游同步目标（SUB2API_ADMIN_API_KEY），驱动账号/探测/同步
func (s *Service) connectionStatus(ctx context.Context) []ConnectionStatus {
	statuses := make([]ConnectionStatus, 0, 3)

	panel := ConnectionStatus{Key: "panel_db", Name: "面板数据库", Detail: "连接正常"}
	if sqlDB, err := s.db.DB(); err != nil {
		panel.Detail = "无法获取连接池: " + err.Error()
	} else if err := sqlDB.PingContext(ctx); err != nil {
		panel.Detail = "连接失败: " + err.Error()
	} else {
		panel.OK = true
	}
	statuses = append(statuses, panel)

	main := ConnectionStatus{Key: "sub2api_db", Name: "sub2api 主库（只读）", Detail: "未配置 SUB2API_DATABASE_URL"}
	if s.mainDB != nil {
		main.OK = true
		main.Detail = "连接正常"
		if sqlDB, err := s.mainDB.DB(); err != nil {
			main.OK = false
			main.Detail = "无法获取连接池: " + err.Error()
		} else if err := sqlDB.PingContext(ctx); err != nil {
			main.OK = false
			main.Detail = "连接失败: " + err.Error()
		} else if !s.mainDB.Migrator().HasTable("usage_logs") || !s.mainDB.Migrator().HasTable("users") {
			main.OK = false
			main.Detail = "连接正常但缺少 usage_logs / users 表：确认连的是 sub2api 业务库而非监控库"
		}
	}
	statuses = append(statuses, main)

	admin := ConnectionStatus{Key: "admin_api", Name: "sub2api Admin API", Detail: "未配置默认目标：设置 SUB2API_ADMIN_API_KEY 与 SUB2API_BASE_URL 后重启面板"}
	var target storage.UpstreamSyncTarget
	err := s.db.WithContext(ctx).Where("enabled = ?", true).Order("id ASC").First(&target).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
	case err != nil:
		admin.Detail = "读取目标失败: " + err.Error()
	default:
		key, decryptErr := s.cipher.Decrypt(target.AdminAPIKeyCipher)
		if decryptErr != nil || strings.TrimSpace(key) == "" {
			admin.Detail = "目标 " + target.Name + " 没有可用的 admin key"
			break
		}
		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if pingErr := s.admin.Ping(pingCtx, sub2api.AdminTarget{BaseURL: target.BaseURL, APIKey: key}); pingErr != nil {
			admin.Detail = "目标 " + target.BaseURL + " 不可达: " + pingErr.Error()
		} else {
			admin.OK = true
			admin.Detail = target.BaseURL + " 鉴权正常"
		}
	}
	statuses = append(statuses, admin)

	return statuses
}

func (s *Service) CleanupInvalidData(ctx context.Context) (*InvalidData, error) {
	_, refs, err := s.invalidDataReferences(ctx)
	if err != nil {
		return nil, err
	}
	hasActionLog := s.db != nil && s.db.Migrator().HasTable("operations_action_logs")
	deleted := InvalidData{}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		deleted.Bindings, err = deleteReferenceIDs(tx, "bl_source_bindings", refs.bindings)
		if err != nil {
			return err
		}
		deleted.ManagedAccounts, err = deleteReferenceIDs(tx, "upstream_sync_managed_accounts", refs.managedAccounts)
		if err != nil {
			return err
		}
		deleted.ProbeRules, err = deleteReferenceIDs(tx, "upstream_monitor_rules", refs.probeRules)
		if err != nil {
			return err
		}
		if !hasActionLog {
			return nil
		}
		message := fmt.Sprintf("deleted bindings=%d managed_accounts=%d probe_rules=%d", deleted.Bindings, deleted.ManagedAccounts, deleted.ProbeRules)
		return tx.Create(&ActionLog{
			Action:    "cleanup_invalid_data",
			Target:    "operations",
			Success:   true,
			Message:   message,
			CreatedAt: s.now().UTC(),
		}).Error
	})
	if err != nil {
		s.recordAction(ctx, "cleanup_invalid_data", "operations", err.Error(), false)
		return nil, err
	}
	return &deleted, nil
}

func databaseServiceStatus(ctx context.Context, db *gorm.DB, id, name string, checkedAt time.Time, optional bool) ServiceStatus {
	status := ServiceStatus{ID: id, Name: name, CheckedAt: &checkedAt}
	if db == nil {
		status.Status = StatusUnknown
		if optional {
			status.Detail = "database is not configured"
		} else {
			status.Detail = "database is unavailable"
		}
		return status
	}
	var value int
	if err := db.WithContext(ctx).Raw("SELECT 1").Scan(&value).Error; err != nil {
		status.Status = StatusError
		status.Detail = err.Error()
		return status
	}
	status.Status = StatusHealthy
	status.Detail = "connected"
	return status
}

func loadDiagnosticSettings(ctx context.Context, db *gorm.DB) (map[string]string, error) {
	result := map[string]string{}
	if db == nil || !db.Migrator().HasTable("settings") {
		return result, nil
	}
	keys := []string{
		"worker_heartbeat_at",
		"worker_last_run_started_at",
		"worker_last_run_finished_at",
		"worker_last_run_status",
		"worker_last_run_message",
		"worker_interval_seconds",
		"worker_next_run_at",
	}
	var rows []diagnosticSetting
	if err := db.WithContext(ctx).Table("settings").Select("key, value").Where("key IN ?", keys).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load worker diagnostic settings: %w", err)
	}
	for _, row := range rows {
		result[row.Key] = row.Value
	}
	return result, nil
}

func populateWorkerDiagnostics(result *Diagnostics, settings map[string]string, now time.Time) {
	heartbeat := parseTimeValue(settings["worker_heartbeat_at"])
	started := parseTimeValue(settings["worker_last_run_started_at"])
	finished := parseTimeValue(settings["worker_last_run_finished_at"])
	result.Worker.HeartbeatAt = heartbeat
	result.Worker.LastRunAt = finished
	if result.Worker.LastRunAt == nil {
		result.Worker.LastRunAt = started
	}
	result.Worker.LastRunStatus = strings.TrimSpace(settings["worker_last_run_status"])
	result.Worker.LastRunMessage = strings.TrimSpace(settings["worker_last_run_message"])

	interval := defaultWorkerInterval
	if seconds, err := strconv.Atoi(strings.TrimSpace(settings["worker_interval_seconds"])); err == nil && seconds > 0 {
		interval = time.Duration(seconds) * time.Second
	}
	staleAfter := 2*interval + time.Minute
	switch {
	case heartbeat == nil:
		result.Worker.Status = StatusUnknown
	case now.Sub(heartbeat.UTC()) > staleAfter:
		result.Worker.Status = StatusWarning
	case strings.EqualFold(result.Worker.LastRunStatus, "failed"):
		result.Worker.Status = StatusError
	default:
		result.Worker.Status = StatusHealthy
	}
}

func loadRecentSyncLogs(ctx context.Context, db *gorm.DB, limit int) ([]diagnosticSyncLog, error) {
	rows := []diagnosticSyncLog{}
	if db == nil || !db.Migrator().HasTable("upstream_sync_logs") {
		return rows, nil
	}
	err := db.WithContext(ctx).Table("upstream_sync_logs").
		Select("id, target_id, action, success, message, detail, created_at").
		Order("created_at DESC, id DESC").Limit(limit).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load recent upstream sync logs: %w", err)
	}
	return rows, nil
}

func loadRecentActionLogs(ctx context.Context, db *gorm.DB, limit int) ([]ActionLog, error) {
	rows := []ActionLog{}
	if db == nil || !db.Migrator().HasTable("operations_action_logs") {
		return rows, nil
	}
	if err := db.WithContext(ctx).Order("created_at DESC, id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load recent operations action logs: %w", err)
	}
	return rows, nil
}

type invalidReferenceIDs struct {
	bindings        []int64
	managedAccounts []int64
	probeRules      []int64
}

func (s *Service) invalidDataReferences(ctx context.Context) (InvalidData, invalidReferenceIDs, error) {
	refs := invalidReferenceIDs{}
	targets := newTargetReferenceCache(s)
	var err error
	refs.bindings, err = s.invalidSourceBindingIDs(ctx, targets)
	if err != nil {
		return InvalidData{}, refs, err
	}
	refs.managedAccounts, err = invalidManagedAccountIDs(ctx, s.db)
	if err != nil {
		return InvalidData{}, refs, err
	}
	refs.probeRules, err = s.invalidMonitorRuleIDs(ctx, targets)
	if err != nil {
		return InvalidData{}, refs, err
	}
	return InvalidData{
		Bindings:        int64(len(refs.bindings)),
		ManagedAccounts: int64(len(refs.managedAccounts)),
		ProbeRules:      int64(len(refs.probeRules)),
	}, refs, nil
}

func (s *Service) invalidSourceBindingIDs(ctx context.Context, targets *targetReferenceCache) ([]int64, error) {
	if s.db == nil || !s.db.Migrator().HasTable("bl_source_bindings") {
		return []int64{}, nil
	}
	var rows []sourceBindingReference
	if err := s.db.WithContext(ctx).Table("bl_source_bindings").
		Select("id, connection_id, target_type, target_id, source_site_id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load source binding references: %w", err)
	}
	sourceIDs, sourceCheck, err := existingIDs(ctx, s.db, "bl_collection_sites", collectInt64(rows, func(row sourceBindingReference) int64 { return row.SourceSiteID }))
	if err != nil {
		return nil, err
	}
	invalid := make([]int64, 0)
	for _, row := range rows {
		missing := row.ConnectionID <= 0 || (sourceCheck && !sourceIDs[row.SourceSiteID])
		references := targetReferenceIDs{}
		if row.ConnectionID > 0 {
			references = targets.get(ctx, row.ConnectionID)
		}
		switch strings.ToLower(strings.TrimSpace(row.TargetType)) {
		case "account":
			missing = missing || (references.AccountsChecked && !references.Accounts[row.TargetID])
		case "group":
			missing = missing || (references.GroupsChecked && !references.Groups[row.TargetID])
		}
		if missing {
			invalid = append(invalid, row.ID)
		}
	}
	return invalid, nil
}

func invalidManagedAccountIDs(ctx context.Context, db *gorm.DB) ([]int64, error) {
	if db == nil || !db.Migrator().HasTable("upstream_sync_managed_accounts") {
		return []int64{}, nil
	}
	var rows []managedAccountReference
	if err := db.WithContext(ctx).Table("upstream_sync_managed_accounts").
		Select("id, sync_group_id, sync_account_id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load managed account references: %w", err)
	}
	groupIDs, groupCheck, err := existingIDs(ctx, db, "upstream_sync_groups", collectInt64(rows, func(row managedAccountReference) int64 { return row.SyncGroupID }))
	if err != nil {
		return nil, err
	}
	accountIDs, accountCheck, err := existingIDs(ctx, db, "upstream_sync_accounts", collectInt64(rows, func(row managedAccountReference) int64 { return row.SyncAccountID }))
	if err != nil {
		return nil, err
	}
	invalid := make([]int64, 0)
	for _, row := range rows {
		if (groupCheck && !groupIDs[row.SyncGroupID]) || (accountCheck && !accountIDs[row.SyncAccountID]) {
			invalid = append(invalid, row.ID)
		}
	}
	return invalid, nil
}

func (s *Service) invalidMonitorRuleIDs(ctx context.Context, targets *targetReferenceCache) ([]int64, error) {
	if s.db == nil || !s.db.Migrator().HasTable("upstream_monitor_rules") {
		return []int64{}, nil
	}
	var rows []monitorRuleReference
	if err := s.db.WithContext(ctx).Table("upstream_monitor_rules").
		Select("id, connection_id, account_id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load monitor rule references: %w", err)
	}
	targetIDs, targetCheck, err := existingIDs(ctx, s.db, "upstream_sync_targets", collectInt64(rows, func(row monitorRuleReference) int64 { return row.ConnectionID }))
	if err != nil {
		return nil, err
	}
	invalid := make([]int64, 0)
	for _, row := range rows {
		missingTarget := targetCheck && !targetIDs[row.ConnectionID]
		references := targetReferenceIDs{}
		if !missingTarget {
			references = targets.get(ctx, row.ConnectionID)
		}
		if row.AccountID <= 0 || missingTarget || (references.AccountsChecked && !references.Accounts[row.AccountID]) {
			invalid = append(invalid, row.ID)
		}
	}
	return invalid, nil
}

func existingIDs(ctx context.Context, db *gorm.DB, table string, ids []int64) (map[int64]bool, bool, error) {
	result := map[int64]bool{}
	if db == nil || !db.Migrator().HasTable(table) {
		return result, false, nil
	}
	if len(ids) == 0 {
		return result, true, nil
	}
	var existing []int64
	if err := db.WithContext(ctx).Table(table).Where("id IN ?", ids).Pluck("id", &existing).Error; err != nil {
		return nil, false, fmt.Errorf("load %s reference ids: %w", table, err)
	}
	for _, id := range existing {
		result[id] = true
	}
	return result, true, nil
}

func collectInt64[T any](rows []T, value func(T) int64) []int64 {
	seen := map[int64]struct{}{}
	for _, row := range rows {
		id := value(row)
		if id > 0 {
			seen[id] = struct{}{}
		}
	}
	result := make([]int64, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	return result
}

func deleteReferenceIDs(tx *gorm.DB, table string, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	result := tx.Table(table).Where("id IN ?", ids).Delete(nil)
	if result.Error != nil {
		return 0, fmt.Errorf("delete invalid %s references: %w", table, result.Error)
	}
	return result.RowsAffected, nil
}
