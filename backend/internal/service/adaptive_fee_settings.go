package service

import (
	"context"
	"math"
	"strconv"
)

// GetAdaptiveServiceFeeBPS reads the Adaptive-only fee as a snapshot in basis
// points. Missing or invalid legacy values retain the historical 15% default.
func (s *SettingService) GetAdaptiveServiceFeeBPS(ctx context.Context) int32 {
	if s == nil || s.settingRepo == nil {
		return DefaultAdaptiveManagementFeeBPS
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyAdaptiveServiceFeePercent)
	if err != nil {
		return DefaultAdaptiveManagementFeeBPS
	}
	percent := parseAdaptiveServiceFeePercent(raw)
	bps := int32(math.Round(percent * 100))
	// Explicit 0% must stay 0; only out-of-range values fall back to 15%.
	if bps < 0 || bps > int32(adaptiveBillingBPSDenominator) {
		return DefaultAdaptiveManagementFeeBPS
	}
	return bps
}

func adaptiveServiceFeePercentFromBPS(bps int32) float64 {
	if err := validateAdaptiveManagementFeeBPS(bps); err != nil {
		return 15
	}
	return float64(bps) / 100
}

func formatAdaptiveServiceFeePercent(bps int32) string {
	return strconv.FormatFloat(adaptiveServiceFeePercentFromBPS(bps), 'f', 2, 64)
}
