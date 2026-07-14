import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import Select from '@/components/common/Select.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import RoutingPolicyBindingDialog from '../RoutingPolicyBindingDialog.vue'
import RoutingPolicySimulationDialog from '../RoutingPolicySimulationDialog.vue'
import PriceBookVersionsDialog from '../PriceBookVersionsDialog.vue'
import RoutingPolicyFormDialog from '../RoutingPolicyFormDialog.vue'

const api = vi.hoisted(() => ({
  bindGroup: vi.fn(),
  simulate: vi.fn(),
  publishPriceBookRevision: vi.fn(),
}))
const showError = vi.hoisted(() => vi.fn())
const showSuccess = vi.hoisted(() => vi.fn())

vi.mock('@/api/admin/routingPolicy', async () => {
  const actual = await vi.importActual<typeof import('@/api/admin/routingPolicy')>('@/api/admin/routingPolicy')
  return { ...actual, routingPolicyApi: { ...actual.routingPolicyApi, ...api } }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, values?: Record<string, unknown>, fallback?: string) => {
        if (key === 'admin.routingPolicies.bindingConfirm') return `${values?.mode}: ${values?.group}`
        return fallback || key
      },
    }),
  }
})

const dialogStub = {
  props: ['show', 'title'],
  template: '<section v-if="show"><slot /><slot name="footer" /></section>',
}

const policy = {
  id: 7,
  name: 'Economy',
  description: '',
  status: 'active',
  draft_revision_id: 11,
  published_revision_id: 10,
  created_at: '2026-07-14T00:00:00Z',
  updated_at: '2026-07-14T00:00:00Z',
} as any

const groups = [
  {
    id: 2,
    name: 'OpenAI 主组',
    platform: 'openai',
    status: 'active',
    subscription_type: 'standard',
    rate_multiplier: 1,
  },
  {
    id: 9,
    name: '旧 Gemini 组',
    platform: 'gemini',
    status: 'inactive',
    subscription_type: 'standard',
    rate_multiplier: 1,
  },
] as any

function findFieldInput(wrapper: VueWrapper, label: string) {
  return wrapper.findAll('input').find(input => input.element.parentElement?.parentElement?.textContent?.includes(label))
}

function findActiveTab(wrapper: VueWrapper, label: string) {
  return wrapper.findAll('button').find(button => button.text().includes(label))
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

async function mountValidForm() {
  const wrapper = mount(RoutingPolicyFormDialog, {
    props: { show: true, groups },
    global: { stubs: { BaseDialog: dialogStub } },
  })
  await findFieldInput(wrapper, 'admin.routingPolicies.name')!.setValue('Invalid policy')
  return wrapper
}

describe('routing policy dialogs', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('keeps inactive groups visible but disabled for a new binding and confirms before submitting', async () => {
    const request = deferred<void>()
    api.bindGroup.mockReturnValue(request.promise)
    const wrapper = mount(RoutingPolicyBindingDialog, {
      props: { show: true, policy, groups, versions: [] },
      global: { stubs: { BaseDialog: dialogStub } },
    })

    const groupSelect = wrapper.findAllComponents(Select)[0]
    const options = groupSelect.props('options') as Array<Record<string, unknown>>
    expect(options).toEqual(expect.arrayContaining([
      expect.objectContaining({ value: 9, disabled: true, status: 'inactive' }),
    ]))
    expect(options.find(option => option.value === 9)?.description).toContain('Gemini')
    expect(options.find(option => option.value === 9)?.description).toContain('9')

    groupSelect.vm.$emit('update:modelValue', 2)
    await wrapper.find('form').trigger('submit')

    expect(api.bindGroup).not.toHaveBeenCalled()
    const confirm = wrapper.findComponent(ConfirmDialog)
    expect(confirm.props('show')).toBe(true)
    expect(confirm.props('message')).toContain('OpenAI 主组')
    expect(confirm.props('message')).toContain('admin.routingPolicies.shadow')

    confirm.vm.$emit('confirm')
    confirm.vm.$emit('confirm')
    await nextTick()
    expect(confirm.props('loading')).toBe(true)
    confirm.vm.$emit('cancel')
    await nextTick()
    expect(confirm.props('show')).toBe(true)
    expect(api.bindGroup).toHaveBeenCalledTimes(1)

    request.resolve()
    await flushPromises()
    expect(api.bindGroup).toHaveBeenCalledWith(7, 2, 'shadow', 10)
    expect(showSuccess).toHaveBeenCalledWith('admin.routingPolicies.bound')
  })

  it('keeps a failed binding confirmation open and allows retry', async () => {
    api.bindGroup.mockRejectedValueOnce(new Error('upstream unavailable')).mockResolvedValueOnce(undefined)
    const wrapper = mount(RoutingPolicyBindingDialog, {
      props: { show: true, policy, groups, versions: [] },
      global: { stubs: { BaseDialog: dialogStub } },
    })
    wrapper.findAllComponents(Select)[0].vm.$emit('update:modelValue', 2)
    await wrapper.find('form').trigger('submit')
    const confirm = wrapper.findComponent(ConfirmDialog)

    confirm.vm.$emit('confirm')
    await flushPromises()
    expect(confirm.props('show')).toBe(true)
    expect(confirm.props('loading')).toBe(false)

    confirm.vm.$emit('confirm')
    await flushPromises()
    expect(api.bindGroup).toHaveBeenCalledTimes(2)
    expect(confirm.props('show')).toBe(false)
  })

  it('searches rich group options by name, platform, status and ID', async () => {
    const wrapper = mount(RoutingPolicyBindingDialog, {
      props: { show: true, policy, groups, versions: [] },
      global: { stubs: { BaseDialog: dialogStub } },
    })
    await wrapper.findAllComponents(Select)[0].find('button.select-trigger').trigger('click')
    await nextTick()
    expect(wrapper.findAllComponents(GroupBadge).length).toBeGreaterThan(0)

    const search = document.querySelector<HTMLInputElement>('.select-search-input')!
    async function visibleOptions(query: string) {
      search.value = query
      search.dispatchEvent(new Event('input', { bubbles: true }))
      await nextTick()
      return Array.from(document.querySelectorAll('.select-option')).map(option => option.textContent || '')
    }

    expect(await visibleOptions('旧 Gemini')).toHaveLength(1)
    expect((await visibleOptions('Gemini'))[0]).toContain('旧 Gemini 组')
    expect((await visibleOptions('inactive'))[0]).toContain('#9')
    expect((await visibleOptions('#2'))[0]).toContain('OpenAI 主组')
    wrapper.unmount()
  })

  it('renders a distinct selected marker and separates cost from price availability', async () => {
    api.simulate.mockResolvedValue({
      group_id: 2,
      model: 'gpt-4o',
      policy,
      revision: { id: 11 },
      selection: {
        selected_account: { id: 41, name: 'fast-upstream', platform: 'openai' },
        candidates: [
          {
            account_id: 41,
            account_name: 'fast-upstream',
            platform: 'openai',
            score: 0.12,
            estimated_cost_usd: 0.00125,
            price_known: true,
            excluded: false,
            health: { error_rate: 0.01, ttft_ms: 320, load: 0, queue: 0, consecutive_failures: 0, samples: 10 },
          },
          {
            account_id: 42,
            account_name: 'unknown-price',
            platform: 'openai',
            score: 0.5,
            estimated_cost_usd: 0,
            price_known: false,
            excluded: true,
            exclusion_reason: 'price_unknown',
            health: { error_rate: 0.1, ttft_ms: 900, load: 0, queue: 0, consecutive_failures: 0, samples: 10 },
          },
        ],
      },
    })
    const wrapper = mount(RoutingPolicySimulationDialog, {
      props: { show: true, policy, groups },
      global: { stubs: { BaseDialog: dialogStub } },
    })
    const groupSelect = wrapper.findComponent(Select)
    const inactiveOption = (groupSelect.props('options') as Array<Record<string, unknown>>).find(option => option.value === 9)
    expect(inactiveOption?.disabled).not.toBe(true)
    groupSelect.vm.$emit('update:modelValue', 9)
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(api.simulate).toHaveBeenCalledWith(7, 9, 'gpt-4o', 11)
    expect(wrapper.find('[data-test="simulation-selected-41"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="simulation-row-41"]').classes()).toContain('bg-primary-50/50')
    expect(wrapper.find('[data-test="simulation-cost-41"]').text()).toContain('$0.001250')
    expect(wrapper.find('[data-test="simulation-price-41"]').text()).toContain('admin.routingPolicies.priceKnown')
    expect(wrapper.find('[data-test="simulation-price-42"]').text()).toContain('admin.routingPolicies.priceUnknown')
  })

  it('shows revision comments and confirms before publishing a draft price version', async () => {
    const request = deferred<void>()
    api.publishPriceBookRevision.mockReturnValueOnce(request.promise).mockResolvedValueOnce(undefined)
    const wrapper = mount(PriceBookVersionsDialog, {
      props: {
        show: true,
        book: { id: 3, name: 'HTTP prices' } as any,
        revisions: [
          { id: 20, price_book_id: 3, version: 4, state: 'draft', comment: 'nightly source refresh', prices: [], created_at: '2026-07-14T00:00:00Z' },
          { id: 19, price_book_id: 3, version: 3, state: 'published', comment: '', prices: [], created_at: '2026-07-13T00:00:00Z' },
          { id: 18, price_book_id: 3, version: 2, state: 'archived', comment: '', prices: [], created_at: '2026-07-12T00:00:00Z' },
        ] as any,
      },
      global: { stubs: { BaseDialog: dialogStub } },
    })

    expect(wrapper.text()).toContain('nightly source refresh')
    expect(wrapper.text()).toContain('admin.routingPolicies.revisionStates.draft')
    expect(wrapper.text()).toContain('admin.routingPolicies.revisionStates.published')
    expect(wrapper.text()).toContain('admin.routingPolicies.revisionStates.archived')
    expect(wrapper.text()).toContain(new Date('2026-07-14T00:00:00Z').toLocaleString())
    await wrapper.get('[data-test="publish-price-revision-4"]').trigger('click')
    expect(api.publishPriceBookRevision).not.toHaveBeenCalled()
    const confirm = wrapper.findComponent(ConfirmDialog)
    expect(confirm.props('show')).toBe(true)
    confirm.vm.$emit('confirm')
    confirm.vm.$emit('confirm')
    await nextTick()
    expect(confirm.props('loading')).toBe(true)
    confirm.vm.$emit('cancel')
    await nextTick()
    expect(confirm.props('show')).toBe(true)
    expect(api.publishPriceBookRevision).toHaveBeenCalledTimes(1)

    request.reject(new Error('price source unavailable'))
    await flushPromises()
    expect(confirm.props('show')).toBe(true)
    expect(confirm.props('loading')).toBe(false)

    confirm.vm.$emit('confirm')
    await flushPromises()
    expect(api.publishPriceBookRevision).toHaveBeenNthCalledWith(2, 3, 4)
    expect(confirm.props('show')).toBe(false)
    expect(showSuccess).toHaveBeenCalledWith('admin.routingPolicies.published')
  })

  it('places client validation beside the scoring fields and opens the scoring tab', async () => {
    const wrapper = await mountValidForm()
    const priceWeight = findFieldInput(wrapper, 'admin.routingPolicies.form.weightPrice')
    await priceWeight!.setValue('-1')
    await wrapper.find('form').trigger('submit')

    const activeTab = findActiveTab(wrapper, 'admin.routingPolicies.form.scoring')
    expect(activeTab?.classes()).toContain('border-primary-500')
    expect(wrapper.find('[data-test="field-error-scoring"]').text()).toContain('admin.routingPolicies.validation.scoring')
    expect(priceWeight!.classes()).toContain('input-error')
    expect(showError).toHaveBeenCalledWith('admin.routingPolicies.validationFailed')
  })

  it('places a missing name error beside the name field on the basic tab', async () => {
    const wrapper = mount(RoutingPolicyFormDialog, {
      props: { show: true, groups },
      global: { stubs: { BaseDialog: dialogStub } },
    })
    await wrapper.find('form').trigger('submit')
    expect(findActiveTab(wrapper, 'admin.routingPolicies.form.basic')?.classes()).toContain('border-primary-500')
    expect(wrapper.get('[data-test="field-error-name"]').text()).toContain('admin.routingPolicies.validation.name')
  })

  it('places timeout and retry errors beside their groups on the timeout tab', async () => {
    const wrapper = await mountValidForm()
    await findFieldInput(wrapper, 'admin.routingPolicies.form.requestTimeout')!.setValue('-1')
    await findFieldInput(wrapper, 'admin.routingPolicies.form.maxAttempts')!.setValue('-1')
    await wrapper.find('form').trigger('submit')
    expect(findActiveTab(wrapper, 'admin.routingPolicies.form.timeoutsRetry')?.classes()).toContain('border-primary-500')
    expect(wrapper.get('[data-test="field-error-timeouts"]').text()).toContain('admin.routingPolicies.validation.timeouts')
    expect(wrapper.get('[data-test="field-error-retry"]').text()).toContain('admin.routingPolicies.validation.retry')
  })

  it('places Hedge and circuit errors beside their groups on the resilience tab', async () => {
    const wrapper = await mountValidForm()
    await findFieldInput(wrapper, 'admin.routingPolicies.form.hedgeDelay')!.setValue('0')
    await findFieldInput(wrapper, 'admin.routingPolicies.form.errorRatePercent')!.setValue('101')
    await wrapper.find('form').trigger('submit')
    expect(wrapper.get('[data-test="hedge-heading"]').text()).toBe('admin.routingPolicies.form.hedge')
    expect(findActiveTab(wrapper, 'admin.routingPolicies.form.resilience')?.classes()).toContain('border-primary-500')
    expect(wrapper.get('[data-test="field-error-hedge"]').text()).toContain('admin.routingPolicies.validation.hedge.delay_ms')
    expect(wrapper.get('[data-test="field-error-circuit"]').text()).toContain('admin.routingPolicies.validation.circuit_breaker.error_rate_percent')
  })

  it('places fallback and budget errors beside their groups on the fallback tab', async () => {
    const wrapper = await mountValidForm()
    await findFieldInput(wrapper, 'admin.routingPolicies.form.maxCostMultiplier')!.setValue('-1')
    await findFieldInput(wrapper, 'admin.routingPolicies.form.maxUpstreamUsd')!.setValue('-1')
    await wrapper.find('form').trigger('submit')
    expect(findActiveTab(wrapper, 'admin.routingPolicies.form.fallbackBudget')?.classes()).toContain('border-primary-500')
    expect(wrapper.get('[data-test="field-error-fallback"]').text()).toContain('admin.routingPolicies.validation.fallback.max_cost_multiplier')
    expect(wrapper.get('[data-test="field-error-budget"]').text()).toContain('admin.routingPolicies.validation.cost_budget')
  })
})
