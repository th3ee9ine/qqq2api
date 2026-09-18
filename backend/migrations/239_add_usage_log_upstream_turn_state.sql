-- Snapshot of the header sent upstream. NULL = historical/unobserved;
-- empty string = observed request without a Turn State header.
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS upstream_turn_state TEXT;
COMMENT ON COLUMN usage_logs.upstream_turn_state IS 'Admin-only snapshot of the outbound x-codex-turn-state header';
