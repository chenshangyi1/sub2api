-- Follow-up safety net for environments that applied an older 230 which set
-- DEFAULT 1 (highest priority). Canonical 230 already uses DEFAULT 30; this
-- migration is idempotent and only reaffirms the bottom-of-band default so
-- unconfigured / imported accounts stay last in the 1..30 scheduling band
-- (same conservative semantics as the pre-band default of 50).
ALTER TABLE accounts ALTER COLUMN priority SET DEFAULT 30;
