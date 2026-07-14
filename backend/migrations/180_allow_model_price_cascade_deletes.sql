-- Migration 178 originally made model prices immutable for both updates and
-- deletes. Keep updates immutable while allowing cascaded deletes of parent
-- price book revisions.

DROP TRIGGER IF EXISTS trg_upstream_model_price_immutable ON upstream_price_model_prices;
CREATE TRIGGER trg_upstream_model_price_immutable
    BEFORE UPDATE ON upstream_price_model_prices
    FOR EACH ROW EXECUTE FUNCTION guard_upstream_model_price_immutable();
