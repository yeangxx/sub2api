-- Configurable multi-upstream routing control plane.
--
-- The tables are deliberately SQL-backed (rather than Ent entities) because
-- routing policies are versioned JSON documents and are read through a small
-- hot-path cache.  Existing groups have no binding and therefore keep the
-- legacy scheduler unchanged.

CREATE TABLE IF NOT EXISTS routing_policies (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(120) NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    draft_revision_id BIGINT,
    published_revision_id BIGINT,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT routing_policies_status_check CHECK (status IN ('active', 'disabled', 'archived'))
);

CREATE TABLE IF NOT EXISTS routing_policy_revisions (
    id BIGSERIAL PRIMARY KEY,
    policy_id BIGINT NOT NULL REFERENCES routing_policies(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    state VARCHAR(20) NOT NULL DEFAULT 'draft',
    schema_version INTEGER NOT NULL DEFAULT 1,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    checksum VARCHAR(128) NOT NULL DEFAULT '',
    comment TEXT NOT NULL DEFAULT '',
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ,
    CONSTRAINT routing_policy_revisions_state_check CHECK (state IN ('draft', 'published', 'archived')),
    CONSTRAINT routing_policy_revisions_version_check CHECK (version > 0),
    CONSTRAINT routing_policy_revisions_schema_version_check CHECK (schema_version > 0),
    UNIQUE (policy_id, version)
);

CREATE INDEX IF NOT EXISTS idx_routing_policy_revisions_policy_state
    ON routing_policy_revisions (policy_id, state, version DESC);

-- A revision's identity and config are immutable once inserted.  Publication
-- only changes state/published_at and is therefore still allowed by this guard.
CREATE OR REPLACE FUNCTION guard_routing_policy_revision_immutable()
RETURNS trigger AS $$
BEGIN
    IF NEW.policy_id IS DISTINCT FROM OLD.policy_id
       OR NEW.version IS DISTINCT FROM OLD.version
       OR NEW.schema_version IS DISTINCT FROM OLD.schema_version
       OR NEW.config IS DISTINCT FROM OLD.config
       OR NEW.checksum IS DISTINCT FROM OLD.checksum THEN
        RAISE EXCEPTION 'routing policy revisions are immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_routing_policy_revision_immutable ON routing_policy_revisions;
CREATE TRIGGER trg_routing_policy_revision_immutable
    BEFORE UPDATE ON routing_policy_revisions
    FOR EACH ROW EXECUTE FUNCTION guard_routing_policy_revision_immutable();

CREATE TABLE IF NOT EXISTS group_routing_policy_bindings (
    group_id BIGINT PRIMARY KEY REFERENCES groups(id) ON DELETE CASCADE,
    policy_id BIGINT NOT NULL REFERENCES routing_policies(id) ON DELETE RESTRICT,
    revision_id BIGINT REFERENCES routing_policy_revisions(id) ON DELETE SET NULL,
    mode VARCHAR(20) NOT NULL DEFAULT 'enforce',
    model_overrides JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT group_routing_policy_bindings_mode_check CHECK (mode IN ('shadow', 'enforce'))
);

CREATE INDEX IF NOT EXISTS idx_group_routing_policy_bindings_policy
    ON group_routing_policy_bindings (policy_id, mode);

CREATE TABLE IF NOT EXISTS routing_policy_audit_logs (
    id BIGSERIAL PRIMARY KEY,
    policy_id BIGINT REFERENCES routing_policies(id) ON DELETE SET NULL,
    revision_id BIGINT REFERENCES routing_policy_revisions(id) ON DELETE SET NULL,
    group_id BIGINT REFERENCES groups(id) ON DELETE SET NULL,
    actor_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    action VARCHAR(40) NOT NULL,
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_routing_policy_audit_logs_policy_created
    ON routing_policy_audit_logs (policy_id, created_at DESC);

CREATE TABLE IF NOT EXISTS upstream_price_books (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(120) NOT NULL UNIQUE,
    source VARCHAR(20) NOT NULL DEFAULT 'manual',
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    currency CHAR(3) NOT NULL DEFAULT 'USD',
    latest_revision_id BIGINT,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT upstream_price_books_source_check CHECK (source IN ('manual', 'http_json')),
    CONSTRAINT upstream_price_books_status_check CHECK (status IN ('active', 'disabled', 'archived'))
);

CREATE TABLE IF NOT EXISTS upstream_price_book_revisions (
    id BIGSERIAL PRIMARY KEY,
    price_book_id BIGINT NOT NULL REFERENCES upstream_price_books(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    state VARCHAR(20) NOT NULL DEFAULT 'draft',
    effective_at TIMESTAMPTZ,
    source_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    comment TEXT NOT NULL DEFAULT '',
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ,
    CONSTRAINT upstream_price_book_revisions_state_check CHECK (state IN ('draft', 'published', 'archived')),
    CONSTRAINT upstream_price_book_revisions_version_check CHECK (version > 0),
    UNIQUE (price_book_id, version)
);

CREATE INDEX IF NOT EXISTS idx_upstream_price_book_revisions_book_state
    ON upstream_price_book_revisions (price_book_id, state, version DESC);

CREATE TABLE IF NOT EXISTS upstream_price_model_prices (
    id BIGSERIAL PRIMARY KEY,
    revision_id BIGINT NOT NULL REFERENCES upstream_price_book_revisions(id) ON DELETE CASCADE,
    model_pattern VARCHAR(200) NOT NULL,
    input_price_per_million NUMERIC(20, 8),
    output_price_per_million NUMERIC(20, 8),
    cache_read_price_per_million NUMERIC(20, 8),
    cache_write_price_per_million NUMERIC(20, 8),
    request_price NUMERIC(20, 8),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT upstream_price_model_prices_nonnegative_check CHECK (
        COALESCE(input_price_per_million, 0) >= 0
        AND COALESCE(output_price_per_million, 0) >= 0
        AND COALESCE(cache_read_price_per_million, 0) >= 0
        AND COALESCE(cache_write_price_per_million, 0) >= 0
        AND COALESCE(request_price, 0) >= 0
    ),
    UNIQUE (revision_id, model_pattern)
);

CREATE INDEX IF NOT EXISTS idx_upstream_price_model_prices_pattern
    ON upstream_price_model_prices (model_pattern);

ALTER TABLE accounts ADD COLUMN IF NOT EXISTS failure_domain VARCHAR(100);
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS reliability_class VARCHAR(30);
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS routing_labels JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS price_book_id BIGINT;

CREATE INDEX IF NOT EXISTS idx_accounts_failure_domain ON accounts (failure_domain);
CREATE INDEX IF NOT EXISTS idx_accounts_reliability_class ON accounts (reliability_class);
CREATE INDEX IF NOT EXISTS idx_accounts_price_book_id ON accounts (price_book_id);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'accounts_price_book_id_fkey'
    ) THEN
        ALTER TABLE accounts
            ADD CONSTRAINT accounts_price_book_id_fkey
            FOREIGN KEY (price_book_id) REFERENCES upstream_price_books(id) ON DELETE SET NULL;
    END IF;
END;
$$;
