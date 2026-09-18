package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdaptivePlanKeepsDegradedAsFailoverTail(t *testing.T) {
	healthy := []AdaptiveRouteCandidate{
		{LeafGroupID: 85, FrozenRateMultiplier: 0.06, HealthKnown: true, Healthy: true, MemberSortOrder: 10},
	}
	degraded := []AdaptiveRouteCandidate{
		{LeafGroupID: 17, FrozenRateMultiplier: 0.085, HealthKnown: true, Healthy: false, MemberSortOrder: 20},
		{LeafGroupID: 14, FrozenRateMultiplier: 0.11, HealthKnown: true, Healthy: false, MemberSortOrder: 30},
	}
	sortAdaptiveRouteCandidates(healthy, AdaptiveRouteModePrice, false)
	sortAdaptiveRouteCandidates(degraded, AdaptiveRouteModePrice, false)
	merged := append(append([]AdaptiveRouteCandidate{}, healthy...), degraded...)

	require.Len(t, merged, 3)
	require.Equal(t, int64(85), merged[0].LeafGroupID)
	require.Equal(t, int64(17), merged[1].LeafGroupID)
	require.Equal(t, int64(14), merged[2].LeafGroupID)
}

func TestSortAdaptiveRouteCandidatesPrefersMappedOverEmpty(t *testing.T) {
	cands := []AdaptiveRouteCandidate{
		{LeafGroupID: 17, FrozenRateMultiplier: 0.05, MappedAccountCount: 0, MemberSortOrder: 1},
		{LeafGroupID: 85, FrozenRateMultiplier: 0.08, MappedAccountCount: 3, MemberSortOrder: 2},
	}
	sortAdaptiveRouteCandidates(cands, AdaptiveRouteModePrice, false)
	require.Equal(t, int64(85), cands[0].LeafGroupID)
	require.Equal(t, int64(17), cands[1].LeafGroupID)
}

func TestAntiStallShouldFailHardEmptyReserveNoMoreSwitches(t *testing.T) {
	s := NewAntiStallSession(AntiStallProSettings{
		Enabled: true, BufferTokens: 8, UpstreamMaxRetry: 1,
		LowBufferTokens: 0, MaxLeafSwitches: 1, MaxDripSeconds: 30,
		DripTokensPerSecond: 1,
	})
	s.RecordLeafSwitch()
	s.BeginRecovery()
	require.True(t, s.ShouldFailHard())
}
