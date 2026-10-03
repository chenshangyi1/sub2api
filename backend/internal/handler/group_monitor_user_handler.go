package handler

import (
	"context"
	"strconv"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// groupMonitorUserCache 缓存用户端渠道监控列表（30 秒）。
// 聚合基于 usage_logs/ops_error_logs 大表，避免每次刷新串行跑 8+ 个重查询。
var groupMonitorUserCache = struct {
	mu      sync.RWMutex
	entries map[string]struct {
		payload groupMonitorUserListResponse
		expires time.Time
	}
}{}

func groupMonitorUserCacheGet(key string) (groupMonitorUserListResponse, bool) {
	groupMonitorUserCache.mu.RLock()
	defer groupMonitorUserCache.mu.RUnlock()
	entry, ok := groupMonitorUserCache.entries[key]
	if !ok || time.Now().After(entry.expires) {
		return groupMonitorUserListResponse{}, false
	}
	return entry.payload, true
}

func groupMonitorUserCacheSet(key string, payload groupMonitorUserListResponse) {
	groupMonitorUserCache.mu.Lock()
	defer groupMonitorUserCache.mu.Unlock()
	if groupMonitorUserCache.entries == nil {
		groupMonitorUserCache.entries = make(map[string]struct {
			payload groupMonitorUserListResponse
			expires time.Time
		})
	}
	groupMonitorUserCache.entries[key] = struct {
		payload groupMonitorUserListResponse
		expires time.Time
	}{payload: payload, expires: time.Now().Add(30 * time.Second)}
}

// GroupMonitorUserHandler 分组监控用户只读 handler。
// 普通登录用户可在「渠道监控」页查看各分组的健康概况：可用率/TTFT/缓存率 + 历史状态条。
// 不暴露具体账号明细。
type GroupMonitorUserHandler struct {
	groupMonitorService *service.GroupMonitorService
}

// NewGroupMonitorUserHandler 创建 handler。
func NewGroupMonitorUserHandler(groupMonitorService *service.GroupMonitorService) *GroupMonitorUserHandler {
	return &GroupMonitorUserHandler{groupMonitorService: groupMonitorService}
}

type groupMonitorUserStats struct {
	Availability float64 `json:"availability"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	AvgTTFTMs    float64 `json:"avg_ttft_ms"`
	CacheRate    float64 `json:"cache_rate"`
	Probes       int     `json:"probes"`
}

type groupMonitorUserSeriesPoint struct {
	Bucket       string  `json:"bucket"`
	Availability float64 `json:"availability"`
	AvgTTFTMs    float64 `json:"avg_ttft_ms"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	CacheRate    float64 `json:"cache_rate"`
	Probes       int     `json:"probes"`
}

type groupMonitorUserAccountState struct {
	Status    string `json:"status"`
	LatencyMs int64  `json:"latency_ms"`
}

type groupMonitorUserRecentRecord struct {
	Status    string  `json:"status"`
	LatencyMs int64   `json:"latency_ms"`
	CheckedAt string  `json:"checked_at"`
	Success   int64   `json:"success"`
	Failed    int64   `json:"failed"`
	CacheRate float64 `json:"cache_rate"`
}

type groupMonitorUserListItem struct {
	ID           int64                                    `json:"id"`
	GroupID      int64                                    `json:"group_id"`
	GroupName    string                                   `json:"group_name"`
	Enabled      bool                                     `json:"enabled"`
	ModelID      string                                   `json:"model_id"`
	LastRunAt    *string                                  `json:"last_run_at,omitempty"`
	NextRunAt    *string                                  `json:"next_run_at,omitempty"`
	AccountCount int                                      `json:"account_count"`
	HealthyCount int                                      `json:"healthy_count"`
	FailedCount  int                                      `json:"failed_count"`
	UnknownCount int                                      `json:"unknown_count"`
	Stats        map[string]groupMonitorUserStats         `json:"stats"`
	Series       map[string][]groupMonitorUserSeriesPoint `json:"series"`
	// AccountStates 分组内账号的最新检测状态序列（按分组内优先级排序，不含账号名）。
	AccountStates []groupMonitorUserAccountState `json:"account_states"`
	// Recent 最近 60 次检测记录（升序，状态条 PAST→NOW）。
	Recent []groupMonitorUserRecentRecord `json:"recent"`
}

type groupMonitorUserListResponse struct {
	Items []groupMonitorUserListItem `json:"items"`
	Total int64                      `json:"total"`
}

// historyWindows 需要聚合的时间窗（1h/1d/7d）。
var historyWindows = []struct {
	Key    string
	Since  func() time.Time
	Bucket int
}{
	{Key: "1h", Since: func() time.Time { return time.Now().Add(-time.Hour) }, Bucket: 12},
	{Key: "1d", Since: func() time.Time { return time.Now().Add(-24 * time.Hour) }, Bucket: 24},
	{Key: "7d", Since: func() time.Time { return time.Now().Add(-7 * 24 * time.Hour) }, Bucket: 28},
}

func round1(v float64) float64 {
	return float64(int64(v*10)) / 10
}

// List GET /api/v1/group-monitors
// 返回全部分组监控（含 1h/1d/7d 聚合指标与状态条序列），供用户端渠道监控页展示。
//
// 监控口径（用户期望）：每个分组的健康由两部分组成并合并到同一卡片：
//  1. 真实流量：该分组用户最近 60 次使用请求（成功 usage_logs + 失败 ops_error_logs）；
//  2. 主动探测：后台成功/失败探测记录（group_monitor_history）。
//
// 性能模型：全部走 (group_id, created_at) 索引的 LIMIT 查询，成本与分组数成正比、
// 与表大小无关；single-flight 防止 499 重试风暴叠加查询；独立 8s 上限保证必返回。
// window 参数（1h/1d/7d，默认 7d）决定统计窗。
func (h *GroupMonitorUserHandler) List(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Error(c, 401, "authentication required")
		return
	}
	window := c.Query("window")
	windowValid := false
	for _, w := range historyWindows {
		if w.Key == window {
			windowValid = true
			break
		}
	}
	if !windowValid {
		window = "7d"
	}

	cacheKey := strconv.FormatInt(subject.UserID, 10) + ":" + window

	// 缓存命中直接返回（30 秒 TTL，覆盖前端自动刷新周期），不再触碰 DB。
	if cached, ok := groupMonitorUserCacheGet(cacheKey); ok {
		response.Success(c, cached)
		return
	}

	// single-flight：并发请求等待同一个计算者，避免重试风暴叠加重查询。
	leader, waitCh := groupMonitorFlightEnter()
	if !leader {
		select {
		case <-waitCh:
			if cached, ok := groupMonitorUserCacheGet(cacheKey); ok {
				response.Success(c, cached)
				return
			}
		case <-time.After(9 * time.Second):
		}
	} else {
		defer groupMonitorFlightExit(waitCh)
	}

	// 独立于客户端生命周期的有界上下文：客户端 abort 不中断计算（结果入缓存供他人），
	// 且最坏 8s 必返回，杜绝 30s 挂起。
	qctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	monitors, total, err := h.groupMonitorService.ListVisibleToUser(qctx, subject.UserID, service.GroupMonitorListParams{
		Page:     1,
		PageSize: 500,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	groupIDs := make([]int64, 0, len(monitors))
	for _, m := range monitors {
		groupIDs = append(groupIDs, m.GroupID)
	}

	// 轻量批量查询（索引扫描，恒定成本）。
	eventsByGroup, _ := h.groupMonitorService.RecentUsageEvents(qctx, groupIDs, 60)
	statesByMonitor, _ := h.groupMonitorService.AccountStatesBatch(qctx)

	// 状态条（5 小时窗、60 桶）：真实请求为主 + 探测附加聚合。
	const barBuckets = 60
	barSince := time.Now().Add(-5 * time.Hour)
	usageEventsSince, _ := h.groupMonitorService.UsageEventsSince(qctx, groupIDs, barSince)
	monitorIDs := make([]int64, 0, len(monitors))
	for _, m := range monitors {
		monitorIDs = append(monitorIDs, m.ID)
	}
	probeRecordsSince, _ := h.groupMonitorService.ProbeRecordsSince(qctx, monitorIDs, barSince)

	// 可用率（uptime-kuma 式）：按时间窗精确 COUNT 全窗聚合，不截断最近 N 条。
	// 真实请求为主 + 探测附加。
	usageAgg7d, _ := h.groupMonitorService.UsageAggSince(qctx, groupIDs, time.Now().Add(-7*24*time.Hour))
	usageAgg1d, _ := h.groupMonitorService.UsageAggSince(qctx, groupIDs, time.Now().Add(-24*time.Hour))
	usageAgg1h, _ := h.groupMonitorService.UsageAggSince(qctx, groupIDs, time.Now().Add(-time.Hour))
	probeAgg7d, _ := h.groupMonitorService.ProbeAggSince(qctx, monitorIDs, time.Now().Add(-7*24*time.Hour))
	probeAgg1d, _ := h.groupMonitorService.ProbeAggSince(qctx, monitorIDs, time.Now().Add(-24*time.Hour))
	probeAgg1h, _ := h.groupMonitorService.ProbeAggSince(qctx, monitorIDs, time.Now().Add(-time.Hour))
	usageAggByWin := map[string]map[int64]*service.GroupWindowAgg{"1h": usageAgg1h, "1d": usageAgg1d, "7d": usageAgg7d}
	probeAggByWin := map[string]map[int64]*service.GroupWindowAgg{"1h": probeAgg1h, "1d": probeAgg1d, "7d": probeAgg7d}

	items := make([]groupMonitorUserListItem, 0, len(monitors))
	for _, m := range monitors {
		item := groupMonitorUserListItem{
			ID:           m.ID,
			GroupID:      m.GroupID,
			GroupName:    m.GroupName,
			Enabled:      m.Enabled,
			ModelID:      m.ModelID,
			AccountCount: m.AccountCount,
			HealthyCount: m.HealthyCount,
			FailedCount:  m.FailedCount,
			UnknownCount: m.UnknownCount,
			Stats:        map[string]groupMonitorUserStats{},
			Series:       map[string][]groupMonitorUserSeriesPoint{},
		}
		if m.LastRunAt != nil {
			s := m.LastRunAt.UTC().Format("2006-01-02T15:04:05Z")
			item.LastRunAt = &s
		}
		if m.NextRunAt != nil {
			s := m.NextRunAt.UTC().Format("2006-01-02T15:04:05Z")
			item.NextRunAt = &s
		}

		events := eventsByGroup[m.GroupID]
		for _, w := range historyWindows {
			since := w.Since()
			// 状态条用的 TTFT/缓存率仍从最近 60 条真实请求事件取（轻量）。
			var ttftSum, ttftN, inputSum, cacheSum int64
			for _, e := range events {
				if e.CreatedAt.Before(since) {
					continue
				}
				if e.Success {
					if e.TTFTMs > 0 {
						ttftSum += e.TTFTMs
						ttftN++
					}
					inputSum += e.InputTokens
					cacheSum += e.CacheRead
				}
			}
			// 可用率/样本数用精确全窗聚合（真实请求 + 探测附加）。
			ua := usageAggByWin[w.Key][m.GroupID]
			pa := probeAggByWin[w.Key][m.ID]
			var succ, failed int64
			if ua != nil {
				succ += ua.Success
				failed += ua.Failed
			}
			if pa != nil {
				succ += pa.Success
				failed += pa.Failed
			}
			us := groupMonitorUserStats{}
			if t := succ + failed; t > 0 {
				us.Availability = round1(float64(succ) / float64(t) * 100)
				us.Probes = int(t)
			}
			if ttftN > 0 {
				us.AvgTTFTMs = round1(float64(ttftSum) / float64(ttftN))
			}
			if ua != nil && ua.AvgLatencyMs > 0 {
				us.AvgLatencyMs = round1(ua.AvgLatencyMs)
			}
			if inputSum+cacheSum > 0 {
				us.CacheRate = round1(float64(cacheSum) / float64(inputSum+cacheSum) * 100)
			}
			item.Stats[w.Key] = us
		}

		// 状态条：5 小时窗 × 60 桶（每桶 5 分钟）。
		// 口径 = 用户真实请求为主（usage_logs 成功 + ops_error_logs 失败），
		// 我的探测记录作为附加聚合并入同一桶（不单独占格、不挤占真实请求）。
		bucketDur := time.Since(barSince) / barBuckets
		type bucketAgg struct {
			success, failed int64
			ttftSum, ttftN  int64
			cacheSum, inSum int64
		}
		buckets := make([]bucketAgg, barBuckets)
		bucketIdx := func(at time.Time) int {
			if at.Before(barSince) {
				return -1
			}
			idx := int(at.Sub(barSince) / bucketDur)
			if idx >= barBuckets {
				idx = barBuckets - 1
			}
			return idx
		}
		for _, e := range usageEventsSince[m.GroupID] {
			idx := bucketIdx(e.CreatedAt)
			if idx < 0 {
				continue
			}
			if e.Success {
				buckets[idx].success++
				if e.TTFTMs > 0 {
					buckets[idx].ttftSum += e.TTFTMs
					buckets[idx].ttftN++
				}
				buckets[idx].inSum += e.InputTokens
				buckets[idx].cacheSum += e.CacheRead
			} else {
				buckets[idx].failed++
			}
		}
		for _, p := range probeRecordsSince[m.ID] {
			idx := bucketIdx(p.CheckedAt)
			if idx < 0 {
				continue
			}
			if p.Status == "success" {
				buckets[idx].success++
				if p.LatencyMs > 0 {
					buckets[idx].ttftSum += p.LatencyMs
					buckets[idx].ttftN++
				}
			} else {
				buckets[idx].failed++
			}
		}
		recent := make([]groupMonitorUserRecentRecord, 0, barBuckets)
		for idx, b := range buckets {
			status := "idle"
			if b.success > 0 || b.failed > 0 {
				status = "success"
				if b.failed > b.success {
					status = "failed"
				}
			}
			rec := groupMonitorUserRecentRecord{
				Status:    status,
				CheckedAt: barSince.Add(bucketDur * time.Duration(idx)).UTC().Format("2006-01-02T15:04:05Z"),
				Success:   b.success,
				Failed:    b.failed,
			}
			if b.ttftN > 0 {
				rec.LatencyMs = b.ttftSum / b.ttftN
			}
			if b.inSum+b.cacheSum > 0 {
				rec.CacheRate = round1(float64(b.cacheSum) / float64(b.inSum+b.cacheSum) * 100)
			}
			recent = append(recent, rec)
		}
		item.Recent = recent

		if states, ok := statesByMonitor[m.ID]; ok {
			out := make([]groupMonitorUserAccountState, 0, len(states))
			for _, r := range states {
				out = append(out, groupMonitorUserAccountState{Status: r.Status, LatencyMs: r.LatencyMs})
			}
			item.AccountStates = out
		} else {
			item.AccountStates = []groupMonitorUserAccountState{}
		}
		items = append(items, item)
	}
	resp := groupMonitorUserListResponse{Items: items, Total: total}
	groupMonitorUserCacheSet(cacheKey, resp)
	response.Success(c, resp)
}

// ---- single-flight ----
var (
	flightMu sync.Mutex
	flightCh chan struct{}
)

func groupMonitorFlightEnter() (leader bool, wait chan struct{}) {
	flightMu.Lock()
	defer flightMu.Unlock()
	if flightCh != nil {
		return false, flightCh
	}
	flightCh = make(chan struct{})
	return true, flightCh
}

func groupMonitorFlightExit(ch chan struct{}) {
	flightMu.Lock()
	if flightCh == ch {
		flightCh = nil
	}
	flightMu.Unlock()
	close(ch)
}

type groupMonitorUserAccountStatus struct {
	AccountID    int64   `json:"account_id"`
	AccountName  string  `json:"account_name"`
	Platform     string  `json:"platform"`
	Status       string  `json:"status"`
	ModelID      string  `json:"model_id"`
	LatencyMs    int64   `json:"latency_ms"`
	ErrorMessage string  `json:"error_message"`
	CheckedAt    *string `json:"checked_at,omitempty"`
}

// GetResults GET /api/v1/group-monitors/:id/results
// 返回某分组监控下各账号的最新检测状态。（保留给管理端/详情使用；用户端主视图不消费账号明细）
func (h *GroupMonitorUserHandler) GetResults(c *gin.Context) {
	subject, authenticated := middleware2.GetAuthSubjectFromContext(c)
	if !authenticated || subject.UserID <= 0 {
		response.Error(c, 401, "authentication required")
		return
	}
	id, ok := parseGroupMonitorUserID(c)
	if !ok {
		return
	}
	if _, err := h.groupMonitorService.GetVisibleToUser(c.Request.Context(), subject.UserID, id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	results, err := h.groupMonitorService.ListResults(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := make([]groupMonitorUserAccountStatus, 0, len(results))
	for _, r := range results {
		s := groupMonitorUserAccountStatus{
			AccountID:    r.AccountID,
			AccountName:  r.AccountName,
			Platform:     r.Platform,
			Status:       r.Status,
			ModelID:      r.ModelID,
			LatencyMs:    r.LatencyMs,
			ErrorMessage: r.ErrorMessage,
		}
		checkedAt := r.CheckedAt.UTC().Format("2006-01-02T15:04:05Z")
		s.CheckedAt = &checkedAt
		items = append(items, s)
	}
	response.Success(c, items)
}

// parseGroupMonitorUserID 解析分组监控 id；非法时返回 false 并已写响应。
func parseGroupMonitorUserID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.ErrorFrom(c, service.ErrGroupMonitorNotFound)
		return 0, false
	}
	return id, true
}
