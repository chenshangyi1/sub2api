package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdaptiveSettlementBaseCostUsesOriginalModelCost(t *testing.T) {
	cost := &CostBreakdown{TotalCost: 2.5, ActualCost: 7.5}

	require.Equal(t, 2.5, adaptiveSettlementBaseCost(cost))
}

func TestAdaptiveSettlementBaseCostUsesFrozenUserMultiplier(t *testing.T) {
	cost := &CostBreakdown{TotalCost: 2.5, ActualCost: 7.5}
	billing := &AdaptiveBillingContext{UserRateMultiplier: 3, UserRateMultiplierSet: true}

	require.Equal(t, 7.5, adaptiveSettlementBaseCostForBilling(cost, billing))
}
