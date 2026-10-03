# Adaptive Route Signal Store - Redis Migration

## Summary

Migrated `AdaptiveRouteSignalStore` from process-local in-memory storage to Redis-backed global state sharing, enabling all sub2api nodes to observe and share leaf group health information.

## Changes

### 1. Core Files Modified

#### `adaptive_route_signals.go`
- Added `redisClient *redis.Client` field to `AdaptiveRouteSignalStore`
- Updated `NewAdaptiveRouteSignalStore(redisClient *redis.Client)` constructor signature
- Modified `RecordLeafOutcome()` to attempt Redis write first, fall back to local cache on error
- Modified `GetAdaptiveRouteSignalSnapshot()` to read from Redis first, fall back to local cache on error
- Added helper methods:
  - `recordToLocalCache()`: Extracted local write logic for reuse
  - `fetchFromRedis()`: Orchestrates Redis reads for all leaf groups

#### `adaptive_route_signals_redis.go` (new)
- `buildRedisSignalKey()`: Constructs Redis keys with pattern `adaptive:signal:{parent}:{leaf}:{model}`
- `recordLeafOutcomeToRedis()`: Writes outcome to Redis sorted set with timestamp score
- `fetchLeafSignalsFromRedis()`: Reads sliding window from Redis for one leaf
- `cleanupOldRedisSignals()`: Optional cleanup (TTL handles this automatically)
- `redisSignalValue` struct: Compact JSON format with short keys `{s:bool, ttft:float64, total:float64}`

#### `wire.go`
- Updated `ProvideAdaptiveRoutePlanner()` to accept `redisClient *redis.Client` parameter
- Passes Redis client to `NewAdaptiveRouteSignalStore(redisClient)`

#### `adaptive_route_signals_test.go`
- Updated all test constructors to use `NewAdaptiveRouteSignalStore(nil)` for local-only mode

#### `adaptive_route_signals_redis_test.go` (new)
- Tests for Redis key format
- Tests for local fallback when Redis is unavailable
- Tests for graceful degradation on Redis errors

## Redis Data Structure

### Sorted Set
- **Key**: `adaptive:signal:{parent_id}:{leaf_id}:{canonical_model}`
- **Score**: Unix timestamp in milliseconds
- **Member**: JSON `{"s":true,"ttft":1234.5,"total":5678.9}`
- **TTL**: 10 minutes (600 seconds)

### Operations

**Write** (on every request outcome):
```redis
ZADD adaptive:signal:108:85:gpt-4o {timestamp_ms} {"s":true,"ttft":1234.5,"total":5678.9}
EXPIRE adaptive:signal:108:85:gpt-4o 600
```

**Read** (on plan generation):
```redis
ZRANGEBYSCORE adaptive:signal:108:85:gpt-4o {now-5min} {now} LIMIT 0 40
```

## Graceful Degradation

### Fallback Strategy
1. **Redis available**: All nodes read/write global state
2. **Redis write fails**: Log warning, write to local cache only
3. **Redis read fails**: Fall back to local cache, mark snapshot as "fallback"
4. **Redis client is nil**: Operate in local-only mode (backward compatible)

### Timeouts
- **Write timeout**: 500ms (RecordLeafOutcome context)
- **Read timeout**: 1s (GetAdaptiveRouteSignalSnapshot context)

### Error Handling
- Redis errors do NOT block request routing
- Errors are logged but suppressed (TODO: add structured logging)
- Local cache is updated on successful Redis write for faster reads

## Health Calculation

Health formula remains unchanged:
```go
healthScore := (1.0 - errorRate) * 0.7 + latencyScore * 0.3
```

- **Unhealthy threshold**: error_rate >= 0.45
- **Window size**: 40 samples
- **Minimum samples**: 3 (before marking Known)

## Deployment Notes

### Redis Configuration
The `redisClient` parameter must be injected via Wire dependency injection. Ensure the existing Redis client is passed to `ProvideAdaptiveRoutePlanner()`.

### Backward Compatibility
Passing `nil` to `NewAdaptiveRouteSignalStore(nil)` enables local-only mode, maintaining backward compatibility with deployments without Redis.

### Monitoring
Snapshot ID indicates the data source:
- `"passive-v1-redis"`: Redis read succeeded
- `"passive-v1-local-fallback"`: Redis failed, using local cache
- `"empty"`: Invalid request or no leaf groups

### Memory Usage
Redis keys are automatically expired after 10 minutes. Compact JSON format minimizes memory:
- Short field names: `s`, `ttft`, `total`
- Only stores non-zero latencies
- Typical value size: ~40 bytes

### Testing
Run tests to verify:
```bash
go test ./internal/service -run TestAdaptiveRouteSignalStore
```

Tests cover:
- Local-only mode (nil Redis client)
- Redis failure fallback
- Key format correctness
- Value marshaling

## Next Steps

1. **Add structured logging**: Replace `_ = err` with proper log statements
2. **Integration test**: Test with real Redis instance (use testcontainers)
3. **Metrics**: Add Prometheus metrics for Redis hit/miss rates
4. **Monitoring**: Alert on high fallback rates (indicates Redis issues)
5. **Capacity planning**: Monitor Redis memory usage and key count

## Migration Path

### Phase 1: Deploy with Redis (current)
- All nodes write to both Redis and local cache
- Reads prefer Redis, fall back to local

### Phase 2: Redis-only (future)
- Remove local cache entirely after Redis is proven stable
- Requires confidence in Redis availability

### Rollback
If issues arise, deploy with `redisClient = nil` to revert to local-only mode without code changes.
