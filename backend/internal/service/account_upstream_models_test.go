package service

import (
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestAccountSupportsUpstreamModel_UnknownAllowsModel(t *testing.T) {
	tests := []struct {
		name    string
		account *Account
	}{
		{name: "nil account"},
		{name: "missing extra", account: &Account{}},
		{name: "empty extra", account: &Account{Extra: map[string]any{}}},
		{name: "explicit unknown", account: &Account{Extra: map[string]any{
			UpstreamModelsSyncStatusExtraKey: UpstreamModelSyncStatusUnknown,
			UpstreamModelsExtraKey:           []string{"upstream-model-a"},
		}}},
		{name: "missing status with list and timestamp", account: &Account{Extra: map[string]any{
			UpstreamModelsExtraKey:         []string{"upstream-model-a"},
			UpstreamModelsSyncedAtExtraKey: "2026-01-02T03:04:05Z",
		}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.account.UpstreamModelCapability().Status; got != UpstreamModelSyncStatusUnknown {
				t.Fatalf("capability status = %q, want unknown", got)
			}
			for _, model := range []string{"model-not-in-a-snapshot", "", " "} {
				if !tt.account.SupportsUpstreamModel(model) {
					t.Errorf("unknown capability must allow %q", model)
				}
			}
		})
	}
}

func TestAccountSupportsUpstreamModel_InvalidStatusAllowsModel(t *testing.T) {
	tests := []struct {
		name   string
		status any
	}{
		{name: "nil"},
		{name: "empty", status: ""},
		{name: "failed", status: "error"},
		{name: "unknown", status: "unknown"},
		{name: "boolean", status: true},
		{name: "float", status: float64(1)},
		{name: "json number", status: json.Number("1")},
		{name: "object", status: map[string]any{"status": "fresh"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := tt.status
			account := &Account{Extra: map[string]any{
				UpstreamModelsSyncStatusExtraKey: status,
				UpstreamModelsExtraKey:           []string{"upstream-model-a"},
			}}
			if got := account.UpstreamModelCapability().Status; got != UpstreamModelSyncStatusUnknown {
				t.Fatalf("status %v produces %q, want unknown", status, got)
			}
			if !account.SupportsUpstreamModel("absent") {
				t.Fatalf("status %v must not activate a restrictive snapshot", status)
			}
		})
	}
}

func TestAccountSupportsUpstreamModel_EmptyOrMalformedListAllowsModel(t *testing.T) {
	tests := []struct {
		name string
		raw  any
	}{
		{name: "missing list"},
		{name: "empty strings", raw: []string{}},
		{name: "empty any", raw: []any{}},
		{name: "blank strings", raw: []string{"", " \t\n"}},
		{name: "invalid entries", raw: []any{nil, true, map[string]any{"id": "model-a"}, []any{"model-b"}}},
		{name: "invalid numbers", raw: []any{json.Number("not-a-number"), math.NaN(), math.Inf(1)}},
		{name: "map instead of list", raw: map[string]any{"id": "model-a"}},
		{name: "scalar string", raw: "model-a"},
		{name: "scalar number", raw: float64(123)},
		{name: "json string scalar", raw: `"model-a"`},
		{name: "json number scalar", raw: json.RawMessage(`123`)},
		{name: "malformed json array", raw: `["model-a"`},
		{name: "trailing json", raw: json.RawMessage(`["model-a"] ["model-b"]`)},
	}
	for _, status := range []UpstreamModelSyncStatus{UpstreamModelSyncStatusFresh, UpstreamModelSyncStatusStale} {
		for _, tt := range tests {
			t.Run(string(status)+"/"+tt.name, func(t *testing.T) {
				account := &Account{Extra: map[string]any{
					UpstreamModelsExtraKey:           tt.raw,
					UpstreamModelsSyncStatusExtraKey: status,
				}}
				if capability := account.UpstreamModelCapability(); capability.Status != UpstreamModelSyncStatusUnknown || len(capability.Models) != 0 {
					t.Fatalf("unusable list must be unknown and empty, got %+v", capability)
				}
				if !account.SupportsUpstreamModel("absent") {
					t.Fatal("unusable model lists must fail open")
				}
			})
		}
	}
}

func TestAccountSupportsUpstreamModel_FreshFiltersMappedModel(t *testing.T) {
	account := &Account{
		Credentials: map[string]any{
			"model_mapping": map[string]any{"public-a": "upstream-model-a", "public-b": "upstream-model-b"},
		},
		Extra: map[string]any{
			UpstreamModelsExtraKey:           []string{"upstream-model-a", "public-b"},
			UpstreamModelsSyncStatusExtraKey: " FRESH ",
		},
	}
	if got := account.UpstreamModelCapability().Status; got != UpstreamModelSyncStatusFresh {
		t.Fatalf("capability status = %q, want fresh", got)
	}
	if !account.SupportsUpstreamModel("public-a") {
		t.Fatal("a mapped request must match the final upstream model in the snapshot")
	}
	if account.SupportsUpstreamModel("public-b") {
		t.Fatal("a snapshot containing only the public alias must not allow its absent mapped target")
	}
	if account.SupportsUpstreamModel("upstream-model-b") {
		t.Fatal("a fresh snapshot must reject a model absent from the snapshot")
	}
}

func TestAccountSupportsUpstreamModel_StaleUsesLastSuccessfulSnapshot(t *testing.T) {
	account := &Account{
		Credentials: map[string]any{"model_mapping": map[string]any{
			"public-a": "upstream-model-a", "public-b": "upstream-model-b",
		}},
		Extra: map[string]any{
			UpstreamModelsExtraKey:           []any{"upstream-model-a"},
			UpstreamModelsSyncStatusExtraKey: " STALE ",
			UpstreamModelsSyncedAtExtraKey:   "2020-01-02T03:04:05Z",
		},
	}
	if got := account.UpstreamModelCapability().Status; got != UpstreamModelSyncStatusStale {
		t.Fatalf("capability status = %q, want stale", got)
	}
	if !account.SupportsUpstreamModel("public-a") {
		t.Fatal("stale snapshots must continue allowing mapped models from the last successful list")
	}
	if account.SupportsUpstreamModel("public-b") {
		t.Fatal("stale snapshots must reject mapped models absent from the last successful list")
	}
}

func TestAccountSupportsUpstreamModel_ResolvesFinalModel(t *testing.T) {
	tests := []struct {
		name      string
		platform  string
		mapping   map[string]any
		requested string
		models    []string
		want      bool
	}{
		{name: "no mapping", requested: "model-a", models: []string{"model-a"}, want: true},
		{name: "empty mapping", mapping: map[string]any{}, requested: "model-a", models: []string{"model-a"}, want: true},
		{name: "unmatched mapping keeps request", mapping: map[string]any{"other": "other-upstream"}, requested: "model-a", models: []string{"model-a"}, want: true},
		{name: "exact wins", mapping: map[string]any{"public-a": "exact", "public-*": "wildcard"}, requested: "public-a", models: []string{"exact"}, want: true},
		{name: "exact does not fall back to wildcard", mapping: map[string]any{"public-a": "exact", "public-*": "wildcard"}, requested: "public-a", models: []string{"wildcard"}},
		{name: "longest wildcard wins", mapping: map[string]any{"public-*": "short", "public-long-*": "long"}, requested: "public-long-a", models: []string{"long"}, want: true},
		{name: "longest wildcard does not fall back", mapping: map[string]any{"public-*": "short", "public-long-*": "long"}, requested: "public-long-a", models: []string{"short"}},
		{name: "one mapping step", mapping: map[string]any{"public-a": "model-a", "model-a": "model-b"}, requested: "public-a", models: []string{"model-a"}, want: true},
		{name: "no recursive mapping", mapping: map[string]any{"public-a": "model-a", "model-a": "model-b"}, requested: "public-a", models: []string{"model-b"}},
		{name: "trim after mapping", mapping: map[string]any{"public-a": " model-a "}, requested: " public-a ", models: []string{"model-a"}, want: true},
		{name: "exact raw key wins before trim", mapping: map[string]any{" public-a ": "raw", "public-a": "trimmed"}, requested: " public-a ", models: []string{"raw"}, want: true},
		{name: "empty target", mapping: map[string]any{"public-a": " "}, requested: "public-a", models: []string{"public-a"}},
		{name: "empty request", requested: "", models: []string{"model-a"}},
		{name: "snapshot has no wildcard semantics", requested: "model-a", models: []string{"model-*"}},
		{name: "literal star can match", requested: "model-*", models: []string{"model-*"}, want: true},
		{name: "preserve vendor prefix", requested: "model-a", models: []string{"provider/model-a"}},
		{name: "preserve model prefix", requested: "model-a", models: []string{"models/model-a"}},
		{name: "case sensitive", requested: "Model-A", models: []string{"model-a"}},
		{name: "mapping case sensitive", mapping: map[string]any{"Public-A": "model-a"}, requested: "public-a", models: []string{"model-a"}},
		{name: "gemini normalized alias", platform: PlatformGemini, mapping: map[string]any{"gemini-3.1-pro-preview": "upstream"}, requested: "gemini-3.1-pro-preview-customtools", models: []string{"upstream"}, want: true},
		{name: "gemini explicit alias wins", platform: PlatformGemini, mapping: map[string]any{"gemini-3.1-pro-preview": "upstream", "gemini-3.1-pro-preview-customtools": "custom"}, requested: "gemini-3.1-pro-preview-customtools", models: []string{"custom"}, want: true},
		{name: "gemini without mapping preserves alias", platform: PlatformGemini, requested: "gemini-3.1-pro-preview-customtools", models: []string{"gemini-3.1-pro-preview"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{
				Platform:    tt.platform,
				Credentials: map[string]any{"model_mapping": tt.mapping},
				Extra: map[string]any{
					UpstreamModelsExtraKey:           tt.models,
					UpstreamModelsSyncStatusExtraKey: UpstreamModelSyncStatusFresh,
				},
			}
			if got := account.SupportsUpstreamModel(tt.requested); got != tt.want {
				t.Fatalf("SupportsUpstreamModel(%q) = %v, want %v", tt.requested, got, tt.want)
			}
		})
	}
}

func TestAccountSupportsUpstreamModel_DeduplicatesAndNormalizesSnapshot(t *testing.T) {
	tests := []struct {
		name string
		raw  any
		want []string
	}{
		{name: "string slice", raw: []string{" b ", "a", "b", "A", "", " "}, want: []string{"A", "a", "b"}},
		{name: "mixed slice", raw: []any{" a ", "a", json.Number("123"), float64(456), "123", "", nil, true, map[string]any{"id": "ignored"}}, want: []string{"123", "456", "a"}},
		{name: "json numbers preserve spelling and precision", raw: []any{json.Number("1.25"), json.Number("9007199254740993"), float64(456.5), int(7), int64(8)}, want: []string{"1.25", "456.5", "7", "8", "9007199254740993"}},
		{name: "json array string", raw: `[" b ","a","b",9007199254740993]`, want: []string{"9007199254740993", "a", "b"}},
		{name: "raw json array", raw: json.RawMessage(`[" b ","a","b",9007199254740993]`), want: []string{"9007199254740993", "a", "b"}},
		{name: "model strings are literal", raw: []any{`["model-a"]`, `"model-b"`, "a,b", "models/model-c"}, want: []string{`"model-b"`, `["model-a"]`, "a,b", "models/model-c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{Extra: map[string]any{
				UpstreamModelsExtraKey:           tt.raw,
				UpstreamModelsSyncStatusExtraKey: UpstreamModelSyncStatusFresh,
			}}
			capability := account.UpstreamModelCapability()
			if capability.Status != UpstreamModelSyncStatusFresh || !slices.Equal(capability.Models, tt.want) {
				t.Fatalf("capability = %+v, want fresh with models %#v", capability, tt.want)
			}
			for _, model := range tt.want {
				if !account.SupportsUpstreamModel(" " + model + " ") {
					t.Errorf("normalized model %q should be supported", model)
				}
			}
			if account.SupportsUpstreamModel("absent") {
				t.Fatal("a normalized snapshot must still reject absent models")
			}
		})
	}
}

func TestAccountSupportsUpstreamModel_Metadata(t *testing.T) {
	wantTime := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	tests := []struct {
		name string
		raw  any
		want time.Time
	}{
		{name: "rfc3339", raw: " 2026-01-02T03:04:05Z ", want: wantTime},
		{name: "unix string", raw: strconv.FormatInt(wantTime.Unix(), 10), want: wantTime},
		{name: "json number", raw: json.Number(strconv.FormatInt(wantTime.Unix(), 10)), want: wantTime},
		{name: "json float", raw: float64(wantTime.Unix()), want: wantTime},
		{name: "integer", raw: wantTime.Unix(), want: wantTime},
		{name: "time value", raw: wantTime, want: wantTime},
		{name: "missing"},
		{name: "malformed", raw: "not-a-time"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{Extra: map[string]any{
				UpstreamModelsExtraKey:           []string{"model-a"},
				UpstreamModelsSyncStatusExtraKey: "fresh",
				UpstreamModelsSyncedAtExtraKey:   tt.raw,
				UpstreamModelsSourceExtraKey:     " upstream-api ",
			}}
			capability := account.UpstreamModelCapability()
			if !capability.SyncedAt.Equal(tt.want) || capability.Source != "upstream-api" {
				t.Fatalf("metadata = %+v, want synced at %v and source upstream-api", capability, tt.want)
			}
			if !account.SupportsUpstreamModel("model-a") || account.SupportsUpstreamModel("absent") {
				t.Fatal("timestamps are metadata, not an additional capability gate or TTL")
			}
		})
	}
	t.Run("malformed source is ignored", func(t *testing.T) {
		account := &Account{Extra: map[string]any{UpstreamModelsSourceExtraKey: map[string]any{"invalid": true}}}
		if got := account.UpstreamModelCapability().Source; got != "" {
			t.Fatalf("malformed source = %q, want empty", got)
		}
	})
}

func TestAccountSupportsUpstreamModel_DoesNotReplaceExistingModelPolicy(t *testing.T) {
	tests := []struct {
		name    string
		account Account
		model   string
		want    bool
	}{
		{name: "api key no mapping", account: Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, model: "custom", want: true},
		{name: "oauth foreign model", account: Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, model: "deepseek-v4"},
		{name: "explicit allowlist", account: Account{Credentials: map[string]any{"model_mapping": map[string]any{"other": "other-upstream"}}}, model: "custom"},
		{name: "passthrough leftover mapping", account: Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"openai_passthrough": true}, Credentials: map[string]any{"model_mapping": map[string]any{"other": "other-upstream"}}}, model: "custom", want: true},
		{name: "anthropic no mapping", account: Account{Platform: PlatformAnthropic}, model: "custom", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &tt.account
			if !account.SupportsUpstreamModel(tt.model) {
				t.Fatal("accounts without a snapshot must not gain a new restriction")
			}
			if got := account.IsModelSupported(tt.model); got != tt.want {
				t.Fatalf("existing model policy = %v, want %v", got, tt.want)
			}
			if account.Extra == nil {
				account.Extra = map[string]any{}
			}
			account.Extra[UpstreamModelsExtraKey] = []string{tt.model}
			account.Extra[UpstreamModelsSyncStatusExtraKey] = "fresh"
			if !account.SupportsUpstreamModel(tt.model) {
				t.Fatal("the capability helper must not apply the independent existing model policy")
			}
			if got := account.IsModelSupported(tt.model); got != tt.want {
				t.Fatalf("snapshot changed the existing model policy to %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAccountSupportsUpstreamModel_SnapshotIsReadOnlyAndObservesUpdates(t *testing.T) {
	rawModels := []string{" model-b ", "model-a", "model-a"}
	account := &Account{
		Credentials: map[string]any{"model_mapping": map[string]any{"public": "model-a"}},
		Extra: map[string]any{
			UpstreamModelsExtraKey:           rawModels,
			UpstreamModelsSyncStatusExtraKey: "fresh",
		},
	}
	capability := account.UpstreamModelCapability()
	capability.Models[0] = "changed"
	if !slices.Equal(rawModels, []string{" model-b ", "model-a", "model-a"}) {
		t.Fatalf("parsing mutated the stored model slice: %#v", rawModels)
	}
	if !account.SupportsUpstreamModel("public") || account.SupportsUpstreamModel("changed") {
		t.Fatal("returned model slices must not alias the stored snapshot")
	}
	account.Credentials["model_mapping"].(map[string]any)["public"] = "model-c"
	if account.SupportsUpstreamModel("public") {
		t.Fatal("capability checks must observe an updated account mapping")
	}
	account.Extra[UpstreamModelsExtraKey] = []any{"model-c"}
	account.Extra[UpstreamModelsSyncStatusExtraKey] = "stale"
	if !account.SupportsUpstreamModel("public") || account.SupportsUpstreamModel("model-a") {
		t.Fatal("capability checks must observe a replaced snapshot")
	}
}

func TestAccountSupportsUpstreamModel_ConcurrentReaders(t *testing.T) {
	account := &Account{
		Credentials: map[string]any{"model_mapping": map[string]any{"public": "model-a"}},
		Extra: map[string]any{
			UpstreamModelsExtraKey:           []any{" model-a ", "model-a"},
			UpstreamModelsSyncStatusExtraKey: "fresh",
		},
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 100 {
				if !account.SupportsUpstreamModel("public") || account.SupportsUpstreamModel("absent") {
					t.Error("concurrent capability check returned an inconsistent result")
					return
				}
			}
		})
	}
	wg.Wait()
}
