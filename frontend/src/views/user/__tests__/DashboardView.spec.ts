import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const {
  getDashboardStats,
  getDashboardTrend,
  getDashboardModels,
  getByDateRange,
  getMyPlatformQuotas,
  refreshUser,
} = vi.hoisted(() => ({
  getDashboardStats: vi.fn(),
  getDashboardTrend: vi.fn(),
  getDashboardModels: vi.fn(),
  getByDateRange: vi.fn(),
  getMyPlatformQuotas: vi.fn(),
  refreshUser: vi.fn(),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    user: { balance: 10 },
    isSimpleMode: false,
    refreshUser,
  }),
}))

vi.mock('@/api/usage', () => ({
  usageAPI: {
    getDashboardStats,
    getDashboardTrend,
    getDashboardModels,
    getByDateRange,
  },
}))

vi.mock('@/api/user', () => ({
  getMyPlatformQuotas,
}))

vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { template: '<div><slot /></div>' },
}))
vi.mock('@/components/common/LoadingSpinner.vue', () => ({
  default: { template: '<div class="spinner" />' },
}))
vi.mock('@/components/user/dashboard/UserDashboardStats.vue', () => ({
  default: { template: '<div class="stats" />', props: ['stats', 'balance', 'isSimple', 'platformQuotas'] },
}))
vi.mock('@/components/user/dashboard/UserDashboardCharts.vue', () => ({
  default: { template: '<div class="charts" />' },
}))
vi.mock('@/components/user/dashboard/UserDashboardRecentUsage.vue', () => ({
  default: { template: '<div class="recent" />' },
}))
vi.mock('@/components/user/dashboard/UserDashboardQuickActions.vue', () => ({
  default: { template: '<div class="actions" />' },
}))

import DashboardView from '../DashboardView.vue'

describe('user DashboardView', () => {
  beforeEach(() => {
    getDashboardStats.mockReset().mockResolvedValue({ total_requests: 1 })
    getDashboardTrend.mockReset().mockResolvedValue({ trend: [] })
    getDashboardModels.mockReset().mockResolvedValue({ models: [] })
    getByDateRange.mockReset().mockResolvedValue({ items: [] })
    getMyPlatformQuotas.mockReset().mockResolvedValue({ platform_quotas: [] })
    refreshUser.mockReset().mockResolvedValue({})
  })

  it('首屏不阻塞 refreshUser，最近用量只取 5 条', async () => {
    let resolveUser: (value: unknown) => void = () => undefined
    refreshUser.mockImplementation(() => new Promise((resolve) => { resolveUser = resolve }))

    const wrapper = mount(DashboardView)
    await flushPromises()

    expect(wrapper.find('.stats').exists()).toBe(true)
    expect(getDashboardStats).toHaveBeenCalledTimes(1)
    expect(getByDateRange).toHaveBeenCalledWith(expect.any(String), expect.any(String), undefined, 5)
    expect(refreshUser).toHaveBeenCalledTimes(1)

    resolveUser({})
    await flushPromises()
    wrapper.unmount()
  })
})
