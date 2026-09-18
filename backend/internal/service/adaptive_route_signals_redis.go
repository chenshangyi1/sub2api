package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis storage configuration for adaptive signals.
const (
	adaptiveSignalRedisKeyPrefix = "adaptive:signal:"
	adaptiveSignalRedisTTL       = 10 * time.Minute
	adaptiveSignalRedisWindow    = 5 * time.Minute
)

// redisSignalValue is the compact JSON stored in Redis sorted sets.
// Uses single-letter keys to minimize memory usage.
type redisSignalValue struct {
	Success bool    `json:"s"`
	TTFT    float64 `json:"ttft,omitempty"`
	Total   float64 `json:"total,omitempty"`
}

// buildRedisSignalKey constructs the Redis key for a parent/leaf/model combination.
func buildRedisSignalKey(parentGroupID, leafGroupID int64, canonicalModel string) string {
	return fmt.Sprintf("%s%d:%d:%s",
		adaptiveSignalRedisKeyPrefix,
		parentGroupID,
		leafGroupID,
		canonicalModel,
	)
}

// recordLeafOutcomeToRedis writes a single outcome to Redis sorted set.
// Returns error if Redis operation fails; caller should fall back to local cache.
func (s *AdaptiveRouteSignalStore) recordLeafOutcomeToRedis(
	ctx context.Context,
	outcome AdaptiveLeafOutcome,
	key string,
	timestampMS int64,
) error {
	if s.redisClient == nil {
		return fmt.Errorf("redis client not configured")
	}

	value := redisSignalValue{
		Success: outcome.Success,
		TTFT:    outcome.FirstTokenLatencyMS,
		Total:   outcome.TotalLatencyMS,
	}

	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal signal value: %w", err)
	}

	redisKey := buildRedisSignalKey(outcome.ParentGroupID, outcome.LeafGroupID, outcome.CanonicalModel)

	// Redis Sorted Set members must be unique; identical JSON payloads (e.g.
	// every failure without latency data is {"s":false}) would otherwise be
	// deduplicated into one member, collapsing the failure count to 1 (H1).
	// Append "<timestampMS>|<seq>" to the member and use the JSON prefix on read.
	seq := s.redisSeq.Add(1)
	member := string(jsonBytes) + "|" + strconv.FormatInt(timestampMS, 10) + "|" + strconv.FormatInt(seq, 10)

	pipe := s.redisClient.Pipeline()
	pipe.ZAdd(ctx, redisKey, redis.Z{
		Score:  float64(timestampMS),
		Member: member,
	})
	pipe.Expire(ctx, redisKey, adaptiveSignalRedisTTL)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("redis pipeline exec: %w", err)
	}

	return nil
}

// fetchLeafSignalsFromRedis reads the sliding window for one leaf from Redis.
// Returns nil series if key doesn't exist or has insufficient samples.
func (s *AdaptiveRouteSignalStore) fetchLeafSignalsFromRedis(
	ctx context.Context,
	parentGroupID, leafGroupID int64,
	canonicalModel string,
	minSamples int,
) (*adaptiveSignalSeries, error) {
	if s.redisClient == nil {
		return nil, fmt.Errorf("redis client not configured")
	}

	redisKey := buildRedisSignalKey(parentGroupID, leafGroupID, canonicalModel)
	now := time.Now().UnixMilli()
	windowStart := now - int64(adaptiveSignalRedisWindow.Milliseconds())

	// Fetch last 40 samples within the time window, ordered by score (timestamp).
	members, err := s.redisClient.ZRangeByScore(ctx, redisKey, &redis.ZRangeBy{
		Min:    strconv.FormatInt(windowStart, 10),
		Max:    strconv.FormatInt(now, 10),
		Offset: 0,
		Count:  int64(s.window),
	}).Result()

	if err != nil {
		return nil, fmt.Errorf("redis zrangebyscore: %w", err)
	}

	if len(members) < minSamples {
		return nil, nil // Not enough samples, return nil (not an error)
	}

	// Parse JSON values and build in-memory series for aggregation.
	ser := &adaptiveSignalSeries{
		success:    make([]bool, len(members)),
		ttftMS:     make([]float64, len(members)),
		totalMS:    make([]float64, len(members)),
		observedAt: make([]int64, len(members)),
		pos:        0,
		count:      len(members),
		cap:        len(members),
	}

	for i, member := range members {
		payload, tsMS := splitRedisSignalMember(member)
		var val redisSignalValue
		if err := json.Unmarshal([]byte(payload), &val); err != nil {
			// Skip malformed entries rather than failing entire read.
			continue
		}
		ser.success[i] = val.Success
		ser.ttftMS[i] = val.TTFT
		ser.totalMS[i] = val.Total
		ser.observedAt[i] = tsMS
	}

	return ser, nil
}

// splitRedisSignalMember splits a ZSET member back into its JSON payload and
// observed timestamp. Members written before the uniqueness-token format are
// plain JSON; they keep an unknown timestamp (0) and stay eligible during the
// rolling migration window.
func splitRedisSignalMember(member string) (payload string, timestampMS int64) {
	idx := strings.IndexByte(member, '|')
	if idx < 0 {
		return member, 0
	}
	payload = member[:idx]
	rest := member[idx+1:]
	if sep := strings.IndexByte(rest, '|'); sep >= 0 {
		rest = rest[:sep]
	}
	ts, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		return payload, 0
	}
	return payload, ts
}

// cleanupOldRedisSignals removes samples older than the TTL window.
// This is optional since EXPIRE handles cleanup, but can be used for immediate cleanup.
func (s *AdaptiveRouteSignalStore) cleanupOldRedisSignals(
	ctx context.Context,
	parentGroupID, leafGroupID int64,
	canonicalModel string,
) error {
	if s.redisClient == nil {
		return fmt.Errorf("redis client not configured")
	}

	redisKey := buildRedisSignalKey(parentGroupID, leafGroupID, canonicalModel)
	cutoff := time.Now().Add(-adaptiveSignalRedisTTL).UnixMilli()

	_, err := s.redisClient.ZRemRangeByScore(ctx, redisKey, "-inf", strconv.FormatInt(cutoff, 10)).Result()
	if err != nil {
		return fmt.Errorf("redis zremrangebyscore: %w", err)
	}

	return nil
}
