package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

type cachedAntiStallAdminConfig struct {
	config    AntiStallAdminConfig
	expiresAt int64
}

var antiStallAdminConfigCache atomic.Value // *cachedAntiStallAdminConfig
var antiStallAdminConfigSF singleflight.Group

const antiStallAdminConfigCacheTTL = 30 * time.Second
const antiStallAdminConfigErrorTTL = 5 * time.Second
const antiStallAdminConfigDBTimeout = 5 * time.Second

// GetAntiStallAdminConfig returns admin-level Anti-Stall PRO config (module + tiers).
func (s *SettingService) GetAntiStallAdminConfig(ctx context.Context) AntiStallAdminConfig {
	defaults := DefaultAntiStallAdminConfig()
	if s == nil || s.settingRepo == nil {
		return defaults
	}
	if cached, ok := antiStallAdminConfigCache.Load().(*cachedAntiStallAdminConfig); ok && cached != nil {
		if time.Now().UnixNano() < cached.expiresAt {
			return cached.config
		}
	}
	result, _, _ := antiStallAdminConfigSF.Do(SettingKeyAntiStallPro, func() (any, error) {
		if cached, ok := antiStallAdminConfigCache.Load().(*cachedAntiStallAdminConfig); ok && cached != nil {
			if time.Now().UnixNano() < cached.expiresAt {
				return cached, nil
			}
		}
		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), antiStallAdminConfigDBTimeout)
		defer cancel()
		raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyAntiStallPro)
		cfg := defaults
		ttl := antiStallAdminConfigCacheTTL
		if err == nil && strings.TrimSpace(raw) != "" {
			// Support both new admin config and legacy single-settings shape.
			var admin AntiStallAdminConfig
			if jerr := json.Unmarshal([]byte(raw), &admin); jerr == nil &&
				(admin.Basic.BufferTokens > 0 || admin.Pro.BufferTokens > 0 || admin.Ultra.BufferTokens > 0 ||
					strings.Contains(raw, "module_enabled") || strings.Contains(raw, `"basic"`)) {
				cfg = admin.Normalize()
			} else {
				// Legacy: flat AntiStallProSettings → map onto all tiers as basic defaults.
				var legacy AntiStallProSettings
				if jerr2 := json.Unmarshal([]byte(raw), &legacy); jerr2 == nil {
					cfg = DefaultAntiStallAdminConfig()
					cfg.ModuleEnabled = legacy.Enabled
					if legacy.BufferTokens > 0 {
						p := AntiStallTierParams{
							BufferTokens:        legacy.BufferTokens,
							DripTokensPerSecond: legacy.DripTokensPerSecond,
							UpstreamMaxRetry:    legacy.UpstreamMaxRetry,
							LowBufferTokens:     legacy.LowBufferTokens,
							MaxDripSeconds:      DefaultAntiStallMaxDripSeconds,
							MaxLeafSwitches:     DefaultAntiStallMaxLeafSwitches,
						}.Normalize()
						cfg.Basic = p
						cfg.Pro = p
						cfg.Ultra = p
					}
				}
			}
		} else if err != nil {
			ttl = antiStallAdminConfigErrorTTL
		}
		entry := &cachedAntiStallAdminConfig{
			config:    cfg,
			expiresAt: time.Now().Add(ttl).UnixNano(),
		}
		antiStallAdminConfigCache.Store(entry)
		return entry, nil
	})
	if entry, ok := result.(*cachedAntiStallAdminConfig); ok && entry != nil {
		return entry.config
	}
	return defaults
}

// UpdateAntiStallAdminConfig persists admin Anti-Stall PRO config and busts cache.
func (s *SettingService) UpdateAntiStallAdminConfig(ctx context.Context, cfg AntiStallAdminConfig) (AntiStallAdminConfig, error) {
	if s == nil || s.settingRepo == nil {
		return DefaultAntiStallAdminConfig(), errors.New("setting service unavailable")
	}
	normalized := cfg.Normalize()
	payload, err := json.Marshal(normalized)
	if err != nil {
		return normalized, err
	}
	if err := s.settingRepo.Set(ctx, SettingKeyAntiStallPro, string(payload)); err != nil {
		return normalized, err
	}
	antiStallAdminConfigCache.Store(&cachedAntiStallAdminConfig{
		config:    normalized,
		expiresAt: time.Now().Add(antiStallAdminConfigCacheTTL).UnixNano(),
	})
	return normalized, nil
}

// GetAntiStallProSettings is deprecated shape: returns module-disabled defaults.
// Prefer GetAntiStallAdminConfig + ResolveAntiStallForKey.
func (s *SettingService) GetAntiStallProSettings(ctx context.Context) AntiStallProSettings {
	admin := s.GetAntiStallAdminConfig(ctx)
	// Legacy callers treated Enabled as global switch.
	if !admin.ModuleEnabled {
		return AntiStallProSettings{Enabled: false}
	}
	// Without a key tier, module alone is not enough to enable streaming hold-back.
	return AntiStallProSettings{Enabled: false, Tier: AntiStallTierOff}
}

// UpdateAntiStallProSettings accepts either legacy flat settings or full admin config.
func (s *SettingService) UpdateAntiStallProSettings(ctx context.Context, settings AntiStallProSettings) (AntiStallProSettings, error) {
	// Map legacy update onto admin config module flag + basic tier.
	admin := s.GetAntiStallAdminConfig(ctx)
	admin.ModuleEnabled = settings.Enabled
	if settings.BufferTokens > 0 {
		p := AntiStallTierParams{
			BufferTokens:        settings.BufferTokens,
			DripTokensPerSecond: settings.DripTokensPerSecond,
			UpstreamMaxRetry:    settings.UpstreamMaxRetry,
			LowBufferTokens:     settings.LowBufferTokens,
			MaxDripSeconds:      settings.MaxDripSeconds,
			MaxLeafSwitches:     settings.MaxLeafSwitches,
		}.Normalize()
		admin.Basic = p
	}
	_, err := s.UpdateAntiStallAdminConfig(ctx, admin)
	return ResolveAntiStallForKey(admin, AntiStallTierBasic), err
}
