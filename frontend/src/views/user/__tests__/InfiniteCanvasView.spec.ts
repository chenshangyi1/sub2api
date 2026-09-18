import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const { listKeys, authStore } = vi.hoisted(() => ({
  listKeys: vi.fn(),
  authStore: { isAuthenticated: true },
}))

vi.mock('@/api/keys', () => ({
  default: { list: listKeys },
  list: listKeys,
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStore,
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => authStore,
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showWarning: vi.fn(),
    sidebarCollapsed: false,
  }),
}))

vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { template: '<div data-testid="app-layout"><slot /></div>' },
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
      locale: { value: 'zh-CN' },
    }),
  }
})

vi.mock('@/utils/embedded-url', () => ({
  detectTheme: () => 'dark',
}))

import InfiniteCanvasView from '../InfiniteCanvasView.vue'

describe('InfiniteCanvasView', () => {
  it('embeds Infinite Canvas against this site without injecting a user key', async () => {
    const wrapper = mount(InfiniteCanvasView, {
      attachTo: document.body,
    })
    await flushPromises()

    expect(listKeys).not.toHaveBeenCalled()
    const iframe = wrapper.get('[data-testid="infinite-canvas-frame"]')
    const src = iframe.attributes('src') || ''
    const url = new URL(src, window.location.origin)
    expect(url.pathname).toBe('/canvas/')
    expect(url.searchParams.get('baseUrl')).toBe(window.location.origin)
    expect(url.searchParams.get('apiKey')).toBeNull()
    expect(url.searchParams.get('lang')).toBe('zh-CN')
    expect(url.searchParams.get('theme')).toBe('dark')
    expect(wrapper.find('[data-testid="infinite-canvas-key-picker"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('fills the remaining viewport without overlaying canvas header controls', async () => {
    const wrapper = mount(InfiniteCanvasView, {
      attachTo: document.body,
    })
    await flushPromises()

    const pageClasses = wrapper.get('.infinite-canvas-page').classes()
    expect(pageClasses).toContain('h-[calc(100dvh-4rem)]')
    expect(pageClasses).toContain('overflow-hidden')
    const iframe = wrapper.get('[data-testid="infinite-canvas-frame"]')
    expect(iframe.classes()).toContain('flex-1')
    expect(iframe.classes()).toContain('h-full')
    expect(wrapper.find('a[target="_blank"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
