ALTER TABLE channel_monitors ADD COLUMN IF NOT EXISTS account_id BIGINT;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'channel_monitors_account_id_fkey'
    ) THEN
        ALTER TABLE channel_monitors
            ADD CONSTRAINT channel_monitors_account_id_fkey
            FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE SET NULL;
    END IF;
END;
$$;

CREATE INDEX IF NOT EXISTS idx_channel_monitors_account_id ON channel_monitors(account_id);
