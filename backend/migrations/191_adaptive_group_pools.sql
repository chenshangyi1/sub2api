-- Durable topology for public Adaptive parent groups. Model capability remains
-- authoritative on each leaf group's schedulable accounts; this schema only
-- defines which leaf groups a parent is allowed to consider.

CREATE TABLE IF NOT EXISTS adaptive_group_configs (
    id                BIGSERIAL PRIMARY KEY,
    parent_group_id   BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    enabled           BOOLEAN NOT NULL DEFAULT TRUE,
    config_generation BIGINT NOT NULL DEFAULT 1 CHECK (config_generation > 0),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_adaptive_group_configs_parent UNIQUE (parent_group_id),
    CONSTRAINT ck_adaptive_group_configs_parent_positive CHECK (parent_group_id > 0)
);

CREATE TABLE IF NOT EXISTS adaptive_group_memberships (
    id            BIGSERIAL PRIMARY KEY,
    config_id     BIGINT NOT NULL REFERENCES adaptive_group_configs(id) ON DELETE CASCADE,
    leaf_group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    enabled       BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order    INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_adaptive_group_memberships_leaf UNIQUE (config_id, leaf_group_id),
    CONSTRAINT ck_adaptive_group_memberships_leaf_positive CHECK (leaf_group_id > 0)
);

CREATE INDEX IF NOT EXISTS idx_adaptive_group_memberships_routing
    ON adaptive_group_memberships (config_id, enabled, sort_order, leaf_group_id);

CREATE INDEX IF NOT EXISTS idx_adaptive_group_memberships_leaf
    ON adaptive_group_memberships (leaf_group_id);

CREATE OR REPLACE FUNCTION next_adaptive_group_generation(current_generation BIGINT)
RETURNS BIGINT AS $$
BEGIN
    IF current_generation >= 9223372036854775807 THEN
        RAISE EXCEPTION 'adaptive config generation exhausted';
    END IF;
    RETURN current_generation + 1;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

-- A group has exactly one role in the Adaptive topology. Parent and leaf groups
-- must share the same platform, which keeps GPT and Claude pools isolated.
CREATE OR REPLACE FUNCTION validate_adaptive_group_parent()
RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
          FROM groups g
         WHERE g.id = NEW.parent_group_id
           AND g.status = 'active'
           AND g.deleted_at IS NULL
    ) THEN
        RAISE EXCEPTION 'adaptive parent group % must be active', NEW.parent_group_id;
    END IF;
    IF EXISTS (
        SELECT 1
          FROM adaptive_group_memberships m
         WHERE m.leaf_group_id = NEW.parent_group_id
    ) THEN
        RAISE EXCEPTION 'adaptive leaf group % cannot also be a parent', NEW.parent_group_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_validate_adaptive_group_parent ON adaptive_group_configs;
CREATE TRIGGER trg_validate_adaptive_group_parent
    BEFORE INSERT OR UPDATE OF parent_group_id ON adaptive_group_configs
    FOR EACH ROW EXECUTE FUNCTION validate_adaptive_group_parent();

CREATE OR REPLACE FUNCTION validate_adaptive_group_membership()
RETURNS TRIGGER AS $$
DECLARE
    parent_group BIGINT;
    parent_platform VARCHAR(50);
    leaf_platform VARCHAR(50);
BEGIN
    SELECT c.parent_group_id, g.platform
      INTO parent_group, parent_platform
      FROM adaptive_group_configs c
      JOIN groups g ON g.id = c.parent_group_id
     WHERE c.id = NEW.config_id
       AND g.status = 'active'
       AND g.deleted_at IS NULL;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'adaptive config % does not exist', NEW.config_id;
    END IF;
    IF NEW.leaf_group_id = parent_group THEN
        RAISE EXCEPTION 'adaptive parent group cannot reference itself';
    END IF;
    IF EXISTS (
        SELECT 1
          FROM adaptive_group_configs c
         WHERE c.parent_group_id = NEW.leaf_group_id
    ) THEN
        RAISE EXCEPTION 'adaptive parent group % cannot be used as a leaf', NEW.leaf_group_id;
    END IF;

    SELECT g.platform
      INTO leaf_platform
      FROM groups g
     WHERE g.id = NEW.leaf_group_id
       AND g.status = 'active'
       AND g.deleted_at IS NULL;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'adaptive leaf group % must be active', NEW.leaf_group_id;
    END IF;
    IF leaf_platform IS DISTINCT FROM parent_platform THEN
        RAISE EXCEPTION 'adaptive parent and leaf platforms must match';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_validate_adaptive_group_membership ON adaptive_group_memberships;
CREATE TRIGGER trg_validate_adaptive_group_membership
    BEFORE INSERT OR UPDATE OF config_id, leaf_group_id ON adaptive_group_memberships
    FOR EACH ROW EXECUTE FUNCTION validate_adaptive_group_membership();

-- Platform edits must preserve existing pool isolation regardless of which
-- admin path performs the group update.
CREATE OR REPLACE FUNCTION validate_adaptive_group_platform_update()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.platform IS NOT DISTINCT FROM OLD.platform THEN
        RETURN NEW;
    END IF;
    IF EXISTS (
        SELECT 1
          FROM adaptive_group_configs c
          JOIN adaptive_group_memberships m ON m.config_id = c.id
          JOIN groups leaf_group ON leaf_group.id = m.leaf_group_id
         WHERE c.parent_group_id = NEW.id
           AND leaf_group.platform IS DISTINCT FROM NEW.platform
    ) OR EXISTS (
        SELECT 1
          FROM adaptive_group_memberships m
          JOIN adaptive_group_configs c ON c.id = m.config_id
          JOIN groups parent_group ON parent_group.id = c.parent_group_id
         WHERE m.leaf_group_id = NEW.id
           AND parent_group.platform IS DISTINCT FROM NEW.platform
    ) THEN
        RAISE EXCEPTION 'group platform update would invalidate an adaptive pool';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_validate_adaptive_group_platform_update ON groups;
CREATE TRIGGER trg_validate_adaptive_group_platform_update
    BEFORE UPDATE OF platform ON groups
    FOR EACH ROW EXECUTE FUNCTION validate_adaptive_group_platform_update();

-- Any topology mutation advances the parent generation. Route plans freeze
-- this value together with their leaf candidates and pricing snapshots.
CREATE OR REPLACE FUNCTION advance_adaptive_group_generation()
RETURNS TRIGGER AS $$
DECLARE
    old_config_id BIGINT;
    new_config_id BIGINT;
BEGIN
    IF TG_OP <> 'INSERT' THEN
        old_config_id := OLD.config_id;
    END IF;
    IF TG_OP <> 'DELETE' THEN
        new_config_id := NEW.config_id;
    END IF;

    IF old_config_id IS NOT NULL THEN
        UPDATE adaptive_group_configs
           SET config_generation = next_adaptive_group_generation(config_generation),
               updated_at = clock_timestamp()
         WHERE id = old_config_id;
    END IF;
    IF new_config_id IS NOT NULL AND new_config_id IS DISTINCT FROM old_config_id THEN
        UPDATE adaptive_group_configs
           SET config_generation = next_adaptive_group_generation(config_generation),
               updated_at = clock_timestamp()
         WHERE id = new_config_id;
    END IF;
    -- AFTER trigger return values are ignored.
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_advance_adaptive_group_generation ON adaptive_group_memberships;
CREATE TRIGGER trg_advance_adaptive_group_generation
    AFTER INSERT OR UPDATE OR DELETE ON adaptive_group_memberships
    FOR EACH ROW EXECUTE FUNCTION advance_adaptive_group_generation();

CREATE OR REPLACE FUNCTION enforce_adaptive_group_config_generation()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.enabled IS DISTINCT FROM OLD.enabled THEN
        NEW.config_generation := next_adaptive_group_generation(OLD.config_generation);
    ELSIF NEW.config_generation < OLD.config_generation THEN
        RAISE EXCEPTION 'adaptive config generation cannot move backwards';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_enforce_adaptive_group_config_generation ON adaptive_group_configs;
CREATE TRIGGER trg_enforce_adaptive_group_config_generation
    BEFORE UPDATE ON adaptive_group_configs
    FOR EACH ROW EXECUTE FUNCTION enforce_adaptive_group_config_generation();
