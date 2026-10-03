package migration

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
)

var legacyTableColumns = map[string][]string{
	"admin_users":                      {"id", "email", "password_hash"},
	"connections":                      {"id", "name", "base_url", "admin_api_key", "enabled", "last_check_at"},
	"settings":                         {"key", "value"},
	"bl_source_bindings":               {"id", "connection_id", "target_type", "target_id", "source_site_id", "source_group_id"},
	"bl_group_rate_rules":              {"id", "connection_id", "group_id", "enabled", "mode", "offset"},
	"bl_account_rate_rules":            {"id", "connection_id", "account_id", "enabled", "mode", "offset"},
	"bl_collection_sites":              {"id", "connection_id", "name", "base_url", "site_type", "password_enc", "auth_mode", "recharge_ratio"},
	"bl_collection_runs":               {"id", "connection_id", "site_id", "status"},
	"bl_collected_group_rates":         {"id", "connection_id", "site_id", "run_id", "group_id", "effective_rate"},
	"bl_collected_model_prices":        {"id", "connection_id", "site_id", "run_id", "model_name"},
	"bl_collected_changes":             {"id", "connection_id", "site_id", "run_id", "entity_type", "entity_key", "field"},
	"announcement_rules":               {"id", "connection_id", "name", "enabled"},
	"upstream_monitor_rules":           {"id", "connection_id", "account_id", "enabled"},
	"upstream_monitor_results":         {"id", "rule_id", "connection_id", "account_id", "status"},
	"api_capability_probe_runs":        {"id", "connection_id", "account_id", "status"},
	"upstream_monitor_rate_exclusions": {"id", "connection_id", "account_id", "group_id", "source_site_id", "source_group_id"},
}

var canonicalImportTables = []string{
	"channels",
	"auth_sessions",
	"rate_snapshots",
	"rate_change_logs",
	"upstream_sync_targets",
	"upstream_sync_groups",
	"upstream_sync_accounts",
	"upstream_sync_managed_accounts",
}

type PreflightOptions struct {
	RequirePostgres bool
	Version         string
}

type PreflightReport struct {
	Dialect           string           `json:"dialect"`
	LegacyRowCounts   map[string]int64 `json:"legacy_row_counts"`
	CanonicalCounts   map[string]int64 `json:"canonical_row_counts"`
	ExistingState     string           `json:"existing_state,omitempty"`
	ExistingImports   int64            `json:"existing_imports"`
	SourceFingerprint string           `json:"source_fingerprint,omitempty"`
	ImportableCounts  map[string]int   `json:"importable_counts,omitempty"`
	SkippedCounts     map[string]int   `json:"skipped_counts,omitempty"`
}

func Preflight(ctx context.Context, db *gorm.DB, opts PreflightOptions) (*PreflightReport, error) {
	if db == nil {
		return nil, errors.New("migration preflight database is nil")
	}
	version := strings.TrimSpace(opts.Version)
	if version == "" {
		version = VersionV007LegacyImport
	}
	report := &PreflightReport{
		Dialect:         db.Dialector.Name(),
		LegacyRowCounts: make(map[string]int64, len(legacyTableColumns)),
		CanonicalCounts: make(map[string]int64, len(canonicalImportTables)),
	}
	if opts.RequirePostgres && report.Dialect != "postgres" {
		return report, fmt.Errorf("production migration requires postgres, got %s", report.Dialect)
	}

	db = db.WithContext(ctx)
	legacyTables := make([]string, 0, len(legacyTableColumns))
	for table := range legacyTableColumns {
		legacyTables = append(legacyTables, table)
	}
	sort.Strings(legacyTables)
	for _, table := range legacyTables {
		if !db.Migrator().HasTable(table) {
			return report, fmt.Errorf("legacy preflight: required table %s is missing", table)
		}
		for _, column := range legacyTableColumns[table] {
			if !db.Migrator().HasColumn(table, column) {
				return report, fmt.Errorf("legacy preflight: required column %s.%s is missing", table, column)
			}
		}
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil {
			return report, fmt.Errorf("legacy preflight: count %s: %w", table, err)
		}
		report.LegacyRowCounts[table] = count
	}

	if db.Migrator().HasTable((&SchemaMigration{}).TableName()) {
		var row SchemaMigration
		err := db.Where("version = ?", version).Take(&row).Error
		if err == nil {
			report.ExistingState = row.State
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return report, fmt.Errorf("legacy preflight: read migration state: %w", err)
		}
	}
	if db.Migrator().HasTable((&LegacyImportMap{}).TableName()) {
		if err := db.Model(&LegacyImportMap{}).
			Where("migration_version = ? AND rolled_back_at IS NULL", version).
			Count(&report.ExistingImports).Error; err != nil {
			return report, fmt.Errorf("legacy preflight: count import ledger: %w", err)
		}
	}

	for _, table := range canonicalImportTables {
		if !db.Migrator().HasTable(table) {
			continue
		}
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil {
			return report, fmt.Errorf("legacy preflight: count canonical table %s: %w", table, err)
		}
		report.CanonicalCounts[table] = count
		if count > 0 && report.ExistingImports == 0 {
			return report, fmt.Errorf("legacy preflight: canonical table %s has %d untracked rows", table, count)
		}
	}
	return report, nil
}

func EnsureMetadata(db *gorm.DB) error {
	if db == nil {
		return errors.New("migration metadata database is nil")
	}
	if err := db.AutoMigrate(&SchemaMigration{}, &LegacyImportMap{}); err != nil {
		return fmt.Errorf("migrate migration metadata: %w", err)
	}
	return nil
}
