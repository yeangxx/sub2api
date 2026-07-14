import type { RoutingPolicyConfig } from '@/api/admin/routingPolicy'

export interface KeyValueRow {
  id: string
  key: string
  value: string
}

let rowSequence = 0

export function newKeyValueRow(key = '', value = ''): KeyValueRow {
  rowSequence += 1
  return { id: `routing-kv-${rowSequence}`, key, value }
}

export function recordsToRows(record: Record<string, string> = {}): KeyValueRow[] {
  return Object.entries(record).map(([key, value]) => newKeyValueRow(key, value))
}

export function rowsToRecord(rows: KeyValueRow[]): Record<string, string> {
  const record: Record<string, string> = {}
  for (const row of rows) {
    const key = row.key.trim()
    if (key) record[key] = row.value.trim()
  }
  return record
}

export function cloneRoutingPolicyConfig(config: RoutingPolicyConfig): RoutingPolicyConfig {
  return JSON.parse(JSON.stringify(config)) as RoutingPolicyConfig
}

export function validateRoutingPolicyConfig(config: RoutingPolicyConfig): string[] {
  const errors: string[] = []
  const weights = Object.values(config.scoring)
  if (weights.some((value) => !Number.isFinite(value) || value < 0)) errors.push('scoring')
  if (!weights.some((value) => value > 0)) errors.push('scoring.total')
  const timeoutValues = [config.timeouts.request_timeout_ms, config.timeouts.soft_ttft_ms, config.timeouts.soft_ttft_min_ms, config.timeouts.soft_ttft_max_ms, config.timeouts.stream_idle_ms]
  if (timeoutValues.some((value) => !Number.isFinite(value) || value < 0)) errors.push('timeouts')
  if (config.timeouts.soft_ttft_min_ms > config.timeouts.soft_ttft_max_ms) errors.push('timeouts.soft_ttft_range')
  if (config.timeouts.adaptive_soft_ttft && config.timeouts.soft_ttft_factor <= 0) errors.push('timeouts.soft_ttft_factor')
  if (config.retry.max_attempts < 0 || config.retry.max_switches < 0) errors.push('retry')
  if (config.hedge.enabled && config.hedge.delay_ms <= 0) errors.push('hedge.delay_ms')
  if (config.hedge.enabled && config.hedge.max_concurrent < 2) errors.push('hedge.max_concurrent')
  if (config.circuit_breaker.error_rate_percent < 0 || config.circuit_breaker.error_rate_percent > 100) errors.push('circuit_breaker.error_rate_percent')
  if (config.circuit_breaker.min_samples < 0 || config.circuit_breaker.consecutive_failures < 0 || config.circuit_breaker.half_open_max_requests < 0) errors.push('circuit_breaker.limits')
  if (config.circuit_breaker.cooldown_ms < 0 || config.circuit_breaker.max_cooldown_ms < 0) errors.push('circuit_breaker.cooldown')
  if (config.fallback.max_cost_multiplier < 0) errors.push('fallback.max_cost_multiplier')
  for (const value of Object.values(config.cost_budget)) {
    const numeric = Number(value || 0)
    if (!Number.isFinite(numeric) || numeric < 0) errors.push('cost_budget')
  }
  return [...new Set(errors)]
}
