package service

import (
	"context"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// Passive leaf outcome observation for Adaptive routing.
// Window samples are process-local and shared by all Adaptive users on this node.
const (
	adaptiveSignalDefaultWindow     = 40
	adaptiveSignalDefaultMinSamples = 3
	// Error rate at/above this with enough samples marks the leaf unhealthy.
	adaptiveSignalUnhealthyErrorRate = 0.45
	adaptiveSignalMaxKeys            = 4096
)

// AdaptiveLeafOutcome is one finished attempt against a leaf for a model.
type AdaptiveLeafOutcome struct {
	ParentGroupID int64
	LeafGroupID   int64
	// CanonicalModel should already be canonicalized (same as plan.CanonicalModel).
	CanonicalModel string
	Success        bool
	// FirstTokenLatencyMS is 0 when unknown (non-stream / not measured).
	FirstTokenLatencyMS float64
	// TotalLatencyMS is wall time of the leaf attempt when known.
	TotalLatencyMS float64
	ObservedAt     time.Time
}

// AdaptiveRouteSignalStore is a sliding-window aggregator that implements
// AdaptiveRouteSignalSource for the leaf planner. When Redis is configured,
// all nodes share global health state; otherwise it falls back to process-local storage.
type AdaptiveRouteSignalStore struct {
	mu          sync.RWMutex
	window      int
	minSamples  int
	series      map[string]*adaptiveSignalSeries // Local fallback cache
	generation  atomic.Int64
	redisClient *redis.Client // Optional; nil means local-only mode
	// redisSeq seeds per-node uniqueness tokens for Redis ZSET members so that
	// samples with identical JSON payloads are never deduplicated (H1).
	redisSeq atomic.Int64
}

type adaptiveSignalSeries struct {
	success []bool
	ttftMS  []float64
	totalMS []float64
	// observedAt holds UnixMilli timestamps parallel to the value arrays so the
	// local cache can expire stale samples (H3). 0 means the timestamp is unknown
	// (legacy/Redis members written before the token format) and stays eligible.
	observedAt []int64
	pos        int
	count      int
	cap        int
}

// NewAdaptiveRouteSignalStore builds a store with production defaults.
// If redisClient is provided, the store uses Redis for shared global state
// with local fallback on Redis errors. If nil, operates in local-only mode.
func NewAdaptiveRouteSignalStore(redisClient *redis.Client) *AdaptiveRouteSignalStore {
	store := &AdaptiveRouteSignalStore{
		window:      adaptiveSignalDefaultWindow,
		minSamples:  adaptiveSignalDefaultMinSamples,
		series:      make(map[string]*adaptiveSignalSeries),
		redisClient: redisClient,
	}
	// Randomize the per-node member sequence so concurrent processes sharing one
	// Redis instance still produce unique ZSET members (H1).
	store.redisSeq.Store(rand.Int63())
	return store
}

func adaptiveSignalKey(parentGroupID, leafGroupID int64, canonicalModel string) string {
	return strconv.FormatInt(parentGroupID, 10) + "|" +
		strconv.FormatInt(leafGroupID, 10) + "|" +
		strings.ToLower(strings.TrimSpace(canonicalModel))
}

// RecordLeafOutcome appends one observation. Safe for concurrent use.
// Writes to Redis when available, with fallback to local cache on Redis errors.
func (s *AdaptiveRouteSignalStore) RecordLeafOutcome(outcome AdaptiveLeafOutcome) {
	if s == nil || outcome.ParentGroupID <= 0 || outcome.LeafGroupID <= 0 {
		return
	}
	model := strings.ToLower(strings.TrimSpace(outcome.CanonicalModel))
	if model == "" {
		return
	}
	if outcome.FirstTokenLatencyMS < 0 || math.IsNaN(outcome.FirstTokenLatencyMS) || math.IsInf(outcome.FirstTokenLatencyMS, 0) {
		outcome.FirstTokenLatencyMS = 0
	}
	if outcome.TotalLatencyMS < 0 || math.IsNaN(outcome.TotalLatencyMS) || math.IsInf(outcome.TotalLatencyMS, 0) {
		outcome.TotalLatencyMS = 0
	}
	// Default to current time if ObservedAt is not set.
	if outcome.ObservedAt.IsZero() {
		outcome.ObservedAt = time.Now()
	}

	key := adaptiveSignalKey(outcome.ParentGroupID, outcome.LeafGroupID, model)
	timestampMS := outcome.ObservedAt.UnixMilli()

	// Try Redis first if configured.
	if s.redisClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		err := s.recordLeafOutcomeToRedis(ctx, outcome, key, timestampMS)
		if err != nil {
			// Log warning but continue to local fallback - don't block on Redis errors.
			// In production, replace this with proper logging.
			_ = err // TODO: add structured logging
		} else {
			// Redis write succeeded, also update local cache for faster reads.
			s.recordToLocalCache(outcome, key)
			return
		}
	}

	// Fallback to local cache (either no Redis or Redis failed).
	s.recordToLocalCache(outcome, key)
}

// recordToLocalCache writes to the in-memory series map.
// Caller must NOT hold s.mu.
func (s *AdaptiveRouteSignalStore) recordToLocalCache(outcome AdaptiveLeafOutcome, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.series == nil {
		s.series = make(map[string]*adaptiveSignalSeries)
	}
	if len(s.series) >= adaptiveSignalMaxKeys {
		if _, ok := s.series[key]; !ok {
			for k := range s.series {
				delete(s.series, k)
				break
			}
		}
	}
	ser := s.series[key]
	if ser == nil {
		capN := s.window
		if capN < 8 {
			capN = adaptiveSignalDefaultWindow
		}
		ser = &adaptiveSignalSeries{
			success:    make([]bool, capN),
			ttftMS:     make([]float64, capN),
			totalMS:    make([]float64, capN),
			observedAt: make([]int64, capN),
			cap:        capN,
		}
		s.series[key] = ser
	}
	ser.success[ser.pos] = outcome.Success
	ser.ttftMS[ser.pos] = outcome.FirstTokenLatencyMS
	ser.totalMS[ser.pos] = outcome.TotalLatencyMS
	ser.observedAt[ser.pos] = outcome.ObservedAt.UnixMilli()
	ser.pos = (ser.pos + 1) % ser.cap
	if ser.count < ser.cap {
		ser.count++
	}
	s.generation.Add(1)
}

// GetAdaptiveRouteSignalSnapshot implements AdaptiveRouteSignalSource.
// Reads from Redis when available, with fallback to local cache on Redis errors.
func (s *AdaptiveRouteSignalStore) GetAdaptiveRouteSignalSnapshot(
	ctx context.Context,
	req AdaptiveRouteSignalRequest,
) (*AdaptiveRouteSignalSnapshot, error) {
	if s == nil {
		return nil, nil
	}
	model := strings.ToLower(strings.TrimSpace(req.CanonicalModel))
	if req.ParentGroupID <= 0 || model == "" || len(req.LeafGroupIDs) == 0 {
		return &AdaptiveRouteSignalSnapshot{
			Generation: 0,
			SnapshotID: "empty",
			Leaves:     map[int64]AdaptiveRouteLeafSignal{},
		}, nil
	}

	out := &AdaptiveRouteSignalSnapshot{
		Generation: s.generation.Load(),
		SnapshotID: "passive-v1-redis",
		Leaves:     make(map[int64]AdaptiveRouteLeafSignal, len(req.LeafGroupIDs)),
	}
	minSamples := s.minSamples
	if minSamples < 1 {
		minSamples = adaptiveSignalDefaultMinSamples
	}

	// Try Redis first if configured.
	useRedis := s.redisClient != nil
	if useRedis {
		redisCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
		defer cancel()

		redisOK := s.fetchFromRedis(redisCtx, req.ParentGroupID, model, req.LeafGroupIDs, minSamples, out)
		if redisOK {
			return out, nil
		}
		// Redis failed, fall through to local cache.
		out.SnapshotID = "passive-v1-local-fallback"
	}

	// Fallback to local cache.
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, leafID := range req.LeafGroupIDs {
		if leafID <= 0 {
			continue
		}
		key := adaptiveSignalKey(req.ParentGroupID, leafID, model)
		ser := s.series[key]
		if ser == nil || ser.count < minSamples {
			out.Leaves[leafID] = AdaptiveRouteLeafSignal{Known: false}
			continue
		}
		out.Leaves[leafID] = aggregateAdaptiveSignalSeries(ser, minSamples, time.Now())
	}
	return out, nil
}

// fetchFromRedis attempts to read all leaf signals from Redis.
// Returns true if successful, false if any error occurred (caller should use local fallback).
func (s *AdaptiveRouteSignalStore) fetchFromRedis(
	ctx context.Context,
	parentGroupID int64,
	model string,
	leafGroupIDs []int64,
	minSamples int,
	out *AdaptiveRouteSignalSnapshot,
) bool {
	for _, leafID := range leafGroupIDs {
		if leafID <= 0 {
			continue
		}

		ser, err := s.fetchLeafSignalsFromRedis(ctx, parentGroupID, leafID, model, minSamples)
		if err != nil {
			// On any Redis error, abort and let caller use local fallback.
			return false
		}

		if ser == nil || ser.count < minSamples {
			out.Leaves[leafID] = AdaptiveRouteLeafSignal{Known: false}
			continue
		}

		out.Leaves[leafID] = aggregateAdaptiveSignalSeries(ser, minSamples, time.Now())
	}
	return true
}

// aggregateAdaptiveSignalSeries folds the sliding window into one leaf signal.
// Samples older than the shared Redis window are excluded so stale local-cache
// data never survives a Redis outage fallback (H3); unknown timestamps (0) are
// kept for members written before the token format.
func aggregateAdaptiveSignalSeries(ser *adaptiveSignalSeries, minSamples int, now time.Time) AdaptiveRouteLeafSignal {
	n := ser.count
	if n < minSamples {
		return AdaptiveRouteLeafSignal{Known: false}
	}
	cutoffMS := int64(0)
	if !now.IsZero() {
		cutoffMS = now.Add(-adaptiveSignalRedisWindow).UnixMilli()
	}
	var fails int64
	var ttftSum, totalSum float64
	var ttftN, totalN int64
	valid := 0
	for i := 0; i < n; i++ {
		ts := int64(0)
		if i < len(ser.observedAt) {
			ts = ser.observedAt[i]
		}
		if cutoffMS > 0 && ts > 0 && ts < cutoffMS {
			continue // stale sample: excluded from aggregation (H3)
		}
		valid++
		if !ser.success[i] {
			fails++
		}
		if ser.ttftMS[i] > 0 {
			ttftSum += ser.ttftMS[i]
			ttftN++
		}
		if ser.totalMS[i] > 0 {
			totalSum += ser.totalMS[i]
			totalN++
		}
	}
	if valid < minSamples {
		return AdaptiveRouteLeafSignal{Known: false}
	}
	errRate := float64(fails) / float64(valid)
	var avgTTFT, avgTotal float64
	if ttftN > 0 {
		avgTTFT = ttftSum / float64(ttftN)
	}
	if totalN > 0 {
		avgTotal = totalSum / float64(totalN)
	}

	latScore := 1.0
	if avgTTFT > 0 {
		latScore = 1.0 - math.Min(avgTTFT, 30000)/30000
	} else if avgTotal > 0 {
		latScore = 1.0 - math.Min(avgTotal, 60000)/60000
	}
	healthScore := (1.0-errRate)*0.7 + latScore*0.3
	if healthScore < 0 {
		healthScore = 0
	}
	if healthScore > 1 {
		healthScore = 1
	}

	return AdaptiveRouteLeafSignal{
		Known:               true,
		Healthy:             errRate < adaptiveSignalUnhealthyErrorRate,
		HealthScore:         healthScore,
		QoSScore:            healthScore,
		ErrorRate:           errRate,
		FirstTokenLatencyMS: avgTTFT,
		TotalLatencyMS:      avgTotal,
		SampleCount:         int64(n),
	}
}
