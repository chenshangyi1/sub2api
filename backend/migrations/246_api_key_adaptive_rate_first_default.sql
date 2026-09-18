-- New Adaptive keys default to rate-first routing (low multiplier to high).
-- Existing explicit preferences are preserved.
ALTER TABLE api_keys
    ALTER COLUMN adaptive_routing_preference SET DEFAULT 'price';
