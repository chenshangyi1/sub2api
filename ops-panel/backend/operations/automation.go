package operations

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

func defaultPoolPolicy() AccountPoolPolicy {
	return AccountPoolPolicy{
		HealthReturnEnabled: true, HealthReturnThreshold: 75,
		SmartExpansionEnabled: true, TotalConcurrency: 900,
		MinAccountConcurrency: 20, MaxAccountConcurrency: 250,
		ExpansionLoadThresholdPct: 80, LoadFactorEnabled: true,
		TotalLoadFactor: 400, MinAccountLoadFactor: 20, MaxAccountLoadFactor: 500,
		PriceProtectionEnabled: true, FailureDisableEnabled: true,
		FailureWindow: 5, FailureCount: 3, SlowWindow: 10,
		SlowFirstTokenMS: 15_000, SlowCount: 5,
		MinAvailableAccounts: 1, TargetHealthyAccounts: 3,
	}
}

func defaultPriorityPolicy() AccountPriorityPolicy {
	return AccountPriorityPolicy{
		Enabled: false, Strategy: "rate", TargetGroupIDs: []int64{},
		SampleSize: 10, LookbackMinutes: 60, FirstTokenCoefficient: 1,
		RateCoefficient: 10_000, MissingSamplePenaltyMS: 5_000,
	}
}

func (s *Service) GetAutomationSettings(ctx context.Context, targetID uint) (*AutomationSettings, error) {
	if err := s.validateTarget(ctx, targetID); err != nil {
		return nil, err
	}
	poolRaw, err := s.getSetting(ctx, targetSettingKey("account_pool_policy", targetID), "")
	if err != nil {
		return nil, err
	}
	priorityRaw, err := s.getSetting(ctx, targetSettingKey("account_priority_rule", targetID), "")
	if err != nil {
		return nil, err
	}
	runtimeRaw, err := s.getSetting(ctx, targetSettingKey("account_pool_policy_runtime", targetID), "")
	if err != nil {
		return nil, err
	}
	result := AutomationSettings{
		AccountPool:     decodePoolPolicy(decodeJSONMap(poolRaw)),
		AccountPriority: decodePriorityPolicy(decodeJSONMap(priorityRaw)),
	}
	runtime := decodeJSONMap(runtimeRaw)
	if raw := stringValue(runtime, "lastRunAt", ""); raw != "" {
		if parsed, parseErr := time.Parse(time.RFC3339Nano, raw); parseErr == nil {
			result.LastAppliedAt = &parsed
		}
	}
	result.LastApplyStatus = stringValue(runtime, "lastStatus", "idle")
	result.LastApplyMessage = stringValue(runtime, "lastMessage", "")
	return &result, nil
}

func decodePoolPolicy(values map[string]any) AccountPoolPolicy {
	defaults := defaultPoolPolicy()
	return AccountPoolPolicy{
		HealthReturnEnabled:       boolValue(values, "healthReturnEnabled", defaults.HealthReturnEnabled),
		HealthReturnThreshold:     int(numberValue(values, "healthReturnThreshold", float64(defaults.HealthReturnThreshold))),
		SmartExpansionEnabled:     boolValue(values, "smartExpansionEnabled", defaults.SmartExpansionEnabled),
		TotalConcurrency:          int(numberValue(values, "totalConcurrency", float64(defaults.TotalConcurrency))),
		MinAccountConcurrency:     int(numberValue(values, "minAccountConcurrency", float64(defaults.MinAccountConcurrency))),
		MaxAccountConcurrency:     int(numberValue(values, "maxAccountConcurrency", float64(defaults.MaxAccountConcurrency))),
		ExpansionLoadThresholdPct: int(numberValue(values, "expansionLoadThresholdPct", float64(defaults.ExpansionLoadThresholdPct))),
		LoadFactorEnabled:         boolValue(values, "loadFactorEnabled", defaults.LoadFactorEnabled),
		TotalLoadFactor:           int(numberValue(values, "totalLoadFactor", float64(defaults.TotalLoadFactor))),
		MinAccountLoadFactor:      int(numberValue(values, "minAccountLoadFactor", float64(defaults.MinAccountLoadFactor))),
		MaxAccountLoadFactor:      int(numberValue(values, "maxAccountLoadFactor", float64(defaults.MaxAccountLoadFactor))),
		PriceProtectionEnabled:    boolValue(values, "priceProtectionEnabled", defaults.PriceProtectionEnabled),
		FailureDisableEnabled:     boolValue(values, "failureDisableEnabled", defaults.FailureDisableEnabled),
		FailureWindow:             int(numberValue(values, "failureWindow", float64(defaults.FailureWindow))),
		FailureCount:              int(numberValue(values, "failureCount", float64(defaults.FailureCount))),
		SlowWindow:                int(numberValue(values, "slowWindow", float64(defaults.SlowWindow))),
		SlowFirstTokenMS:          int(numberValue(values, "slowFirstTokenMs", float64(defaults.SlowFirstTokenMS))),
		SlowCount:                 int(numberValue(values, "slowCount", float64(defaults.SlowCount))),
		MinAvailableAccounts:      int(numberValue(values, "minAvailableAccounts", float64(defaults.MinAvailableAccounts))),
		TargetHealthyAccounts:     int(numberValue(values, "targetHealthyAccounts", float64(defaults.TargetHealthyAccounts))),
	}
}

func decodePriorityPolicy(values map[string]any) AccountPriorityPolicy {
	defaults := defaultPriorityPolicy()
	result := AccountPriorityPolicy{
		Enabled:                boolValue(values, "enabled", defaults.Enabled),
		Strategy:               stringValue(values, "strategy", defaults.Strategy),
		SampleSize:             int(numberValue(values, "sampleSize", float64(defaults.SampleSize))),
		LookbackMinutes:        int(numberValue(values, "lookbackMinutes", float64(defaults.LookbackMinutes))),
		FirstTokenCoefficient:  numberValue(values, "firstTokenCoefficient", defaults.FirstTokenCoefficient),
		RateCoefficient:        numberValue(values, "rateCoefficient", defaults.RateCoefficient),
		MissingSamplePenaltyMS: int(numberValue(values, "missingSamplePenaltyMs", float64(defaults.MissingSamplePenaltyMS))),
		TargetGroupIDs:         []int64{},
	}
	if raw, ok := values["targetGroupIds"].([]any); ok {
		seen := map[int64]struct{}{}
		for _, item := range raw {
			value, ok := item.(float64)
			id := int64(value)
			if ok && value == float64(id) && id > 0 {
				seen[id] = struct{}{}
			}
		}
		for id := range seen {
			result.TargetGroupIDs = append(result.TargetGroupIDs, id)
		}
		sort.Slice(result.TargetGroupIDs, func(i, j int) bool { return result.TargetGroupIDs[i] < result.TargetGroupIDs[j] })
	}
	return result
}

func normalizeAutomation(input AutomationSettings) (AutomationSettings, error) {
	pool := input.AccountPool
	pool.HealthReturnThreshold = clampInt(pool.HealthReturnThreshold, 1, 100)
	pool.TotalConcurrency = clampInt(pool.TotalConcurrency, 1, 1_000_000)
	pool.MinAccountConcurrency = clampInt(pool.MinAccountConcurrency, 1, 100_000)
	pool.MaxAccountConcurrency = clampInt(pool.MaxAccountConcurrency, pool.MinAccountConcurrency, 100_000)
	pool.ExpansionLoadThresholdPct = clampInt(pool.ExpansionLoadThresholdPct, 1, 100)
	pool.TotalLoadFactor = clampInt(pool.TotalLoadFactor, 1, 1_000_000)
	pool.MinAccountLoadFactor = clampInt(pool.MinAccountLoadFactor, 1, 100_000)
	pool.MaxAccountLoadFactor = clampInt(pool.MaxAccountLoadFactor, pool.MinAccountLoadFactor, 100_000)
	pool.FailureWindow = clampInt(pool.FailureWindow, 1, 60)
	pool.FailureCount = clampInt(pool.FailureCount, 1, pool.FailureWindow)
	pool.SlowWindow = clampInt(pool.SlowWindow, 1, 60)
	pool.SlowFirstTokenMS = clampInt(pool.SlowFirstTokenMS, 1, 3_600_000)
	pool.SlowCount = clampInt(pool.SlowCount, 1, pool.SlowWindow)
	pool.MinAvailableAccounts = clampInt(pool.MinAvailableAccounts, 1, 100_000)
	pool.TargetHealthyAccounts = clampInt(pool.TargetHealthyAccounts, pool.MinAvailableAccounts, 100_000)

	priority := input.AccountPriority
	if priority.Strategy != "rate" && priority.Strategy != "latency_rate" {
		return input, fmt.Errorf("%w: priority strategy must be rate or latency_rate", ErrInvalid)
	}
	priority.SampleSize = clampInt(priority.SampleSize, 1, 200)
	priority.LookbackMinutes = clampInt(priority.LookbackMinutes, 1, 43_200)
	priority.MissingSamplePenaltyMS = clampInt(priority.MissingSamplePenaltyMS, 0, 3_600_000)
	if !isFiniteNonNegative(priority.FirstTokenCoefficient) || !isFiniteNonNegative(priority.RateCoefficient) {
		return input, fmt.Errorf("%w: priority coefficients must be non-negative", ErrInvalid)
	}
	seen := map[int64]struct{}{}
	priority.TargetGroupIDs = priority.TargetGroupIDs[:0]
	for _, id := range input.AccountPriority.TargetGroupIDs {
		if id > 0 {
			seen[id] = struct{}{}
		}
	}
	for id := range seen {
		priority.TargetGroupIDs = append(priority.TargetGroupIDs, id)
	}
	sort.Slice(priority.TargetGroupIDs, func(i, j int) bool { return priority.TargetGroupIDs[i] < priority.TargetGroupIDs[j] })
	input.AccountPool = pool
	input.AccountPriority = priority
	return input, nil
}

func (s *Service) SaveAutomationSettings(ctx context.Context, targetID uint, input AutomationSettings) (*AutomationSettings, error) {
	if err := s.validateTarget(ctx, targetID); err != nil {
		return nil, err
	}
	normalized, err := normalizeAutomation(input)
	if err != nil {
		return nil, err
	}
	poolKey := targetSettingKey("account_pool_policy", targetID)
	priorityKey := targetSettingKey("account_priority_rule", targetID)
	poolRaw, _ := s.getSetting(ctx, poolKey, "")
	priorityRaw, _ := s.getSetting(ctx, priorityKey, "")
	pool := decodeJSONMap(poolRaw)
	priority := decodeJSONMap(priorityRaw)
	applyPoolJSON(pool, normalized.AccountPool)
	applyPriorityJSON(priority, normalized.AccountPriority)
	now := s.now().UTC()
	pool["updatedAt"] = now.Format(time.RFC3339Nano)
	priority["updatedAt"] = now.Format(time.RFC3339Nano)
	poolBytes, _ := json.Marshal(pool)
	priorityBytes, _ := json.Marshal(priority)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := setSetting(tx, poolKey, string(poolBytes), now); err != nil {
			return err
		}
		return setSetting(tx, priorityKey, string(priorityBytes), now)
	})
	if err != nil {
		return nil, err
	}
	s.recordAction(ctx, "save_automation", fmt.Sprintf("target:%d", targetID), "", true)
	return s.GetAutomationSettings(ctx, targetID)
}

func applyPoolJSON(out map[string]any, value AccountPoolPolicy) {
	out["healthReturnEnabled"] = value.HealthReturnEnabled
	out["healthReturnThreshold"] = value.HealthReturnThreshold
	out["smartExpansionEnabled"] = value.SmartExpansionEnabled
	out["totalConcurrency"] = value.TotalConcurrency
	out["minAccountConcurrency"] = value.MinAccountConcurrency
	out["maxAccountConcurrency"] = value.MaxAccountConcurrency
	out["expansionLoadThresholdPct"] = value.ExpansionLoadThresholdPct
	out["loadFactorEnabled"] = value.LoadFactorEnabled
	out["totalLoadFactor"] = value.TotalLoadFactor
	out["minAccountLoadFactor"] = value.MinAccountLoadFactor
	out["maxAccountLoadFactor"] = value.MaxAccountLoadFactor
	out["priceProtectionEnabled"] = value.PriceProtectionEnabled
	out["failureDisableEnabled"] = value.FailureDisableEnabled
	out["failureWindow"] = value.FailureWindow
	out["failureCount"] = value.FailureCount
	out["slowWindow"] = value.SlowWindow
	out["slowFirstTokenMs"] = value.SlowFirstTokenMS
	out["slowCount"] = value.SlowCount
	out["minAvailableAccounts"] = value.MinAvailableAccounts
	out["targetHealthyAccounts"] = value.TargetHealthyAccounts
}

func applyPriorityJSON(out map[string]any, value AccountPriorityPolicy) {
	out["enabled"] = value.Enabled
	out["strategy"] = value.Strategy
	out["targetGroupIds"] = value.TargetGroupIDs
	out["sampleSize"] = value.SampleSize
	out["lookbackMinutes"] = value.LookbackMinutes
	out["firstTokenCoefficient"] = value.FirstTokenCoefficient
	out["rateCoefficient"] = value.RateCoefficient
	out["missingSamplePenaltyMs"] = value.MissingSamplePenaltyMS
}

func (s *Service) ApplyAutomation(ctx context.Context, targetID uint) (*AutomationSettings, error) {
	if err := s.validateTarget(ctx, targetID); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	runtimeKey := targetSettingKey("account_pool_policy_runtime", targetID)
	runtimeRaw, _ := s.getSetting(ctx, runtimeKey, "")
	runtime := decodeJSONMap(runtimeRaw)
	runtime["lastRunAt"] = now.Format(time.RFC3339Nano)
	runtime["lastStatus"] = "running"
	runtime["lastMessage"] = "queued for extension worker"
	runtimeBytes, _ := json.Marshal(runtime)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := setSetting(tx, runtimeKey, string(runtimeBytes), now); err != nil {
			return err
		}
		if err := setSetting(tx, "worker_run_requested_at", now.Format(time.RFC3339Nano), now); err != nil {
			return err
		}
		if err := setSetting(tx, "worker_run_requested_target_id", fmt.Sprintf("%d", targetID), now); err != nil {
			return err
		}
		return setSetting(tx, "worker_run_requested_mode", "automation", now)
	})
	if err != nil {
		return nil, err
	}
	s.recordAction(ctx, "apply_automation", fmt.Sprintf("target:%d", targetID), "queued for extension worker", true)
	return s.GetAutomationSettings(ctx, targetID)
}

func parseTimeValue(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return &parsed
}

// 执行权模式：worker 本地执行（local）或移交 sub2api 内置健康调度器（delegated）。
const SettingKeyPoolExecution = "account_pool_execution"

func (s *Service) GetPoolExecutionMode(ctx context.Context) (string, error) {
	v, err := s.getSetting(ctx, SettingKeyPoolExecution, "local")
	if err != nil {
		return "local", nil
	}
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "delegated" {
		return "delegated", nil
	}
	return "local", nil
}

func (s *Service) SetPoolExecutionMode(ctx context.Context, mode string) (string, error) {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode != "delegated" {
		mode = "local"
	}
	now := s.now().UTC()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return setSetting(tx, SettingKeyPoolExecution, mode, now)
	})
	if err != nil {
		return "", err
	}
	return mode, nil
}
