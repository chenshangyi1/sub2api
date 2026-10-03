package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestModelSnapshotChangesInvalidateSchedulerAndAvailability(t *testing.T) {
	for _, key := range []string{service.UpstreamModelsExtraKey, service.UpstreamModelsSyncStatusExtraKey, service.UpstreamModelsIdentityExtraKey} {
		updates := map[string]any{key: nil}
		require.True(t, shouldEnqueueSchedulerOutboxForExtraUpdates(updates), key)
		require.True(t, modelAvailabilityExtraUpdatesRelevant(updates), key)
	}
}
