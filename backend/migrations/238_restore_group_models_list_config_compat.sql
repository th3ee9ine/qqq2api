-- 238: restore the legacy groups.models_list_config column for the custom fork.
--
-- Upstream migration 235 renamed models_list_config to model_allowlist.  The
-- custom fork intentionally keeps both fields because the generated Ent
-- schema and legacy admin APIs still read/write models_list_config.  Databases
-- that applied the upstream migration therefore need the compatibility column
-- restored without losing the allowlist data.
--
-- The migration is idempotent and handles all upgrade states:
--   1) model_allowlist only: add models_list_config and copy the configuration;
--   2) both columns: only backfill an empty legacy column;
--   3) neither column: add models_list_config with the normal empty default.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_attribute
        WHERE attrelid = 'groups'::regclass
          AND attname = 'model_allowlist'
          AND NOT attisdropped
    ) THEN
        ALTER TABLE groups
            ADD COLUMN IF NOT EXISTS models_list_config JSONB NOT NULL DEFAULT '{}'::jsonb;

        EXECUTE $backfill$
            UPDATE groups
               SET models_list_config = model_allowlist
             WHERE COALESCE(models_list_config, '{}'::jsonb) = '{}'::jsonb
               AND COALESCE(model_allowlist, '{}'::jsonb) <> '{}'::jsonb
        $backfill$;
    ELSE
        ALTER TABLE groups
            ADD COLUMN IF NOT EXISTS models_list_config JSONB NOT NULL DEFAULT '{}'::jsonb;
    END IF;
END
$$;

UPDATE groups SET models_list_config = '{}'::jsonb WHERE models_list_config IS NULL;

ALTER TABLE groups
    ALTER COLUMN models_list_config SET DEFAULT '{}'::jsonb;
ALTER TABLE groups
    ALTER COLUMN models_list_config SET NOT NULL;

COMMENT ON COLUMN groups.models_list_config IS
    'Legacy group model list configuration retained for compatibility';
