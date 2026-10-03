package operations

import "time"

type Status string

const (
	StatusHealthy Status = "healthy"
	StatusWarning Status = "warning"
	StatusError   Status = "error"
	StatusUnknown Status = "unknown"
)

type Account struct {
	ID                         int64      `json:"id"`
	Name                       string     `json:"name"`
	Platform                   string     `json:"platform,omitempty"`
	Type                       string     `json:"type,omitempty"`
	Status                     string     `json:"status"`
	Schedulable                bool       `json:"schedulable"`
	Concurrency                int        `json:"concurrency"`
	Priority                   int        `json:"priority"`
	LoadFactor                 *float64   `json:"load_factor,omitempty"`
	RateMultiplier             float64    `json:"rate_multiplier"`
	GroupNames                 []string   `json:"group_names"`
	HealthScore                *float64   `json:"health_score,omitempty"`
	HealthState                string     `json:"health_state,omitempty"`
	HealthWeight               int        `json:"health_weight,omitempty"`
	Balance                    *float64   `json:"balance,omitempty"`
	BalanceCurrency            string     `json:"balance_currency,omitempty"`
	BalanceThreshold           *float64   `json:"balance_threshold,omitempty"`
	ExpiresAt                  *time.Time `json:"expires_at,omitempty"`
	TemporaryUnavailable       bool       `json:"temporary_unavailable"`
	TemporaryUnavailableUntil  *time.Time `json:"temporary_unavailable_until,omitempty"`
	TemporaryUnavailableReason string     `json:"temporary_unavailable_reason,omitempty"`
	LastError                  string     `json:"last_error,omitempty"`
	UpdatedAt                  *time.Time `json:"updated_at,omitempty"`
}

type AccountSummary struct {
	Total                int `json:"total"`
	Schedulable          int `json:"schedulable"`
	Errors               int `json:"errors"`
	BalanceLow           int `json:"balance_low"`
	TemporaryUnavailable int `json:"temporary_unavailable"`
}

type AccountPage struct {
	Items    []Account      `json:"items"`
	Summary  AccountSummary `json:"summary"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
	Pages    int            `json:"pages"`
}

type AccountFilter struct {
	Page     int
	PageSize int
	Search   string
	Schedule string
}

type Probe struct {
	ID               int64      `json:"id"`
	Name             string     `json:"name"`
	AccountID        *int64     `json:"account_id,omitempty"`
	GroupName        string     `json:"group_name,omitempty"`
	Provider         string     `json:"provider"`
	Endpoint         string     `json:"endpoint"`
	Enabled          bool       `json:"enabled"`
	Mode             string     `json:"mode"`
	Status           Status     `json:"status"`
	Model            string     `json:"model,omitempty"`
	CandidateModels  []string   `json:"candidate_models"`
	LatencyMS        *int       `json:"latency_ms,omitempty"`
	FirstTokenMS     *int       `json:"first_token_ms,omitempty"`
	TokensPerSecond  *float64   `json:"tokens_per_second,omitempty"`
	Availability7D   *float64   `json:"availability_7d,omitempty"`
	CapabilityPassed int        `json:"capability_passed"`
	CapabilityTotal  int        `json:"capability_total"`
	LastCheckedAt    *time.Time `json:"last_checked_at,omitempty"`
	NextRunAt        *time.Time `json:"next_run_at,omitempty"`
	LastError        string     `json:"last_error,omitempty"`
}

type ProbeSummary struct {
	Total   int `json:"total"`
	Healthy int `json:"healthy"`
	Warning int `json:"warning"`
	Error   int `json:"error"`
}

type ProbePage struct {
	Items   []Probe      `json:"items"`
	Summary ProbeSummary `json:"summary"`
}

type ProbeBatchFilter struct {
	ProbeIDs   []int64
	AccountIDs []int64
}

type AnalyticsPeriod struct {
	Key                 string    `json:"key"`
	Label               string    `json:"label"`
	StartAt             time.Time `json:"start_at"`
	EndAt               time.Time `json:"end_at"`
	UserCost            float64   `json:"user_cost"`
	UpstreamCost        float64   `json:"upstream_cost"`
	AdministratorCost   float64   `json:"administrator_cost"`
	OperatingCost       float64   `json:"operating_cost"`
	Profit              float64   `json:"profit"`
	ProfitMargin        float64   `json:"profit_margin"`
	Requests            int64     `json:"requests"`
	ActiveUsers         int64     `json:"active_users"`
	StreamRequests      int64     `json:"stream_requests"`
	TotalTokens         int64     `json:"total_tokens"`
	InputTokens         int64     `json:"input_tokens"`
	OutputTokens        int64     `json:"output_tokens"`
	CacheReadTokens     int64     `json:"cache_read_tokens"`
	CacheCreationTokens int64     `json:"cache_creation_tokens"`
	CacheHitRate        float64   `json:"cache_hit_rate"`
	AverageFirstTokenMS float64   `json:"average_first_token_ms"`
	P95FirstTokenMS     float64   `json:"p95_first_token_ms"`
	SlowFirstTokenRate  float64   `json:"slow_first_token_rate"`
}

type AnalyticsDay struct {
	AnalyticsPeriod
	Date string `json:"date"`
}

type AnalyticsHeatmapCell struct {
	Date                string  `json:"date"`
	Hour                int     `json:"hour"`
	Requests            int64   `json:"requests"`
	Failures            int64   `json:"failures"`
	AverageFirstTokenMS float64 `json:"average_first_token_ms"`
}

type SlowRequest struct {
	ID           int64     `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	UserID       int64     `json:"user_id,omitempty"`
	UserName     string    `json:"user_name,omitempty"`
	AccountID    int64     `json:"account_id,omitempty"`
	Model        string    `json:"model"`
	Stream       bool      `json:"stream"`
	DurationMS   int       `json:"duration_ms"`
	FirstTokenMS *int      `json:"first_token_ms,omitempty"`
	StatusCode   int       `json:"status_code"`
	Error        string    `json:"error,omitempty"`
}

type Analytics struct {
	Range        string                 `json:"range"`
	Summary      AnalyticsPeriod        `json:"summary"`
	Daily        []AnalyticsDay         `json:"daily"`
	Heatmap      []AnalyticsHeatmapCell `json:"heatmap"`
	SlowRequests []SlowRequest          `json:"slow_requests"`
}

type TargetSettings struct {
	AccountBalanceAlertEnabled     bool    `json:"account_balance_alert_enabled"`
	AccountBalanceDefaultThreshold float64 `json:"account_balance_default_threshold"`
	AccountBalanceCooldownMinutes  int     `json:"account_balance_cooldown_minutes"`
	AccountBalanceWebhookURL       string  `json:"account_balance_webhook_url"`
	AccountBalanceWebhookTemplate  string  `json:"account_balance_webhook_template"`
	SuppressNativeMonitors         bool    `json:"suppress_native_monitors"`
}

type OperationsSettings struct {
	HeavyProbeIntervalMinutes int `json:"heavy_probe_interval_minutes"`
}

type AccountPoolPolicy struct {
	HealthReturnEnabled       bool `json:"health_return_enabled"`
	HealthReturnThreshold     int  `json:"health_return_threshold"`
	SmartExpansionEnabled     bool `json:"smart_expansion_enabled"`
	TotalConcurrency          int  `json:"total_concurrency"`
	MinAccountConcurrency     int  `json:"min_account_concurrency"`
	MaxAccountConcurrency     int  `json:"max_account_concurrency"`
	ExpansionLoadThresholdPct int  `json:"expansion_load_threshold_pct"`
	LoadFactorEnabled         bool `json:"load_factor_enabled"`
	TotalLoadFactor           int  `json:"total_load_factor"`
	MinAccountLoadFactor      int  `json:"min_account_load_factor"`
	MaxAccountLoadFactor      int  `json:"max_account_load_factor"`
	PriceProtectionEnabled    bool `json:"price_protection_enabled"`
	FailureDisableEnabled     bool `json:"failure_disable_enabled"`
	FailureWindow             int  `json:"failure_window"`
	FailureCount              int  `json:"failure_count"`
	SlowWindow                int  `json:"slow_window"`
	SlowFirstTokenMS          int  `json:"slow_first_token_ms"`
	SlowCount                 int  `json:"slow_count"`
	MinAvailableAccounts      int  `json:"min_available_accounts"`
	TargetHealthyAccounts     int  `json:"target_healthy_accounts"`
}

type AccountPriorityPolicy struct {
	Enabled                bool    `json:"enabled"`
	Strategy               string  `json:"strategy"`
	TargetGroupIDs         []int64 `json:"target_group_ids"`
	SampleSize             int     `json:"sample_size"`
	LookbackMinutes        int     `json:"lookback_minutes"`
	FirstTokenCoefficient  float64 `json:"first_token_coefficient"`
	RateCoefficient        float64 `json:"rate_coefficient"`
	MissingSamplePenaltyMS int     `json:"missing_sample_penalty_ms"`
}

type AutomationSettings struct {
	AccountPool      AccountPoolPolicy     `json:"account_pool"`
	AccountPriority  AccountPriorityPolicy `json:"account_priority"`
	LastAppliedAt    *time.Time            `json:"last_applied_at,omitempty"`
	LastApplyStatus  string                `json:"last_apply_status,omitempty"`
	LastApplyMessage string                `json:"last_apply_message,omitempty"`
}

type AnnouncementRule struct {
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	Enabled         bool       `json:"enabled"`
	TitleTemplate   string     `json:"title_template"`
	ContentTemplate string     `json:"content_template"`
	TargetGroupIDs  []int64    `json:"target_group_ids"`
	Status          string     `json:"status"`
	NotifyMode      string     `json:"notify_mode"`
	LastPublishedAt *time.Time `json:"last_published_at,omitempty"`
}

type ServiceStatus struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Status       Status     `json:"status"`
	Detail       string     `json:"detail,omitempty"`
	CheckedAt    *time.Time `json:"checked_at,omitempty"`
	RestartCount *int       `json:"restart_count,omitempty"`
}

type TaskStatus struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Message    string     `json:"message,omitempty"`
}

type DiagnosticLog struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Action    string    `json:"action"`
	Target    string    `json:"target,omitempty"`
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
}

type InvalidData struct {
	Bindings        int64 `json:"bindings"`
	ManagedAccounts int64 `json:"managed_accounts"`
	ProbeRules      int64 `json:"probe_rules"`
}

type Diagnostics struct {
	Services    []ServiceStatus    `json:"services"`
	Connections []ConnectionStatus `json:"connections"`
	Worker      struct {
		Status         Status     `json:"status"`
		HeartbeatAt    *time.Time `json:"heartbeat_at,omitempty"`
		LastRunAt      *time.Time `json:"last_run_at,omitempty"`
		LastRunStatus  string     `json:"last_run_status,omitempty"`
		LastRunMessage string     `json:"last_run_message,omitempty"`
	} `json:"worker"`
	Tasks       []TaskStatus    `json:"tasks"`
	RecentLogs  []DiagnosticLog `json:"recent_logs"`
	InvalidData InvalidData     `json:"invalid_data"`
}

// ConnectionStatus 面板与 sub2api 各接入面的自检结果。
type ConnectionStatus struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type ActionLog struct {
	ID        uint      `gorm:"primaryKey"`
	Action    string    `gorm:"size:128;not null;index"`
	Target    string    `gorm:"size:256;not null;index"`
	Success   bool      `gorm:"not null;index"`
	Message   string    `gorm:"type:text"`
	CreatedAt time.Time `gorm:"not null;index"`
}

func (ActionLog) TableName() string { return "operations_action_logs" }
