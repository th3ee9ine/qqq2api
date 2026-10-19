-- Group cumulative billing is independent of request-log retention.
-- The existing daily rollups intentionally follow the remaining usage_logs;
-- keep those for today/yesterday, and store the lifetime amount separately.
CREATE TABLE IF NOT EXISTS usage_group_lifetime_totals (
    -- Independent from the source rows and object lifecycle; no cascade FK.
    group_id BIGINT PRIMARY KEY,
    actual_cost NUMERIC NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE usage_group_lifetime_totals IS
    'Cumulative group actual_cost retained after request-log cleanup and partition removal.';
COMMENT ON COLUMN usage_group_lifetime_totals.actual_cost IS
    'Sum of recorded usage_logs.actual_cost plus recoverable historical buckets; log deletion is not a refund.';

-- Serialize the seed and trigger installation with every raw-log mutation,
-- including direct writes to existing partitions. Ordinary reads continue.
-- Read state and buckets in the SAME seed statement below: its MVCC snapshot
-- also makes a concurrent daily rebuild atomic from the seed's perspective.
-- Do not additionally lock the state row here. Cleanup takes that row before
-- modifying usage_logs, while INSERT takes the source-table lock first; a
-- second lock here could deadlock either path depending on its order.
LOCK TABLE usage_logs IN SHARE MODE;

-- Prefer extant raw data for each group/day, even if a published daily bucket
-- is missing or stale. A valid, closed bucket supplements the seed only when
-- that group/day has no surviving raw records, so the sources never overlap.
-- Use the stored bucket timezone, not the migration connection's timezone.
-- Already-deleted logs AND deleted/invalid buckets cannot be reconstructed;
-- partially retained days are seeded from the surviving raw records only.
-- Manual replay must never replace durable totals with a shorter raw window.
WITH valid_state AS (
    SELECT timezone_name, closed_before
    FROM usage_group_rollup_state
    WHERE id = 1
        AND closed_before <= (clock_timestamp() AT TIME ZONE timezone_name)::date
),
raw_daily AS (
    SELECT
        ul.group_id,
        (ul.created_at AT TIME ZONE COALESCE(
            (SELECT timezone_name FROM valid_state), 'UTC'
        ))::date AS bucket_date,
        SUM(ul.actual_cost) AS actual_cost
    FROM usage_logs ul
    WHERE ul.group_id IS NOT NULL
    GROUP BY 1, 2
),
recoverable AS (
    SELECT group_id, actual_cost FROM raw_daily
    UNION ALL
    SELECT rollup.group_id, rollup.actual_cost
    FROM usage_group_daily_rollups rollup
    JOIN valid_state state ON rollup.bucket_date < state.closed_before
    WHERE NOT EXISTS (
        SELECT 1
        FROM raw_daily raw
        WHERE raw.group_id = rollup.group_id
            AND raw.bucket_date = rollup.bucket_date
    )
)
INSERT INTO usage_group_lifetime_totals (group_id, actual_cost, updated_at)
SELECT group_id, COALESCE(SUM(actual_cost), 0), NOW()
FROM recoverable
GROUP BY group_id
ORDER BY group_id
ON CONFLICT (group_id) DO NOTHING;

-- One statement-level UPSERT per affected group, even for a large log batch.
-- A heavily used group still has one counter-row lock; batching and sorted
-- lock acquisition keep this small without introducing shards to every read.
CREATE OR REPLACE FUNCTION usage_group_lifetime_after_insert()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO usage_group_lifetime_totals (group_id, actual_cost, updated_at)
    SELECT group_id, COALESCE(SUM(actual_cost), 0), NOW()
    FROM inserted_usage_logs
    WHERE group_id IS NOT NULL
    GROUP BY group_id
    ORDER BY group_id
    ON CONFLICT (group_id) DO UPDATE SET
        actual_cost = usage_group_lifetime_totals.actual_cost + EXCLUDED.actual_cost,
        updated_at = EXCLUDED.updated_at;

    RETURN NULL;
END;
$$;

-- Preserve corrections to retained source rows without rebuilding lifetime
-- totals from a potentially truncated log window. Transition tables also cover
-- UPDATEs that move a row between partitions, where AFTER ROW UPDATE does not
-- fire. Changes to created_at alone have zero net cost. DELETE has no trigger:
-- retention, manual log cleanup, cascades and DROP PARTITION are not refunds.
CREATE OR REPLACE FUNCTION usage_group_lifetime_after_update()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    -- Attribution corrections (including group_id -> NULL) subtract the old
    -- group's amount and add the new group's amount. ON DELETE SET NULL for a
    -- deleted group follows this same rule; it does not affect visible groups.
    INSERT INTO usage_group_lifetime_totals (group_id, actual_cost, updated_at)
    SELECT delta.group_id, SUM(delta.actual_cost), NOW()
    FROM (
        SELECT group_id, -actual_cost AS actual_cost FROM old_updated_usage_logs
        UNION ALL
        SELECT group_id, actual_cost FROM new_updated_usage_logs
    ) AS delta
    WHERE delta.group_id IS NOT NULL
    GROUP BY delta.group_id
    HAVING SUM(delta.actual_cost) <> 0
    ORDER BY delta.group_id
    ON CONFLICT (group_id) DO UPDATE SET
        actual_cost = usage_group_lifetime_totals.actual_cost + EXCLUDED.actual_cost,
        updated_at = EXCLUDED.updated_at;

    RETURN NULL;
END;
$$;

-- PostgreSQL runs same-kind triggers in name order. Keep the existing daily
-- rollup invalidation/state lock before these durable counter writes.
DROP TRIGGER IF EXISTS usage_logs_group_usage_lifetime_insert ON usage_logs;
CREATE TRIGGER usage_logs_group_usage_lifetime_insert
AFTER INSERT ON usage_logs
REFERENCING NEW TABLE AS inserted_usage_logs
FOR EACH STATEMENT
EXECUTE FUNCTION usage_group_lifetime_after_insert();

DROP TRIGGER IF EXISTS usage_logs_group_usage_lifetime_update ON usage_logs;
CREATE TRIGGER usage_logs_group_usage_lifetime_update
AFTER UPDATE ON usage_logs
REFERENCING OLD TABLE AS old_updated_usage_logs NEW TABLE AS new_updated_usage_logs
FOR EACH STATEMENT
EXECUTE FUNCTION usage_group_lifetime_after_update();

-- Statement-level triggers do not inherit onto partitions: install both on
-- every existing child for direct child writes. Statements routed through the
-- parent invoke only the parent's statement triggers, so these child triggers
-- do not double-count ordinary application IO.
DO $$
DECLARE
    child RECORD;
BEGIN
    FOR child IN
        WITH RECURSIVE descendants AS (
            SELECT inhrelid
            FROM pg_inherits
            WHERE inhparent = 'usage_logs'::regclass
            UNION ALL
            SELECT inh.inhrelid
            FROM pg_inherits inh
            JOIN descendants parent ON inh.inhparent = parent.inhrelid
        )
        SELECT child_ns.nspname AS schema_name, child_rel.relname AS table_name
        FROM descendants
        JOIN pg_class child_rel ON child_rel.oid = descendants.inhrelid
        JOIN pg_namespace child_ns ON child_ns.oid = child_rel.relnamespace
    LOOP
        EXECUTE format(
            'DROP TRIGGER IF EXISTS usage_logs_group_usage_lifetime_insert ON %I.%I',
            child.schema_name, child.table_name
        );
        EXECUTE format(
            'CREATE TRIGGER usage_logs_group_usage_lifetime_insert
             AFTER INSERT ON %I.%I
             REFERENCING NEW TABLE AS inserted_usage_logs
             FOR EACH STATEMENT
             EXECUTE FUNCTION usage_group_lifetime_after_insert()',
            child.schema_name, child.table_name
        );
        EXECUTE format(
            'DROP TRIGGER IF EXISTS usage_logs_group_usage_lifetime_update ON %I.%I',
            child.schema_name, child.table_name
        );
        EXECUTE format(
            'CREATE TRIGGER usage_logs_group_usage_lifetime_update
             AFTER UPDATE ON %I.%I
             REFERENCING OLD TABLE AS old_updated_usage_logs NEW TABLE AS new_updated_usage_logs
             FOR EACH STATEMENT
             EXECUTE FUNCTION usage_group_lifetime_after_update()',
            child.schema_name, child.table_name
        );
    END LOOP;
END;
$$;
