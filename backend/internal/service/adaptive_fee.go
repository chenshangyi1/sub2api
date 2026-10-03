package service

import (
	"math"
)

// AdaptiveChargeBreakdown separates the original model cost from the user
// charge. Account multipliers are intentionally absent: they belong to
// upstream/profit accounting, never to the Adaptive customer charge.
type AdaptiveChargeBreakdown struct {
	OriginalCost            float64
	EffectiveUserMultiplier float64
	UserCostBeforeFee       float64
	ServiceFee              float64
	UserCharge              float64
}

// CalculateAdaptiveUserCharge applies a user-specific group multiplier when
// present, otherwise the selected leaf group's default multiplier. The service
// fee is an Adaptive-only percentage applied after the user multiplier.
func CalculateAdaptiveUserCharge(originalCost, leafMultiplier float64, userMultiplier *float64, serviceFeePercent float64) (AdaptiveChargeBreakdown, error) {
	for _, value := range []float64{originalCost, leafMultiplier, serviceFeePercent} {
		if err := validateAdaptiveBillingFloat(value); err != nil {
			return AdaptiveChargeBreakdown{}, err
		}
	}
	if serviceFeePercent > 100 {
		return AdaptiveChargeBreakdown{}, ErrAdaptiveBillingInvalidAmount
	}
	effectiveMultiplier := leafMultiplier
	if userMultiplier != nil {
		if err := validateAdaptiveBillingFloat(*userMultiplier); err != nil {
			return AdaptiveChargeBreakdown{}, err
		}
		effectiveMultiplier = *userMultiplier
	}
	userCost := originalCost * effectiveMultiplier
	fee := userCost * serviceFeePercent / 100
	return AdaptiveChargeBreakdown{
		OriginalCost:            originalCost,
		EffectiveUserMultiplier: effectiveMultiplier,
		UserCostBeforeFee:       userCost,
		ServiceFee:              fee,
		UserCharge:              userCost + fee,
	}, nil
}

// CalculateAdaptiveUpstreamCost applies the account multiplier only to the
// original model cost used for upstream/profit accounting.
func CalculateAdaptiveUpstreamCost(originalCost, accountMultiplier float64) float64 {
	if math.IsNaN(originalCost) || math.IsInf(originalCost, 0) || math.IsNaN(accountMultiplier) || math.IsInf(accountMultiplier, 0) {
		return 0
	}
	return originalCost * accountMultiplier
}
