package service

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCalculateAdaptiveUserChargeUsesUserMultiplierOverride(t *testing.T) {
	userMultiplier := 1.25
	got, err := CalculateAdaptiveUserCharge(2, 3, &userMultiplier, 10)

	require.NoError(t, err)
	require.InDelta(t, 2, got.OriginalCost, 1e-12)
	require.InDelta(t, 1.25, got.EffectiveUserMultiplier, 1e-12)
	require.InDelta(t, 2.5, got.UserCostBeforeFee, 1e-12)
	require.InDelta(t, 0.25, got.ServiceFee, 1e-12)
	require.InDelta(t, 2.75, got.UserCharge, 1e-12)
}

func TestCalculateAdaptiveUserChargeUsesLeafMultiplierWhenUserOverrideMissing(t *testing.T) {
	got, err := CalculateAdaptiveUserCharge(2, 1.5, nil, 0)

	require.NoError(t, err)
	require.InDelta(t, 1.5, got.EffectiveUserMultiplier, 1e-12)
	require.InDelta(t, 3, got.UserCostBeforeFee, 1e-12)
	require.InDelta(t, 3, got.UserCharge, 1e-12)
}

func TestCalculateAdaptiveUserChargeSeparatesUpstreamAccountCost(t *testing.T) {
	got, err := CalculateAdaptiveUserCharge(2, 1.5, nil, 15)

	require.NoError(t, err)
	require.InDelta(t, 2.3, CalculateAdaptiveUpstreamCost(2, 1.15), 1e-12)
	require.InDelta(t, 3, got.UserCostBeforeFee, 1e-12)
	require.InDelta(t, 3.45, got.UserCharge, 1e-12)
}

func TestCalculateAdaptiveUserChargeRejectsInvalidInputs(t *testing.T) {
	for _, input := range []struct {
		name string
		cost float64
		leaf float64
		fee  float64
	}{
		{name: "nan cost", cost: math.NaN(), leaf: 1, fee: 0},
		{name: "negative leaf", cost: 1, leaf: -1, fee: 0},
		{name: "fee above one hundred percent", cost: 1, leaf: 1, fee: 100.01},
	} {
		t.Run(input.name, func(t *testing.T) {
			_, err := CalculateAdaptiveUserCharge(input.cost, input.leaf, nil, input.fee)
			require.ErrorIs(t, err, ErrAdaptiveBillingInvalidAmount)
		})
	}
}
