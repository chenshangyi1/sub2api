package handler

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAdaptiveHoldRateIncludesUserMultiplierOverride(t *testing.T) {
	candidates := []service.AdaptiveRouteCandidate{
		{LeafGroupID: 1, FrozenRateMultiplier: 0.20},
		{LeafGroupID: 2, FrozenRateMultiplier: 0.40},
	}

	got := adaptiveHoldRate(context.Background(), candidates, 42, func(_ context.Context, userID, groupID int64, fallback float64) float64 {
		if userID == 42 && groupID == 1 {
			return 1.25
		}
		return fallback
	})

	require.Equal(t, 1.25, got)
}
