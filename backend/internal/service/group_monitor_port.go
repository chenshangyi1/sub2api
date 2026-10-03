package service

import (
	"context"
	"time"
)

// GroupMonitor 分组监控配置（service 层模型，不直接暴露 ent/DB 类型）。
type GroupMonitor struct {
	ID               int64
	GroupID          int64
	GroupName        string
	Enabled          bool
	UserVisible      bool
	UserVisibleSet   bool
	IsExclusive      bool
	SubscriptionType string
	IntervalMinutes  int
	ModelID          string
	AutoRecover      bool
	MaxOutputTokens  int
	LastRunAt        *time.Time
	NextRunAt        *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time

	// 聚合统计（列表时由 repository 一并填充，避免前端 N+1）。
	AccountCount int
	HealthyCount int
	FailedCount  int
	UnknownCount int
}

// GroupMonitorListParams 列表查询过滤参数。
type GroupMonitorListParams struct {
	Page     int
	PageSize int
	Enabled  *bool
	Search   string
}

// GroupMonitorAccount 分组下待检测的账号。
type GroupMonitorAccount struct {
	ID       int64
	Name     string
	Platform string
}

// GroupMonitorResult 单账号一次检测的结果（upsert 到结果表）。
type GroupMonitorResult struct {
	MonitorID    int64
	AccountID    int64
	Status       string
	ModelID      string
	LatencyMs    int64
	TTFTMs       int64
	InputTokens  int64
	CacheRead    int64
	CacheCreate  int64
	ErrorMessage string
	CheckedAt    time.Time
}

// GroupMonitorHistoryStats 分组监控在某个时间窗内的聚合指标。
type GroupMonitorHistoryStats struct {
	Probes       int     // 探测总次数（账号×轮）
	Successes    int     // 成功次数
	Availability float64 // 可用率 0-100
	AvgLatencyMs float64 // 平均总耗时（成功探测）
	AvgTTFTMs    float64 // 平均首字延迟（成功探测）
	CacheRate    float64 // 缓存命中率 0-100
}

// GroupMonitorSeriesPoint 历史状态条序列点（一个时间桶）。
type GroupMonitorSeriesPoint struct {
	Bucket       string // 桶起始时间 RFC3339
	Probes       int
	Successes    int
	Availability float64 // 0-100
	AvgTTFTMs    float64
	AvgLatencyMs float64
	CacheRate    float64 // 0-100
}

// GroupMonitorHistoryRecord 单条历史探测记录（用户端状态条数据源）。
type GroupMonitorHistoryRecord struct {
	Status    string
	LatencyMs int64
	CheckedAt time.Time
}

// GroupUsageStats 分组被动用量聚合（来自 usage_logs，成功请求）。
type GroupUsageStats struct {
	Requests  int64
	AvgTTFTMs float64
	CacheRate float64
}

// GroupPassiveStats 分组真实请求聚合（成功 usage_logs + 失败 ops_error_logs）。
type GroupPassiveStats struct {
	Success   int64
	Failed    int64
	AvgTTFTMs float64
	CacheRate float64
	// Buckets 按时间桶的成功/失败序列（状态条数据源），旧→新。
	Buckets []GroupPassiveBucket
}

// GroupPassiveBucket 单个时间桶的真实请求成功/失败与 TTFT。
type GroupPassiveBucket struct {
	Success   int64
	Failed    int64
	AvgTTFTMs float64
	CacheRate float64
	// BucketStart 桶起始时间（用于前端展示具体时间）。
	BucketStart time.Time
}

// GroupMonitorAccountStatus 分组监控下单个账号的最新状态（含账号元信息）。
type GroupMonitorAccountStatus struct {
	AccountID    int64     `json:"account_id"`
	AccountName  string    `json:"account_name"`
	Platform     string    `json:"platform"`
	Status       string    `json:"status"`
	ModelID      string    `json:"model_id"`
	LatencyMs    int64     `json:"latency_ms"`
	ErrorMessage string    `json:"error_message"`
	CheckedAt    time.Time `json:"checked_at"`
}

// GroupUsageEvent 单条真实请求事件（成功或失败），用于轻量监控聚合。
type GroupUsageEvent struct {
	GroupID     int64
	CreatedAt   time.Time
	Success     bool
	TTFTMs      int64
	InputTokens int64
	CacheRead   int64
}

// GroupProbeRecord 单条探测记录（状态条时间桶聚合用）。
type GroupProbeRecord struct {
	MonitorID int64
	CheckedAt time.Time
	Status    string
	LatencyMs int64
}

// GroupWindowAgg 分组在某时间窗内的精确聚合（uptime-kuma 式可用率数据源）。
// 直接 COUNT 全窗数据，不做最近 N 条截断，保证可用率真实。
type GroupWindowAgg struct {
	Success      int64
	Failed       int64
	AvgTTFTMs    float64
	AvgLatencyMs float64
	CacheRate    float64
}

// GroupMonitorRepository 分组监控数据访问接口。
type GroupMonitorRepository interface {
	Create(ctx context.Context, m *GroupMonitor) error
	GetByID(ctx context.Context, id int64) (*GroupMonitor, error)
	GetByGroupID(ctx context.Context, groupID int64) (*GroupMonitor, error)
	Update(ctx context.Context, m *GroupMonitor) error
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context, params GroupMonitorListParams) ([]*GroupMonitor, int64, error)
	ListDue(ctx context.Context, now time.Time) ([]*GroupMonitor, error)
	UpdateAfterRun(ctx context.Context, id int64, lastRunAt, nextRunAt time.Time) error

	// ListGroupAccounts 枚举分组下所有启用账号（join account_groups）。
	ListGroupAccounts(ctx context.Context, groupID int64) ([]*GroupMonitorAccount, error)

	// UpsertResult 写入单个账号的最新检测状态。
	UpsertResult(ctx context.Context, r *GroupMonitorResult) error
	// DeleteResultsForUnschedulableAccounts 清理手动暂停账号的旧检测结果。
	DeleteResultsForUnschedulableAccounts(ctx context.Context, monitorID int64) error
	// AppendHistory 追加单次探测的历史记录（支撑可用率/TTFT/缓存率聚合）。
	AppendHistory(ctx context.Context, r *GroupMonitorResult) error
	// QueryHistoryStats 统计某监控在指定时间窗内的聚合指标。
	QueryHistoryStats(ctx context.Context, monitorID int64, since time.Time) (*GroupMonitorHistoryStats, error)
	// QueryHistorySeries 返回某监控在时间窗内按桶聚合的序列（状态条）。
	QueryHistorySeries(ctx context.Context, monitorID int64, since time.Time, bucketCount int) ([]GroupMonitorSeriesPoint, error)
	// ListRecentRecords 返回某监控最近 limit 条探测记录（按时间升序，聚合到分钟）。
	ListRecentRecords(ctx context.Context, monitorID int64, limit int) ([]GroupMonitorHistoryRecord, error)
	// PruneHistory 删除早于 cutoff 的历史记录，返回删除行数。
	PruneHistory(ctx context.Context, cutoff time.Time) (int64, error)
	// QueryHistoryStatsBatch 一次性聚合所有监控在时间窗内的统计。
	QueryHistoryStatsBatch(ctx context.Context, since time.Time) (map[int64]*GroupMonitorHistoryStats, error)
	// ListRecentRecordsBatch 一次性返回所有监控的最近 limit 条记录。
	ListRecentRecordsBatch(ctx context.Context, limitPerMonitor int) (map[int64][]GroupMonitorHistoryRecord, error)
	// ListAccountStatesBatch 一次性返回所有监控的账号最新状态。
	ListAccountStatesBatch(ctx context.Context) (map[int64][]*GroupMonitorAccountStatus, error)
	// QueryGroupUsageStatsBatch 按 group_id 聚合 usage_logs（被动用量：请求数/平均TTFT/缓存率）。
	QueryGroupUsageStatsBatch(ctx context.Context, since time.Time) (map[int64]*GroupUsageStats, error)
	// QueryGroupPassiveStatsBatch 按 group_id 聚合真实请求（成功+失败+TTFT+分桶序列）。
	QueryGroupPassiveStatsBatch(ctx context.Context, since time.Time, bucketCount int) (map[int64]*GroupPassiveStats, error)
	// GroupNamesBatch 按 group_id 批量返回分组名。
	GroupNamesBatch(ctx context.Context, groupIDs []int64) (map[int64]string, error)
	// RecentGroupModels 返回某分组最近成功请求的模型（按使用量降序，探测候选用）。
	RecentGroupModels(ctx context.Context, groupID int64, since time.Time, limit int) ([]string, error)
	// ListResults 返回某监控下所有账号的最新状态（含账号名/平台）。
	ListResults(ctx context.Context, monitorID int64) ([]*GroupMonitorAccountStatus, error)
	// ResetResults 清空某监控的全部结果（账号被移出分组时兜底）。
	ResetResults(ctx context.Context, monitorID int64) error
	// ListRecentUsageEvents 按 group_id 各取最近 limit 条真实请求事件
	// （成功 usage_logs + 失败 ops_error_logs 合并，索引扫描，成本恒定）。
	ListRecentUsageEvents(ctx context.Context, groupIDs []int64, limit int) (map[int64][]GroupUsageEvent, error)
	// ListUsageEventsSince 按 group_id 返回 since 之后的真实请求事件（时间桶聚合用）。
	ListUsageEventsSince(ctx context.Context, groupIDs []int64, since time.Time) (map[int64][]GroupUsageEvent, error)
	// ListProbeRecordsSince 按 monitor_id 返回 since 之后的探测记录（时间桶聚合用）。
	ListProbeRecordsSince(ctx context.Context, monitorIDs []int64, since time.Time) (map[int64][]GroupProbeRecord, error)
	// AggregateUsageSince 按 group_id 精确聚合 since 之后的真实请求（COUNT 全窗，不截断）。
	AggregateUsageSince(ctx context.Context, groupIDs []int64, since time.Time) (map[int64]*GroupWindowAgg, error)
	// AggregateProbesSince 按 monitor_id 精确聚合 since 之后的探测记录。
	AggregateProbesSince(ctx context.Context, monitorIDs []int64, since time.Time) (map[int64]*GroupWindowAgg, error)
}
