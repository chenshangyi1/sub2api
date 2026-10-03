package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestDetectModelPlatform(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		platform string
		ok       bool
	}{
		{name: "claude", model: "claude-sonnet-4-5", platform: PlatformAnthropic, ok: true},
		{name: "anthropic prefix", model: "anthropic/claude-opus-4-5", platform: PlatformAnthropic, ok: true},
		{name: "gpt", model: "gpt-5.1", platform: PlatformOpenAI, ok: true},
		{name: "o series", model: "o3-mini", platform: PlatformOpenAI, ok: true},
		{name: "embedding", model: "text-embedding-3-large", platform: PlatformOpenAI, ok: true},
		{name: "gemini", model: "gemini-3-pro", platform: PlatformGemini, ok: true},
		{name: "gemini models prefix", model: "models/gemini-2.5-flash", platform: PlatformGemini, ok: true},
		{name: "learnlm", model: "learnlm-2.0-flash-experimental", platform: PlatformGemini, ok: true},
		{name: "grok", model: "grok-4", platform: PlatformGrok, ok: true},
		{name: "xai prefix", model: "xai/grok-4", platform: PlatformGrok, ok: true},
		{name: "kimi", model: "kimi-k2-thinking", platform: PlatformCN, ok: true},
		{name: "kimi code bare k3", model: "K3", platform: PlatformCN, ok: true},
		{name: "kimi code bare k3 256k", model: "k3-256k", platform: PlatformCN, ok: true},
		{name: "kimi code provider prefix", model: "kimi-code/k3", platform: PlatformCN, ok: true},
		{name: "moonshot prefix", model: "moonshot/moonshot-v1-32k", platform: PlatformCN, ok: true},
		{name: "zhipu", model: "glm-5.2", platform: PlatformCN, ok: true},
		{name: "deepseek", model: "deepseek-v4-pro", platform: PlatformCN, ok: true},
		{name: "minimax m series", model: "MiniMax-M2.7", platform: PlatformCN, ok: true},
		{name: "minimax provider prefix", model: "minimax/MiniMax-M2.5", platform: PlatformCN, ok: true},
		{name: "unknown k3 alias", model: "k3-preview", ok: false},
		{name: "unknown", model: "llama-4-maverick", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			platform, ok := DetectModelPlatform(tt.model)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.platform, platform)
		})
	}
}

func TestQuotaPlatformCompositeUsesResolvedOrForceOnly(t *testing.T) {
	apiKey := &APIKey{Group: &Group{Platform: PlatformComposite}}

	require.Equal(t, "", QuotaPlatform(context.Background(), apiKey))
	require.Equal(t, PlatformGemini, QuotaPlatform(WithResolvedTargetPlatform(context.Background(), PlatformGemini), apiKey))
	require.Equal(t, PlatformAntigravity, QuotaPlatform(context.WithValue(context.Background(), ctxkey.ForcePlatform, PlatformAntigravity), apiKey))

	ctx := WithResolvedTargetPlatform(context.Background(), PlatformAnthropic)
	ctx = context.WithValue(ctx, ctxkey.ForcePlatform, PlatformAntigravity)
	require.Equal(t, PlatformAntigravity, QuotaPlatform(ctx, apiKey))
}

func TestQuotaPlatformAdaptiveUsesResolvedOrForceOnly(t *testing.T) {
	apiKey := &APIKey{Group: &Group{Platform: PlatformAdaptive}}

	require.Equal(t, "", QuotaPlatform(context.Background(), apiKey))
	require.Equal(t, PlatformAnthropic, QuotaPlatform(WithResolvedTargetPlatform(context.Background(), PlatformAnthropic), apiKey))
	require.Equal(t, PlatformGemini, QuotaPlatform(context.WithValue(context.Background(), ctxkey.ForcePlatform, PlatformGemini), apiKey))
}

func TestCompositeGroupSchedulerHasAllCanonicalPlatformBuckets(t *testing.T) {
	seen := make(map[string]struct{})
	for _, bucket := range schedulerCanonicalBuckets(99) {
		seen[bucket.Platform] = struct{}{}
	}
	platforms := make([]string, 0, len(seen))
	for platform := range seen {
		platforms = append(platforms, platform)
	}
	require.ElementsMatch(t,
		[]string{PlatformAnthropic, PlatformGemini, PlatformOpenAI, PlatformAntigravity, PlatformGrok, PlatformCN, PlatformVideo},
		platforms,
	)
}

func TestCompositeConcretePlatformsIncludeCNProviders(t *testing.T) {
	for _, platform := range []string{PlatformCN, PlatformVideo, PlatformKimi, PlatformZhipu, PlatformDeepseek} {
		require.True(t, isConcreteRequestPlatform(platform), platform)
		require.True(t, canCopyAccountsFromGroupPlatform(PlatformComposite, platform), platform)
	}
	require.False(t, canCopyAccountsFromGroupPlatform(PlatformAdaptive, PlatformOpenAI))
	require.False(t, canCopyAccountsFromGroupPlatform(PlatformAdaptive, PlatformAnthropic))
}
