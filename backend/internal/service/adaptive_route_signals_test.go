package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdaptiveRouteSignalStoreColdStartUntilMinSamples(t *testing.T) {
	store := NewAdaptiveRouteSignalStore(nil)
	parent, leaf := int64(108), int64(85)
	model := "gpt-5.6-sol"

	for i := 0; i < adaptiveSignalDefaultMinSamples-1; i++ {
		store.RecordLeafOutcome(AdaptiveLeafOutcome{
			ParentGroupID: parent, LeafGroupID: leaf, CanonicalModel: model, Success: true, TotalLatencyMS: 100,
		})
	}
	snap, err := store.GetAdaptiveRouteSignalSnapshot(context.Background(), AdaptiveRouteSignalRequest{
		ParentGroupID: parent, CanonicalModel: model, LeafGroupIDs: []int64{leaf},
	})
	require.NoError(t, err)
	require.False(t, snap.Leaves[leaf].Known)

	store.RecordLeafOutcome(AdaptiveLeafOutcome{
		ParentGroupID: parent, LeafGroupID: leaf, CanonicalModel: model, Success: true, TotalLatencyMS: 100,
	})
	snap, err = store.GetAdaptiveRouteSignalSnapshot(context.Background(), AdaptiveRouteSignalRequest{
		ParentGroupID: parent, CanonicalModel: model, LeafGroupIDs: []int64{leaf},
	})
	require.NoError(t, err)
	require.True(t, snap.Leaves[leaf].Known)
	require.True(t, snap.Leaves[leaf].Healthy)
	require.Equal(t, float64(0), snap.Leaves[leaf].ErrorRate)
}

func TestAdaptiveRouteSignalStoreUnhealthyOnHighErrorRate(t *testing.T) {
	store := NewAdaptiveRouteSignalStore(nil)
	parent, leaf := int64(108), int64(85)
	model := "gpt-5.6-sol"

	store.RecordLeafOutcome(AdaptiveLeafOutcome{
		ParentGroupID: parent, LeafGroupID: leaf, CanonicalModel: model, Success: true, TotalLatencyMS: 200,
	})
	for i := 0; i < 4; i++ {
		store.RecordLeafOutcome(AdaptiveLeafOutcome{
			ParentGroupID: parent, LeafGroupID: leaf, CanonicalModel: model, Success: false, TotalLatencyMS: 50,
		})
	}
	snap, err := store.GetAdaptiveRouteSignalSnapshot(context.Background(), AdaptiveRouteSignalRequest{
		ParentGroupID: parent, CanonicalModel: model, LeafGroupIDs: []int64{leaf},
	})
	require.NoError(t, err)
	sig := snap.Leaves[leaf]
	require.True(t, sig.Known)
	require.False(t, sig.Healthy)
	require.InDelta(t, 0.8, sig.ErrorRate, 0.01)
	require.Less(t, sig.HealthScore, 0.5)
}

func TestAdaptiveRouteSignalStoreIsolatesLeafAndModel(t *testing.T) {
	store := NewAdaptiveRouteSignalStore(nil)
	parent := int64(108)
	for i := 0; i < 5; i++ {
		store.RecordLeafOutcome(AdaptiveLeafOutcome{
			ParentGroupID: parent, LeafGroupID: 85, CanonicalModel: "gpt-a", Success: true, FirstTokenLatencyMS: 100,
		})
		store.RecordLeafOutcome(AdaptiveLeafOutcome{
			ParentGroupID: parent, LeafGroupID: 17, CanonicalModel: "gpt-a", Success: false, TotalLatencyMS: 10,
		})
	}
	for i := 0; i < 5; i++ {
		store.RecordLeafOutcome(AdaptiveLeafOutcome{
			ParentGroupID: parent, LeafGroupID: 17, CanonicalModel: "gpt-b", Success: true, TotalLatencyMS: 80,
		})
	}

	snap, err := store.GetAdaptiveRouteSignalSnapshot(context.Background(), AdaptiveRouteSignalRequest{
		ParentGroupID: parent, CanonicalModel: "gpt-a", LeafGroupIDs: []int64{85, 17},
	})
	require.NoError(t, err)
	require.True(t, snap.Leaves[85].Healthy)
	require.False(t, snap.Leaves[17].Healthy)

	snapB, err := store.GetAdaptiveRouteSignalSnapshot(context.Background(), AdaptiveRouteSignalRequest{
		ParentGroupID: parent, CanonicalModel: "gpt-b", LeafGroupIDs: []int64{17},
	})
	require.NoError(t, err)
	require.True(t, snapB.Leaves[17].Healthy)
}

func TestSortAdaptiveRouteCandidatesPrefersHealthyThenColdStartThenUnhealthy(t *testing.T) {
	cands := []AdaptiveRouteCandidate{
		{LeafGroupID: 1, FrozenRateMultiplier: 0.2, HealthKnown: true, Healthy: false, HealthScore: 0.1, MemberSortOrder: 10},
		{LeafGroupID: 2, FrozenRateMultiplier: 0.05, HealthKnown: true, Healthy: true, HealthScore: 0.9, MemberSortOrder: 20},
		{LeafGroupID: 3, FrozenRateMultiplier: 0.1, HealthKnown: false, MemberSortOrder: 30},
	}
	sortAdaptiveRouteCandidates(cands, AdaptiveRouteModePrice, false)
	require.Equal(t, int64(2), cands[0].LeafGroupID)
	require.Equal(t, int64(3), cands[1].LeafGroupID)
	require.Equal(t, int64(1), cands[2].LeafGroupID)
}

func TestSortAdaptiveRouteCandidatesPricePrefersCheapColdStartOverHealthyExpensive(t *testing.T) {
	candidates := []AdaptiveRouteCandidate{
		{LeafGroupID: 34, FrozenRateMultiplier: 0.22, MemberSortOrder: 40, HealthKnown: true, Healthy: true, HealthScore: 0.95},
		{LeafGroupID: 85, FrozenRateMultiplier: 0.06, MemberSortOrder: 10, HealthKnown: false},
		{LeafGroupID: 17, FrozenRateMultiplier: 0.085, MemberSortOrder: 20, HealthKnown: false},
		{LeafGroupID: 14, FrozenRateMultiplier: 0.11, MemberSortOrder: 30, HealthKnown: false},
	}

	sortAdaptiveRouteCandidates(candidates, AdaptiveRouteModePrice, false)

	require.Equal(t, []int64{85, 17, 14, 34}, []int64{
		candidates[0].LeafGroupID, candidates[1].LeafGroupID, candidates[2].LeafGroupID, candidates[3].LeafGroupID,
	})
}

func TestSortAdaptiveRouteCandidatesIntelligenceUsesPriceOrManualOrder(t *testing.T) {
	candidates := []AdaptiveRouteCandidate{
		{LeafGroupID: 34, FrozenRateMultiplier: 0.22, MemberSortOrder: 40, HealthKnown: false},
		{LeafGroupID: 85, FrozenRateMultiplier: 0.025, MemberSortOrder: 10, HealthKnown: false},
		{LeafGroupID: 17, FrozenRateMultiplier: 0.085, MemberSortOrder: 20, HealthKnown: false},
	}

	sortAdaptiveRouteCandidates(candidates, AdaptiveRouteModeIntelligence, false)
	require.Equal(t, []int64{34, 17, 85}, []int64{
		candidates[0].LeafGroupID, candidates[1].LeafGroupID, candidates[2].LeafGroupID,
	})

	sortAdaptiveRouteCandidates(candidates, AdaptiveRouteModeIntelligence, true)
	require.Equal(t, []int64{85, 17, 34}, []int64{
		candidates[0].LeafGroupID, candidates[1].LeafGroupID, candidates[2].LeafGroupID,
	})
}
