package operations

import (
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

type extensionIntArray string

func (extensionIntArray) GormDataType() string { return "string" }

func (extensionIntArray) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Dialector.Name() {
	case "postgres":
		return "integer[]"
	case "mysql":
		return "json"
	default:
		return "text"
	}
}

type extensionJSON string

func (extensionJSON) GormDataType() string { return "string" }

func (extensionJSON) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Dialector.Name() {
	case "postgres":
		return "jsonb"
	case "mysql":
		return "json"
	default:
		return "text"
	}
}

type blSourceBindingSchema struct {
	ID              int64  `gorm:"type:integer;primaryKey;autoIncrement"`
	ConnectionID    int64  `gorm:"type:integer;not null;uniqueIndex:ux_bl_source_binding,priority:1;index:ix_bl_source_binding_target,priority:1"`
	TargetType      string `gorm:"not null;uniqueIndex:ux_bl_source_binding,priority:2;index:ix_bl_source_binding_target,priority:2"`
	TargetID        int64  `gorm:"type:integer;not null;uniqueIndex:ux_bl_source_binding,priority:3;index:ix_bl_source_binding_target,priority:3"`
	SourceSiteID    int64  `gorm:"type:integer;not null;uniqueIndex:ux_bl_source_binding,priority:4"`
	SourceSiteName  string `gorm:"not null"`
	SourceGroupID   string `gorm:"not null;uniqueIndex:ux_bl_source_binding,priority:5"`
	SourceGroupName *string
	SourcePlatform  *string
	CreatedAt       time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt       time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
}

func (blSourceBindingSchema) TableName() string { return "bl_source_bindings" }

type blGroupRateRuleSchema struct {
	ID           int64   `gorm:"type:integer;primaryKey;autoIncrement"`
	ConnectionID int64   `gorm:"type:integer;not null;uniqueIndex:ux_bl_group_rate_rule,priority:1;index:ix_bl_group_rate_rule,priority:1"`
	GroupID      int64   `gorm:"type:integer;not null;uniqueIndex:ux_bl_group_rate_rule,priority:2;index:ix_bl_group_rate_rule,priority:2"`
	Enabled      bool    `gorm:"not null;default:false"`
	Mode         string  `gorm:"not null;default:first"`
	Offset       float64 `gorm:"not null;default:0"`
	Expression   *string
	CreatedAt    time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt    time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
}

func (blGroupRateRuleSchema) TableName() string { return "bl_group_rate_rules" }

type blAccountRateRuleSchema struct {
	ID           int64   `gorm:"type:integer;primaryKey;autoIncrement"`
	ConnectionID int64   `gorm:"type:integer;not null;uniqueIndex:ux_bl_account_rate_rule,priority:1;index:ix_bl_account_rate_rule,priority:1"`
	AccountID    int64   `gorm:"type:integer;not null;uniqueIndex:ux_bl_account_rate_rule,priority:2;index:ix_bl_account_rate_rule,priority:2"`
	Enabled      bool    `gorm:"not null;default:false"`
	Mode         string  `gorm:"not null;default:first"`
	Offset       float64 `gorm:"not null;default:0"`
	Expression   *string
	CreatedAt    time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt    time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
}

func (blAccountRateRuleSchema) TableName() string { return "bl_account_rate_rules" }

type blCollectionSiteSchema struct {
	ID                  int64   `gorm:"type:integer;primaryKey;autoIncrement"`
	ConnectionID        int64   `gorm:"type:integer;not null;index:ix_bl_collection_site_enabled,priority:1;index:ix_bl_collection_site_type,priority:1"`
	Name                string  `gorm:"not null"`
	BaseURL             string  `gorm:"not null"`
	SiteType            string  `gorm:"not null;default:sub2api;index:ix_bl_collection_site_type,priority:2"`
	Email               string  `gorm:"not null;default:''"`
	PasswordEnc         string  `gorm:"not null;default:''"`
	AuthMode            string  `gorm:"not null;default:password"`
	Enabled             bool    `gorm:"not null;default:true;index:ix_bl_collection_site_enabled,priority:2"`
	IntervalMin         int     `gorm:"not null;default:60"`
	RechargeRatio       float64 `gorm:"not null;default:1"`
	AccessToken         *string
	RefreshToken        *string
	TokenExpire         *int64
	NewAPIUserID        *string `gorm:"column:new_api_user_id"`
	LastRunAt           *time.Time
	LastStatus          *string
	LastError           *string
	ConsecutiveFailures int `gorm:"not null;default:0"`
	LastSuccessAt       *time.Time
	CreatedAt           time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt           time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
}

func (blCollectionSiteSchema) TableName() string { return "bl_collection_sites" }

type blCollectionRunSchema struct {
	ID           int64  `gorm:"type:integer;primaryKey;autoIncrement;index:ix_bl_collection_run_connection,priority:4;index:ix_bl_collection_run_site,priority:3"`
	ConnectionID int64  `gorm:"type:integer;not null;index:ix_bl_collection_run_connection,priority:1"`
	SiteID       int64  `gorm:"type:integer;not null;index:ix_bl_collection_run_connection,priority:2;index:ix_bl_collection_run_site,priority:1"`
	Status       string `gorm:"not null;index:ix_bl_collection_run_connection,priority:3;index:ix_bl_collection_run_site,priority:2"`
	Error        *string
	GroupCount   int       `gorm:"not null;default:0"`
	ModelCount   int       `gorm:"not null;default:0"`
	ChangeCount  int       `gorm:"not null;default:0"`
	StartedAt    time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
	FinishedAt   *time.Time
}

func (blCollectionRunSchema) TableName() string { return "bl_collection_runs" }

type blCollectedGroupRateSchema struct {
	ID               int64  `gorm:"type:integer;primaryKey;autoIncrement;index:ix_bl_group_rate_lookup,priority:4"`
	ConnectionID     int64  `gorm:"type:integer;not null;index:ix_bl_group_rate_lookup,priority:1"`
	SiteID           int64  `gorm:"type:integer;not null;index:ix_bl_group_rate_lookup,priority:2"`
	RunID            int64  `gorm:"type:integer;not null;index"`
	GroupID          string `gorm:"not null;index:ix_bl_group_rate_lookup,priority:3"`
	Name             string `gorm:"not null"`
	Platform         *string
	SubscriptionType *string
	IsExclusive      bool `gorm:"not null;default:false"`
	RateMultiplier   *float64
	UserRate         *float64
	EffectiveRate    *float64
	Raw              *string
	CollectedAt      time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
}

func (blCollectedGroupRateSchema) TableName() string { return "bl_collected_group_rates" }

type blCollectedModelPriceSchema struct {
	ID               int64   `gorm:"type:integer;primaryKey;autoIncrement;index:ix_bl_model_price_lookup,priority:6"`
	ConnectionID     int64   `gorm:"type:integer;not null;index:ix_bl_model_price_lookup,priority:1"`
	SiteID           int64   `gorm:"type:integer;not null;index:ix_bl_model_price_lookup,priority:2"`
	RunID            int64   `gorm:"type:integer;not null;index"`
	ChannelName      *string `gorm:"index:ix_bl_model_price_lookup,priority:3"`
	Platform         *string `gorm:"index:ix_bl_model_price_lookup,priority:4"`
	ModelName        string  `gorm:"not null;index:ix_bl_model_price_lookup,priority:5"`
	BillingMode      *string
	InputPrice       *float64
	OutputPrice      *float64
	CacheWritePrice  *float64
	CacheReadPrice   *float64
	ImageOutputPrice *float64
	PerRequestPrice  *float64
	ModelRatio       *float64
	CompletionRatio  *float64
	ModelPrice       *float64
	QuotaType        *string
	VendorName       *string
	Description      *string
	Raw              *string
	CollectedAt      time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
}

func (blCollectedModelPriceSchema) TableName() string { return "bl_collected_model_prices" }

type blCollectedChangeSchema struct {
	ID           int64  `gorm:"type:integer;primaryKey;autoIncrement"`
	ConnectionID int64  `gorm:"type:integer;not null;index:ix_bl_collected_change,priority:1"`
	SiteID       int64  `gorm:"type:integer;not null;index:ix_bl_collected_change,priority:2"`
	RunID        int64  `gorm:"type:integer;not null;index"`
	EntityType   string `gorm:"not null;index:ix_bl_collected_change,priority:3"`
	EntityKey    string `gorm:"not null"`
	Field        string `gorm:"not null;index:ix_bl_collected_change,priority:4"`
	OldValue     *string
	NewValue     *string
	ChangeType   string `gorm:"not null"`
	Raw          *string
	CreatedAt    time.Time `gorm:"not null;default:CURRENT_TIMESTAMP;index:ix_bl_collected_change,priority:5"`
}

func (blCollectedChangeSchema) TableName() string { return "bl_collected_changes" }

type announcementRuleSchema struct {
	ID              int64  `gorm:"type:integer;primaryKey;autoIncrement"`
	ConnectionID    int64  `gorm:"type:integer;not null;index:ix_announcement_rule_enabled,priority:1"`
	Name            string `gorm:"not null"`
	Enabled         bool   `gorm:"not null;default:true;index:ix_announcement_rule_enabled,priority:2"`
	TitleTemplate   string `gorm:"not null"`
	ContentTemplate string `gorm:"not null"`
	TargetGroupIDs  extensionIntArray
	Status          string    `gorm:"not null;default:active"`
	NotifyMode      string    `gorm:"not null;default:silent"`
	CreatedAt       time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt       time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
}

func (announcementRuleSchema) TableName() string { return "announcement_rules" }

type upstreamMonitorRuleSchema struct {
	ID                        int64 `gorm:"type:integer;primaryKey;autoIncrement"`
	ConnectionID              int64 `gorm:"type:integer;not null;uniqueIndex:ux_upstream_monitor_rule,priority:1;index:ix_upstream_monitor_due,priority:1;index:ix_upstream_monitor_account,priority:1;index:ix_upstream_monitor_channel,priority:1;index:ix_upstream_monitor_group,priority:1"`
	AccountID                 int64 `gorm:"type:integer;not null;uniqueIndex:ux_upstream_monitor_rule,priority:2;index:ix_upstream_monitor_account,priority:2"`
	AccountName               *string
	Enabled                   bool `gorm:"not null;default:true;index:ix_upstream_monitor_due,priority:2"`
	CheckIntervalMinutes      int  `gorm:"not null;default:10"`
	FailureThreshold          int  `gorm:"not null;default:3"`
	PauseMinutes              int  `gorm:"not null;default:30"`
	ModelID                   *string
	Prompt                    *string
	Sub2APIChannelMonitorID   *int64  `gorm:"column:sub2api_channel_monitor_id;type:integer;index:ix_upstream_monitor_channel,priority:2"`
	Sub2APIGroupID            *int64  `gorm:"column:sub2api_group_id;type:integer;index:ix_upstream_monitor_group,priority:2"`
	Sub2APIGroupName          *string `gorm:"column:sub2api_group_name"`
	NativeMonitorSuppressedAt *time.Time
	ConsecutiveFailures       int `gorm:"not null;default:0"`
	ConsecutiveLightFailures  int `gorm:"not null;default:0"`
	ConsecutiveHeavyFailures  int `gorm:"not null;default:0"`
	TotalChecks               int `gorm:"not null;default:0"`
	SuccessChecks             int `gorm:"not null;default:0"`
	LastStatus                *string
	LastMessage               *string
	LastLatencyMS             *int `gorm:"column:last_latency_ms"`
	LastHeavyCheckedAt        *time.Time
	LastCheckMode             *string
	LastFirstTokenMS          *int     `gorm:"column:last_first_token_ms"`
	LastStreamTPS             *float64 `gorm:"column:last_stream_tps"`
	LastCheckedAt             *time.Time
	NextCheckAt               *time.Time `gorm:"index:ix_upstream_monitor_due,priority:3"`
	PausedUntil               *time.Time
	PauseStartedAt            *time.Time
	CreatedAt                 time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt                 time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
}

func (upstreamMonitorRuleSchema) TableName() string { return "upstream_monitor_rules" }

type upstreamMonitorResultSchema struct {
	ID           int64  `gorm:"type:integer;primaryKey;autoIncrement"`
	RuleID       int64  `gorm:"type:integer;not null;index:ix_upstream_monitor_result_rule,priority:1"`
	ConnectionID int64  `gorm:"type:integer;not null;index:ix_upstream_monitor_result_account,priority:1"`
	AccountID    int64  `gorm:"type:integer;not null;index:ix_upstream_monitor_result_account,priority:2"`
	Status       string `gorm:"not null"`
	Message      *string
	LatencyMS    *int `gorm:"column:latency_ms"`
	Model        *string
	CheckMode    string    `gorm:"not null;default:heavy"`
	FirstTokenMS *int      `gorm:"column:first_token_ms"`
	StreamTPS    *float64  `gorm:"column:stream_tps"`
	StartedAt    time.Time `gorm:"not null"`
	FinishedAt   time.Time `gorm:"not null"`
	CreatedAt    time.Time `gorm:"not null;default:CURRENT_TIMESTAMP;index:ix_upstream_monitor_result_rule,priority:2;index:ix_upstream_monitor_result_account,priority:3"`
}

func (upstreamMonitorResultSchema) TableName() string { return "upstream_monitor_results" }

type apiCapabilityProbeRunSchema struct {
	ID           int64         `gorm:"type:integer;primaryKey;autoIncrement"`
	ConnectionID int64         `gorm:"type:integer;not null;index:ix_api_probe_monitor,priority:1;index:ix_api_probe_account,priority:1"`
	AccountID    int64         `gorm:"type:integer;not null;index:ix_api_probe_account,priority:2"`
	MonitorID    *int64        `gorm:"type:integer;index:ix_api_probe_monitor,priority:2"`
	Provider     string        `gorm:"not null"`
	Model        string        `gorm:"not null"`
	Status       string        `gorm:"not null"`
	Results      extensionJSON `gorm:"not null"`
	StartedAt    time.Time     `gorm:"not null"`
	FinishedAt   time.Time     `gorm:"not null"`
	CreatedAt    time.Time     `gorm:"not null;default:CURRENT_TIMESTAMP;index:ix_api_probe_monitor,priority:3;index:ix_api_probe_account,priority:3"`
}

func (apiCapabilityProbeRunSchema) TableName() string { return "api_capability_probe_runs" }

type upstreamMonitorRateExclusionSchema struct {
	ID              int64 `gorm:"type:integer;primaryKey;autoIncrement"`
	ConnectionID    int64 `gorm:"type:integer;not null;uniqueIndex:ux_upstream_monitor_rate_exclusion,priority:1;index:ix_rate_exclusion_group,priority:1;index:ix_rate_exclusion_account,priority:1;index:ix_rate_exclusion_source,priority:1"`
	AccountID       int64 `gorm:"type:integer;not null;uniqueIndex:ux_upstream_monitor_rate_exclusion,priority:2;index:ix_rate_exclusion_account,priority:2"`
	AccountName     *string
	GroupID         int64  `gorm:"type:integer;not null;uniqueIndex:ux_upstream_monitor_rate_exclusion,priority:3;index:ix_rate_exclusion_group,priority:3"`
	SourceSiteID    int64  `gorm:"type:integer;not null;uniqueIndex:ux_upstream_monitor_rate_exclusion,priority:4;index:ix_rate_exclusion_source,priority:2"`
	SourceSiteName  string `gorm:"not null"`
	SourceGroupID   string `gorm:"not null;uniqueIndex:ux_upstream_monitor_rate_exclusion,priority:5;index:ix_rate_exclusion_source,priority:3"`
	SourceGroupName *string
	SourcePlatform  *string
	Reason          *string
	Active          bool      `gorm:"not null;default:true;index:ix_rate_exclusion_group,priority:2;index:ix_rate_exclusion_account,priority:3;index:ix_rate_exclusion_source,priority:4"`
	PausedAt        time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
	RestoredAt      *time.Time
	CreatedAt       time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt       time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
}

func (upstreamMonitorRateExclusionSchema) TableName() string {
	return "upstream_monitor_rate_exclusions"
}

type extensionSchemaContract struct {
	table   string
	model   any
	columns []string
}

var extensionSchemaContracts = []extensionSchemaContract{
	{table: "settings", model: &settingRow{}, columns: []string{"key", "value", "updated_at"}},
	{table: "bl_source_bindings", model: &blSourceBindingSchema{}, columns: []string{"id", "connection_id", "target_type", "target_id", "source_site_id", "source_site_name", "source_group_id", "source_group_name", "source_platform", "created_at", "updated_at"}},
	{table: "bl_group_rate_rules", model: &blGroupRateRuleSchema{}, columns: []string{"id", "connection_id", "group_id", "enabled", "mode", "offset", "expression", "created_at", "updated_at"}},
	{table: "bl_account_rate_rules", model: &blAccountRateRuleSchema{}, columns: []string{"id", "connection_id", "account_id", "enabled", "mode", "offset", "expression", "created_at", "updated_at"}},
	{table: "bl_collection_sites", model: &blCollectionSiteSchema{}, columns: []string{"id", "connection_id", "name", "base_url", "site_type", "email", "password_enc", "auth_mode", "enabled", "interval_min", "recharge_ratio", "access_token", "refresh_token", "token_expire", "new_api_user_id", "last_run_at", "last_status", "last_error", "consecutive_failures", "last_success_at", "created_at", "updated_at"}},
	{table: "bl_collection_runs", model: &blCollectionRunSchema{}, columns: []string{"id", "connection_id", "site_id", "status", "error", "group_count", "model_count", "change_count", "started_at", "finished_at"}},
	{table: "bl_collected_group_rates", model: &blCollectedGroupRateSchema{}, columns: []string{"id", "connection_id", "site_id", "run_id", "group_id", "name", "platform", "subscription_type", "is_exclusive", "rate_multiplier", "user_rate", "effective_rate", "raw", "collected_at"}},
	{table: "bl_collected_model_prices", model: &blCollectedModelPriceSchema{}, columns: []string{"id", "connection_id", "site_id", "run_id", "channel_name", "platform", "model_name", "billing_mode", "input_price", "output_price", "cache_write_price", "cache_read_price", "image_output_price", "per_request_price", "model_ratio", "completion_ratio", "model_price", "quota_type", "vendor_name", "description", "raw", "collected_at"}},
	{table: "bl_collected_changes", model: &blCollectedChangeSchema{}, columns: []string{"id", "connection_id", "site_id", "run_id", "entity_type", "entity_key", "field", "old_value", "new_value", "change_type", "raw", "created_at"}},
	{table: "announcement_rules", model: &announcementRuleSchema{}, columns: []string{"id", "connection_id", "name", "enabled", "title_template", "content_template", "target_group_ids", "status", "notify_mode", "created_at", "updated_at"}},
	{table: "upstream_monitor_rules", model: &upstreamMonitorRuleSchema{}, columns: []string{"id", "connection_id", "account_id", "account_name", "enabled", "check_interval_minutes", "failure_threshold", "pause_minutes", "model_id", "prompt", "sub2api_channel_monitor_id", "sub2api_group_id", "sub2api_group_name", "native_monitor_suppressed_at", "consecutive_failures", "consecutive_light_failures", "consecutive_heavy_failures", "total_checks", "success_checks", "last_status", "last_message", "last_latency_ms", "last_heavy_checked_at", "last_check_mode", "last_first_token_ms", "last_stream_tps", "last_checked_at", "next_check_at", "paused_until", "pause_started_at", "created_at", "updated_at"}},
	{table: "upstream_monitor_results", model: &upstreamMonitorResultSchema{}, columns: []string{"id", "rule_id", "connection_id", "account_id", "status", "message", "latency_ms", "model", "check_mode", "first_token_ms", "stream_tps", "started_at", "finished_at", "created_at"}},
	{table: "api_capability_probe_runs", model: &apiCapabilityProbeRunSchema{}, columns: []string{"id", "connection_id", "account_id", "monitor_id", "provider", "model", "status", "results", "started_at", "finished_at", "created_at"}},
	{table: "upstream_monitor_rate_exclusions", model: &upstreamMonitorRateExclusionSchema{}, columns: []string{"id", "connection_id", "account_id", "account_name", "group_id", "source_site_id", "source_site_name", "source_group_id", "source_group_name", "source_platform", "reason", "active", "paused_at", "restored_at", "created_at", "updated_at"}},
	{table: "operations_action_logs", model: &ActionLog{}, columns: []string{"id", "action", "target", "success", "message", "created_at"}},
}

type extensionForeignKey struct {
	table      string
	column     string
	references string
	refColumn  string
}

var extensionForeignKeys = []extensionForeignKey{
	{table: "bl_source_bindings", column: "connection_id", references: "connections", refColumn: "id"},
	{table: "bl_group_rate_rules", column: "connection_id", references: "connections", refColumn: "id"},
	{table: "bl_account_rate_rules", column: "connection_id", references: "connections", refColumn: "id"},
	{table: "bl_collection_sites", column: "connection_id", references: "connections", refColumn: "id"},
	{table: "bl_collection_runs", column: "connection_id", references: "connections", refColumn: "id"},
	{table: "bl_collection_runs", column: "site_id", references: "bl_collection_sites", refColumn: "id"},
	{table: "bl_collected_group_rates", column: "connection_id", references: "connections", refColumn: "id"},
	{table: "bl_collected_group_rates", column: "site_id", references: "bl_collection_sites", refColumn: "id"},
	{table: "bl_collected_group_rates", column: "run_id", references: "bl_collection_runs", refColumn: "id"},
	{table: "bl_collected_model_prices", column: "connection_id", references: "connections", refColumn: "id"},
	{table: "bl_collected_model_prices", column: "site_id", references: "bl_collection_sites", refColumn: "id"},
	{table: "bl_collected_model_prices", column: "run_id", references: "bl_collection_runs", refColumn: "id"},
	{table: "bl_collected_changes", column: "connection_id", references: "connections", refColumn: "id"},
	{table: "bl_collected_changes", column: "site_id", references: "bl_collection_sites", refColumn: "id"},
	{table: "bl_collected_changes", column: "run_id", references: "bl_collection_runs", refColumn: "id"},
	{table: "announcement_rules", column: "connection_id", references: "connections", refColumn: "id"},
	{table: "upstream_monitor_rules", column: "connection_id", references: "connections", refColumn: "id"},
	{table: "upstream_monitor_results", column: "rule_id", references: "upstream_monitor_rules", refColumn: "id"},
	{table: "upstream_monitor_results", column: "connection_id", references: "connections", refColumn: "id"},
	{table: "api_capability_probe_runs", column: "connection_id", references: "connections", refColumn: "id"},
	{table: "upstream_monitor_rate_exclusions", column: "connection_id", references: "connections", refColumn: "id"},
}

func ensureExtensionSchema(db *gorm.DB) error {
	created := make(map[string]bool, len(extensionSchemaContracts))
	return db.Transaction(func(tx *gorm.DB) error {
		for _, contract := range extensionSchemaContracts {
			if tx.Migrator().HasTable(contract.table) {
				continue
			}
			if err := tx.AutoMigrate(contract.model); err != nil {
				return fmt.Errorf("create extension table %s: %w", contract.table, err)
			}
			created[contract.table] = true
		}
		if tx.Dialector.Name() == "postgres" {
			if created["announcement_rules"] {
				if err := tx.Exec("ALTER TABLE announcement_rules ALTER COLUMN target_group_ids SET DEFAULT '{}', ALTER COLUMN target_group_ids SET NOT NULL").Error; err != nil {
					return fmt.Errorf("set announcement target group defaults: %w", err)
				}
			}
			for _, foreignKey := range extensionForeignKeys {
				if !created[foreignKey.table] {
					continue
				}
				name := "fk_ops_" + foreignKey.table + "_" + foreignKey.column
				query := fmt.Sprintf(
					"ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s) ON DELETE CASCADE",
					foreignKey.table, name, foreignKey.column, foreignKey.references, foreignKey.refColumn,
				)
				if err := tx.Exec(query).Error; err != nil {
					return fmt.Errorf("create extension foreign key %s: %w", name, err)
				}
			}
		}
		for _, contract := range extensionSchemaContracts {
			for _, column := range contract.columns {
				if !tx.Migrator().HasColumn(contract.table, column) {
					return fmt.Errorf("extension table %s is missing required column %s", contract.table, column)
				}
			}
		}
		return nil
	})
}
