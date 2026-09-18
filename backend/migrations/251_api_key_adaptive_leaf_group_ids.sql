-- Per-key Adaptive leaf allowlist. NULL/empty means every enabled leaf in the
-- parent pool is eligible. Non-empty BIGINT[] restricts planning and catalogs
-- to those leaf group IDs (must still be members of the parent pool).
ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS adaptive_leaf_group_ids BIGINT[] NULL;
