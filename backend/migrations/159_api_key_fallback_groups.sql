ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS fallback_group_ids JSONB NOT NULL DEFAULT '[]'::jsonb;

UPDATE api_keys
SET fallback_group_ids = '[]'::jsonb
WHERE fallback_group_ids IS NULL;
