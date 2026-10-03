package service

import (
	"math"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestCalculateAdaptiveManagementFeeDecimal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		base        string
		wantBase    string
		wantFee     string
		wantCapture string
	}{
		{
			name:        "zero has no fee",
			base:        "0",
			wantBase:    "0.0000000000",
			wantFee:     "0.0000000000",
			wantCapture: "0.0000000000",
		},
		{
			name:        "ordinary amount",
			base:        "100",
			wantBase:    "100.0000000000",
			wantFee:     "15.0000000000",
			wantCapture: "115.0000000000",
		},
		{
			name:        "small decimal rounds to five units",
			base:        "0.000000003333333333",
			wantBase:    "0.0000000033",
			wantFee:     "0.0000000005",
			wantCapture: "0.0000000038",
		},
		{
			name:        "small decimal rounds to four units",
			base:        "0.000000002666666667",
			wantBase:    "0.0000000027",
			wantFee:     "0.0000000004",
			wantCapture: "0.0000000031",
		},
		{
			name:        "exact half rounds up",
			base:        "0.000000003",
			wantBase:    "0.0000000030",
			wantFee:     "0.0000000005",
			wantCapture: "0.0000000035",
		},
		{
			name:        "fee is derived from recorded base",
			base:        "0.00000000096",
			wantBase:    "0.0000000010",
			wantFee:     "0.0000000002",
			wantCapture: "0.0000000012",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := CalculateAdaptiveManagementFeeDecimal(decimal.RequireFromString(tt.base))
			require.NoError(t, err)
			require.Equal(t, DefaultAdaptiveManagementFeeBPS, got.ManagementFeeBPS)
			require.Equal(t, tt.wantBase, got.BaseAmount.StringFixed(AdaptiveBillingMoneyScale))
			require.Equal(t, tt.wantFee, got.FeeAmount.StringFixed(AdaptiveBillingMoneyScale))
			require.Equal(t, tt.wantCapture, got.CaptureAmount.StringFixed(AdaptiveBillingMoneyScale))
			require.True(t, got.CaptureAmount.Equal(got.BaseAmount.Add(got.FeeAmount)))
		})
	}
}

func TestCalculateAdaptiveManagementFeeHoldDecimal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		upper string
		want  string
	}{
		{name: "zero", upper: "0", want: "0.0000000000"},
		{name: "ordinary amount", upper: "100", want: "115.0000000000"},
		{name: "ceil preserves upper bound", upper: "0.0000000001", want: "0.0000000002"},
		{name: "ceil at one extra digit", upper: "1.00000000001", want: "1.1500000001"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := CalculateAdaptiveManagementFeeHoldDecimal(decimal.RequireFromString(tt.upper))
			require.NoError(t, err)
			require.Equal(t, tt.want, got.StringFixed(AdaptiveBillingMoneyScale))
		})
	}
}

func TestAdaptiveManagementFeeHoldCoversCapture(t *testing.T) {
	t.Parallel()

	tests := []struct {
		actual string
		upper  string
	}{
		{actual: "0", upper: "0"},
		{actual: "0.000000002666666667", upper: "0.000000003333333333"},
		{actual: "1.23456789012", upper: "1.23456789013"},
		{actual: "999999.9999999999", upper: "1000000"},
	}

	for _, tt := range tests {
		capture, err := CalculateAdaptiveManagementFeeDecimal(decimal.RequireFromString(tt.actual))
		require.NoError(t, err)
		hold, err := CalculateAdaptiveManagementFeeHoldDecimal(decimal.RequireFromString(tt.upper))
		require.NoError(t, err)
		require.NoError(t, ValidateAdaptiveManagementFeeCapture(capture.CaptureAmount, hold))
	}
}

func TestAdaptiveManagementFeeRejectsInvalidFloatAmounts(t *testing.T) {
	t.Parallel()

	invalid := []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)}
	for _, value := range invalid {
		_, err := CalculateAdaptiveManagementFee(value)
		require.ErrorIs(t, err, ErrAdaptiveBillingInvalidAmount)

		_, err = CalculateAdaptiveManagementFeeHold(value)
		require.ErrorIs(t, err, ErrAdaptiveBillingInvalidAmount)
	}
}

func TestAdaptiveManagementFeeRejectsNegativeDecimalAmounts(t *testing.T) {
	t.Parallel()

	_, err := CalculateAdaptiveManagementFeeDecimal(decimal.RequireFromString("-0.00000000001"))
	require.ErrorIs(t, err, ErrAdaptiveBillingInvalidAmount)

	_, err = CalculateAdaptiveManagementFeeHoldDecimal(decimal.RequireFromString("-0.00000000001"))
	require.ErrorIs(t, err, ErrAdaptiveBillingInvalidAmount)
}

func TestAdaptiveManagementFeeBPSValidationAndSnapshot(t *testing.T) {
	t.Parallel()

	zeroFee, err := CalculateAdaptiveManagementFeeDecimalWithBPS(decimal.NewFromInt(10), 0)
	require.NoError(t, err)
	require.True(t, zeroFee.FeeAmount.IsZero())
	require.Equal(t, int32(0), zeroFee.ManagementFeeBPS)

	customFee, err := CalculateAdaptiveManagementFeeDecimalWithBPS(decimal.NewFromInt(10), 2500)
	require.NoError(t, err)
	require.Equal(t, "2.5000000000", customFee.FeeAmount.StringFixed(AdaptiveBillingMoneyScale))
	require.Equal(t, int32(2500), customFee.ManagementFeeBPS)

	_, err = CalculateAdaptiveManagementFeeDecimalWithBPS(decimal.NewFromInt(10), -1)
	require.ErrorIs(t, err, ErrAdaptiveBillingInvalidBasisPoints)
	_, err = CalculateAdaptiveManagementFeeHoldDecimalWithBPS(decimal.NewFromInt(10), 10001)
	require.ErrorIs(t, err, ErrAdaptiveBillingInvalidBasisPoints)
}

func TestValidateAdaptiveManagementFeeCapture(t *testing.T) {
	t.Parallel()

	hold := decimal.RequireFromString("1.0000000000")
	require.NoError(t, ValidateAdaptiveManagementFeeCapture(hold, hold))
	require.NoError(t, ValidateAdaptiveManagementFeeCapture(decimal.RequireFromString("1.00000000001"), hold))
	require.ErrorIs(
		t,
		ValidateAdaptiveManagementFeeCapture(decimal.RequireFromString("1.0000000001"), hold),
		ErrAdaptiveBillingCaptureExceedsHold,
	)
	require.ErrorIs(
		t,
		ValidateAdaptiveManagementFeeCapture(decimal.RequireFromString("-0.0000000001"), hold),
		ErrAdaptiveBillingInvalidAmount,
	)
	require.ErrorIs(t, ValidateAdaptiveManagementFeeCaptureFloat64(math.NaN(), 1), ErrAdaptiveBillingInvalidAmount)
	require.ErrorIs(t, ValidateAdaptiveManagementFeeCaptureFloat64(1, math.Inf(1)), ErrAdaptiveBillingInvalidAmount)
}

func TestAdaptiveManagementFeeCalculationIsRepeatable(t *testing.T) {
	t.Parallel()

	base := decimal.RequireFromString("123456.7890123456789")
	first, err := CalculateAdaptiveManagementFeeDecimal(base)
	require.NoError(t, err)
	second, err := CalculateAdaptiveManagementFeeDecimal(base)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, "123456.7890123456789", base.String())

	upper := decimal.RequireFromString("123456.7890123456790")
	firstHold, err := CalculateAdaptiveManagementFeeHoldDecimal(upper)
	require.NoError(t, err)
	secondHold, err := CalculateAdaptiveManagementFeeHoldDecimal(upper)
	require.NoError(t, err)
	require.True(t, firstHold.Equal(secondHold))
	require.Equal(t, "123456.789012345679", upper.String())
}
