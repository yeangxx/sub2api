import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'

const { listPriceBooksMock, showErrorMock } = vi.hoisted(() => ({
  listPriceBooksMock: vi.fn(),
  showErrorMock: vi.fn()
}))

vi.mock('@/api/admin/routingPolicy', () => ({
  routingPolicyApi: { listPriceBooks: listPriceBooksMock }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: showErrorMock })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

import AccountRoutingFields from '../AccountRoutingFields.vue'

const SelectStub = defineComponent({
  name: 'RoutingSelectStub',
  props: ['modelValue', 'options', 'disabled', 'placeholder'],
  emits: ['update:modelValue'],
  template: '<div />'
})

const mountFields = () => mount(AccountRoutingFields, {
  props: {
    failureDomain: '',
    reliabilityClass: '',
    routingLabels: {},
    priceBookId: null
  },
  global: {
    stubs: {
      Select: SelectStub,
      Icon: true,
      KeyValueEditor: true
    }
  }
})

const priceBook = {
  id: 7,
  name: 'Primary USD',
  currency: 'USD',
  status: 'active'
}

describe('AccountRoutingFields', () => {
  beforeEach(() => {
    listPriceBooksMock.mockReset()
    showErrorMock.mockReset()
  })

  it('disables the price book select while loading and enables it after success', async () => {
    let resolveRequest!: (value: unknown[]) => void
    listPriceBooksMock.mockReturnValue(new Promise(resolve => { resolveRequest = resolve }))
    const wrapper = mountFields()
    const priceSelect = wrapper.getComponent('[data-testid="price-book-select"]')

    expect(priceSelect.props('disabled')).toBe(true)
    resolveRequest([priceBook])
    await flushPromises()

    expect(priceSelect.props('disabled')).toBe(false)
    expect(priceSelect.props('options')).toEqual([
      expect.objectContaining({ value: 7, label: expect.stringContaining('Primary USD') })
    ])
  })

  it('shows an error and toast, then retries loading price books', async () => {
    listPriceBooksMock
      .mockRejectedValueOnce(new Error('network down'))
      .mockResolvedValueOnce([priceBook])
    const wrapper = mountFields()
    await flushPromises()

    expect(showErrorMock).toHaveBeenCalledWith('admin.accounts.routing.priceBookLoadFailed')
    expect(wrapper.get('[data-testid="price-book-load-error"]').text())
      .toContain('admin.accounts.routing.priceBookLoadFailed')

    await wrapper.get('[data-testid="retry-price-books"]').trigger('click')
    await flushPromises()

    expect(listPriceBooksMock).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-testid="price-book-load-error"]').exists()).toBe(false)
    expect(wrapper.getComponent('[data-testid="price-book-select"]').props('disabled')).toBe(false)
  })

  it('converts a selected price book ID to number and emits null when cleared', async () => {
    listPriceBooksMock.mockResolvedValue([])
    const wrapper = mountFields()
    await flushPromises()
    const priceSelect = wrapper.getComponent('[data-testid="price-book-select"]')

    priceSelect.vm.$emit('update:modelValue', '7')
    priceSelect.vm.$emit('update:modelValue', null)

    expect(wrapper.emitted('update:priceBookId')).toEqual([[7], [null]])
  })
})
