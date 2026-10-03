-- Upgrade the deployed legacy shape {"model": price} to the official v0.1.176
-- shape {"model": {"480p": price, "720p": price, "1080p": price}}.
-- A legacy model price applied to every resolution, so duplicating it across all
-- supported tiers preserves billing before administrators choose tier-specific prices.
WITH legacy AS (
    SELECT id,
           jsonb_object_agg(
               model,
               jsonb_build_object('480p', price, '720p', price, '1080p', price)
           ) AS nested_prices
    FROM groups
    CROSS JOIN LATERAL jsonb_each(video_model_prices) AS entry(model, price)
    WHERE jsonb_typeof(video_model_prices) = 'object'
      AND EXISTS (
          SELECT 1
          FROM jsonb_each(video_model_prices)
      )
      AND NOT EXISTS (
          SELECT 1
          FROM jsonb_each(video_model_prices) AS value_entry(_, value)
          WHERE jsonb_typeof(value) <> 'number'
      )
    GROUP BY id
)
UPDATE groups AS target
SET video_model_prices = legacy.nested_prices
FROM legacy
WHERE target.id = legacy.id;

COMMENT ON COLUMN groups.video_model_prices IS
    'Optional model-family x resolution video price overrides in USD per second; flat legacy values were expanded by migration 222';
