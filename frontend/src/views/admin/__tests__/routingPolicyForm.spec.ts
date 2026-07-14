import { describe, expect, it } from 'vitest'
import { routingPresetConfig } from '@/api/admin/routingPolicy'
import {
  cloneRoutingPolicyConfig,
  recordsToRows,
  rowsToRecord,
  validateRoutingPolicyConfig,
} from '../routingPolicyForm'

describe('routing policy form model', () => {
  it('clones every preset into an independently editable valid config', () => {
    for (const preset of ['economy', 'standard', 'professional'] as const) {
      const source = routingPresetConfig(preset)
      const clone = cloneRoutingPolicyConfig(source)
      expect(validateRoutingPolicyConfig(clone)).toEqual([])
      clone.scoring.price += 1
      expect(clone.scoring.price).not.toBe(source.scoring.price)
    }
  })

  it('round-trips routing labels while ignoring incomplete rows', () => {
    const rows = recordsToRows({ tier: 'economy', region: 'hk' })
    rows.push({ id: 'empty', key: '', value: 'ignored' })
    expect(rowsToRecord(rows)).toEqual({ tier: 'economy', region: 'hk' })
  })

  it('rejects an enabled hedge without enough concurrency', () => {
    const config = cloneRoutingPolicyConfig(routingPresetConfig('standard'))
    config.hedge.max_concurrent = 1
    expect(validateRoutingPolicyConfig(config)).toContain('hedge.max_concurrent')
  })
})
