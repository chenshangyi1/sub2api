ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS block_openai_fast BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN api_keys.block_openai_fast IS
    'Strip OpenAI fast/priority service tier and prevent priority injection for this API key';
