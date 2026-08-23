-- API Keys are system-wide resources.  Keep the legacy user_id foreign key
-- for schema compatibility, but point existing rows at the active technical
-- administrator so older readers also observe one owner.
DO $$
DECLARE
    admin_id BIGINT;
BEGIN
    SELECT id
      INTO admin_id
      FROM users
     WHERE role = 'admin'
       AND status = 'active'
     ORDER BY id
     LIMIT 1;

    IF admin_id IS NOT NULL THEN
        -- Batch image jobs retain the technical user_id column for schema
        -- compatibility, but are system-wide alongside API Keys.
        UPDATE batch_image_jobs AS jobs
           SET user_id = admin_id,
               updated_at = NOW()
         WHERE jobs.user_id IS DISTINCT FROM admin_id;

        UPDATE api_keys
           SET user_id = admin_id,
               updated_at = NOW()
         WHERE user_id IS DISTINCT FROM admin_id;
    END IF;
END $$;

-- Subscription billing has been retired. Normalize every legacy group to the
-- remaining standard billing model and clear fields that only affected a user
-- subscription. This is intentionally independent of whether an administrator
-- exists so a partially initialized database is normalized as well.
UPDATE groups
   SET subscription_type = 'standard',
       daily_limit_usd = NULL,
       weekly_limit_usd = NULL,
       monthly_limit_usd = NULL,
       peak_rate_enabled = FALSE,
       peak_start = '',
       peak_end = '',
       peak_rate_multiplier = 1.0,
       updated_at = NOW()
 WHERE subscription_type IS DISTINCT FROM 'standard'
    OR daily_limit_usd IS NOT NULL
    OR weekly_limit_usd IS NOT NULL
    OR monthly_limit_usd IS NOT NULL
    OR peak_rate_enabled IS DISTINCT FROM FALSE
    OR peak_start IS DISTINCT FROM ''
    OR peak_end IS DISTINCT FROM ''
    OR peak_rate_multiplier IS DISTINCT FROM 1.0;

COMMENT ON COLUMN api_keys.user_id IS
    'Legacy technical owner. API Keys are global and always use the active administrator subject.';

COMMENT ON COLUMN groups.subscription_type IS
    'Legacy compatibility field. Subscription billing is retired; all groups use standard billing.';
