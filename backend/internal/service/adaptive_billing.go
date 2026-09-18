package service

import (
	"errors"
	"math"

	"github.com/shopspring/decimal"
)

const (
	// DefaultAdaptiveManagementFeeBPS is the immutable fee snapshot used when a
	// reservation does not explicitly carry one. 1,500 basis points is 15%.
	DefaultAdaptiveManagementFeeBPS int32 = 1500
	AdaptiveBillingMoneyScale       int32 = 10

	adaptiveBillingBPSDenominator int64 = 10000
)

var (
	ErrAdaptiveBillingInvalidAmount      = errors.New("adaptive billing amount must be finite and non-negative")
	ErrAdaptiveBillingInvalidBasisPoints = errors.New("adaptive billing basis points are invalid")
	ErrAdaptiveBillingCaptureExceedsHold = errors.New("adaptive billing capture exceeds hold")
)

// AdaptiveManagementFeeResult is the fixed-precision settlement snapshot for
// one Adaptive request. Its three monetary values always use 10 decimal places
// of precision, and CaptureAmount is exactly BaseAmount + FeeAmount.
type AdaptiveManagementFeeResult struct {
	BaseAmount       decimal.Decimal
	FeeAmount        decimal.Decimal
	CaptureAmount    decimal.Decimal
	ManagementFeeBPS int32
}

// AdaptiveManagementFeeSettlement separates the customer's authorized charge
// from an underestimated base amount absorbed by the platform.
type AdaptiveManagementFeeSettlement struct {
	UncappedBaseAmount        decimal.Decimal
	CustomerCharge            AdaptiveManagementFeeResult
	PlatformOverageBaseAmount decimal.Decimal
}

// CalculateAdaptiveManagementFee calculates the default 15% management fee.
// The float adapter rejects non-finite values before entering decimal math.
func CalculateAdaptiveManagementFee(baseAmount float64) (AdaptiveManagementFeeResult, error) {
	if err := validateAdaptiveBillingFloat(baseAmount); err != nil {
		return AdaptiveManagementFeeResult{}, err
	}
	return CalculateAdaptiveManagementFeeDecimal(decimal.NewFromFloat(baseAmount))
}

// CalculateAdaptiveManagementFeeDecimal calculates the default 15% fee using
// decimal fixed-point math. Settlement amounts use half-up rounding at scale 10.
func CalculateAdaptiveManagementFeeDecimal(baseAmount decimal.Decimal) (AdaptiveManagementFeeResult, error) {
	return CalculateAdaptiveManagementFeeDecimalWithBPS(baseAmount, DefaultAdaptiveManagementFeeBPS)
}

// CalculateAdaptiveManagementFeeDecimalWithBPS calculates a fee from the BPS
// snapshot stored on a reservation. New reservations default to 1,500 BPS;
// retaining the snapshot here makes retries independent of later configuration.
func CalculateAdaptiveManagementFeeDecimalWithBPS(baseAmount decimal.Decimal, bps int32) (AdaptiveManagementFeeResult, error) {
	return calculateAdaptiveManagementFeeDecimal(baseAmount, bps)
}

// CalculateAdaptiveManagementFeeHold returns a conservative reservation upper
// bound: ceil_10(maxCandidateUpperBound * 1.15).
func CalculateAdaptiveManagementFeeHold(maxCandidateUpperBound float64) (decimal.Decimal, error) {
	if err := validateAdaptiveBillingFloat(maxCandidateUpperBound); err != nil {
		return decimal.Zero, err
	}
	return CalculateAdaptiveManagementFeeHoldDecimal(decimal.NewFromFloat(maxCandidateUpperBound))
}

// CalculateAdaptiveManagementFeeHoldDecimal is the decimal fixed-point form of
// CalculateAdaptiveManagementFeeHold.
func CalculateAdaptiveManagementFeeHoldDecimal(maxCandidateUpperBound decimal.Decimal) (decimal.Decimal, error) {
	return CalculateAdaptiveManagementFeeHoldDecimalWithBPS(maxCandidateUpperBound, DefaultAdaptiveManagementFeeBPS)
}

// CalculateAdaptiveManagementFeeHoldDecimalWithBPS calculates a conservative
// hold using the same immutable BPS snapshot as settlement.
func CalculateAdaptiveManagementFeeHoldDecimalWithBPS(maxCandidateUpperBound decimal.Decimal, bps int32) (decimal.Decimal, error) {
	return calculateAdaptiveManagementFeeHoldDecimal(maxCandidateUpperBound, bps)
}

// CalculateAdaptiveManagementFeeSettlementDecimalWithBPS caps the customer
// charge at the component amounts authorized before the request. The uncapped
// base and platform overage remain available for cost and audit accounting.
func CalculateAdaptiveManagementFeeSettlementDecimalWithBPS(
	actualBase, heldBase, heldManagementFee, heldTotal decimal.Decimal,
	bps int32,
) (AdaptiveManagementFeeSettlement, error) {
	if actualBase.IsNegative() || heldBase.IsNegative() || heldManagementFee.IsNegative() || heldTotal.IsNegative() {
		return AdaptiveManagementFeeSettlement{}, ErrAdaptiveBillingInvalidAmount
	}
	if err := validateAdaptiveManagementFeeBPS(bps); err != nil {
		return AdaptiveManagementFeeSettlement{}, err
	}

	uncappedBase := actualBase.Round(AdaptiveBillingMoneyScale)
	authorizedBase := heldBase.RoundCeil(AdaptiveBillingMoneyScale)
	authorizedFee := heldManagementFee.RoundCeil(AdaptiveBillingMoneyScale)
	authorizedTotal := heldTotal.RoundCeil(AdaptiveBillingMoneyScale)
	if !authorizedBase.Add(authorizedFee).Equal(authorizedTotal) {
		return AdaptiveManagementFeeSettlement{}, ErrAdaptiveBillingInvalidAmount
	}

	billableBase := uncappedBase
	if billableBase.GreaterThan(authorizedBase) {
		billableBase = authorizedBase
	}
	charge, err := calculateAdaptiveManagementFeeDecimal(billableBase, bps)
	if err != nil {
		return AdaptiveManagementFeeSettlement{}, err
	}
	if charge.FeeAmount.GreaterThan(authorizedFee) || charge.CaptureAmount.GreaterThan(authorizedTotal) {
		return AdaptiveManagementFeeSettlement{}, ErrAdaptiveBillingCaptureExceedsHold
	}

	return AdaptiveManagementFeeSettlement{
		UncappedBaseAmount:        uncappedBase,
		CustomerCharge:            charge,
		PlatformOverageBaseAmount: uncappedBase.Sub(charge.BaseAmount).Round(AdaptiveBillingMoneyScale),
	}, nil
}

// ValidateAdaptiveManagementFeeCapture enforces the reservation invariant that
// settlement may consume at most the amount previously held.
func ValidateAdaptiveManagementFeeCapture(captureAmount, holdAmount decimal.Decimal) error {
	if captureAmount.IsNegative() || holdAmount.IsNegative() {
		return ErrAdaptiveBillingInvalidAmount
	}
	capture := captureAmount.Round(AdaptiveBillingMoneyScale)
	hold := holdAmount.RoundCeil(AdaptiveBillingMoneyScale)
	if capture.GreaterThan(hold) {
		return ErrAdaptiveBillingCaptureExceedsHold
	}
	return nil
}

// ValidateAdaptiveManagementFeeCaptureFloat64 is the guarded float adapter for
// repository boundaries that have not yet migrated to decimal values.
func ValidateAdaptiveManagementFeeCaptureFloat64(captureAmount, holdAmount float64) error {
	if err := validateAdaptiveBillingFloat(captureAmount); err != nil {
		return err
	}
	if err := validateAdaptiveBillingFloat(holdAmount); err != nil {
		return err
	}
	return ValidateAdaptiveManagementFeeCapture(
		decimal.NewFromFloat(captureAmount),
		decimal.NewFromFloat(holdAmount),
	)
}

func calculateAdaptiveManagementFeeDecimal(baseAmount decimal.Decimal, bps int32) (AdaptiveManagementFeeResult, error) {
	if baseAmount.IsNegative() {
		return AdaptiveManagementFeeResult{}, ErrAdaptiveBillingInvalidAmount
	}
	if err := validateAdaptiveManagementFeeBPS(bps); err != nil {
		return AdaptiveManagementFeeResult{}, err
	}

	base := baseAmount.Round(AdaptiveBillingMoneyScale)
	fee := decimal.Zero
	if !base.IsZero() && bps != 0 {
		fee = base.
			Mul(decimal.NewFromInt(int64(bps))).
			Div(decimal.NewFromInt(adaptiveBillingBPSDenominator)).
			Round(AdaptiveBillingMoneyScale)
	}

	return AdaptiveManagementFeeResult{
		BaseAmount:       base,
		FeeAmount:        fee,
		CaptureAmount:    base.Add(fee).Round(AdaptiveBillingMoneyScale),
		ManagementFeeBPS: bps,
	}, nil
}

func calculateAdaptiveManagementFeeHoldDecimal(maxCandidateUpperBound decimal.Decimal, bps int32) (decimal.Decimal, error) {
	if maxCandidateUpperBound.IsNegative() {
		return decimal.Zero, ErrAdaptiveBillingInvalidAmount
	}
	if err := validateAdaptiveManagementFeeBPS(bps); err != nil {
		return decimal.Zero, err
	}
	if maxCandidateUpperBound.IsZero() {
		return decimal.Zero, nil
	}

	multiplierBPS := decimal.NewFromInt(adaptiveBillingBPSDenominator + int64(bps))
	return maxCandidateUpperBound.
		Mul(multiplierBPS).
		Div(decimal.NewFromInt(adaptiveBillingBPSDenominator)).
		RoundCeil(AdaptiveBillingMoneyScale), nil
}

func validateAdaptiveBillingFloat(value float64) error {
	if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return ErrAdaptiveBillingInvalidAmount
	}
	return nil
}

func validateAdaptiveManagementFeeBPS(bps int32) error {
	if bps < 0 || bps > int32(adaptiveBillingBPSDenominator) {
		return ErrAdaptiveBillingInvalidBasisPoints
	}
	return nil
}

func isAdaptiveManagementFeeBPS(bps int32) bool {
	return validateAdaptiveManagementFeeBPS(bps) == nil
}
