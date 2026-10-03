package service

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAdaptiveServiceFeePercent(t *testing.T) {
	require.Equal(t, 15.0, parseAdaptiveServiceFeePercent(""))
	require.Equal(t, 0.0, parseAdaptiveServiceFeePercent("0"))
	require.Equal(t, 100.0, parseAdaptiveServiceFeePercent("100"))
	require.Equal(t, 15.0, parseAdaptiveServiceFeePercent("101"))
}

func TestAdaptiveServiceFeeBPSPreservesExplicitZero(t *testing.T) {
	percent := parseAdaptiveServiceFeePercent("0")
	bps := int32(math.Round(percent * 100))
	require.Equal(t, int32(0), bps)
	require.Equal(t, 0.0, adaptiveServiceFeePercentFromBPS(bps))

	svc := &SettingService{settingRepo: adaptiveFeeSettingRepoStub{raw: "0"}}
	require.Equal(t, int32(0), svc.GetAdaptiveServiceFeeBPS(context.Background()))
}

type adaptiveFeeSettingRepoStub struct {
	raw string
}

func (adaptiveFeeSettingRepoStub) Get(context.Context, string) (*Setting, error) {
	return nil, nil
}
func (s adaptiveFeeSettingRepoStub) GetValue(context.Context, string) (string, error) {
	return s.raw, nil
}
func (adaptiveFeeSettingRepoStub) Set(context.Context, string, string) error { return nil }
func (adaptiveFeeSettingRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	return nil, nil
}
func (adaptiveFeeSettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	return nil
}
func (adaptiveFeeSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	return nil, nil
}
func (adaptiveFeeSettingRepoStub) Delete(context.Context, string) error { return nil }
