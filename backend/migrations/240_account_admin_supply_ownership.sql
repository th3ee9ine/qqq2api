-- Account administrators own the accounts they supply, and their configured
-- multiplier becomes the account-side earnings multiplier copied to accounts.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS supply_rate_multiplier DECIMAL(10,4) NOT NULL DEFAULT 1.0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'users_supply_rate_multiplier_nonnegative'
          AND conrelid = 'users'::regclass
    ) THEN
        ALTER TABLE users
            ADD CONSTRAINT users_supply_rate_multiplier_nonnegative
            CHECK (supply_rate_multiplier >= 0);
    END IF;
END $$;

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS account_admin_id BIGINT;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'accounts_account_admin_id_fkey'
          AND conrelid = 'accounts'::regclass
    ) THEN
        ALTER TABLE accounts
            ADD CONSTRAINT accounts_account_admin_id_fkey
            FOREIGN KEY (account_admin_id) REFERENCES users(id) ON DELETE SET NULL;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_accounts_account_admin_id
    ON accounts(account_admin_id);

COMMENT ON COLUMN users.supply_rate_multiplier IS
    'Account supply earnings multiplier configured for an account administrator.';
COMMENT ON COLUMN accounts.account_admin_id IS
    'Account administrator that supplied this account; NULL means no assigned owner.';
