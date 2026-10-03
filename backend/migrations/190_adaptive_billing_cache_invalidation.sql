-- Adaptive reservations mutate values embedded in API-key auth snapshots.
-- Enqueue invalidation in the same transaction so a process crash between
-- COMMIT and the synchronous cache delete cannot leave cross-instance L1/L2
-- auth caches stale until their normal TTL.

CREATE OR REPLACE FUNCTION enqueue_adaptive_api_key_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM enqueue_auth_cache_invalidation(OLD.key);
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_api_keys_adaptive_auth_cache_invalidation ON api_keys;
CREATE TRIGGER trg_api_keys_adaptive_auth_cache_invalidation
AFTER UPDATE OF reserved_quota_usd, reserved_usage_5h_usd,
    reserved_usage_1d_usd, reserved_usage_7d_usd ON api_keys
FOR EACH ROW
WHEN (OLD.reserved_quota_usd IS DISTINCT FROM NEW.reserved_quota_usd
   OR OLD.reserved_usage_5h_usd IS DISTINCT FROM NEW.reserved_usage_5h_usd
   OR OLD.reserved_usage_1d_usd IS DISTINCT FROM NEW.reserved_usage_1d_usd
   OR OLD.reserved_usage_7d_usd IS DISTINCT FROM NEW.reserved_usage_7d_usd)
EXECUTE FUNCTION enqueue_adaptive_api_key_auth_cache_invalidation();

CREATE OR REPLACE FUNCTION enqueue_adaptive_user_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT encode(sha256(convert_to(k.key, 'UTF8')), 'hex')
    FROM api_keys AS k
    WHERE k.user_id = OLD.id
      AND k.deleted_at IS NULL
      AND k.key <> '';
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_users_adaptive_auth_cache_invalidation ON users;
CREATE TRIGGER trg_users_adaptive_auth_cache_invalidation
AFTER UPDATE OF adaptive_reserved_balance ON users
FOR EACH ROW
WHEN (OLD.adaptive_reserved_balance IS DISTINCT FROM NEW.adaptive_reserved_balance)
EXECUTE FUNCTION enqueue_adaptive_user_auth_cache_invalidation();
