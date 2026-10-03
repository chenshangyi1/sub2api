-- Account scheduling priority is a compact 1..30 band (1 = highest priority).
-- Normalize legacy data before constraining new requests so routing and
-- administrative APIs agree.
--
-- The column default is 30 (lowest priority band), NOT 1: a default of 1 would
-- make every newly created/imported account the hottest routing target and
-- reverse the relative order of existing accounts (legacy default 50 clamps to
-- 30). Keeping the default at the bottom of the band preserves the conservative
-- "unconfigured = last" semantics of the pre-1..30 default (50).
ALTER TABLE accounts ALTER COLUMN priority SET DEFAULT 30;

UPDATE accounts
SET priority = CASE
  WHEN priority < 1 THEN 1
  WHEN priority > 30 THEN 30
  ELSE priority
END
WHERE priority < 1 OR priority > 30;
