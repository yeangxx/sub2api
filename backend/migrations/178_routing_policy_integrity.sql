-- Enforce the invariants relied upon by the runtime even when rows are
-- written outside the admin HTTP handlers.

CREATE UNIQUE INDEX IF NOT EXISTS uq_routing_policy_published_revision
    ON routing_policy_revisions (policy_id) WHERE state = 'published';
CREATE UNIQUE INDEX IF NOT EXISTS uq_upstream_price_book_published_revision
    ON upstream_price_book_revisions (price_book_id) WHERE state = 'published';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'routing_policy_revisions_policy_id_id_key'
    ) THEN
        ALTER TABLE routing_policy_revisions ADD CONSTRAINT routing_policy_revisions_policy_id_id_key UNIQUE (policy_id, id);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'upstream_price_book_revisions_price_book_id_id_key'
    ) THEN
        ALTER TABLE upstream_price_book_revisions ADD CONSTRAINT upstream_price_book_revisions_price_book_id_id_key UNIQUE (price_book_id, id);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'group_routing_policy_bindings_policy_revision_fk'
    ) THEN
        ALTER TABLE group_routing_policy_bindings
            ADD CONSTRAINT group_routing_policy_bindings_policy_revision_fk
            FOREIGN KEY (policy_id, revision_id)
            REFERENCES routing_policy_revisions(policy_id, id);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'routing_policies_draft_revision_fk'
    ) THEN
        ALTER TABLE routing_policies
            ADD CONSTRAINT routing_policies_draft_revision_fk
            FOREIGN KEY (id, draft_revision_id)
            REFERENCES routing_policy_revisions(policy_id, id)
            DEFERRABLE INITIALLY DEFERRED;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'routing_policies_published_revision_fk'
    ) THEN
        ALTER TABLE routing_policies
            ADD CONSTRAINT routing_policies_published_revision_fk
            FOREIGN KEY (id, published_revision_id)
            REFERENCES routing_policy_revisions(policy_id, id)
            DEFERRABLE INITIALLY DEFERRED;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'upstream_price_books_latest_revision_fk'
    ) THEN
        ALTER TABLE upstream_price_books
            ADD CONSTRAINT upstream_price_books_latest_revision_fk
            FOREIGN KEY (id, latest_revision_id)
            REFERENCES upstream_price_book_revisions(price_book_id, id)
            DEFERRABLE INITIALLY DEFERRED;
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION guard_upstream_price_revision_immutable()
RETURNS trigger AS $$
BEGIN
    IF NEW.price_book_id IS DISTINCT FROM OLD.price_book_id
       OR NEW.version IS DISTINCT FROM OLD.version
       OR NEW.source_snapshot IS DISTINCT FROM OLD.source_snapshot THEN
        RAISE EXCEPTION 'upstream price book revisions are immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_upstream_price_revision_immutable ON upstream_price_book_revisions;
CREATE TRIGGER trg_upstream_price_revision_immutable
    BEFORE UPDATE ON upstream_price_book_revisions
    FOR EACH ROW EXECUTE FUNCTION guard_upstream_price_revision_immutable();

CREATE OR REPLACE FUNCTION guard_upstream_model_price_immutable()
RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'upstream model prices are immutable';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_upstream_model_price_immutable ON upstream_price_model_prices;
CREATE TRIGGER trg_upstream_model_price_immutable
    BEFORE UPDATE OR DELETE ON upstream_price_model_prices
    FOR EACH ROW EXECUTE FUNCTION guard_upstream_model_price_immutable();
