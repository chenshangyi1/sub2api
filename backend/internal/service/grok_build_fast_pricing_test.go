//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// Catch accidental fallback to Grok 4.6 or a lower dynamic catalog card at the
// actual token-cost entry point, including requests that have no resolver.
func TestGrokBuildFastSiteCardRequestBilling(t *testing.T) {
	for _, catalog := range []map[string]*LiteLLMModelPricing{
		nil,
		{"grok-4.7-build-fast": {InputCostPerToken: 2e-6, OutputCostPerToken: 6e-6, CacheReadInputTokenCost: 0.5e-6}},
		{"grok-4.7-build-fast": {InputCostPerToken: 2e-6, OutputCostPerToken: 6e-6, CacheReadInputTokenCost: 0.5e-6,
			LongContextInputTokenThreshold: 200000, LongContextInputCostMultiplier: 2, LongContextOutputCostMultiplier: 2}},
	} {
		bs := NewBillingService(&config.Config{}, &PricingService{pricingData: catalog})
		for _, model := range []string{"grok-4.7-build-fast", " XAI/GROK-4.7-BUILD-FAST ", "x-ai/grok-4.7-build-fast", "grok/grok-4.7-build-fast"} {
			for _, withResolver := range []bool{false, true} {
				var resolver *ModelPricingResolver
				if withResolver {
					resolver = NewModelPricingResolver(nil, bs)
				}
				for _, tc := range []struct{ rate, actual float64 }{{0, 0}, {0.2, 0.0007144}, {1, 0.003572}} {
					cost, err := bs.CalculateTokenCostForRequest(TokenCostRequest{
						Ctx: context.Background(), Model: model, Resolver: resolver, RateMultiplier: tc.rate,
						Group:  &Group{ID: 8},
						Tokens: UsageTokens{InputTokens: 188, OutputTokens: 139, CacheReadTokens: 1152},
					})
					require.NoError(t, err, model)
					require.InDelta(t, 0.000752, cost.InputCost, 1e-12, model)
					require.InDelta(t, 0.001668, cost.OutputCost, 1e-12, model)
					require.InDelta(t, 0.001152, cost.CacheReadCost, 1e-12, model)
					require.InDelta(t, 0.003572, cost.TotalCost, 1e-12, model)
					require.InDelta(t, tc.actual, cost.ActualCost, 1e-12, model)
				}
			}
			require.True(t, bs.HasIdentifiedTokenPricing(model), model)
			p, source, err := bs.getModelPricingWithSource(model)
			require.NoError(t, err)
			require.Equal(t, PricingSourceFallback, source)
			require.Zero(t, p.LongContextInputThreshold)
			require.Zero(t, p.LongContextInputMultiplier)
			require.Zero(t, p.LongContextOutputMultiplier)
		}
	}
}

func TestGrokBuildFastSiteCardDoesNotBorrowOldLongContextTier(t *testing.T) {
	bs := NewBillingService(&config.Config{}, nil)
	resolver := NewModelPricingResolver(nil, bs)
	// Check both sides of the old threshold; a million-token request alone
	// could pass accidentally because the old card doubled to the same rates.
	for _, input := range []int{199999, 200000, 200001, 1000000} {
		cost, err := bs.CalculateTokenCostForRequest(TokenCostRequest{
			Model: "grok-4.7-build-fast", Resolver: resolver, RateMultiplier: 0.2,
			Group:  &Group{ID: 8, LongContextPricingEnabled: true},
			Tokens: UsageTokens{InputTokens: input, OutputTokens: 1000},
		})
		require.NoError(t, err)
		want := float64(input)*4e-6 + 1000*12e-6
		require.InDelta(t, want, cost.TotalCost, 1e-12)
		require.InDelta(t, want*0.2, cost.ActualCost, 1e-12)
		require.False(t, cost.LongContextBillingApplied)
	}
	for _, tokens := range []UsageTokens{
		{}, {CacheReadTokens: 1000000},
		{InputTokens: 1000000, OutputTokens: 1000000, CacheReadTokens: 1000000},
	} {
		cost, err := bs.CalculateTokenCostForRequest(TokenCostRequest{
			Model: "grok-4.7-build-fast", Resolver: resolver, RateMultiplier: 0.2,
			Group: &Group{ID: 8, LongContextPricingEnabled: true}, Tokens: tokens,
		})
		require.NoError(t, err)
		if tokens.InputTokens > 0 {
			require.InDelta(t, 17, cost.TotalCost, 1e-12)
			require.InDelta(t, 3.4, cost.ActualCost, 1e-12)
		} else if tokens.CacheReadTokens > 0 {
			require.InDelta(t, 1, cost.TotalCost, 1e-12)
			require.InDelta(t, 0.2, cost.ActualCost, 1e-12)
		} else {
			require.Zero(t, cost.TotalCost)
			require.Zero(t, cost.ActualCost)
		}
	}
}

func TestGrokBuildFastSiteCardPartialAndFreeGroupOverrides(t *testing.T) {
	bs := NewBillingService(&config.Config{}, nil)
	zero := 0.0
	for _, tc := range []struct {
		name string
		card ChannelModelPricing
		want float64
	}{
		{"partial", ChannelModelPricing{InputPrice: &zero}, 13},
		{"free", ChannelModelPricing{InputPrice: &zero, OutputPrice: &zero, CacheReadPrice: &zero}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card := tc.card
			card.Models = []string{"xai/grok-4.7-build-fast"}
			card.BillingMode = BillingModeToken
			cost, err := bs.CalculateTokenCostForRequest(TokenCostRequest{
				Model: "xai/grok-4.7-build-fast", Resolver: NewModelPricingResolver(nil, bs), RateMultiplier: 0.2,
				Group:  &Group{ID: 8, ModelPricing: []ChannelModelPricing{card}},
				Tokens: UsageTokens{InputTokens: 1000000, OutputTokens: 1000000, CacheReadTokens: 1000000},
			})
			require.NoError(t, err)
			require.InDelta(t, tc.want, cost.TotalCost, 1e-12)
			require.InDelta(t, tc.want*0.2, cost.ActualCost, 1e-12)
		})
	}
}

func TestGrokBuildFastSiteCardKeepsExplicitOverridesAndOtherModels(t *testing.T) {
	bs := NewBillingService(&config.Config{}, nil)
	input, output, cache := 1e-6, 2e-6, 0.1e-6
	card := ChannelModelPricing{Models: []string{"grok-4.7-build-fast"}, BillingMode: BillingModeToken,
		InputPrice: &input, OutputPrice: &output, CacheReadPrice: &cache}
	p, err := bs.GetModelPricingWithChannel("grok-4.7-build-fast", &card)
	require.NoError(t, err)
	require.Equal(t, input, p.InputPricePerToken)
	require.Equal(t, output, p.OutputPricePerToken)
	require.Equal(t, cache, p.CacheReadPricePerToken)
	cost, err := bs.CalculateTokenCostForRequest(TokenCostRequest{
		Model: "grok-4.7-build-fast", Resolver: NewModelPricingResolver(nil, bs), RateMultiplier: 0.2,
		Group:  &Group{ID: 8, ModelPricing: []ChannelModelPricing{card}},
		Tokens: UsageTokens{InputTokens: 1000000, OutputTokens: 1000000, CacheReadTokens: 1000000},
	})
	require.NoError(t, err)
	require.InDelta(t, 3.1, cost.TotalCost, 1e-12)
	require.InDelta(t, 0.62, cost.ActualCost, 1e-12)
	for _, model := range []string{"grok-4.6", "grok-4.7", "grok-4.7-build-fast-unknown"} {
		p, err := bs.GetModelPricing(model)
		require.NoError(t, err)
		require.InDelta(t, 2e-6, p.InputPricePerToken, 1e-15, model)
		require.InDelta(t, 6e-6, p.OutputPricePerToken, 1e-15, model)
		require.InDelta(t, 0.5e-6, p.CacheReadPricePerToken, 1e-15, model)
	}
	// A custom group must not change the shared default card.
	p, err = bs.GetModelPricing("grok-4.7-build-fast")
	require.NoError(t, err)
	require.InDelta(t, 4e-6, p.InputPricePerToken, 1e-15)
}
