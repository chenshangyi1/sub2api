//go:build unit

package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

func TestIsDeepSeekModel(t *testing.T) {
	deepseek := []string{
		"deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp",
		"deepseek-chat", "deepseek-reasoner", "deepseek-v3-2-251201",
		"deepseek-coder", "deepseek-foo", "deepseek-v4-pro-0813",
		"DEEPSEEK-V4-PRO", " deepseek-v4-flash ",
	}
	for _, m := range deepseek {
		require.True(t, isDeepSeekModel(m), "model %q should be deepseek", m)
	}

	nonDeepseek := []string{
		"gpt-5.4", "claude-sonnet-4", "deepseekcoder", // 无连字符不算 deepseek- 前缀
		"", " deepseek", // 无连字符后缀
	}
	for _, m := range nonDeepseek {
		require.False(t, isDeepSeekModel(m), "model %q should not be deepseek", m)
	}
}

func TestNormalizeDeepSeekModelName(t *testing.T) {
	tests := map[string]string{
		"deepseek-ai/DeepSeek-V3.2":     "deepseek-v3.2",
		"DEEPSEEK-V3-2":                 "deepseek-v3.2",
		"deepseek_ai_DeepSeek_V4_Pro":   "deepseek-v4-pro",
		"deepseek-v4pro":                "deepseek-v4-pro",
		"deepseek-ai/deepseek-v4-flash": "deepseek-v4-flash",
		"deepseek-flash":                "deepseek-v4.1-flash",
		"gpt-5.4":                       "gpt-5.4",
	}
	for input, want := range tests {
		require.Equal(t, want, normalizeDeepSeekModelName(input), input)
	}
}

// TestGetModelPricing_DeepseekFlashAliasKeeps303Rates 锁定价格回退后的别名语义：
// deepseek-flash 保留为可用模型名，但仍沿用 303 的 Flash 三档价格。
func TestGetModelPricing_DeepseekFlashAliasKeeps303Rates(t *testing.T) {
	bs := NewBillingService(&config.Config{}, &PricingService{})

	alias, err := bs.GetModelPricing("deepseek-flash")
	require.NoError(t, err)
	legacy, err := bs.GetModelPricing("deepseek-v4-flash")
	require.NoError(t, err)

	for name, pricing := range map[string]*ModelPricing{
		"deepseek-flash":    alias,
		"deepseek-v4-flash": legacy,
	} {
		require.InDelta(t, 2.2e-7, pricing.InputPricePerToken, 1e-15, name)
		require.InDelta(t, 6.6e-7, pricing.OutputPricePerToken, 1e-15, name)
		require.InDelta(t, 7e-9, pricing.CacheReadPricePerToken, 1e-15, name)
	}
}

func TestGetModelPricing_DeepseekV41FlashAliases(t *testing.T) {
	bs := NewBillingService(&config.Config{}, &PricingService{})

	for _, model := range []string{"deepseek-flash", "deepseek-v4.1-flash", "deepseek-v4.1"} {
		pricing, err := bs.GetModelPricing(model)
		require.NoError(t, err, model)
		require.InDelta(t, 0.15e-6, pricing.InputPricePerToken, 1e-15, model)
		require.InDelta(t, 0.60e-6, pricing.OutputPricePerToken, 1e-15, model)
		require.InDelta(t, 0.003e-6, pricing.CacheReadPricePerToken, 1e-15, model)
	}
}

func TestGetModelPricing_DeepseekV4ProAliasesUseOfficialCard(t *testing.T) {
	bs := NewBillingService(&config.Config{}, &PricingService{})
	for _, model := range []string{"deepseek-ai/DeepSeek-V4-Pro", "DEEPSEEK-V4PRO", "deepseek_ai_DeepSeek_V4_Pro"} {
		pricing, err := bs.GetModelPricing(model)
		require.NoError(t, err, model)
		require.InDelta(t, 4.35e-7, pricing.InputPricePerToken, 1e-15, model)
		require.InDelta(t, 8.7e-7, pricing.OutputPricePerToken, 1e-15, model)
	}
}

func TestGetModelPricing_UnknownDeepSeekDoesNotUseLowFlashFallback(t *testing.T) {
	bs := NewBillingService(&config.Config{}, &PricingService{})
	_, err := bs.GetModelPricing("DeepSeek-V9-Unknown")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// 动态目录/本地兜底的 DeepSeek 标准价不按时段重复加倍；分组/渠道自定义价也不叠加
// ---------------------------------------------------------------------------

func TestCalculateCostUnified_DeepseekDefaultCardUsesStandardPriceAtAnyTime(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 1000}
	offPeakTotal := 1000*2.2e-7 + 500*6.6e-7 + 1000*7e-9

	offPeak, err := bs.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "deepseek-v4-flash", Tokens: tokens,
		RateMultiplier: 1.0, Resolver: resolver,
		PricingAt: time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC), // 周一低谷
	})
	require.NoError(t, err)
	require.InDelta(t, offPeakTotal, offPeak.TotalCost, 1e-10)

	peak, err := bs.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "deepseek-v4-flash", Tokens: tokens,
		RateMultiplier: 1.0, Resolver: resolver,
		PricingAt: time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC), // 周一高峰
	})
	require.NoError(t, err)
	require.InDelta(t, offPeakTotal, peak.TotalCost, 1e-10)
}

func TestCalculateCostUnified_DeepseekProDefaultCardUsesStandardPriceAtAnyTime(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 1000}
	offPeakTotal := 1000*6.6e-7 + 500*1.98e-6 + 1000*2.2e-8

	offPeak, err := bs.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "deepseek-v4-pro", Tokens: tokens,
		RateMultiplier: 1.0, Resolver: resolver,
		PricingAt: time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.InDelta(t, offPeakTotal, offPeak.TotalCost, 1e-10)

	peak, err := bs.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "deepseek-v4-pro", Tokens: tokens,
		RateMultiplier: 1.0, Resolver: resolver,
		PricingAt: time.Date(2026, 8, 24, 6, 30, 0, 0, time.UTC), // 周一高峰
	})
	require.NoError(t, err)
	require.InDelta(t, offPeakTotal, peak.TotalCost, 1e-10)
}

func TestCalculateCostUnified_DeepseekVersionedNameUsesStandardPriceAtAnyTime(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 1000}
	offPeakTotal := 1000*2.2e-7 + 500*6.6e-7 + 1000*7e-9

	offPeak, err := bs.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "deepseek-v4-flash-0731", Tokens: tokens,
		RateMultiplier: 1.0, Resolver: resolver,
		PricingAt: time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC), // 周一低谷
	})
	require.NoError(t, err)
	require.InDelta(t, offPeakTotal, offPeak.TotalCost, 1e-10)

	peak, err := bs.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "deepseek-v4-flash-0731", Tokens: tokens,
		RateMultiplier: 1.0, Resolver: resolver,
		PricingAt: time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC), // 周一高峰
	})
	require.NoError(t, err)
	require.InDelta(t, offPeakTotal, peak.TotalCost, 1e-10)
}

func TestCalculateCostUnified_DeepseekGroupPricingNotScaledByPeak(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	inputPrice := 1e-6
	outputPrice := 2e-6
	group := &Group{
		ID: 1, Name: "ds-group", Platform: PlatformDeepseek, Status: StatusActive,
		ModelPricing: []ChannelModelPricing{{
			Models: []string{"deepseek-v4-flash"}, BillingMode: BillingModeToken,
			InputPrice: &inputPrice, OutputPrice: &outputPrice,
		}},
	}
	resolved := resolver.Resolve(context.Background(), PricingInput{Model: "deepseek-v4-flash", Group: group})
	require.Equal(t, PricingSourceGroup, resolved.Source)

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 1000}
	// 分组自定义价：1000*1e-6 + 500*2e-6 + 1000*7e-9（缓存读沿用官方 flash 价）
	groupTotal := 1000*1e-6 + 500*2e-6 + 1000*7e-9

	for _, pricingAt := range []time.Time{
		time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC), // 低谷
		time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC),  // 高峰
	} {
		cost, err := bs.CalculateCostUnified(CostInput{
			Ctx: context.Background(), Model: "deepseek-v4-flash", Group: group,
			Tokens: tokens, RateMultiplier: 1.0, Resolver: resolver, PricingAt: pricingAt,
		})
		require.NoError(t, err)
		require.InDelta(t, groupTotal, cost.TotalCost, 1e-10,
			"分组自定义定价不应叠加官方峰谷倍率（pricingAt=%v）", pricingAt)
	}
}

func TestCalculateCostUnified_NonDeepseekDefaultCardNotScaledByPeak(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500}
	total := 1000*3e-6 + 500*15e-6 // claude-sonnet-4 fallback

	for _, pricingAt := range []time.Time{
		time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC),
	} {
		cost, err := bs.CalculateCostUnified(CostInput{
			Ctx: context.Background(), Model: "claude-sonnet-4", Tokens: tokens,
			RateMultiplier: 1.0, Resolver: resolver, PricingAt: pricingAt,
		})
		require.NoError(t, err)
		require.InDelta(t, total, cost.TotalCost, 1e-10,
			"非 DeepSeek 模型不应受官方峰谷倍率影响（pricingAt=%v）", pricingAt)
	}
}

func TestCalculateCostUnified_DeepseekPricingAtZeroFallsBackToNow(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500}
	base := CostInput{
		Ctx: context.Background(), Model: "deepseek-v4-flash", Tokens: tokens,
		RateMultiplier: 1.0, Resolver: resolver,
	}

	// PricingAt 零值 → 回退 timezone.Now()，与显式传入当前时刻结果一致。
	costZero, err := bs.CalculateCostUnified(base)
	require.NoError(t, err)

	costNow, err := bs.CalculateCostUnified(CostInput{
		Ctx: base.Ctx, Model: base.Model, Tokens: base.Tokens,
		RateMultiplier: base.RateMultiplier, Resolver: base.Resolver,
		PricingAt: timezone.Now(),
	})
	require.NoError(t, err)
	require.Equal(t, costZero.TotalCost, costNow.TotalCost)
}

// ---------------------------------------------------------------------------
// 动态上游标准价优先；未知 DeepSeek fail-closed
// ---------------------------------------------------------------------------

func TestGetModelPricing_DeepseekKeepsDynamicUpstreamRates(t *testing.T) {
	// 动态目录价格代表上游标准价，不能被本地官方价卡覆盖。
	pricingSvc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"deepseek-v4-flash":            {InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, CacheReadInputTokenCost: 1e-8},
		"deepseek-v4-pro":              {InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, CacheReadInputTokenCost: 1e-8},
		"deepseek-v4-flash-vision-exp": {InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, CacheReadInputTokenCost: 1e-8},
		"deepseek-chat":                {InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, CacheReadInputTokenCost: 1e-8},
		"deepseek-reasoner":            {InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, CacheReadInputTokenCost: 1e-8},
	}}
	bs := NewBillingService(&config.Config{}, pricingSvc)

	tests := []struct {
		model                    string
		input, output, cacheRead float64
	}{
		{"deepseek-v4-flash", 1e-6, 2e-6, 1e-8},
		{"deepseek-v4-flash-vision-exp", 1e-6, 2e-6, 1e-8},
		{"deepseek-v4-pro", 1e-6, 2e-6, 1e-8},
		{"deepseek-chat", 1e-6, 2e-6, 1e-8},
		{"deepseek-reasoner", 1e-6, 2e-6, 1e-8},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			pricing, err := bs.GetModelPricing(tt.model)
			require.NoError(t, err)
			require.InDelta(t, tt.input, pricing.InputPricePerToken, 1e-15)
			require.InDelta(t, tt.output, pricing.OutputPricePerToken, 1e-15)
			require.InDelta(t, tt.cacheRead, pricing.CacheReadPricePerToken, 1e-15)
			require.True(t, bs.HasIdentifiedTokenPricing(tt.model))
		})
	}

	// 版本化名称若动态目录提供了精确条目，也保留动态价格。
	versioned := []struct {
		model                    string
		input, output, cacheRead float64
	}{
		{"deepseek-v4-pro-0813", 1e-6, 2e-6, 1e-8},
		{"deepseek-v4-flash-0731", 1e-6, 2e-6, 1e-8},
	}
	for _, tt := range versioned {
		t.Run(tt.model, func(t *testing.T) {
			pricing, err := bs.GetModelPricing(tt.model)
			require.NoError(t, err)
			require.InDelta(t, tt.input, pricing.InputPricePerToken, 1e-15)
			require.InDelta(t, tt.output, pricing.OutputPricePerToken, 1e-15)
			require.InDelta(t, tt.cacheRead, pricing.CacheReadPricePerToken, 1e-15)
		})
	}
}

func TestGetModelPricing_UnknownDeepseekFailsClosed(t *testing.T) {
	// JSON 含 $0 占位条目时，未知 DeepSeek 仍然不能低价兜底。
	pricingSvc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"deepseek-v3-2-251201": {InputCostPerToken: 0, OutputCostPerToken: 0},
	}}
	bs := NewBillingService(&config.Config{}, pricingSvc)

	for _, m := range []string{"deepseek-v9-unknown", "deepseek-foo"} {
		t.Run(m, func(t *testing.T) {
			_, err := bs.GetModelPricing(m)
			require.Error(t, err)
		})
	}
}

func TestIsKnownDeepSeekModel(t *testing.T) {
	for _, model := range []string{
		"deepseek-v3.2", "deepseek-ai/DeepSeek-V3-2-251201",
		"deepseek-v4-pro", "deepseek-v4-flash", "deepseek-v4.1-flash",
		"deepseek-chat", "deepseek-reasoner",
	} {
		require.True(t, isKnownDeepSeekModel(model), model)
	}
	for _, model := range []string{
		"deepseek-v9", "deepseek-foo", "deepseek-v4-proxy", "deepseek-v4-flash-vision-exp",
		"gpt-5.4",
	} {
		if model == "deepseek-v4-flash-vision-exp" {
			require.True(t, isKnownDeepSeekModel(model), model)
			continue
		}
		require.False(t, isKnownDeepSeekModel(model), model)
	}
}

// ---------------------------------------------------------------------------
// 本地兜底 JSON：无 $0 占位条目，官方模型价格为官方标准价
// ---------------------------------------------------------------------------

func TestDeepseekPricingFileMatchesOfficialRates(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"))
	require.NoError(t, err)

	pricingSvc := &PricingService{}
	pricingData, err := pricingSvc.parsePricingData(data)
	require.NoError(t, err)

	_, ok := pricingData["deepseek-v3-2-251201"]
	require.False(t, ok, "deepseek-v3-2-251201（$0 占位条目）必须从价格表中移除")
	for _, discontinued := range []string{"deepseek-chat", "deepseek-reasoner"} {
		_, ok := pricingData[discontinued]
		require.False(t, ok, "%s 已停止服务，必须从价格表中移除", discontinued)
	}

	tests := []struct {
		model                    string
		input, output, cacheRead float64
	}{
		{"deepseek-v4-flash", 2.2e-7, 6.6e-7, 7e-9},
		{"deepseek-v4-flash-vision-exp", 2.2e-7, 6.6e-7, 7e-9},
		{"deepseek-v4-pro", 6.6e-7, 1.98e-6, 2.2e-8},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			entry, ok := pricingData[tt.model]
			require.True(t, ok, "model %s must exist in pricing file", tt.model)
			require.InDelta(t, tt.input, entry.InputCostPerToken, 1e-15)
			require.InDelta(t, tt.output, entry.OutputCostPerToken, 1e-15)
			require.InDelta(t, tt.cacheRead, entry.CacheReadInputTokenCost, 1e-15)
		})
	}
}
