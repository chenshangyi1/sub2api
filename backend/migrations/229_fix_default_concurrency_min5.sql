-- 将存量 default_concurrency 低于 5 的部署修正到 5。
-- 仅影响已通过管理面板被调低到 1–4 的实例；
-- 原本高于 5 的值（含从配置文件继承的 300）不受影响。
UPDATE settings
SET   value = '5'
WHERE key   = 'default_concurrency'
  AND value ~ '^\d+$'
  AND value::int < 5;
