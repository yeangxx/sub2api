import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import RoutingPoliciesView from '../RoutingPoliciesView.vue'

const api = vi.hoisted(() => ({
  list: vi.fn(),
  listPriceBooks: vi.fn(),
  versions: vi.fn(),
  syncPriceBook: vi.fn(),
}))
const showError = vi.hoisted(() => vi.fn())
const showSuccess = vi.hoisted(() => vi.fn())

vi.mock('@/api/admin/routingPolicy', async () => {
  const actual = await vi.importActual<typeof import('@/api/admin/routingPolicy')>('@/api/admin/routingPolicy')
  return { ...actual, routingPolicyApi: { ...actual.routingPolicyApi, ...api } }
})

vi.mock('@/api/admin', () => ({
  adminAPI: { groups: { getAllIncludingInactive: vi.fn().mockResolvedValue([]) } },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

describe('RoutingPoliciesView dialogs', () => {
  beforeEach(() => {
    api.list.mockResolvedValue([])
    api.listPriceBooks.mockResolvedValue([])
    api.versions.mockResolvedValue([])
    showError.mockReset()
    showSuccess.mockReset()
    vi.spyOn(window, 'prompt').mockReturnValue(null)
    vi.spyOn(window, 'alert').mockImplementation(() => undefined)
    vi.spyOn(window, 'confirm').mockReturnValue(false)
  })

  it('opens an application dialog when creating a policy without native browser prompts', async () => {
    const wrapper = mount(RoutingPoliciesView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          BaseDialog: {
            props: ['show', 'title'],
            template: '<div v-if="show" data-test="base-dialog"><slot /><slot name="footer" /></div>',
          },
        },
      },
    })
    await flushPromises()

    const createButton = wrapper.findAll('button').find((button) =>
      button.text().includes('admin.routingPolicies.create'),
    )
    expect(createButton).toBeTruthy()
    await createButton!.trigger('click')

    expect(window.prompt).not.toHaveBeenCalled()
    expect(window.alert).not.toHaveBeenCalled()
    expect(window.confirm).not.toHaveBeenCalled()
    expect(wrapper.find('[data-test="base-dialog"]').exists()).toBe(true)
  })

  it('disables HTTP price synchronization while the same book is syncing', async () => {
    let resolveSync!: () => void
    api.listPriceBooks.mockResolvedValue([{
      id: 5,
      name: 'Remote prices',
      source: 'http_json',
      status: 'active',
      currency: 'USD',
      created_at: '2026-07-14T00:00:00Z',
      updated_at: '2026-07-14T00:00:00Z',
    }])
    api.syncPriceBook.mockReturnValue(new Promise<void>(resolve => { resolveSync = resolve }))
    const wrapper = mount(RoutingPoliciesView, {
      global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } },
    })
    await flushPromises()
    const booksTab = wrapper.findAll('button').find(button => button.text().includes('admin.routingPolicies.priceBooks'))
    await booksTab!.trigger('click')
    const syncButton = wrapper.get('button[title="admin.routingPolicies.sync"]')

    await syncButton.trigger('click')
    await syncButton.trigger('click')
    expect(api.syncPriceBook).toHaveBeenCalledTimes(1)
    expect(syncButton.attributes('disabled')).toBeDefined()

    resolveSync()
    await flushPromises()
    expect(wrapper.get('button[title="admin.routingPolicies.sync"]').attributes('disabled')).toBeUndefined()
  })
})
