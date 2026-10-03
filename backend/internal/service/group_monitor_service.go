package service

import (
	"context"
	"strings"
	"sync"
	"time"
)

// GroupMonitorService 分组监控管理服务。
type groupMonitorUserLookup interface {
	GetByID(ctx context.Context, id int64) (*User, error)
}

type groupMonitorActiveSubscriptionLister interface {
	ListActiveByUserID(ctx context.Context, userID int64) ([]UserSubscription, error)
}

type GroupMonitorService struct {
	repo           GroupMonitorRepository
	accountTestSvc *AccountTestService
	rateLimitSvc   *RateLimitService
	userRepo       groupMonitorUserLookup
	userSubRepo    groupMonitorActiveSubscriptionLister
}

// NewGroupMonitorService 创建分组监控服务。accountTestSvc / rateLimitSvc 允许为 nil
// （仅 CRUD 场景），执行检测前会做 nil 校验。
func NewGroupMonitorService(repo GroupMonitorRepository, accountTestSvc *AccountTestService, rateLimitSvc *RateLimitService) *GroupMonitorService {
	return &GroupMonitorService{repo: repo, accountTestSvc: accountTestSvc, rateLimitSvc: rateLimitSvc}
}

// SetVisibilitySources attaches the repositories used by user-facing monitor filtering.
func (s *GroupMonitorService) SetVisibilitySources(userRepo groupMonitorUserLookup, userSubRepo groupMonitorActiveSubscriptionLister) {
	if s == nil {
		return
	}
	s.userRepo = userRepo
	s.userSubRepo = userSubRepo
}

const (
	groupMonitorStatusSuccess = "success"
	groupMonitorStatusFailed  = "failed"
	groupMonitorStatusUnknown = "unknown"

	groupMonitorMinIntervalMinutes = 5
	groupMonitorMaxIntervalMinutes = 1440
	groupMonitorMinOutputTokens    = 1
	groupMonitorMaxOutputTokens    = 256
)

// List 列表查询（含分组名与账号状态统计）。
func (s *GroupMonitorService) List(ctx context.Context, params GroupMonitorListParams) ([]*GroupMonitor, int64, error) {
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 || params.PageSize > 200 {
		params.PageSize = 20
	}
	return s.repo.List(ctx, params)
}

// ListVisibleToUser filters monitors by the same public/explicit/active-subscription rules as API-key group selection.
func (s *GroupMonitorService) ListVisibleToUser(ctx context.Context, userID int64, params GroupMonitorListParams) ([]*GroupMonitor, int64, error) {
	user, subscribed, err := s.resolveUserMonitorVisibility(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	monitors, _, err := s.List(ctx, params)
	if err != nil {
		return nil, 0, err
	}
	visible := make([]*GroupMonitor, 0, len(monitors))
	for _, monitor := range monitors {
		if groupMonitorVisibleToUser(monitor, user, subscribed) {
			visible = append(visible, monitor)
		}
	}
	return visible, int64(len(visible)), nil
}

func (s *GroupMonitorService) resolveUserMonitorVisibility(ctx context.Context, userID int64) (*User, map[int64]bool, error) {
	if s == nil || s.userRepo == nil || userID <= 0 {
		return nil, nil, ErrGroupMonitorNotFound
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil || user == nil {
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, ErrGroupMonitorNotFound
	}
	subscribed := make(map[int64]bool)
	if s.userSubRepo == nil {
		return user, subscribed, nil
	}
	subs, err := s.userSubRepo.ListActiveByUserID(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	for _, sub := range subs {
		subscribed[sub.GroupID] = true
	}
	return user, subscribed, nil
}

func groupMonitorVisibleToUser(monitor *GroupMonitor, user *User, subscribed map[int64]bool) bool {
	if monitor == nil || user == nil {
		return false
	}
	if monitor.UserVisibleSet && !monitor.UserVisible && !user.IsAdmin() {
		return false
	}
	if monitor.SubscriptionType == SubscriptionTypeSubscription && !subscribed[monitor.GroupID] {
		return false
	}
	return !monitor.IsExclusive || user.CanBindGroup(monitor.GroupID, true)
}

// GetVisibleToUser returns a monitor only when the user may see its group.
func (s *GroupMonitorService) GetVisibleToUser(ctx context.Context, userID, monitorID int64) (*GroupMonitor, error) {
	monitor, err := s.Get(ctx, monitorID)
	if err != nil {
		return nil, err
	}
	user, subscribed, err := s.resolveUserMonitorVisibility(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !groupMonitorVisibleToUser(monitor, user, subscribed) {
		return nil, ErrGroupMonitorNotFound
	}
	return monitor, nil
}

// Get 查询单个监控。
func (s *GroupMonitorService) Get(ctx context.Context, id int64) (*GroupMonitor, error) {
	return s.repo.GetByID(ctx, id)
}

// Create 创建分组监控。interval_minutes 归一化到 [5, 1440]，model_id 可空（自动选模型）。
func (s *GroupMonitorService) Create(ctx context.Context, m *GroupMonitor) (*GroupMonitor, error) {
	if m.GroupID <= 0 {
		return nil, ErrGroupMonitorInvalidGroup
	}
	if existing, err := s.repo.GetByGroupID(ctx, m.GroupID); err == nil && existing != nil {
		return nil, ErrGroupMonitorDuplicateGroup
	}
	m.IntervalMinutes = normalizeGroupMonitorInterval(m.IntervalMinutes)
	m.MaxOutputTokens = normalizeGroupMonitorOutputTokens(m.MaxOutputTokens)
	m.ModelID = strings.TrimSpace(m.ModelID)
	if err := s.repo.Create(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

// Update 更新监控配置。
func (s *GroupMonitorService) Update(ctx context.Context, id int64, m *GroupMonitor) (*GroupMonitor, error) {
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	m.ID = existing.ID
	m.GroupID = existing.GroupID
	m.IntervalMinutes = normalizeGroupMonitorInterval(m.IntervalMinutes)
	m.MaxOutputTokens = normalizeGroupMonitorOutputTokens(m.MaxOutputTokens)
	m.ModelID = strings.TrimSpace(m.ModelID)
	if err := s.repo.Update(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

// Delete 删除监控（结果通过外键 CASCADE 清理）。
func (s *GroupMonitorService) Delete(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}

// ListResults 返回某监控下所有账号的最新状态。
func (s *GroupMonitorService) ListResults(ctx context.Context, id int64) ([]*GroupMonitorAccountStatus, error) {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return nil, err
	}
	return s.repo.ListResults(ctx, id)
}

// HistoryStats 返回某监控在时间窗内的聚合指标（可用率/延迟/TTFT/缓存率）。
func (s *GroupMonitorService) HistoryStats(ctx context.Context, id int64, since time.Time) (*GroupMonitorHistoryStats, error) {
	return s.repo.QueryHistoryStats(ctx, id, since)
}

// HistorySeries 返回某监控在时间窗内按桶聚合的状态序列（历史状态条）。
func (s *GroupMonitorService) HistorySeries(ctx context.Context, id int64, since time.Time, bucketCount int) ([]GroupMonitorSeriesPoint, error) {
	return s.repo.QueryHistorySeries(ctx, id, since, bucketCount)
}

// RecentRecords 返回某监控最近 limit 条探测记录（升序）。
func (s *GroupMonitorService) RecentRecords(ctx context.Context, id int64, limit int) ([]GroupMonitorHistoryRecord, error) {
	return s.repo.ListRecentRecords(ctx, id, limit)
}

// PruneHistory 清理早于 retention 的历史记录。
func (s *GroupMonitorService) PruneHistory(ctx context.Context, retention time.Duration) (int64, error) {
	return s.repo.PruneHistory(ctx, time.Now().Add(-retention))
}

// HistoryStatsBatch 一次性聚合所有监控在时间窗内的统计。
func (s *GroupMonitorService) HistoryStatsBatch(ctx context.Context, since time.Time) (map[int64]*GroupMonitorHistoryStats, error) {
	return s.repo.QueryHistoryStatsBatch(ctx, since)
}

// RecentRecordsBatch 一次性返回所有监控的最近 limit 条记录。
func (s *GroupMonitorService) RecentRecordsBatch(ctx context.Context, limitPerMonitor int) (map[int64][]GroupMonitorHistoryRecord, error) {
	return s.repo.ListRecentRecordsBatch(ctx, limitPerMonitor)
}

// AccountStatesBatch 一次性返回所有监控的账号最新状态。
func (s *GroupMonitorService) AccountStatesBatch(ctx context.Context) (map[int64][]*GroupMonitorAccountStatus, error) {
	return s.repo.ListAccountStatesBatch(ctx)
}

// UsageStatsBatch 按 group_id 聚合被动用量（usage_logs）。
func (s *GroupMonitorService) UsageStatsBatch(ctx context.Context, since time.Time) (map[int64]*GroupUsageStats, error) {
	return s.repo.QueryGroupUsageStatsBatch(ctx, since)
}

// PassiveStatsBatch 按 group_id 聚合真实请求（成功+失败+TTFT+分桶）。
func (s *GroupMonitorService) PassiveStatsBatch(ctx context.Context, since time.Time, bucketCount int) (map[int64]*GroupPassiveStats, error) {
	return s.repo.QueryGroupPassiveStatsBatch(ctx, since, bucketCount)
}

// GroupNamesBatch 按 group_id 批量返回分组名。
func (s *GroupMonitorService) GroupNamesBatch(ctx context.Context, groupIDs []int64) (map[int64]string, error) {
	return s.repo.GroupNamesBatch(ctx, groupIDs)
}

// RecentUsageEvents 按分组各取最近 limit 条真实请求事件（轻量索引扫描）。
func (s *GroupMonitorService) RecentUsageEvents(ctx context.Context, groupIDs []int64, limit int) (map[int64][]GroupUsageEvent, error) {
	return s.repo.ListRecentUsageEvents(ctx, groupIDs, limit)
}

// UsageEventsSince 返回 since 之后各分组的真实请求事件。
func (s *GroupMonitorService) UsageEventsSince(ctx context.Context, groupIDs []int64, since time.Time) (map[int64][]GroupUsageEvent, error) {
	return s.repo.ListUsageEventsSince(ctx, groupIDs, since)
}

// ProbeRecordsSince 返回 since 之后各监控的探测记录。
func (s *GroupMonitorService) ProbeRecordsSince(ctx context.Context, monitorIDs []int64, since time.Time) (map[int64][]GroupProbeRecord, error) {
	return s.repo.ListProbeRecordsSince(ctx, monitorIDs, since)
}

// UsageAggSince 按分组精确聚合 since 之后的真实请求（全窗 COUNT）。
func (s *GroupMonitorService) UsageAggSince(ctx context.Context, groupIDs []int64, since time.Time) (map[int64]*GroupWindowAgg, error) {
	return s.repo.AggregateUsageSince(ctx, groupIDs, since)
}

// ProbeAggSince 按监控精确聚合 since 之后的探测记录。
func (s *GroupMonitorService) ProbeAggSince(ctx context.Context, monitorIDs []int64, since time.Time) (map[int64]*GroupWindowAgg, error) {
	return s.repo.AggregateProbesSince(ctx, monitorIDs, since)
}

// RecentGroupModels 返回某分组最近成功请求的模型（探测候选）。
func (s *GroupMonitorService) RecentGroupModels(ctx context.Context, groupID int64, since time.Time, limit int) ([]string, error) {
	return s.repo.RecentGroupModels(ctx, groupID, since, limit)
}

// RunCheck 手动触发一轮检测（同步）。返回检测后的账号状态列表。
func (s *GroupMonitorService) RunCheck(ctx context.Context, id int64) ([]*GroupMonitorAccountStatus, error) {
	if s.accountTestSvc == nil {
		return nil, ErrGroupMonitorNoProbe
	}
	m, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.RunOneMonitor(ctx, m); err != nil {
		return nil, err
	}
	return s.repo.ListResults(ctx, id)
}

// ListDue 返回所有到期（next_run_at <= now）的启用监控，供 runner 定时调度。
func (s *GroupMonitorService) ListDue(ctx context.Context, now time.Time) ([]*GroupMonitor, error) {
	return s.repo.ListDue(ctx, now)
}

// RunOneMonitor 对单个监控执行一轮检测：枚举分组下账号，逐个极简探测，写结果，
// 更新 last_run_at / next_run_at。runner 与手动 RunCheck 共用。
func (s *GroupMonitorService) RunOneMonitor(ctx context.Context, m *GroupMonitor) error {
	if s.accountTestSvc == nil {
		return ErrGroupMonitorNoProbe
	}
	accounts, err := s.repo.ListGroupAccounts(ctx, m.GroupID)
	if err != nil {
		return err
	}
	// 手动暂停的账号不探测：先清理其旧结果，避免历史状态残留展示。
	if err := s.repo.DeleteResultsForUnschedulableAccounts(ctx, m.ID); err != nil {
		return err
	}

	now := time.Now()
	results := make([]*GroupMonitorResult, len(accounts))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)
	for i, acct := range accounts {
		wg.Add(1)
		go func(idx int, a *GroupMonitorAccount) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r := s.probeAccount(ctx, m, a)
			r.CheckedAt = now
			results[idx] = r
		}(i, acct)
	}
	wg.Wait()

	// 先写所有结果，再统一推进调度时间，避免写库失败导致 next_run_at 不同步。
	for _, r := range results {
		if err := s.repo.UpsertResult(ctx, r); err != nil {
			return err
		}
		if err := s.repo.AppendHistory(ctx, r); err != nil {
			return err
		}
	}

	nextRun := now.Add(time.Duration(m.IntervalMinutes) * time.Minute)
	return s.repo.UpdateAfterRun(ctx, m.ID, now, nextRun)
}

// groupMonitorProbeTimeout 单账号探测超时，避免单个账号 hang 拖慢整轮检测。
const groupMonitorProbeTimeout = 30 * time.Second

// probeAccount 极简探测单个账号，返回待落库的结果。
// 模型不匹配（model_not_found / not supported）时按平台候选模型 fallback，
// 避免 Gemini/Claude 等账号因默认模型不受支持而秒失败。
func (s *GroupMonitorService) probeAccount(ctx context.Context, m *GroupMonitor, acct *GroupMonitorAccount) *GroupMonitorResult {
	probeCtx, cancel := context.WithTimeout(ctx, groupMonitorProbeTimeout)
	defer cancel()

	result := &GroupMonitorResult{
		MonitorID: m.ID,
		AccountID: acct.ID,
		Status:    groupMonitorStatusUnknown,
		ModelID:   m.ModelID,
	}

	probe, usedModel, err := s.probeWithFallback(probeCtx, acct, m)
	if err != nil {
		result.Status = groupMonitorStatusFailed
		result.ErrorMessage = err.Error()
		return result
	}
	if probe == nil {
		result.Status = groupMonitorStatusFailed
		result.ErrorMessage = "empty probe result"
		return result
	}
	if usedModel != "" {
		result.ModelID = usedModel
	}

	result.LatencyMs = probe.LatencyMs
	result.TTFTMs = probe.TTFTMs
	result.InputTokens = probe.InputTokens
	result.CacheRead = probe.CacheReadTokens
	result.CacheCreate = probe.CacheCreationTokens
	if probe.Status == "success" {
		result.Status = groupMonitorStatusSuccess
		result.ErrorMessage = ""
		if m.AutoRecover {
			s.tryRecoverAccount(ctx, acct.ID)
		}
	} else {
		result.Status = groupMonitorStatusFailed
		result.ErrorMessage = probe.ErrorMessage
	}
	return result
}

// probeWithFallback 依次尝试候选模型探测账号；模型不匹配类错误才继续尝试下一个候选。
// 候选优先级：配置模型 > 分组最近成功模型（usage_logs 被动数据）> 账号 mapping 模型 > 平台默认。
func (s *GroupMonitorService) probeWithFallback(ctx context.Context, acct *GroupMonitorAccount, m *GroupMonitor) (probe *ScheduledTestResult, usedModel string, err error) {
	candidates := []string{}
	if strings.TrimSpace(m.ModelID) != "" {
		candidates = append(candidates, strings.TrimSpace(m.ModelID))
	}
	if recent, rerr := s.repo.RecentGroupModels(ctx, m.GroupID, time.Now().Add(-24*time.Hour), 3); rerr == nil {
		for _, rm := range recent {
			if !containsString(candidates, rm) {
				candidates = append(candidates, rm)
			}
		}
	}
	if suggested, serr := s.accountTestSvc.SuggestProbeModel(ctx, acct.ID); serr == nil && strings.TrimSpace(suggested) != "" {
		if !containsString(candidates, suggested) {
			candidates = append(candidates, suggested)
		}
	}
	for _, d := range groupMonitorProbeCandidates("", acct.Platform) {
		if !containsString(candidates, d) {
			candidates = append(candidates, d)
		}
	}
	if len(candidates) == 0 {
		candidates = []string{""}
	}

	var lastErr error
	for _, cand := range candidates {
		probe, err = s.accountTestSvc.RunHealthProbe(ctx, acct.ID, cand)
		if err == nil && probe != nil && probe.Status == "success" {
			return probe, cand, nil
		}
		lastErr = err
		// 仅模型不匹配类失败继续 fallback，其余（网络/余额/403）立即返回。
		if !groupMonitorIsModelMismatch(probeErrorText(err, probe)) {
			return probe, cand, lastErr
		}
	}
	return probe, "", lastErr
}

func containsString(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

func probeErrorText(err error, probe *ScheduledTestResult) string {
	if probe != nil && probe.ErrorMessage != "" {
		return probe.ErrorMessage
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

// groupMonitorIsModelMismatch 判断探测失败是否为"模型不受支持"类错误。
func groupMonitorIsModelMismatch(text string) bool {
	low := strings.ToLower(text)
	return strings.Contains(low, "not supported") ||
		strings.Contains(low, "model_not_found") ||
		strings.Contains(low, "no available channel for model") ||
		strings.Contains(low, "model not found") ||
		strings.Contains(low, "does not support")
}

// groupMonitorProbeCandidates 返回探测候选模型列表：优先配置值，空则平台默认 + 常用 fallback。
func groupMonitorProbeCandidates(modelID, platform string) []string {
	if strings.TrimSpace(modelID) != "" {
		return []string{strings.TrimSpace(modelID)}
	}
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "anthropic", "claude":
		return []string{"", "claude-sonnet-4-5-20250929", "claude-3-5-sonnet-20241022", "claude-3-haiku-20240307"}
	case "gemini":
		return []string{"", "gemini-2.0-flash", "gemini-1.5-flash", "gemini-2.5-pro"}
	case "openai":
		return []string{"", "gpt-4.1-mini", "gpt-4o-mini"}
	case "grok":
		return []string{"", "grok-3-mini"}
	default:
		return []string{""}
	}
}

func (s *GroupMonitorService) tryRecoverAccount(ctx context.Context, accountID int64) {
	if s.rateLimitSvc == nil {
		return
	}
	if _, err := s.rateLimitSvc.RecoverAccountAfterSuccessfulTest(ctx, accountID); err != nil {
		// 恢复失败不阻断检测循环，仅由上游恢复结果决定是否清状态。
		return
	}
}

func normalizeGroupMonitorInterval(minutes int) int {
	if minutes < groupMonitorMinIntervalMinutes {
		return groupMonitorMinIntervalMinutes
	}
	if minutes > groupMonitorMaxIntervalMinutes {
		return groupMonitorMaxIntervalMinutes
	}
	return minutes
}

func normalizeGroupMonitorOutputTokens(tokens int) int {
	if tokens < groupMonitorMinOutputTokens {
		return groupMonitorMinOutputTokens
	}
	if tokens > groupMonitorMaxOutputTokens {
		return groupMonitorMaxOutputTokens
	}
	return tokens
}
