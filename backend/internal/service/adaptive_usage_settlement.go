package service

import (
	"context"
	"math"

	"github.com/shopspring/decimal"
)

func adaptiveSettlementBaseCost(cost *CostBreakdown) float64 {
	if cost == nil {
		return 0
	}
	return cost.TotalCost
}

func adaptiveSettlementBaseCostForBilling(cost *CostBreakdown, billing *AdaptiveBillingContext) float64 {
	base := adaptiveSettlementBaseCost(cost)
	if billing == nil || !billing.UserRateMultiplierSet {
		return base
	}
	return base * billing.UserRateMultiplier
}

// settleAdaptiveCustomerUsage finalizes an Adaptive request:
// base cost B is the original model-price cost before any account or user/group
// multiplier. Capture applies the frozen user multiplier and management fee.
// When coordinator is nil the path fails closed so Adaptive traffic cannot
// silently under-bill.
func settleAdaptiveCustomerUsage(
	ctx context.Context,
	coordinator *AdaptiveBillingCoordinator,
	billing *AdaptiveBillingContext,
	usageLog *UsageLog,
	baseActualCost float64,
) error {
	if billing == nil || billing.Probe {
		return nil
	}
	if coordinator == nil {
		return ErrAdaptiveBillingPathNotWired
	}
	if usageLog == nil {
		return ErrAdaptiveBillingContextInvalid
	}
	if err := billing.ValidateForSettlement(); err != nil {
		return err
	}

	base := decimal.Zero
	if !math.IsNaN(baseActualCost) && !math.IsInf(baseActualCost, 0) && baseActualCost > 0 {
		base = decimal.NewFromFloat(baseActualCost)
	}

	_, _, err := coordinator.Capture(ctx, billing, usageLog, base)
	return err
}

// ReleaseAdaptiveCustomerHold releases an authorized Adaptive hold when the
// request never produced verifiable customer usage (all attempts failed or
// cancelled before commit).
func ReleaseAdaptiveCustomerHold(
	ctx context.Context,
	coordinator *AdaptiveBillingCoordinator,
	billing *AdaptiveBillingContext,
	reason string,
) error {
	if billing == nil || billing.Probe {
		return nil
	}
	if coordinator == nil {
		return ErrAdaptiveBillingPathNotWired
	}
	_, err := coordinator.Release(ctx, billing, reason, billing.LastFailedEvidenceHash)
	return err
}
