import { describe, expect, expectTypeOf, it } from 'vitest'
import type { UpdateAccountRequest } from '@/types'
import {
  withAccountRoutingCreateFields,
  withAccountRoutingUpdateFields
} from '../accountRoutingForm'

const createRouting = () => ({
  failure_domain: 'az-a',
  reliability_class: 'trusted',
  routing_labels: { region: 'ap-east' },
  price_book_id: 12 as number | null
})

describe('account routing form payloads', () => {
  it('adds routing fields to account creation payloads', () => {
    const routing = createRouting()
    const payload = withAccountRoutingCreateFields({
      name: 'upstream-a',
      platform: 'openai',
      type: 'apikey',
      credentials: {}
    }, routing)

    expect(payload).toMatchObject(routing)
    routing.routing_labels.region = 'changed-after-submit'
    expect(payload.routing_labels).toEqual({ region: 'ap-east' })
  })

  it('uses zero to clear a price book in account update payloads', () => {
    const routing = createRouting()
    const payload = withAccountRoutingUpdateFields({ name: 'upstream-a' }, {
      ...routing,
      price_book_id: null
    })

    expectTypeOf(payload).toEqualTypeOf<UpdateAccountRequest>()

    expect(payload).toMatchObject({
      failure_domain: 'az-a',
      reliability_class: 'trusted',
      routing_labels: { region: 'ap-east' },
      price_book_id: 0
    })
  })
})
