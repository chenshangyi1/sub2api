-- Unify legacy CN account/group platforms (kimi/zhipu/deepseek) into platform=cn.
-- Vendor is stored in credentials.cn_vendor. OpenAI/Anthropic/Grok rows are not touched.
-- Widen quota / composite-route CHECKs first so the UPDATE to 'cn' is legal.

ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                        'cn', 'video', 'kimi', 'zhipu', 'deepseek'));

ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;

ALTER TABLE composite_model_routes
    ADD CONSTRAINT composite_model_routes_target_platform_check
    CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                               'cn', 'video', 'kimi', 'zhipu', 'deepseek'));

UPDATE accounts
SET
    credentials = CASE
        WHEN COALESCE(credentials->>'cn_vendor', '') = '' THEN
            jsonb_set(COALESCE(credentials, '{}'::jsonb), '{cn_vendor}', to_jsonb(platform), true)
        ELSE credentials
    END,
    platform = 'cn'
WHERE platform IN ('kimi', 'zhipu', 'deepseek');

UPDATE groups
SET platform = 'cn'
WHERE platform IN ('kimi', 'zhipu', 'deepseek');

UPDATE composite_model_routes
SET target_platform = 'cn'
WHERE target_platform IN ('kimi', 'zhipu', 'deepseek');

-- Keep legacy kimi/zhipu/deepseek rows; also expose the unified identities.
UPDATE channel_monitor_v2_config
SET
    platforms = (
        SELECT COALESCE(jsonb_agg(elem), '[]'::jsonb)
        FROM (
            SELECT elem
            FROM jsonb_array_elements(platforms) AS existing(elem)
            UNION ALL
            SELECT elem
            FROM jsonb_array_elements(
                '[{"platform":"cn","enabled":true,"models":[]},{"platform":"video","enabled":true,"models":[]}]'::jsonb
            ) AS missing(elem)
            WHERE NOT EXISTS (
                SELECT 1
                FROM jsonb_array_elements(channel_monitor_v2_config.platforms) AS have(elem)
                WHERE lower(have.elem->>'platform') = lower(missing.elem->>'platform')
            )
        ) combined
    ),
    version = version + 1,
    updated_at = NOW()
WHERE id = 1
  AND (
      NOT (platforms @> '[{"platform":"cn"}]'::jsonb)
      OR NOT (platforms @> '[{"platform":"video"}]'::jsonb)
  );
