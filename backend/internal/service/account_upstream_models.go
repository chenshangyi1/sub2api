package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	UpstreamModelsExtraKey           = "upstream_models"
	UpstreamModelsSyncedAtExtraKey   = "upstream_models_synced_at"
	UpstreamModelsSyncStatusExtraKey = "upstream_models_sync_status"
	UpstreamModelsSourceExtraKey     = "upstream_models_source"
	UpstreamModelsIdentityExtraKey   = "upstream_models_identity"
)

var upstreamModelSnapshotExtraKeys = []string{
	UpstreamModelsExtraKey, UpstreamModelsSyncedAtExtraKey, UpstreamModelsSyncStatusExtraKey,
	UpstreamModelsSourceExtraKey, UpstreamModelsIdentityExtraKey,
}

func stripUpstreamModelSnapshotExtra(extra map[string]any) {
	for _, key := range upstreamModelSnapshotExtraKeys {
		delete(extra, key)
	}
}

// UpstreamModelsIdentity stores only a digest, never upstream credentials. A
// result from a replaced credential or endpoint is treated as unknown on read.
func (a *Account) UpstreamModelsIdentity() string {
	if a == nil {
		return ""
	}
	values := []string{strconv.FormatInt(a.ID, 10), a.Platform, a.Type}
	for _, key := range []string{"base_url", "api_key", "access_token"} {
		value, _ := a.Credentials[key].(string)
		values = append(values, value)
	}
	if a.ProxyID != nil {
		values = append(values, strconv.FormatInt(*a.ProxyID, 10))
	}
	payload, _ := json.Marshal(values)
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

type UpstreamModelSyncStatus string

const (
	UpstreamModelSyncStatusUnknown UpstreamModelSyncStatus = "unknown"
	UpstreamModelSyncStatusFresh   UpstreamModelSyncStatus = "fresh"
	UpstreamModelSyncStatusStale   UpstreamModelSyncStatus = "stale"
)

// UpstreamModelCapability is the last successful account-level model observation.
type UpstreamModelCapability struct {
	Status   UpstreamModelSyncStatus `json:"status"`
	Models   []string                `json:"models"`
	SyncedAt time.Time               `json:"synced_at,omitempty"`
	Source   string                  `json:"source,omitempty"`
}

func (a *Account) UpstreamModelCapability() UpstreamModelCapability {
	capability := UpstreamModelCapability{Status: UpstreamModelSyncStatusUnknown}
	if a == nil || a.Extra == nil {
		return capability
	}
	if identity, ok := a.Extra[UpstreamModelsIdentityExtraKey].(string); ok && identity != "" && identity != a.UpstreamModelsIdentity() {
		return capability
	}
	capability.Models = normalizeUpstreamModelIDs(a.Extra[UpstreamModelsExtraKey])
	capability.SyncedAt = parseExtraTime(a.Extra[UpstreamModelsSyncedAtExtraKey])
	capability.Source, _ = a.Extra[UpstreamModelsSourceExtraKey].(string)
	capability.Source = strings.TrimSpace(capability.Source)
	if len(capability.Models) > 0 {
		capability.Status = normalizeUpstreamModelSyncStatus(a.Extra[UpstreamModelsSyncStatusExtraKey])
	}
	return capability
}

// SupportsUpstreamModel adds an observation gate, not an alternative to the
// existing account whitelist. Unknown accounts retain their current policy.
func (a *Account) SupportsUpstreamModel(requestedModel string) bool {
	if a == nil {
		return true
	}
	mapped, _ := a.ResolveMappedModel(requestedModel)
	return a.supportsObservedUpstreamModel(mapped)
}

// Callers that already resolve the wire model must not apply mappings twice.
func (a *Account) supportsObservedUpstreamModel(model string) bool {
	capability := a.UpstreamModelCapability()
	if capability.Status == UpstreamModelSyncStatusUnknown {
		return true
	}
	model = strings.TrimSpace(model)
	i := sort.SearchStrings(capability.Models, model)
	return model != "" && i < len(capability.Models) && capability.Models[i] == model
}

// supportsUpstreamModelForRequest uses the same final model as forwarding. The
// compact and passthrough paths must not reapply the normal account mapping.
func supportsUpstreamModelForRequest(ctx context.Context, a *Account, requestedModel string, requireCompact bool) bool {
	if a == nil {
		return false
	}
	if strings.TrimSpace(requestedModel) == "" || normalizeUpstreamModelSyncStatus(a.Extra[UpstreamModelsSyncStatusExtraKey]) == UpstreamModelSyncStatusUnknown {
		return true
	}
	var model string
	switch a.Platform {
	case PlatformOpenAI, PlatformGrok:
		model = resolveOpenAIAccountUpstreamModelForRequest(a, requestedModel, requireCompact)
	case PlatformAntigravity:
		model = mapAntigravityModel(a, requestedModel)
		if enabled, ok := ThinkingEnabledFromContext(ctx); ok {
			model = applyThinkingModelSuffix(model, enabled)
		}
	default:
		model = resolveAccountUpstreamModel(a, requestedModel)
	}
	return a.supportsObservedUpstreamModel(model)
}

func normalizeUpstreamModelSyncStatus(value any) UpstreamModelSyncStatus {
	var status string
	switch v := value.(type) {
	case string:
		status = v
	case UpstreamModelSyncStatus:
		status = string(v)
	}
	switch UpstreamModelSyncStatus(strings.ToLower(strings.TrimSpace(status))) {
	case UpstreamModelSyncStatusFresh:
		return UpstreamModelSyncStatusFresh
	case UpstreamModelSyncStatusStale:
		return UpstreamModelSyncStatusStale
	default:
		return UpstreamModelSyncStatusUnknown
	}
}

func normalizeUpstreamModelIDs(raw any) []string {
	var values []any
	switch v := raw.(type) {
	case []string:
		values = make([]any, len(v))
		for i, model := range v {
			values[i] = model
		}
	case []any:
		values = v
	case json.RawMessage:
		decoder := json.NewDecoder(strings.NewReader(string(v)))
		decoder.UseNumber()
		if !json.Valid(v) || decoder.Decode(&values) != nil {
			return nil
		}
	case string:
		decoder := json.NewDecoder(strings.NewReader(v))
		decoder.UseNumber()
		if !json.Valid([]byte(v)) || decoder.Decode(&values) != nil {
			return nil
		}
	default:
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	var models []string
	for _, value := range values {
		model := ""
		switch v := value.(type) {
		case string:
			model = strings.TrimSpace(v)
		case json.Number:
			if f, err := v.Float64(); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
				model = v.String()
			}
		case float64:
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				model = strconv.FormatFloat(v, 'f', -1, 64)
			}
		case float32:
			if f := float64(v); !math.IsNaN(f) && !math.IsInf(f, 0) {
				model = strconv.FormatFloat(f, 'f', -1, 32)
			}
		case int:
			model = strconv.Itoa(v)
		case int64:
			model = strconv.FormatInt(v, 10)
		case uint64:
			model = strconv.FormatUint(v, 10)
		}
		if model == "" {
			continue
		}
		if _, exists := seen[model]; !exists {
			seen[model] = struct{}{}
			models = append(models, model)
		}
	}
	sort.Strings(models)
	return models
}
