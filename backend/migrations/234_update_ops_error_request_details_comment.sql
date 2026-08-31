-- Align the request diagnostics column comment with its raw-value semantics.
-- Migration 233's applied checksums are preserved; this correction is forward-only.

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

COMMENT ON COLUMN ops_error_logs.request_details IS
  'Original client request metadata and JSON payload for error diagnostics; bounded and not replayable';
