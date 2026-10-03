package migration

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type legacyAdminUser struct {
	ID           int64  `gorm:"column:id"`
	Email        string `gorm:"column:email"`
	PasswordHash string `gorm:"column:password_hash"`
}

func (legacyAdminUser) TableName() string { return "admin_users" }

type legacyConnection struct {
	ID          uint       `gorm:"column:id"`
	Name        string     `gorm:"column:name"`
	BaseURL     string     `gorm:"column:base_url"`
	AdminAPIKey string     `gorm:"column:admin_api_key"`
	Enabled     bool       `gorm:"column:enabled"`
	SyncMode    string     `gorm:"column:sync_mode"`
	LastCheckAt *time.Time `gorm:"column:last_check_at"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
}

func (legacyConnection) TableName() string { return "connections" }

type legacyCollectionSite struct {
	ID                  uint       `gorm:"column:id"`
	ConnectionID        uint       `gorm:"column:connection_id"`
	Name                string     `gorm:"column:name"`
	BaseURL             string     `gorm:"column:base_url"`
	SiteType            string     `gorm:"column:site_type"`
	Email               string     `gorm:"column:email"`
	PasswordEnc         string     `gorm:"column:password_enc"`
	AuthMode            string     `gorm:"column:auth_mode"`
	Enabled             bool       `gorm:"column:enabled"`
	IntervalMin         int        `gorm:"column:interval_min"`
	RechargeRatio       float64    `gorm:"column:recharge_ratio"`
	AccessToken         *string    `gorm:"column:access_token"`
	RefreshToken        *string    `gorm:"column:refresh_token"`
	TokenExpire         *int64     `gorm:"column:token_expire"`
	NewAPIUserID        *string    `gorm:"column:new_api_user_id"`
	LastRunAt           *time.Time `gorm:"column:last_run_at"`
	LastStatus          *string    `gorm:"column:last_status"`
	LastError           *string    `gorm:"column:last_error"`
	ConsecutiveFailures int        `gorm:"column:consecutive_failures"`
	LastSuccessAt       *time.Time `gorm:"column:last_success_at"`
	CreatedAt           time.Time  `gorm:"column:created_at"`
	UpdatedAt           time.Time  `gorm:"column:updated_at"`
}

func (legacyCollectionSite) TableName() string { return "bl_collection_sites" }

type legacyGroupRate struct {
	ID             uint      `gorm:"column:id"`
	ConnectionID   uint      `gorm:"column:connection_id"`
	SiteID         uint      `gorm:"column:site_id"`
	RunID          uint      `gorm:"column:run_id"`
	GroupID        string    `gorm:"column:group_id"`
	Name           string    `gorm:"column:name"`
	Platform       *string   `gorm:"column:platform"`
	RateMultiplier *float64  `gorm:"column:rate_multiplier"`
	UserRate       *float64  `gorm:"column:user_rate"`
	EffectiveRate  *float64  `gorm:"column:effective_rate"`
	CollectedAt    time.Time `gorm:"column:collected_at"`
}

func (legacyGroupRate) TableName() string { return "bl_collected_group_rates" }

type legacyRateChange struct {
	ID           uint      `gorm:"column:id"`
	ConnectionID uint      `gorm:"column:connection_id"`
	SiteID       uint      `gorm:"column:site_id"`
	RunID        uint      `gorm:"column:run_id"`
	EntityType   string    `gorm:"column:entity_type"`
	EntityKey    string    `gorm:"column:entity_key"`
	Field        string    `gorm:"column:field"`
	OldValue     *string   `gorm:"column:old_value"`
	NewValue     *string   `gorm:"column:new_value"`
	ChangeType   string    `gorm:"column:change_type"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

func (legacyRateChange) TableName() string { return "bl_collected_changes" }

type legacySourceBinding struct {
	ID              uint      `gorm:"column:id"`
	ConnectionID    uint      `gorm:"column:connection_id"`
	TargetType      string    `gorm:"column:target_type"`
	TargetID        int64     `gorm:"column:target_id"`
	SourceSiteID    uint      `gorm:"column:source_site_id"`
	SourceSiteName  string    `gorm:"column:source_site_name"`
	SourceGroupID   string    `gorm:"column:source_group_id"`
	SourceGroupName *string   `gorm:"column:source_group_name"`
	SourcePlatform  *string   `gorm:"column:source_platform"`
	CreatedAt       time.Time `gorm:"column:created_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`
}

func (legacySourceBinding) TableName() string { return "bl_source_bindings" }

type legacySnapshot struct {
	Admins      []legacyAdminUser
	Connections []legacyConnection
	Sites       []legacyCollectionSite
	Rates       []legacyGroupRate
	Changes     []legacyRateChange
	Bindings    []legacySourceBinding
}

func loadLegacySnapshot(ctx context.Context, db *gorm.DB) (*legacySnapshot, error) {
	if db == nil {
		return nil, errors.New("load legacy snapshot database is nil")
	}
	db = db.WithContext(ctx)
	snapshot := &legacySnapshot{}
	queries := []struct {
		name string
		run  func() error
	}{
		{"admin users", func() error { return db.Order("id ASC").Find(&snapshot.Admins).Error }},
		{"connections", func() error { return db.Order("id ASC").Find(&snapshot.Connections).Error }},
		{"collection sites", func() error { return db.Order("id ASC").Find(&snapshot.Sites).Error }},
		{"source bindings", func() error { return db.Order("id ASC").Find(&snapshot.Bindings).Error }},
		{"rate changes", func() error {
			return db.Where("entity_type = ? AND field = ?", "group", "rateMultiplier").
				Order("id ASC").Find(&snapshot.Changes).Error
		}},
	}
	for _, query := range queries {
		if err := query.run(); err != nil {
			return nil, fmt.Errorf("load legacy %s: %w", query.name, err)
		}
	}

	var latestRuns []struct {
		ConnectionID uint `gorm:"column:connection_id"`
		SiteID       uint `gorm:"column:site_id"`
		RunID        uint `gorm:"column:run_id"`
	}
	if err := db.Table("bl_collection_runs").
		Select("connection_id, site_id, MAX(id) AS run_id").
		Where("status = ?", "success").
		Group("connection_id, site_id").
		Order("connection_id ASC, site_id ASC").Scan(&latestRuns).Error; err != nil {
		return nil, fmt.Errorf("load latest successful collection runs: %w", err)
	}
	runIDs := make([]uint, 0, len(latestRuns))
	for _, run := range latestRuns {
		runIDs = append(runIDs, run.RunID)
	}
	if len(runIDs) > 0 {
		var rates []legacyGroupRate
		if err := db.Where("run_id IN ?", runIDs).Order("site_id ASC, group_id ASC, id ASC").Find(&rates).Error; err != nil {
			return nil, fmt.Errorf("load latest legacy rates: %w", err)
		}
		bySource := make(map[string]legacyGroupRate, len(rates))
		for _, rate := range rates {
			key := sourceRateKey(rate.SiteID, rate.GroupID)
			if current, ok := bySource[key]; !ok || rate.ID > current.ID {
				bySource[key] = rate
			}
		}
		snapshot.Rates = make([]legacyGroupRate, 0, len(bySource))
		for _, rate := range bySource {
			snapshot.Rates = append(snapshot.Rates, rate)
		}
		sort.Slice(snapshot.Rates, func(i, j int) bool {
			if snapshot.Rates[i].SiteID == snapshot.Rates[j].SiteID {
				if snapshot.Rates[i].GroupID == snapshot.Rates[j].GroupID {
					return snapshot.Rates[i].ID < snapshot.Rates[j].ID
				}
				return snapshot.Rates[i].GroupID < snapshot.Rates[j].GroupID
			}
			return snapshot.Rates[i].SiteID < snapshot.Rates[j].SiteID
		})
	}
	return snapshot, nil
}

func (s *legacySnapshot) validate(legacyCipher *LegacyCipher) error {
	if legacyCipher == nil {
		return errors.New("legacy encryption key is required")
	}
	connections := make(map[uint]legacyConnection, len(s.Connections))
	connectionNames := make(map[string]uint, len(s.Connections))
	for _, connection := range s.Connections {
		if connection.ID == 0 || strings.TrimSpace(connection.Name) == "" || strings.TrimSpace(connection.BaseURL) == "" {
			return fmt.Errorf("legacy connection %d has incomplete identity", connection.ID)
		}
		plainAPIKey, err := legacyCipher.Decrypt(connection.AdminAPIKey)
		if err != nil {
			return fmt.Errorf("legacy connection %d admin key is not decryptable", connection.ID)
		}
		if strings.TrimSpace(plainAPIKey) == "" {
			return fmt.Errorf("legacy connection %d has an empty admin key", connection.ID)
		}
		nameKey := strings.ToLower(strings.TrimSpace(connection.Name))
		if other, exists := connectionNames[nameKey]; exists {
			return fmt.Errorf("legacy connections %d and %d have duplicate canonical name", other, connection.ID)
		}
		connectionNames[nameKey] = connection.ID
		connections[connection.ID] = connection
	}
	for _, admin := range s.Admins {
		if admin.ID <= 0 || strings.TrimSpace(admin.Email) == "" {
			return fmt.Errorf("legacy administrator %d has incomplete identity", admin.ID)
		}
		if _, err := bcrypt.Cost([]byte(admin.PasswordHash)); err != nil {
			return fmt.Errorf("legacy administrator %d has an invalid bcrypt hash", admin.ID)
		}
	}

	sites := make(map[uint]legacyCollectionSite, len(s.Sites))
	siteNames := make(map[string]uint, len(s.Sites))
	for _, site := range s.Sites {
		if site.ID == 0 || strings.TrimSpace(site.Name) == "" || strings.TrimSpace(site.BaseURL) == "" {
			return fmt.Errorf("legacy collection site %d has incomplete identity", site.ID)
		}
		if _, exists := connections[site.ConnectionID]; !exists {
			return fmt.Errorf("legacy collection site %d references missing connection %d", site.ID, site.ConnectionID)
		}
		nameKey := strings.ToLower(strings.TrimSpace(site.Name))
		if other, exists := siteNames[nameKey]; exists {
			return fmt.Errorf("legacy collection sites %d and %d have duplicate canonical name", other, site.ID)
		}
		siteNames[nameKey] = site.ID
		siteType := strings.ToLower(strings.TrimSpace(site.SiteType))
		if siteType != "sub2api" && siteType != "new_api" {
			return fmt.Errorf("legacy collection site %d has unsupported type %q", site.ID, site.SiteType)
		}
		authMode := strings.ToLower(strings.TrimSpace(site.AuthMode))
		if authMode != "password" && authMode != "manual_token" {
			return fmt.Errorf("legacy collection site %d has unsupported auth mode %q", site.ID, site.AuthMode)
		}
		if !isPositiveFinite(site.RechargeRatio) {
			return fmt.Errorf("legacy collection site %d has invalid recharge ratio", site.ID)
		}
		if site.PasswordEnc != "" {
			if _, err := legacyCipher.Decrypt(site.PasswordEnc); err != nil {
				return fmt.Errorf("legacy collection site %d password is not decryptable", site.ID)
			}
		}
		if authMode == "manual_token" && site.Enabled && strings.TrimSpace(stringValue(site.AccessToken)) == "" {
			return fmt.Errorf("enabled manual-token collection site %d has no access token", site.ID)
		}
		sites[site.ID] = site
	}

	for _, rate := range s.Rates {
		site, exists := sites[rate.SiteID]
		if !exists || site.ConnectionID != rate.ConnectionID {
			return fmt.Errorf("legacy rate %d has an invalid site or connection reference", rate.ID)
		}
		value, ok := effectiveLegacyRate(rate)
		if ok && !isPositiveFinite(value/site.RechargeRatio) {
			return fmt.Errorf("legacy rate %d normalizes to an invalid value", rate.ID)
		}
	}
	for _, change := range s.Changes {
		site, exists := sites[change.SiteID]
		if !exists || site.ConnectionID != change.ConnectionID {
			return fmt.Errorf("legacy rate change %d has an invalid site or connection reference", change.ID)
		}
		if _, ok := parseLegacyRatio(change.NewValue, site.RechargeRatio); !ok {
			continue
		}
	}
	for _, binding := range s.Bindings {
		site, exists := sites[binding.SourceSiteID]
		if !exists || site.ConnectionID != binding.ConnectionID {
			return fmt.Errorf("legacy source binding %d has an invalid site or connection reference", binding.ID)
		}
		if binding.TargetID <= 0 || strings.TrimSpace(binding.SourceGroupID) == "" {
			return fmt.Errorf("legacy source binding %d has incomplete identity", binding.ID)
		}
		targetType := strings.ToLower(strings.TrimSpace(binding.TargetType))
		if targetType != "group" && targetType != "account" {
			return fmt.Errorf("legacy source binding %d has unsupported target type %q", binding.ID, binding.TargetType)
		}
	}
	return nil
}

func (s *legacySnapshot) fingerprint() (string, error) {
	return Fingerprint(s)
}

func sourceRateKey(siteID uint, groupID string) string {
	return strconv.FormatUint(uint64(siteID), 10) + ":" + strings.TrimSpace(groupID)
}

func effectiveLegacyRate(rate legacyGroupRate) (float64, bool) {
	for _, candidate := range []*float64{rate.EffectiveRate, rate.UserRate, rate.RateMultiplier} {
		if candidate != nil && isPositiveFinite(*candidate) {
			return *candidate, true
		}
	}
	return 0, false
}

func parseLegacyRatio(raw *string, divisor float64) (float64, bool) {
	if raw == nil || !isPositiveFinite(divisor) {
		return 0, false
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(*raw), 64)
	if err != nil || !isPositiveFinite(value) {
		return 0, false
	}
	value /= divisor
	return value, isPositiveFinite(value)
}

func isPositiveFinite(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
