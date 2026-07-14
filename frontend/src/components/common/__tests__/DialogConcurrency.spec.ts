import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, nextTick, ref } from 'vue'
import BaseDialog from '../BaseDialog.vue'
import ConfirmDialog from '../ConfirmDialog.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

afterEach(() => {
  document.body.classList.remove('modal-open')
  document.body.innerHTML = ''
})

describe('dialog concurrency', () => {
  it('blocks every confirm and cancel entry point while loading', async () => {
    const wrapper = mount(ConfirmDialog, {
      attachTo: document.body,
      props: { show: true, title: 'Confirm', message: 'Continue?', loading: true },
    })
    await flushPromises()

    const busyRegion = document.querySelector<HTMLElement>('[aria-busy="true"]')
    const buttons = Array.from(document.querySelectorAll<HTMLButtonElement>('.modal-footer button'))
    expect(busyRegion).not.toBeNull()
    expect(buttons).toHaveLength(2)
    expect(buttons.every(button => button.disabled)).toBe(true)
    const closeButton = document.querySelector('[aria-label="Close modal"]')

    buttons.forEach(button => button.click())
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await nextTick()
    expect(wrapper.emitted('confirm')).toBeUndefined()
    expect(wrapper.emitted('cancel')).toBeUndefined()

    wrapper.unmount()
    expect(closeButton).toBeNull()
    expect(document.body.classList.contains('modal-open')).toBe(false)
  })

  it('keeps the body locked and restores parent focus when a nested dialog closes', async () => {
    const Host = defineComponent({
      components: { BaseDialog },
      setup() {
        const parentOpen = ref(false)
        const childOpen = ref(false)
        return { parentOpen, childOpen }
      },
      template: `
        <button data-test="parent-opener" @click="parentOpen = true">Open parent</button>
        <BaseDialog :show="parentOpen" title="Parent" @close="parentOpen = false">
          <button data-test="child-opener" @click="childOpen = true">Open child</button>
          <BaseDialog :show="childOpen" title="Child" @close="childOpen = false">
            <button>Child action</button>
          </BaseDialog>
        </BaseDialog>
      `,
    })
    const wrapper = mount(Host, { attachTo: document.body })
    const parentOpener = wrapper.get('[data-test="parent-opener"]')
    parentOpener.element.focus()
    await parentOpener.trigger('click')
    await flushPromises()
    expect(document.body.classList.contains('modal-open')).toBe(true)

    const childOpener = document.querySelector<HTMLButtonElement>('[data-test="child-opener"]')!
    childOpener.focus()
    childOpener.click()
    await flushPromises()
    expect(document.querySelectorAll('.modal-overlay')).toHaveLength(2)

    const childTitle = Array.from(document.querySelectorAll<HTMLElement>('.modal-title')).find(title => title.textContent === 'Child')!
    childTitle.closest('.modal-overlay')!.querySelector<HTMLButtonElement>('[aria-label="Close modal"]')!.click()
    await flushPromises()
    expect(document.body.classList.contains('modal-open')).toBe(true)
    expect(document.activeElement).toBe(childOpener)

    const parentTitle = Array.from(document.querySelectorAll<HTMLElement>('.modal-title')).find(title => title.textContent === 'Parent')!
    parentTitle.closest('.modal-overlay')!.querySelector<HTMLButtonElement>('[aria-label="Close modal"]')!.click()
    await flushPromises()
    expect(document.body.classList.contains('modal-open')).toBe(false)
    wrapper.unmount()
  })
})
