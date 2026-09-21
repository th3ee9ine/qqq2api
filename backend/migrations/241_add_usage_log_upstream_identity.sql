-- Final outbound Codex identity header snapshots. NULL means the request was
-- historical or unobserved; empty string means an observed request omitted it.
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS upstream_originator TEXT,
    ADD COLUMN IF NOT EXISTS upstream_user_agent TEXT,
    ADD COLUMN IF NOT EXISTS upstream_version TEXT;

COMMENT ON COLUMN usage_logs.upstream_originator IS 'Admin-only snapshot of the outbound Originator header';
COMMENT ON COLUMN usage_logs.upstream_user_agent IS 'Admin-only snapshot of the outbound User-Agent header';
COMMENT ON COLUMN usage_logs.upstream_version IS 'Admin-only snapshot of the outbound Version header';
