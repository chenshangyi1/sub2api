package service

import (
	"math"
	"sort"
	"strings"
)

const (
	AdaptiveRoutingPreferenceIntelligence = "intelligence"
	AdaptiveRoutingPreferencePrice        = "price"
)

// NormalizeAdaptiveMaxRateMultiplier returns nil for unlimited (nil input or <0).
// Zero is a valid ceiling (only free leaves). NaN/Inf are rejected as unlimited.
func NormalizeAdaptiveMaxRateMultiplier(raw *float64) *float64 {
	if raw == nil {
		return nil
	}
	v := *raw
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return nil
	}
	out := v
	return &out
}

// NormalizeAdaptiveRoutingPreference validates and defaults preference.
func NormalizeAdaptiveRoutingPreference(raw string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return AdaptiveRoutingPreferenceIntelligence, nil
	}
	switch v {
	case AdaptiveRoutingPreferenceIntelligence, AdaptiveRoutingPreferencePrice:
		return v, nil
	default:
		return "", ErrInvalidAdaptiveRoutingPreference
	}
}

// AdaptiveRouteModeFromPreference maps API key preference to planner mode.
func AdaptiveRouteModeFromPreference(pref string) AdaptiveRouteMode {
	if strings.EqualFold(strings.TrimSpace(pref), AdaptiveRoutingPreferencePrice) {
		return AdaptiveRouteModePrice
	}
	return AdaptiveRouteModeIntelligence
}

// NormalizeAdaptiveLeafGroupIDs unique-sorts positive leaf group IDs.
// Empty input means "all enabled leaves" (no allowlist).
func NormalizeAdaptiveLeafGroupIDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// AdaptiveLeafAllowlist returns a set used by the planner. Nil means no filter.
func AdaptiveLeafAllowlist(ids []int64) map[int64]struct{} {
	normalized := NormalizeAdaptiveLeafGroupIDs(ids)
	if len(normalized) == 0 {
		return nil
	}
	out := make(map[int64]struct{}, len(normalized))
	for _, id := range normalized {
		out[id] = struct{}{}
	}
	return out
}
