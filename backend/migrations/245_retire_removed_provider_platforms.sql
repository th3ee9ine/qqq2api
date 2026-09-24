-- Retire provider integrations removed from the runtime.
--
-- Historical platform values and the migrations that introduced them remain
-- intact so existing rows can still be read and audited.  Existing accounts
-- and groups are made unavailable for scheduling; the application allowlists
-- reject new accounts, groups, routes, and API-key traffic for these values.
-- This is intentionally a soft retirement rather than a destructive delete.

UPDATE accounts
SET schedulable = FALSE,
    status = CASE WHEN status = 'active' THEN 'disabled' ELSE status END,
    error_message = COALESCE(NULLIF(error_message, ''), 'platform retired'),
    updated_at = NOW()
WHERE platform IN ('opencode_go', 'kimi', 'zhipu', 'minimax')
  AND deleted_at IS NULL;

UPDATE groups
SET status = CASE WHEN status = 'active' THEN 'disabled' ELSE status END,
    updated_at = NOW()
WHERE platform IN ('opencode_go', 'kimi', 'zhipu', 'minimax')
  AND deleted_at IS NULL;
