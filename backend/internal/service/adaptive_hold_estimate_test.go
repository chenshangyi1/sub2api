package service

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestEstimateAdaptiveHoldBaseFromPreauthorizationRequiresCalculator(t *testing.T) {
	t.Parallel()
	_, err := EstimateAdaptiveHoldBaseFromPreauthorization(nil, CostInput{Model: "gpt-4o"}, []byte(`{"model":"gpt-4o"}`))
	require.ErrorIs(t, err, ErrInvalidBillingPreauthorizationEstimate)
}

func TestEstimateBalancePreauthorizationTokensFeedsAdaptiveHold(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"max_tokens":128}`)
	tokens := EstimateBalancePreauthorizationTokens(body)
	require.GreaterOrEqual(t, tokens.InputTokens, DefaultBalancePreauthorizationInputTokens)
	require.Equal(t, 128, tokens.OutputTokens)
	require.True(t, decimal.NewFromInt(int64(tokens.InputTokens)).IsPositive())
}
