-- User-facing group visibility switch.
-- When false, the group is hidden from user-facing available-group lists
-- (API key binding, marketplace channels, etc.) while remaining fully usable
-- as Adaptive leaf / admin-managed routing infrastructure.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS user_visible BOOLEAN NOT NULL DEFAULT TRUE;

CREATE INDEX IF NOT EXISTS idx_groups_user_visible_active
    ON groups (user_visible)
    WHERE deleted_at IS NULL;
