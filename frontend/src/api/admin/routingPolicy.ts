import { apiClient } from '../client'

export type RoutingMode = 'shadow' | 'enforce'
export type RoutingPreset = 'economy' | 'standard' | 'professional'
export type RoutingPolicyStatus = 'active' | 'disabled' | 'archived'
export type RevisionState = 'draft' | 'published' | 'archived'

export interface RoutingCandidateFilters {
  platforms: string[]
  required_labels: Record<string, string>
  excluded_labels: Record<string, string>
  reliability_classes: string[]
  require_known_price: boolean
  require_trusted_upstream: boolean
}

export interface RoutingScoringWeights {
  price: number
  error_rate: number
  ttft: number
  load: number
  queue: number
  reliability: number
}

export interface RoutingTimeoutPolicy {
  request_timeout_ms: number
  soft_ttft_ms: number
  adaptive_soft_ttft: boolean
  soft_ttft_factor: number
  soft_ttft_min_ms: number
  soft_ttft_max_ms: number
  stream_idle_ms: number
}

export interface RoutingRetryPolicy {
  max_attempts: number
  retryable_status_codes: number[]
  retry_transport_errors: boolean
  max_switches: number
}

export interface RoutingHedgePolicy {
  enabled: boolean
  delay_ms: number
  max_concurrent: number
  require_different_failure_domain: boolean
  require_no_semantic_output: boolean
}

export interface RoutingCircuitBreakerPolicy {
  consecutive_failures: number
  min_samples: number
  error_rate_percent: number
  cooldown_ms: number
  max_cooldown_ms: number
  half_open_max_requests: number
}

export interface RoutingFallbackPolicy {
  group_ids: number[]
  allow_cross_tier: boolean
  max_cost_multiplier: number
  require_explicit_model_map: boolean
}

export interface RoutingCostBudget {
  max_upstream_usd: string
  max_attempt_cost_usd: string
  reserve_for_hedge_usd: string
}

export interface RoutingPolicyConfig {
  schema_version: 1
  candidate_filters: RoutingCandidateFilters
  scoring: RoutingScoringWeights
  timeouts: RoutingTimeoutPolicy
  retry: RoutingRetryPolicy
  hedge: RoutingHedgePolicy
  circuit_breaker: RoutingCircuitBreakerPolicy
  fallback: RoutingFallbackPolicy
  model_mappings: Record<string, string>
  cost_budget: RoutingCostBudget
}

export interface RoutingPolicy {
  id: number
  name: string
  description: string
  status: RoutingPolicyStatus
  draft_revision_id?: number | null
  published_revision_id?: number | null
  created_at: string
  updated_at: string
}

export interface RoutingRevision {
  id: number
  policy_id: number
  version: number
  state: RevisionState
  schema_version: number
  config: RoutingPolicyConfig
  checksum: string
  comment: string
  created_at: string
  published_at?: string | null
}

export interface PriceSourceConfig {
  url?: string
  headers?: Record<string, string>
}

export interface UpstreamModelPrice {
  id?: number
  revision_id?: number
  model_pattern: string
  input_price_per_million: string
  output_price_per_million: string
  cache_read_price_per_million: string
  cache_write_price_per_million: string
  request_price: string
  metadata?: Record<string, unknown>
  created_at?: string
}

export interface PriceBook {
  id: number
  name: string
  source: 'manual' | 'http_json'
  status: RoutingPolicyStatus
  currency: string
  latest_revision_id?: number | null
  source_config?: PriceSourceConfig
  created_at: string
  updated_at: string
}

export interface PriceBookRevision {
  id: number
  price_book_id: number
  version: number
  state: RevisionState
  effective_at?: string | null
  source_snapshot?: Record<string, unknown>
  comment: string
  prices: UpstreamModelPrice[]
  created_at: string
  published_at?: string | null
}

export interface RoutingSimulationAccount {
  id: number
  name: string
  platform: string
  failure_domain?: string
  reliability_class?: string
}

export interface RoutingSimulationCandidate {
  account_id: number
  account_name: string
  platform: string
  score: number
  estimated_cost_usd: number
  price_known: boolean
  excluded: boolean
  exclusion_reason?: string
  health: {
    error_rate: number
    ttft_ms: number
    load: number
    queue: number
    consecutive_failures: number
    samples: number
    circuit_state?: string
  }
}

export interface RoutingSimulationResult {
  group_id: number
  model: string
  policy: RoutingPolicy
  revision: RoutingRevision
  selection?: {
    selected_account?: RoutingSimulationAccount
    candidates: RoutingSimulationCandidate[]
  }
}

export interface RoutingPolicyCreateRequest {
  name: string
  description?: string
  status?: RoutingPolicyStatus
  config: RoutingPolicyConfig
  comment?: string
}

export interface RoutingPolicyUpdateRequest {
  name?: string
  description?: string
  status?: RoutingPolicyStatus
  config?: RoutingPolicyConfig
  comment?: string
}

export interface PriceBookInput {
  name: string
  source: 'manual' | 'http_json'
  status: RoutingPolicyStatus
  currency: string
  source_config?: PriceSourceConfig
}

export interface PriceBookRevisionInput {
  effective_at?: string
  comment?: string
  prices: UpstreamModelPrice[]
}

export const routingPolicyApi = {
  async list(): Promise<RoutingPolicy[]> {
    const { data } = await apiClient.get<RoutingPolicy[]>('/admin/routing-policies')
    return data
  },
  async get(id: number): Promise<RoutingPolicy> {
    const { data } = await apiClient.get<RoutingPolicy>(`/admin/routing-policies/${id}`)
    return data
  },
  async create(payload: RoutingPolicyCreateRequest): Promise<{ policy: RoutingPolicy; revision: RoutingRevision }> {
    const { data } = await apiClient.post('/admin/routing-policies', payload)
    return data
  },
  async update(id: number, payload: RoutingPolicyUpdateRequest): Promise<{ policy: RoutingPolicy; revision?: RoutingRevision }> {
    const { data } = await apiClient.put(`/admin/routing-policies/${id}`, payload)
    return data
  },
  async remove(id: number): Promise<void> {
    await apiClient.delete(`/admin/routing-policies/${id}`)
  },
  async versions(id: number): Promise<RoutingRevision[]> {
    const { data } = await apiClient.get<RoutingRevision[]>(`/admin/routing-policies/${id}/versions`)
    return data
  },
  async validate(id: number, config: RoutingPolicyConfig): Promise<{ valid: boolean }> {
    const { data } = await apiClient.post(`/admin/routing-policies/${id}/validate`, { config })
    return data
  },
  async publish(id: number, revisionId: number): Promise<{ published: boolean; revision: RoutingRevision }> {
    const { data } = await apiClient.post(`/admin/routing-policies/${id}/publish`, { revision_id: revisionId })
    return data
  },
  async restore(id: number, version: number): Promise<RoutingRevision> {
    const { data } = await apiClient.post(`/admin/routing-policies/${id}/versions/${version}/restore`)
    return data
  },
  async simulate(id: number, groupId: number, model: string, revisionId?: number): Promise<RoutingSimulationResult> {
    const { data } = await apiClient.post(`/admin/routing-policies/${id}/simulate`, {
      group_id: groupId,
      model,
      ...(revisionId ? { revision_id: revisionId } : {})
    })
    return data
  },
  async bindGroup(id: number, groupId: number, mode: RoutingMode, revisionId?: number): Promise<{ group_id: number; policy_id: number; revision_id?: number; mode: RoutingMode }> {
    const { data } = await apiClient.post(`/admin/routing-policies/${id}/bindings/groups/${groupId}`, {
      mode,
      ...(revisionId ? { revision_id: revisionId } : {})
    })
    return data
  },
  async unbindGroup(id: number, groupId: number): Promise<void> {
    await apiClient.delete(`/admin/routing-policies/${id}/bindings/groups/${groupId}`)
  },
  async listPriceBooks(): Promise<PriceBook[]> {
    const { data } = await apiClient.get<PriceBook[]>('/admin/upstream-price-books')
    return data
  },
  async createPriceBook(payload: PriceBookInput): Promise<PriceBook> {
    const { data } = await apiClient.post('/admin/upstream-price-books', payload)
    return data
  },
  async updatePriceBook(id: number, payload: Partial<PriceBookInput>): Promise<PriceBook> {
    const { data } = await apiClient.put(`/admin/upstream-price-books/${id}`, payload)
    return data
  },
  async revisions(id: number): Promise<PriceBookRevision[]> {
    const { data } = await apiClient.get<PriceBookRevision[]>(`/admin/upstream-price-books/${id}/revisions`)
    return data
  },
  async createPriceBookRevision(id: number, payload: PriceBookRevisionInput): Promise<PriceBookRevision> {
    const { data } = await apiClient.post(`/admin/upstream-price-books/${id}/revisions`, payload)
    return data
  },
  async publishPriceBookRevision(id: number, version: number): Promise<{ published: boolean; revision: PriceBookRevision }> {
    const { data } = await apiClient.post(`/admin/upstream-price-books/${id}/revisions/${version}/publish`)
    return data
  },
  async syncPriceBook(id: number): Promise<PriceBookRevision> {
    const { data } = await apiClient.post(`/admin/upstream-price-books/${id}/sync`)
    return data
  }
}

export function routingPresetConfig(preset: RoutingPreset): RoutingPolicyConfig {
  const common: RoutingPolicyConfig = {
    schema_version: 1,
    candidate_filters: {
      platforms: [],
      required_labels: {},
      excluded_labels: {},
      reliability_classes: [],
      require_known_price: false,
      require_trusted_upstream: false,
    },
    retry: {
      max_attempts: 3,
      max_switches: 2,
      retry_transport_errors: true,
      retryable_status_codes: [408, 409, 429, 500, 502, 503, 504, 529]
    },
    hedge: { enabled: true, delay_ms: 1500, max_concurrent: 2, require_different_failure_domain: true, require_no_semantic_output: true },
    circuit_breaker: { consecutive_failures: 3, min_samples: 10, error_rate_percent: 50, cooldown_ms: 30000, max_cooldown_ms: 300000, half_open_max_requests: 1 },
    fallback: { group_ids: [], allow_cross_tier: true, max_cost_multiplier: 2, require_explicit_model_map: true },
    model_mappings: {},
    cost_budget: { max_upstream_usd: '0', max_attempt_cost_usd: '0', reserve_for_hedge_usd: '0' },
    scoring: { price: 35, error_rate: 30, ttft: 20, load: 10, queue: 5, reliability: 0 },
    timeouts: { request_timeout_ms: 120000, soft_ttft_ms: 5000, adaptive_soft_ttft: true, soft_ttft_factor: 1.1, soft_ttft_min_ms: 1500, soft_ttft_max_ms: 5000, stream_idle_ms: 30000 }
  }
  if (preset === 'economy') {
    common.candidate_filters.require_known_price = true
    common.scoring = { price: 65, error_rate: 15, ttft: 5, load: 10, queue: 5, reliability: 0 }
    common.hedge.delay_ms = 2500
    common.timeouts = { ...common.timeouts, soft_ttft_factor: 1.25, soft_ttft_min_ms: 2500, soft_ttft_max_ms: 8000, soft_ttft_ms: 8000 }
    common.fallback.max_cost_multiplier = 1.5
  } else if (preset === 'professional') {
    common.candidate_filters.require_trusted_upstream = true
    common.scoring = { price: 10, error_rate: 45, ttft: 30, load: 10, queue: 5, reliability: 10 }
    common.hedge.delay_ms = 800
    common.timeouts = { ...common.timeouts, soft_ttft_factor: 1, soft_ttft_min_ms: 800, soft_ttft_max_ms: 3000, soft_ttft_ms: 3000 }
    common.fallback.max_cost_multiplier = 3
  }
  return common
}

export default routingPolicyApi
