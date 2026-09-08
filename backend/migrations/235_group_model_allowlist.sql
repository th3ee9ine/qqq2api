-- 235: 保留旧 models_list_config，同时新增 model_allowlist 并回填。
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='groups' AND column_name='model_allowlist') THEN
        ALTER TABLE groups ADD COLUMN model_allowlist JSONB NOT NULL DEFAULT '{}'::jsonb;
    END IF;
END$$;
UPDATE groups SET model_allowlist = models_list_config
 WHERE COALESCE(model_allowlist, '{}'::jsonb) = '{}'::jsonb
   AND COALESCE(models_list_config, '{}'::jsonb) <> '{}'::jsonb;
COMMENT ON COLUMN groups.model_allowlist IS 'Group model allowlist: constrains both model listing responses and request admission';
