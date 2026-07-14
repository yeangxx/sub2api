-- Seed editable, unbound routing policy presets. Existing groups are not
-- changed; administrators must publish/bind a preset explicitly.

INSERT INTO routing_policies (name, description, status)
VALUES
    ('economy', 'Lowest viable upstream cost with health and budget guards.', 'active'),
    ('standard', 'Balanced upstream cost, reliability and latency.', 'active'),
    ('professional', 'Trusted upstreams prioritized for reliability and latency.', 'active')
ON CONFLICT (name) DO NOTHING;

WITH presets(name, config) AS (
    VALUES
    ('economy', '{"schema_version":1,"scoring":{"price":65,"error_rate":15,"ttft":5,"load":10,"queue":5},"timeouts":{"request_timeout_ms":120000,"adaptive_soft_ttft":true,"soft_ttft_factor":1.25,"soft_ttft_min_ms":2500,"soft_ttft_max_ms":8000,"soft_ttft_ms":8000,"stream_idle_ms":30000},"retry":{"max_attempts":3,"max_switches":2,"retry_transport_errors":true,"retryable_status_codes":[408,409,429,500,502,503,504,529]},"hedge":{"enabled":true,"delay_ms":2500,"max_concurrent":2,"require_different_failure_domain":true,"require_no_semantic_output":true},"circuit_breaker":{"consecutive_failures":3,"min_samples":10,"error_rate_percent":50,"cooldown_ms":30000,"max_cooldown_ms":300000,"half_open_max_requests":1},"fallback":{"allow_cross_tier":true,"max_cost_multiplier":1.5,"require_explicit_model_map":true},"cost_budget":{},"candidate_filters":{"require_known_price":true}}'::jsonb),
    ('standard', '{"schema_version":1,"scoring":{"price":35,"error_rate":30,"ttft":20,"load":10,"queue":5},"timeouts":{"request_timeout_ms":120000,"adaptive_soft_ttft":true,"soft_ttft_factor":1.10,"soft_ttft_min_ms":1500,"soft_ttft_max_ms":5000,"soft_ttft_ms":5000,"stream_idle_ms":30000},"retry":{"max_attempts":3,"max_switches":2,"retry_transport_errors":true,"retryable_status_codes":[408,409,429,500,502,503,504,529]},"hedge":{"enabled":true,"delay_ms":1500,"max_concurrent":2,"require_different_failure_domain":true,"require_no_semantic_output":true},"circuit_breaker":{"consecutive_failures":3,"min_samples":10,"error_rate_percent":50,"cooldown_ms":30000,"max_cooldown_ms":300000,"half_open_max_requests":1},"fallback":{"allow_cross_tier":true,"max_cost_multiplier":2,"require_explicit_model_map":true},"cost_budget":{}}'::jsonb),
    ('professional', '{"schema_version":1,"scoring":{"price":10,"error_rate":45,"ttft":30,"load":10,"queue":5,"reliability":10},"timeouts":{"request_timeout_ms":120000,"adaptive_soft_ttft":true,"soft_ttft_factor":1,"soft_ttft_min_ms":800,"soft_ttft_max_ms":3000,"soft_ttft_ms":3000,"stream_idle_ms":30000},"retry":{"max_attempts":3,"max_switches":2,"retry_transport_errors":true,"retryable_status_codes":[408,409,429,500,502,503,504,529]},"hedge":{"enabled":true,"delay_ms":800,"max_concurrent":2,"require_different_failure_domain":true,"require_no_semantic_output":true},"circuit_breaker":{"consecutive_failures":3,"min_samples":10,"error_rate_percent":50,"cooldown_ms":30000,"max_cooldown_ms":300000,"half_open_max_requests":1},"fallback":{"allow_cross_tier":true,"max_cost_multiplier":3,"require_explicit_model_map":true},"cost_budget":{},"candidate_filters":{"require_trusted_upstream":true}}'::jsonb)
), created AS (
    INSERT INTO routing_policy_revisions (policy_id, version, state, schema_version, config, comment)
    SELECT p.id, 1, 'published', 1, presets.config, 'seeded editable preset'
    FROM presets JOIN routing_policies p ON p.name = presets.name
    WHERE NOT EXISTS (
        SELECT 1 FROM routing_policy_revisions r WHERE r.policy_id = p.id AND r.version = 1
    )
    RETURNING policy_id, id
)
UPDATE routing_policies p
SET published_revision_id = COALESCE(p.published_revision_id, c.id),
    updated_at = NOW()
FROM created c
WHERE p.id = c.policy_id;

-- Repair pointers when a previous migration run inserted the revision but was
-- interrupted before updating the policy row.
UPDATE routing_policies p
SET published_revision_id = r.id,
    updated_at = NOW()
FROM routing_policy_revisions r
WHERE r.policy_id = p.id
  AND r.state = 'published'
  AND p.name IN ('economy', 'standard', 'professional')
  AND p.published_revision_id IS NULL;
