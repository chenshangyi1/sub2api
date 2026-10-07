//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGetModelPricing_DeepseekV32ProviderAliasesUseSiteBaseCard(t *testing.T) {
	bs := NewBillingService(&config.Config{}, &PricingService{})
	for _, model := range []string{
		"deepseek-ai/DeepSeek-V3.2",
		"deepseek-ai/deepseek-v3.2",
		"deepseek-v3-2",
		"Pro/deepseek-ai/DeepSeek-V3.2",
		"deepseek-v3-2-251201",
	} {
		pricing, err := bs.GetModelPricing(model)
		require.NoError(t, err, model)
		require.True(t, bs.HasIdentifiedTokenPricing(model), model)
		require.InDelta(t, 4e-6, pricing.InputPricePerToken, 1e-15, model)
		require.InDelta(t, 6e-6, pricing.OutputPricePerToken, 1e-15, model)
		require.InDelta(t, 0.4e-6, pricing.CacheReadPricePerToken, 1e-15, model)
	}
}

func TestDeepseekV32PolicyLeavesOtherModelsAndUnknownSuffixesAlone(t *testing.T) {
	catalog := map[string]*LiteLLMModelPricing{}
	for _, model := range []string{"deepseek-v4-pro", "deepseek-v4-flash", "deepseek-v4.1-flash", "deepseek-chat", "gpt-5.4"} {
		catalog[model] = &LiteLLMModelPricing{InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, CacheReadInputTokenCost: 0.1e-6}
	}
	bs := NewBillingService(&config.Config{}, &PricingService{pricingData: catalog})
	for model := range catalog {
		pricing, err := bs.GetModelPricing(model)
		require.NoError(t, err, model)
		require.InDelta(t, 1e-6, pricing.InputPricePerToken, 1e-15, model)
		require.InDelta(t, 2e-6, pricing.OutputPricePerToken, 1e-15, model)
		require.InDelta(t, 0.1e-6, pricing.CacheReadPricePerToken, 1e-15, model)
	}
	for _, model := range []string{"deepseek-v3.2-unknown", "deepseek-ai/DeepSeek-V3.2-fake"} {
		_, err := bs.GetModelPricing(model)
		require.ErrorIs(t, err, ErrModelPricingUnavailable, model)
		require.False(t, bs.HasIdentifiedTokenPricing(model), model)
	}
}

func TestDeepseekV32SiteBaseCardIgnoresDynamicUSDPrices(t *testing.T) {
	for _, catalog := range []map[string]*LiteLLMModelPricing{
		{"deepseek-v3-2-251201": {InputCostPerToken: 0, OutputCostPerToken: 0}},
		{"deepseek-v3.2": {InputCostPerToken: 0.28e-6, OutputCostPerToken: 0.42e-6, CacheReadInputTokenCost: 0.028e-6}},
		{"deepseek-v4-flash": {InputCostPerToken: 0.22e-6, OutputCostPerToken: 0.66e-6, CacheReadInputTokenCost: 0.007e-6}},
	} {
		bs := NewBillingService(&config.Config{}, &PricingService{pricingData: catalog})
		resolver := NewModelPricingResolver(nil, bs)
		for _, model := range []string{"deepseek-v3.2", "deepseek-ai/deepseek-v3.2", "Pro/deepseek-ai/DeepSeek-V3.2", "deepseek-v3-2-251201"} {
			for _, multiplier := range []float64{0.9, 1, 1.3} {
				cost, err := bs.CalculateCostUnified(CostInput{
					Ctx: context.Background(), Model: model, Resolver: resolver, RateMultiplier: multiplier,
					Tokens: UsageTokens{InputTokens: 1000000, OutputTokens: 1000000, CacheReadTokens: 1000000},
				})
				require.NoError(t, err, model)
				require.InDelta(t, 4, cost.InputCost, 1e-10, model)
				require.InDelta(t, 6, cost.OutputCost, 1e-10, model)
				require.InDelta(t, 0.4, cost.CacheReadCost, 1e-10, model)
				require.InDelta(t, 10.4, cost.TotalCost, 1e-10, model)
				require.InDelta(t, 10.4*multiplier, cost.ActualCost, 1e-10, model)
			}
		}
	}
}

func TestDeepseekV32SiteBaseCardPreservesConfiguredOverrides(t *testing.T) {
	bs := NewBillingService(&config.Config{}, &PricingService{})
	input, output, cache := 1e-6, 2e-6, 0.1e-6
	card := ChannelModelPricing{Models: []string{"deepseek-ai/DeepSeek-V3.2"}, BillingMode: BillingModeToken,
		InputPrice: &input, OutputPrice: &output, CacheReadPrice: &cache}
	pricing, err := bs.GetModelPricingWithChannel("deepseek-ai/DeepSeek-V3.2", &card)
	require.NoError(t, err)
	require.Equal(t, input, pricing.InputPricePerToken)
	require.Equal(t, output, pricing.OutputPricePerToken)
	require.Equal(t, cache, pricing.CacheReadPricePerToken)
	resolver := NewModelPricingResolver(nil, bs)
	group := &Group{ID: 1, ModelPricing: []ChannelModelPricing{card}}
	cost, err := bs.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "deepseek-ai/DeepSeek-V3.2", Group: group, Resolver: resolver,
		Tokens: UsageTokens{InputTokens: 1000000, OutputTokens: 1000000, CacheReadTokens: 1000000}, RateMultiplier: 1.3,
	})
	require.NoError(t, err)
	require.InDelta(t, 3.1, cost.TotalCost, 1e-10)
	require.InDelta(t, 4.03, cost.ActualCost, 1e-10)
	base, err := bs.GetModelPricing("deepseek-ai/DeepSeek-V3.2")
	require.NoError(t, err)
	require.InDelta(t, 4e-6, base.InputPricePerToken, 1e-15)
}
