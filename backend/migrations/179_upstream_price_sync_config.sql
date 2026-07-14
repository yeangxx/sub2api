ALTER TABLE upstream_price_books
    ADD COLUMN IF NOT EXISTS source_config JSONB NOT NULL DEFAULT '{}'::jsonb;
