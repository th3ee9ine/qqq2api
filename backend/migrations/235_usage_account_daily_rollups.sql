-- Persist per-account usage counters independently from usage_logs.
--
-- usage_logs is intentionally purged by the data-retention worker.  The
-- account list, however, exposes lifetime/today request, token and billing
-- counters.  Keeping one immutable daily bucket per account lets those
-- counters survive physical deletion of the source rows while retaining the
-- existing usage_logs retention policy.

CREATE TABLE IF NOT EXISTS usage_account_daily_rollups (
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    bucket_date DATE NOT NULL,
    total_requests BIGINT NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    -- Keep these aggregates untyped NUMERIC so repeated UPSERTs never round
    -- away fractional billing units before a retained raw row is reconciled.
    -- The source usage-log prices are already bounded NUMERIC values, while
    -- account multipliers can add scale to account_cost.
    standard_cost NUMERIC NOT NULL DEFAULT 0,
    account_cost NUMERIC NOT NULL DEFAULT 0,
    user_cost NUMERIC NOT NULL DEFAULT 0,
    total_duration_ms BIGINT NOT NULL DEFAULT 0,
    -- Number of rows with a recorded duration.  The account usage modal has
    -- historically used AVG(duration_ms), which ignores NULL durations.
    duration_count BIGINT NOT NULL DEFAULT 0,
    -- Lifetime counters exclude malformed/imported rows timestamped before
    -- the account was created or after the backfill/insert cutoff.  The
    -- backfill captures its cutoff after the table lock is acquired; the
    -- insert trigger captures one at trigger execution time.
    -- The all-row counters above intentionally keep the historical semantics
    -- of the account usage modal.
    lifetime_requests BIGINT NOT NULL DEFAULT 0,
    lifetime_input_tokens BIGINT NOT NULL DEFAULT 0,
    lifetime_output_tokens BIGINT NOT NULL DEFAULT 0,
    lifetime_cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    lifetime_cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    lifetime_standard_cost NUMERIC NOT NULL DEFAULT 0,
    lifetime_account_cost NUMERIC NOT NULL DEFAULT 0,
    lifetime_user_cost NUMERIC NOT NULL DEFAULT 0,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, bucket_date)
);

CREATE INDEX IF NOT EXISTS idx_usage_account_daily_rollups_bucket_date
    ON usage_account_daily_rollups (bucket_date DESC);

COMMENT ON TABLE usage_account_daily_rollups IS
    'Immutable per-account daily usage counters retained after usage_logs cleanup.';
COMMENT ON COLUMN usage_account_daily_rollups.bucket_date IS
    'Natural date in the database session/application timezone.';
COMMENT ON COLUMN usage_account_daily_rollups.standard_cost IS
    'Sum of usage_logs.total_cost (system/list-price billing).';
COMMENT ON COLUMN usage_account_daily_rollups.account_cost IS
    'Sum of account_stats_cost (or total_cost) multiplied by account_rate_multiplier.';
COMMENT ON COLUMN usage_account_daily_rollups.user_cost IS
    'Sum of usage_logs.actual_cost (user/API-key billing).';

-- Backfill rows written before this migration.  A bucket that already exists
-- is left untouched: replaying this file after retention cleanup must never
-- replace a durable total with a partial aggregate of the remaining raw rows.
-- (The migration runner records this file in schema_migrations, but keeping
-- the statement safe on manual replay protects development/repair workflows.)
-- Hold a write-conflicting lock while the backfill and trigger installation
-- complete.  Without this, an INSERT committed between the backfill SELECT
-- and CREATE TRIGGER could be absent from both the historical snapshot and
-- the new rollup.  SHARE permits ordinary reads while serializing inserts,
-- including inserts routed to partitions of usage_logs.
LOCK TABLE usage_logs IN SHARE MODE;

DO $backfill$
DECLARE
    -- The migration runner sends this file as one multi-statement query.  A
    -- statement_timestamp() there would be fixed at the query-message start,
    -- before LOCK TABLE has finished waiting.  Capture a single wall-clock
    -- cutoff inside this block, which starts only after the lock is held.
    cutoff timestamptz := clock_timestamp();
BEGIN
INSERT INTO usage_account_daily_rollups (
    account_id,
    bucket_date,
    total_requests,
    input_tokens,
    output_tokens,
    cache_creation_tokens,
    cache_read_tokens,
    standard_cost,
    account_cost,
    user_cost,
    total_duration_ms,
    duration_count,
    lifetime_requests,
    lifetime_input_tokens,
    lifetime_output_tokens,
    lifetime_cache_creation_tokens,
    lifetime_cache_read_tokens,
    lifetime_standard_cost,
    lifetime_account_cost,
    lifetime_user_cost,
    computed_at
)
SELECT
    ul.account_id,
    (ul.created_at AT TIME ZONE current_setting('TimeZone'))::date AS bucket_date,
    COUNT(*) AS total_requests,
    COALESCE(SUM(COALESCE(ul.input_tokens, 0)), 0) AS input_tokens,
    COALESCE(SUM(COALESCE(ul.output_tokens, 0)), 0) AS output_tokens,
    COALESCE(SUM(COALESCE(ul.cache_creation_tokens, 0)), 0) AS cache_creation_tokens,
    COALESCE(SUM(COALESCE(ul.cache_read_tokens, 0)), 0) AS cache_read_tokens,
    COALESCE(SUM(COALESCE(ul.total_cost, 0)), 0) AS standard_cost,
    COALESCE(SUM(COALESCE(ul.account_stats_cost, ul.total_cost, 0) * COALESCE(ul.account_rate_multiplier, 1)), 0) AS account_cost,
    COALESCE(SUM(COALESCE(ul.actual_cost, 0)), 0) AS user_cost,
    COALESCE(SUM(COALESCE(ul.duration_ms, 0)), 0) AS total_duration_ms,
    COUNT(ul.duration_ms) AS duration_count,
    COUNT(*) FILTER (WHERE ul.created_at >= a.created_at AND ul.created_at <= cutoff) AS lifetime_requests,
    COALESCE(SUM(COALESCE(ul.input_tokens, 0)) FILTER (WHERE ul.created_at >= a.created_at AND ul.created_at <= cutoff), 0) AS lifetime_input_tokens,
    COALESCE(SUM(COALESCE(ul.output_tokens, 0)) FILTER (WHERE ul.created_at >= a.created_at AND ul.created_at <= cutoff), 0) AS lifetime_output_tokens,
    COALESCE(SUM(COALESCE(ul.cache_creation_tokens, 0)) FILTER (WHERE ul.created_at >= a.created_at AND ul.created_at <= cutoff), 0) AS lifetime_cache_creation_tokens,
    COALESCE(SUM(COALESCE(ul.cache_read_tokens, 0)) FILTER (WHERE ul.created_at >= a.created_at AND ul.created_at <= cutoff), 0) AS lifetime_cache_read_tokens,
    COALESCE(SUM(COALESCE(ul.total_cost, 0)) FILTER (WHERE ul.created_at >= a.created_at AND ul.created_at <= cutoff), 0) AS lifetime_standard_cost,
    COALESCE(SUM(COALESCE(ul.account_stats_cost, ul.total_cost, 0) * COALESCE(ul.account_rate_multiplier, 1)) FILTER (WHERE ul.created_at >= a.created_at AND ul.created_at <= cutoff), 0) AS lifetime_account_cost,
    COALESCE(SUM(COALESCE(ul.actual_cost, 0)) FILTER (WHERE ul.created_at >= a.created_at AND ul.created_at <= cutoff), 0) AS lifetime_user_cost,
    NOW()
FROM usage_logs ul
JOIN accounts a ON a.id = ul.account_id
WHERE ul.account_id IS NOT NULL
GROUP BY ul.account_id, (ul.created_at AT TIME ZONE current_setting('TimeZone'))::date
ON CONFLICT (account_id, bucket_date)
DO NOTHING;
END;
$backfill$;

-- Keep the rollup in sync for every normal and batched usage-log INSERT.  A
-- statement trigger with a transition table performs one UPSERT per account /
-- day rather than one row-level lock per request.
CREATE OR REPLACE FUNCTION usage_account_daily_rollup_after_insert()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    -- Capture one cutoff for the whole inserted statement.  Besides keeping
    -- all lifetime columns internally consistent, clock_timestamp() reflects
    -- the time after any preceding INSERT work and does not inherit a stale
    -- transaction-start timestamp.
    cutoff timestamptz := clock_timestamp();
BEGIN
    INSERT INTO usage_account_daily_rollups (
        account_id,
        bucket_date,
        total_requests,
        input_tokens,
        output_tokens,
        cache_creation_tokens,
        cache_read_tokens,
        standard_cost,
        account_cost,
        user_cost,
        total_duration_ms,
        duration_count,
        lifetime_requests,
        lifetime_input_tokens,
        lifetime_output_tokens,
        lifetime_cache_creation_tokens,
        lifetime_cache_read_tokens,
        lifetime_standard_cost,
        lifetime_account_cost,
        lifetime_user_cost,
        computed_at
    )
    SELECT
        i.account_id,
        (i.created_at AT TIME ZONE current_setting('TimeZone'))::date AS bucket_date,
        COUNT(*) AS total_requests,
        COALESCE(SUM(COALESCE(i.input_tokens, 0)), 0) AS input_tokens,
        COALESCE(SUM(COALESCE(i.output_tokens, 0)), 0) AS output_tokens,
        COALESCE(SUM(COALESCE(i.cache_creation_tokens, 0)), 0) AS cache_creation_tokens,
        COALESCE(SUM(COALESCE(i.cache_read_tokens, 0)), 0) AS cache_read_tokens,
        COALESCE(SUM(COALESCE(i.total_cost, 0)), 0) AS standard_cost,
        COALESCE(SUM(COALESCE(i.account_stats_cost, i.total_cost, 0) * COALESCE(i.account_rate_multiplier, 1)), 0) AS account_cost,
        COALESCE(SUM(COALESCE(i.actual_cost, 0)), 0) AS user_cost,
        COALESCE(SUM(COALESCE(i.duration_ms, 0)), 0) AS total_duration_ms,
        COUNT(i.duration_ms) AS duration_count,
        COUNT(*) FILTER (WHERE i.created_at >= a.created_at AND i.created_at <= cutoff) AS lifetime_requests,
        COALESCE(SUM(COALESCE(i.input_tokens, 0)) FILTER (WHERE i.created_at >= a.created_at AND i.created_at <= cutoff), 0) AS lifetime_input_tokens,
        COALESCE(SUM(COALESCE(i.output_tokens, 0)) FILTER (WHERE i.created_at >= a.created_at AND i.created_at <= cutoff), 0) AS lifetime_output_tokens,
        COALESCE(SUM(COALESCE(i.cache_creation_tokens, 0)) FILTER (WHERE i.created_at >= a.created_at AND i.created_at <= cutoff), 0) AS lifetime_cache_creation_tokens,
        COALESCE(SUM(COALESCE(i.cache_read_tokens, 0)) FILTER (WHERE i.created_at >= a.created_at AND i.created_at <= cutoff), 0) AS lifetime_cache_read_tokens,
        COALESCE(SUM(COALESCE(i.total_cost, 0)) FILTER (WHERE i.created_at >= a.created_at AND i.created_at <= cutoff), 0) AS lifetime_standard_cost,
        COALESCE(SUM(COALESCE(i.account_stats_cost, i.total_cost, 0) * COALESCE(i.account_rate_multiplier, 1)) FILTER (WHERE i.created_at >= a.created_at AND i.created_at <= cutoff), 0) AS lifetime_account_cost,
        COALESCE(SUM(COALESCE(i.actual_cost, 0)) FILTER (WHERE i.created_at >= a.created_at AND i.created_at <= cutoff), 0) AS lifetime_user_cost,
        NOW()
    FROM inserted_usage_logs i
    JOIN accounts a ON a.id = i.account_id
    WHERE i.account_id IS NOT NULL
    GROUP BY i.account_id, (i.created_at AT TIME ZONE current_setting('TimeZone'))::date
    -- Keep UPSERT lock acquisition deterministic across concurrent batches.
    -- Without this sort, hash aggregation may emit account/day groups in
    -- different orders and two batches can deadlock while taking the same
    -- rollup row locks.
    ORDER BY i.account_id, (i.created_at AT TIME ZONE current_setting('TimeZone'))::date
    ON CONFLICT (account_id, bucket_date)
    DO UPDATE SET
        total_requests = usage_account_daily_rollups.total_requests + EXCLUDED.total_requests,
        input_tokens = usage_account_daily_rollups.input_tokens + EXCLUDED.input_tokens,
        output_tokens = usage_account_daily_rollups.output_tokens + EXCLUDED.output_tokens,
        cache_creation_tokens = usage_account_daily_rollups.cache_creation_tokens + EXCLUDED.cache_creation_tokens,
        cache_read_tokens = usage_account_daily_rollups.cache_read_tokens + EXCLUDED.cache_read_tokens,
        standard_cost = usage_account_daily_rollups.standard_cost + EXCLUDED.standard_cost,
        account_cost = usage_account_daily_rollups.account_cost + EXCLUDED.account_cost,
        user_cost = usage_account_daily_rollups.user_cost + EXCLUDED.user_cost,
        total_duration_ms = usage_account_daily_rollups.total_duration_ms + EXCLUDED.total_duration_ms,
        duration_count = usage_account_daily_rollups.duration_count + EXCLUDED.duration_count,
        lifetime_requests = usage_account_daily_rollups.lifetime_requests + EXCLUDED.lifetime_requests,
        lifetime_input_tokens = usage_account_daily_rollups.lifetime_input_tokens + EXCLUDED.lifetime_input_tokens,
        lifetime_output_tokens = usage_account_daily_rollups.lifetime_output_tokens + EXCLUDED.lifetime_output_tokens,
        lifetime_cache_creation_tokens = usage_account_daily_rollups.lifetime_cache_creation_tokens + EXCLUDED.lifetime_cache_creation_tokens,
        lifetime_cache_read_tokens = usage_account_daily_rollups.lifetime_cache_read_tokens + EXCLUDED.lifetime_cache_read_tokens,
        lifetime_standard_cost = usage_account_daily_rollups.lifetime_standard_cost + EXCLUDED.lifetime_standard_cost,
        lifetime_account_cost = usage_account_daily_rollups.lifetime_account_cost + EXCLUDED.lifetime_account_cost,
        lifetime_user_cost = usage_account_daily_rollups.lifetime_user_cost + EXCLUDED.lifetime_user_cost,
        computed_at = EXCLUDED.computed_at;

    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS usage_logs_account_daily_rollup_insert ON usage_logs;
CREATE TRIGGER usage_logs_account_daily_rollup_insert
AFTER INSERT ON usage_logs
REFERENCING NEW TABLE AS inserted_usage_logs
FOR EACH STATEMENT
EXECUTE FUNCTION usage_account_daily_rollup_after_insert();

-- PostgreSQL routes ordinary INSERT INTO usage_logs statements through the
-- parent trigger above.  A caller can nevertheless target a partition
-- directly (for example, a bulk import or a maintenance job); statement
-- triggers on a partitioned parent are not invoked for that form.  Install
-- the same transition-table trigger on currently existing partitions so
-- direct writes cannot bypass the durable counters.  Future partitions still
-- inherit the parent trigger for normal routed writes and should be created
-- through INSERT INTO usage_logs rather than addressed directly.
DO $$
DECLARE
    child RECORD;
BEGIN
    FOR child IN
        SELECT child_ns.nspname AS schema_name, child_rel.relname AS table_name
        FROM pg_inherits inh
        JOIN pg_class parent_rel ON parent_rel.oid = inh.inhparent
        JOIN pg_namespace parent_ns ON parent_ns.oid = parent_rel.relnamespace
        JOIN pg_class child_rel ON child_rel.oid = inh.inhrelid
        JOIN pg_namespace child_ns ON child_ns.oid = child_rel.relnamespace
        WHERE parent_ns.nspname = current_schema()
          AND parent_rel.relname = 'usage_logs'
    LOOP
        EXECUTE format(
            'DROP TRIGGER IF EXISTS usage_logs_account_daily_rollup_insert ON %I.%I',
            child.schema_name,
            child.table_name
        );
        EXECUTE format(
            'CREATE TRIGGER usage_logs_account_daily_rollup_insert
             AFTER INSERT ON %I.%I
             REFERENCING NEW TABLE AS inserted_usage_logs
             FOR EACH STATEMENT
             EXECUTE FUNCTION usage_account_daily_rollup_after_insert()',
            child.schema_name,
            child.table_name
        );
    END LOOP;
END;
$$;
