package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// TestAdaptiveRouteSignalStore_RedisIntegration tests Redis operations with a mock.
// In production, this would use a real Redis instance or testcontainers.
func TestAdaptiveRouteSignalStore_RedisKeyFormat(t *testing.T) {
	parent := int64(108)
	leaf := int64(85)
	model := "gpt-4o-mini"

	expected := "adaptive:signal:108:85:gpt-4o-mini"
	actual := buildRedisSignalKey(parent, leaf, model)

	require.Equal(t, expected, actual)
}

func TestAdaptiveRouteSignalStore_RedisValueMarshaling(t *testing.T) {
	val := redisSignalValue{
		Success: true,
		TTFT:    1234.5,
		Total:   5678.9,
	}

	// Verify JSON is compact
	expected := `{"s":true,"ttft":1234.5,"total":5678.9}`

	encoded, err := json.Marshal(val)
	require.NoError(t, err)
	require.JSONEq(t, expected, string(encoded))
}

// TestAdaptiveRouteSignalStore_LocalFallbackOnNilRedis verifies local-only mode.
func TestAdaptiveRouteSignalStore_LocalFallbackOnNilRedis(t *testing.T) {
	store := NewAdaptiveRouteSignalStore(nil)
	parent, leaf := int64(108), int64(85)
	model := "gpt-4o"

	// Record samples
	for i := 0; i < 5; i++ {
		store.RecordLeafOutcome(AdaptiveLeafOutcome{
			ParentGroupID:  parent,
			LeafGroupID:    leaf,
			CanonicalModel: model,
			Success:        true,
			TotalLatencyMS: 100,
			ObservedAt:     time.Now(),
		})
	}

	// Should work with local cache
	snap, err := store.GetAdaptiveRouteSignalSnapshot(context.Background(), AdaptiveRouteSignalRequest{
		ParentGroupID:  parent,
		CanonicalModel: model,
		LeafGroupIDs:   []int64{leaf},
	})

	require.NoError(t, err)
	require.True(t, snap.Leaves[leaf].Known)
	require.True(t, snap.Leaves[leaf].Healthy)
	require.Equal(t, int64(5), snap.Leaves[leaf].SampleCount)
}

// TestAdaptiveRouteSignalStore_RedisFailureFallback simulates Redis unavailability.
func TestAdaptiveRouteSignalStore_RedisFailureFallback(t *testing.T) {
	// Create a Redis client pointing to a non-existent server (will fail)
	redisClient := redis.NewClient(&redis.Options{
		Addr:         "localhost:63799", // Non-existent port
		DialTimeout:  100 * time.Millisecond,
		ReadTimeout:  100 * time.Millisecond,
		WriteTimeout: 100 * time.Millisecond,
	})
	defer redisClient.Close()

	store := NewAdaptiveRouteSignalStore(redisClient)
	parent, leaf := int64(108), int64(85)
	model := "gpt-4o"

	// Record should fall back to local cache on Redis failure
	for i := 0; i < 5; i++ {
		store.RecordLeafOutcome(AdaptiveLeafOutcome{
			ParentGroupID:  parent,
			LeafGroupID:    leaf,
			CanonicalModel: model,
			Success:        true,
			TotalLatencyMS: 100,
			ObservedAt:     time.Now(),
		})
	}

	// Read should fall back to local cache on Redis failure
	snap, err := store.GetAdaptiveRouteSignalSnapshot(context.Background(), AdaptiveRouteSignalRequest{
		ParentGroupID:  parent,
		CanonicalModel: model,
		LeafGroupIDs:   []int64{leaf},
	})

	require.NoError(t, err)
	require.True(t, snap.Leaves[leaf].Known)
	require.True(t, snap.Leaves[leaf].Healthy)
	require.Contains(t, snap.SnapshotID, "fallback", "Should indicate fallback mode")
}
