import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Account } from '@/types'
import AccountSchedulingStateCell from '../AccountSchedulingStateCell.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (params?.time) return `${key}:${params.time}`
        return key
      },
    }),
  }
})

function mountCell(account: Partial<Account>) {
  return mount(AccountSchedulingStateCell, { props: { account: account as Account } })
}

describe('AccountSchedulingStateCell', () => {
  afterEach(() => vi.useRealTimers())

  it('updates on expiry without refetching and stops its timer on unmount', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-16T12:00:00Z'))
    const wrapper = mountCell({
      status: 'active', schedulable: true, overload_until: '2026-09-16T12:00:01Z',
    })
    expect(wrapper.text()).toContain('schedulingState.cooldown')
    await vi.advanceTimersByTimeAsync(1100)
    expect(wrapper.text()).toContain('schedulingState.eligible')
    expect(wrapper.text()).not.toContain('schedulingState.overload')
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('shows every active cooldown reason, the latest until, and the live temp reason', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-16T12:00:00Z'))
    const wrapper = mountCell({
      status: 'active',
      schedulable: true,
      temp_unschedulable_until: '2026-09-16T12:01:00Z',
      temp_unschedulable_reason: 'upstream 503',
      overload_until: '2026-09-16T12:02:00Z',
      rate_limit_reset_at: '2026-09-16T12:05:00Z',
    })
    const text = wrapper.text()
    expect(text).toContain('schedulingState.cooldown')
    expect(text).toContain('schedulingState.temporary')
    expect(text).toContain('schedulingState.overload')
    expect(text).toContain('schedulingState.rateLimit')
    expect(text).toContain('schedulingState.until')
    expect(text).toContain('upstream 503')
  })

  it('does not treat paused accounts as locally eligible after cooldown', () => {
    const wrapper = mountCell({
      status: 'active',
      schedulable: false,
      overload_until: '2099-01-01T00:00:00Z',
    })
    expect(wrapper.text()).toContain('schedulingState.paused')
    expect(wrapper.text()).not.toContain('schedulingState.eligible')
    expect(wrapper.text()).not.toContain('schedulingState.cooldown')
  })
})
