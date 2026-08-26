-- Limit how many non-deleted accounts automatic assignment may place on one proxy.
-- Zero preserves the legacy unlimited behavior.
ALTER TABLE proxies
    ADD COLUMN IF NOT EXISTS max_accounts INT NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
          FROM pg_constraint
         WHERE conname = 'proxies_max_accounts_non_negative'
           AND conrelid = 'proxies'::regclass
    ) THEN
        ALTER TABLE proxies
            ADD CONSTRAINT proxies_max_accounts_non_negative
            CHECK (max_accounts >= 0);
    END IF;
END $$;

COMMENT ON COLUMN proxies.max_accounts IS
    'Non-deleted account limit for automatic proxy assignment. Zero means unlimited.';
