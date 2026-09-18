import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AccountCapacityCell from '../AccountCapacityCell.vue'
import type { Account } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (params?.current != null) return `${key}:${params.current}`
        return key
      }
    })
  }
})

function makeAccount(overrides: Partial<Account>): Account {
  return {
    id: 1,
    name: 'account',
    platform: 'openai',
    type: 'apikey',
    proxy_id: null,
    concurrency: 0,
    priority: 1,
    status: 'active',
    error_message: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: true,
    created_at: '2026-03-15T00:00:00Z',
    updated_at: '2026-03-15T00:00:00Z',
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null,
    ...overrides,
  }
}

describe('AccountCapacityCell', () => {
  it('shows idle unlimited concurrency as 0/∞ without treating 0 as full', () => {
    const wrapper = mount(AccountCapacityCell, {
      props: { account: makeAccount({ concurrency: 0, current_concurrency: 0 }) }
    })
    expect(wrapper.text()).toContain('0')
    expect(wrapper.text()).toContain('∞')
    expect(wrapper.text()).not.toContain('0 / 0')
    const badge = wrapper.findComponent({ name: 'CapacityBadge' })
    expect(badge.props('max')).toBe('∞')
    expect(badge.props('colorClass')).toContain('bg-gray-100')
    expect(badge.props('tooltip')).toBe('admin.accounts.capacity.concurrency.unlimited')
  })

  it('keeps in-use unlimited concurrency yellow instead of red', () => {
    const wrapper = mount(AccountCapacityCell, {
      props: { account: makeAccount({ concurrency: 0, current_concurrency: 12 }) }
    })
    const badge = wrapper.findComponent({ name: 'CapacityBadge' })
    expect(badge.props('current')).toBe(12)
    expect(badge.props('max')).toBe('∞')
    expect(badge.props('colorClass')).toContain('bg-yellow-100')
    expect(badge.props('tooltip')).toBe('admin.accounts.capacity.concurrency.inUseUnlimited:12')
  })

  it('still marks a finite cap as full in red', () => {
    const wrapper = mount(AccountCapacityCell, {
      props: { account: makeAccount({ concurrency: 8, current_concurrency: 8 }) }
    })
    const badge = wrapper.findComponent({ name: 'CapacityBadge' })
    expect(badge.props('max')).toBe(8)
    expect(badge.props('colorClass')).toContain('bg-red-100')
    expect(badge.props('tooltip')).toBe('admin.accounts.capacity.concurrency.full')
  })
})
