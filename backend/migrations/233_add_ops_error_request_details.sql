-- Store a bounded client request snapshot for error diagnostics. Values are
-- retained as received (without field redaction); the bound protects queue and
-- database resources.
-- This intentionally does not restore the retired request replay columns.

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

ALTER TABLE ops_error_logs
  ADD COLUMN IF NOT EXISTS request_details JSONB;

COMMENT ON COLUMN ops_error_logs.request_details IS
  'Original client request metadata and JSON payload for error diagnostics; bounded and not replayable';
