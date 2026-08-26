-- API Keys own their concurrency limit independently from the technical
-- administrator account. Existing and new keys default to unlimited.
ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS concurrency INT NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
          FROM pg_constraint
         WHERE conname = 'api_keys_concurrency_non_negative'
           AND conrelid = 'api_keys'::regclass
    ) THEN
        ALTER TABLE api_keys
            ADD CONSTRAINT api_keys_concurrency_non_negative
            CHECK (concurrency >= 0);
    END IF;
END $$;

COMMENT ON COLUMN api_keys.concurrency IS
    'Concurrent request limit for this API key. Zero means unlimited.';
