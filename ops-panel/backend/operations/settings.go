package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const defaultBalanceTemplate = "Sub2API operations account balance alert\nTarget: {{connectionName}}\nAccount: {{accountName}} (#{{accountId}})\nBalance: {{remaining}} {{unit}}\nThreshold: {{threshold}} {{unit}}\nChecked at: {{checkedAt}}"

type settingRow struct {
	Key       string     `gorm:"primaryKey;column:key"`
	Value     string     `gorm:"column:value"`
	UpdatedAt *time.Time `gorm:"column:updated_at"`
}

func (settingRow) TableName() string { return "settings" }

func (s *Service) getSetting(ctx context.Context, key, fallback string) (string, error) {
	var row settingRow
	err := s.db.WithContext(ctx).Where("key = ?", key).Take(&row).Error
	if err == nil {
		return row.Value, nil
	}
	if err == gorm.ErrRecordNotFound {
		return fallback, nil
	}
	return "", err
}

func setSetting(tx *gorm.DB, key, value string, at time.Time) error {
	updatedAt := at.UTC()
	row := settingRow{Key: key, Value: value, UpdatedAt: &updatedAt}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.Assignments(map[string]any{"value": value, "updated_at": at.UTC()}),
	}).Create(&row).Error
}

func targetSettingKey(name string, targetID uint) string {
	return name + ":" + strconv.FormatUint(uint64(targetID), 10)
}

func (s *Service) balanceThresholds(ctx context.Context, targetID uint) (map[int64]float64, float64, error) {
	raw, err := s.getSetting(ctx, targetSettingKey("account_balance_thresholds", targetID), "{}")
	if err != nil {
		return nil, 0, err
	}
	values := map[string]float64{}
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		values = map[string]float64{}
	}
	thresholds := make(map[int64]float64, len(values))
	for key, value := range values {
		id, parseErr := strconv.ParseInt(key, 10, 64)
		if parseErr == nil && id > 0 && isFiniteNonNegative(value) {
			thresholds[id] = value
		}
	}
	defaultRaw, err := s.getSetting(ctx, targetSettingKey("account_balance_default_threshold", targetID), "0")
	if err != nil {
		return nil, 0, err
	}
	defaultValue, _ := strconv.ParseFloat(defaultRaw, 64)
	if !isFiniteNonNegative(defaultValue) {
		defaultValue = 0
	}
	return thresholds, defaultValue, nil
}

func (s *Service) GetSettings(ctx context.Context) (*OperationsSettings, error) {
	heavyRaw, err := s.getSetting(ctx, "upstream_monitor_heavy_interval_minutes", "60")
	if err != nil {
		return nil, err
	}
	heavy, _ := strconv.Atoi(heavyRaw)
	if heavy <= 0 {
		heavy = 60
	}
	return &OperationsSettings{HeavyProbeIntervalMinutes: clampInt(heavy, 5, 10_080)}, nil
}

func (s *Service) SaveSettings(ctx context.Context, input OperationsSettings) (*OperationsSettings, error) {
	input.HeavyProbeIntervalMinutes = clampInt(input.HeavyProbeIntervalMinutes, 5, 10_080)
	if err := setSetting(
		s.db.WithContext(ctx),
		"upstream_monitor_heavy_interval_minutes",
		strconv.Itoa(input.HeavyProbeIntervalMinutes),
		s.now().UTC(),
	); err != nil {
		return nil, err
	}
	s.recordAction(ctx, "save_operations_settings", "operations", "", true)
	return s.GetSettings(ctx)
}

func (s *Service) GetTargetSettings(ctx context.Context, targetID uint) (*TargetSettings, error) {
	if err := s.validateTarget(ctx, targetID); err != nil {
		return nil, err
	}
	webhookRaw, err := s.getSetting(ctx, targetSettingKey("account_balance_webhook", targetID), "")
	if err != nil {
		return nil, err
	}
	webhook := struct {
		Enabled         bool   `json:"enabled"`
		URL             string `json:"url"`
		CooldownMinutes int    `json:"cooldownMinutes"`
		Template        string `json:"template"`
	}{CooldownMinutes: 360, Template: defaultBalanceTemplate}
	if webhookRaw != "" {
		_ = json.Unmarshal([]byte(webhookRaw), &webhook)
	}
	_, defaultThreshold, err := s.balanceThresholds(ctx, targetID)
	if err != nil {
		return nil, err
	}
	suppressRaw, err := s.getSetting(ctx, targetSettingKey("suppress_native_monitors", targetID), "true")
	if err != nil {
		return nil, err
	}
	return &TargetSettings{
		AccountBalanceAlertEnabled:     webhook.Enabled,
		AccountBalanceDefaultThreshold: defaultThreshold,
		AccountBalanceCooldownMinutes:  clampInt(webhook.CooldownMinutes, 0, 43_200),
		AccountBalanceWebhookURL:       strings.TrimSpace(webhook.URL),
		AccountBalanceWebhookTemplate:  webhook.Template,
		SuppressNativeMonitors:         !strings.EqualFold(strings.TrimSpace(suppressRaw), "false"),
	}, nil
}

func (s *Service) SaveTargetSettings(ctx context.Context, targetID uint, input TargetSettings) (*TargetSettings, error) {
	if err := s.validateTarget(ctx, targetID); err != nil {
		return nil, err
	}
	input.AccountBalanceWebhookURL = strings.TrimSpace(input.AccountBalanceWebhookURL)
	input.AccountBalanceWebhookTemplate = strings.TrimSpace(input.AccountBalanceWebhookTemplate)
	if input.AccountBalanceWebhookTemplate == "" {
		input.AccountBalanceWebhookTemplate = defaultBalanceTemplate
	}
	if !isFiniteNonNegative(input.AccountBalanceDefaultThreshold) {
		return nil, fmt.Errorf("%w: balance threshold must be a non-negative number", ErrInvalid)
	}
	input.AccountBalanceCooldownMinutes = clampInt(input.AccountBalanceCooldownMinutes, 0, 43_200)
	if input.AccountBalanceWebhookURL != "" {
		parsed, err := url.ParseRequestURI(input.AccountBalanceWebhookURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return nil, fmt.Errorf("%w: webhook URL must use http or https", ErrInvalid)
		}
	}
	if input.AccountBalanceAlertEnabled && input.AccountBalanceWebhookURL == "" {
		return nil, fmt.Errorf("%w: webhook URL is required when balance alerts are enabled", ErrInvalid)
	}
	now := s.now().UTC()
	webhook, _ := json.Marshal(map[string]any{
		"enabled":         input.AccountBalanceAlertEnabled,
		"url":             input.AccountBalanceWebhookURL,
		"cooldownMinutes": input.AccountBalanceCooldownMinutes,
		"template":        input.AccountBalanceWebhookTemplate,
		"updatedAt":       now.Format(time.RFC3339Nano),
	})
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		settings := map[string]string{
			targetSettingKey("account_balance_webhook", targetID):           string(webhook),
			targetSettingKey("account_balance_default_threshold", targetID): strconv.FormatFloat(input.AccountBalanceDefaultThreshold, 'f', -1, 64),
			targetSettingKey("suppress_native_monitors", targetID):          strconv.FormatBool(input.SuppressNativeMonitors),
		}
		for key, value := range settings {
			if err := setSetting(tx, key, value, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.recordAction(ctx, "save_target_settings", fmt.Sprintf("target:%d", targetID), "", true)
	return s.GetTargetSettings(ctx, targetID)
}

func (s *Service) TestBalanceWebhook(ctx context.Context, targetID uint) error {
	target, _, err := s.target(ctx, targetID)
	if err != nil {
		return err
	}
	settings, err := s.GetTargetSettings(ctx, targetID)
	if err != nil {
		return err
	}
	if settings.AccountBalanceWebhookURL == "" {
		return fmt.Errorf("%w: save a webhook URL before testing", ErrInvalid)
	}
	body, _ := json.Marshal(map[string]any{
		"event": "account_balance_low_test", "connectionId": targetID,
		"connectionName": target.Name, "accountName": "Webhook Test",
		"remaining": 1.23, "threshold": settings.AccountBalanceDefaultThreshold,
		"unit": "USD", "checkedAt": s.now().UTC().Format(time.RFC3339Nano),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, settings.AccountBalanceWebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			err = fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
		}
	}
	if err != nil {
		s.recordAction(ctx, "test_balance_webhook", fmt.Sprintf("target:%d", targetID), err.Error(), false)
		return err
	}
	s.recordAction(ctx, "test_balance_webhook", fmt.Sprintf("target:%d", targetID), "", true)
	return nil
}

func isFiniteNonNegative(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func decodeJSONMap(raw string) map[string]any {
	out := map[string]any{}
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}

func numberValue(values map[string]any, key string, fallback float64) float64 {
	value, ok := values[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case float64:
		if !math.IsNaN(typed) && !math.IsInf(typed, 0) {
			return typed
		}
	case json.Number:
		if parsed, err := typed.Float64(); err == nil {
			return parsed
		}
	}
	return fallback
}

func boolValue(values map[string]any, key string, fallback bool) bool {
	value, ok := values[key].(bool)
	if !ok {
		return fallback
	}
	return value
}

func stringValue(values map[string]any, key, fallback string) string {
	value, ok := values[key].(string)
	if !ok {
		return fallback
	}
	return value
}
