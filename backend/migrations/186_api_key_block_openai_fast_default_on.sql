ALTER TABLE api_keys
    ALTER COLUMN block_openai_fast SET DEFAULT TRUE;

UPDATE api_keys
SET block_openai_fast = TRUE,
    updated_at = NOW()
WHERE block_openai_fast = FALSE
  AND deleted_at IS NULL;
