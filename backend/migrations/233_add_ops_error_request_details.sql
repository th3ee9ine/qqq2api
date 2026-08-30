-- Store a bounded, sanitized client request snapshot for error diagnostics.
-- This intentionally does not restore the retired request replay columns.

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

ALTER TABLE ops_error_logs
  ADD COLUMN IF NOT EXISTS request_details JSONB;

COMMENT ON COLUMN ops_error_logs.request_details IS
  'Sanitized client request metadata and JSON payload for error diagnostics; bounded and not replayable';
