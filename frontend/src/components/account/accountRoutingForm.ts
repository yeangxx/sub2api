import type { CreateAccountRequest, UpdateAccountRequest } from '@/types'

export interface AccountRoutingFormValues {
  failure_domain: string
  reliability_class: string
  routing_labels: Record<string, string>
  price_book_id: number | null
}

const accountRoutingPayload = (routing: AccountRoutingFormValues) => ({
  failure_domain: routing.failure_domain,
  reliability_class: routing.reliability_class,
  routing_labels: { ...routing.routing_labels }
})

export const withAccountRoutingCreateFields = (
  payload: CreateAccountRequest,
  routing: AccountRoutingFormValues
): CreateAccountRequest => ({
  ...payload,
  ...accountRoutingPayload(routing),
  price_book_id: routing.price_book_id
})

export const withAccountRoutingUpdateFields = (
  payload: UpdateAccountRequest,
  routing: AccountRoutingFormValues
): UpdateAccountRequest => ({
  ...payload,
  ...accountRoutingPayload(routing),
  price_book_id: routing.price_book_id ?? 0
})
