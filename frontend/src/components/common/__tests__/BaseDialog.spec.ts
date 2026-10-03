import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, nextTick, ref } from 'vue'
import BaseDialog from '../BaseDialog.vue'
import { resetDialogStackForTests } from '../dialogStack'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

describe('BaseDialog', () => {
  afterEach(() => {
    document.body.innerHTML = ''
    resetDialogStackForTests()
  })

  it('resets body scroll position when reopened', async () => {
    const wrapper = mount(BaseDialog, {
      attachTo: document.body,
      props: { show: false, title: 'Details' },
      slots: { default: '<div style="height: 2000px">content</div>' },
      global: { stubs: { Icon: true } }
    })

    await wrapper.setProps({ show: true })
    await nextTick()
    const body = document.body.querySelector<HTMLElement>('.modal-body')
    expect(body).not.toBeNull()
    body!.scrollTop = 480

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await nextTick()

    expect(document.body.querySelector<HTMLElement>('.modal-body')?.scrollTop).toBe(0)
    wrapper.unmount()
  })

  it('only the top nested dialog should respond to Escape', async () => {
    const w = mount(defineComponent({
      components: { BaseDialog },
      setup() { return { child: ref(true), parent: ref(true) } },
      template: '<BaseDialog :show="parent" title="账号" @close="parent=false"><BaseDialog :show="child" title="策略预览" @close="child=false" /></BaseDialog>',
    }), { attachTo: document.body, global: { stubs: { Icon: true } } })
    await flushPromises()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect((w.vm as unknown as { parent: boolean; child: boolean }).parent).toBe(true)
    expect((w.vm as unknown as { parent: boolean; child: boolean }).child).toBe(false)
    w.unmount()
  })

  it('closing the child dialog should keep scrolling locked while the parent is open', async () => {
    const w = mount(defineComponent({
      components: { BaseDialog },
      setup() { return { child: ref(true) } },
      template: '<BaseDialog :show="true" title="账号"><BaseDialog :show="child" title="策略预览" @close="child=false" /></BaseDialog>',
    }), { attachTo: document.body, global: { stubs: { Icon: true } } })
    await flushPromises()
    expect(document.body.classList.contains('modal-open')).toBe(true)
    const dialogs = document.querySelectorAll('[role=dialog]')
    ;(dialogs[dialogs.length - 1].querySelector('button') as HTMLButtonElement).click()
    await flushPromises()
    expect(document.body.classList.contains('modal-open')).toBe(true)
    w.unmount()
    expect(document.body.classList.contains('modal-open')).toBe(false)
  })
})
