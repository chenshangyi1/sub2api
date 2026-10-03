"use client"

import { useEffect, useState } from "react"
import { apiFetch } from "@/lib/api"
import { useRefreshTick } from "@/lib/refresh-context"
import type {
  AppVersion,
  BalanceTrendPoint,
  CaptchaConfig,
  Channel,
  ChannelPage,
  CostTrendPoint,
  DashboardSummary,
  NotificationChannel,
  NotificationLogPage,
  RateChangeLogPage,
  RateSnapshot,
  SystemConfigResponse,
  UpstreamAnnouncementPage,
} from "@/lib/api-types"
import type { TargetAnalytics } from "@/lib/operations-api"

export interface QueryState<T> {
  data: T | null
  loading: boolean
  error: string | null
  refetch: () => void
  setData: (data: T) => void
}

/**
 * In-flight 请求去重：同一个 URL 在同一个 tick 内只发一次，所有 useApi 共享 Promise。
 *
 * 为什么需要：useDashboardSummary() 在 5 个组件里都被调用，没去重的话每次 mount /
 * refresh 都会发 5 个相同请求。开发环境叠加 StrictMode 翻倍后会更夸张。
 */
const inflight = new Map<string, Promise<unknown>>()

/** Cache 已完成的响应一小段时间，便于同一帧内挂载的多个组件共享结果（即使第一次的 Promise 已经 resolve）。 */
interface CacheEntry {
  data: unknown
  expiresAt: number
}
const cache = new Map<string, CacheEntry>()
const CACHE_TTL_MS = 800
const CACHE_MAX_ENTRIES = 128

function cacheKey(path: string, tick: number, bump: number) {
  return `${path}#${tick}#${bump}`
}

function pruneCache(now: number) {
  for (const [key, entry] of cache) {
    if (entry.expiresAt <= now) cache.delete(key)
  }
  while (cache.size > CACHE_MAX_ENTRIES) {
    const oldest = cache.keys().next().value
    if (oldest === undefined) break
    cache.delete(oldest)
  }
}

function fetchShared<T>(path: string, key: string): Promise<T> {
  const now = Date.now()
  pruneCache(now)

  const cached = cache.get(key)
  if (cached && cached.expiresAt > now) {
    return Promise.resolve(cached.data as T)
  }

  const existing = inflight.get(key) as Promise<T> | undefined
  if (existing) return existing

  const p = apiFetch<T>(path)
    .then((d) => {
      const completedAt = Date.now()
      cache.set(key, { data: d, expiresAt: completedAt + CACHE_TTL_MS })
      pruneCache(completedAt)
      return d
    })
    .finally(() => {
      // 让下一帧（refresh tick++）拉到新的数据，不要永远 hold 住旧 promise
      inflight.delete(key)
    })
  inflight.set(key, p)
  return p
}

interface QuerySnapshot<T> {
  path: string | null
  data: T | null
  loading: boolean
  error: string | null
}

/**
 * useApi 通用数据获取 hook（stale-while-revalidate）。
 * - 首次加载：loading = true，组件显示加载占位
 * - 后续刷新（refresh tick / refetch）：保留旧 data 继续展示，loading 不切回 true，后台静默拉新
 * - 同 URL + 同 tick 的并发调用共享一次请求
 */
function useApi<T>(path: string | null, watchRefresh = true): QueryState<T> {
  const [snapshot, setSnapshot] = useState<QuerySnapshot<T>>({
    path,
    data: null,
    loading: path !== null,
    error: null,
  })
  const [bump, setBump] = useState(0)
  const refreshTick = useRefreshTick()
  const globalTick = watchRefresh ? refreshTick : 0

  useEffect(() => {
    if (path === null) {
      setSnapshot({ path: null, data: null, loading: false, error: null })
      return
    }
    let cancelled = false
    setSnapshot((previous) => {
      const hasCurrentData = previous.path === path && previous.data !== null
      return {
        path,
        data: hasCurrentData ? previous.data : null,
        loading: !hasCurrentData,
        error: null,
      }
    })
    fetchShared<T>(path, cacheKey(path, globalTick, bump))
      .then((d) => {
        if (cancelled) return
        setSnapshot({ path, data: d, loading: false, error: null })
      })
      .catch((e: Error) => {
        if (cancelled) return
        setSnapshot((previous) => ({
          path,
          data: previous.path === path ? previous.data : null,
          loading: false,
          error: e.message,
        }))
      })
    return () => {
      cancelled = true
    }
  }, [path, bump, globalTick])

  const current = snapshot.path === path
    ? snapshot
    : { path, data: null, loading: path !== null, error: null }

  return {
    data: current.data,
    loading: current.loading,
    error: current.error,
    refetch: () => setBump((b) => b + 1),
    setData: (nextData) => setSnapshot({ path, data: nextData, loading: false, error: null }),
  }
}

export function useDashboardSummary() {
  return useApi<DashboardSummary>("/dashboard/summary")
}

/** 本地自然日起止（RFC3339），用于网关用量「今日」统计 */
/** 网关使用记录聚合统计（默认今日本地时区） */
export function useOperationsAnalyticsToday() {
  return useApi<TargetAnalytics>("/operations/analytics?range=day")
}

export function useAppVersion() {
  return useApi<AppVersion>("/version", false)
}

export function useBalanceTrend(days = 7) {
  return useApi<BalanceTrendPoint[]>(`/dashboard/balance-trend?days=${days}`)
}

export function useCostTrend(days = 7) {
  return useApi<CostTrendPoint[]>(`/dashboard/cost-trend?days=${days}`)
}

export function useChannels() {
  return useApi<Channel[]>("/channels")
}

export function useChannelsPage(page = 1, pageSize = 9, allocationGroup = "") {
  const query = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
  if (allocationGroup.trim()) query.set("allocation_group", allocationGroup.trim())
  return useApi<ChannelPage>(`/channels?${query.toString()}`)
}

export function useChannelRates(channelID: number | null, onlyWithKeys = false) {
  const path =
    channelID == null
      ? null
      : onlyWithKeys
        ? `/channels/${channelID}/rates?only_with_keys=1`
        : `/channels/${channelID}/rates`
  return useApi<RateSnapshot[]>(path)
}

// useMultiChannelRates 把多个上游渠道的倍率分组拉回来合并去重，
// 供订阅规则"多选渠道 + 指定分组"场景使用。复用 fetchShared 缓存，
// 单渠道请求仍与 useChannelRates 共享，不会重复打接口。
export function useMultiChannelRates(channelIDs: number[]) {
  const key = Array.from(new Set(channelIDs)).sort((a, b) => a - b).join(",")
  const [snapshot, setSnapshot] = useState<{
    key: string
    data: RateSnapshot[] | null
  }>({ key, data: null })
  const [loading, setLoading] = useState(key !== "")
  const [bump, setBump] = useState(0)
  const refreshTick = useRefreshTick()

  useEffect(() => {
    if (key === "") {
      setSnapshot({ key, data: null })
      setLoading(false)
      return
    }
    let cancelled = false
    setLoading(true)
    const ids = key.split(",").map(Number)
    Promise.all(
      ids.map((id) =>
        fetchShared<RateSnapshot[]>(
          `/channels/${id}/rates`,
          cacheKey(`/channels/${id}/rates`, refreshTick, bump),
        ),
      ),
    )
      .then((results) => {
        if (cancelled) return
        setSnapshot({ key, data: results.flat() })
      })
      .catch(() => {
        if (!cancelled) setSnapshot({ key, data: null })
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
    // channelIDs 是数组引用，用排序后的 key 字符串做依赖避免每次渲染都触发
  }, [key, refreshTick, bump])

  const current = snapshot.key === key
  return {
    data: current ? snapshot.data : null,
    loading: key !== "" && (!current || loading),
    refetch: () => setBump((b) => b + 1),
  }
}

export function useRateChanges(page = 1, pageSize = 20, channelID?: number) {
  const q = new URLSearchParams()
  q.set("page", String(page))
  q.set("page_size", String(pageSize))
  if (channelID != null) q.set("channel_id", String(channelID))
  return useApi<RateChangeLogPage>(`/rate-changes?${q.toString()}`)
}

export function useNotificationChannels() {
  return useApi<NotificationChannel[]>("/notifications/channels")
}

export function useNotificationLogs(page = 1, pageSize = 20) {
  return useApi<NotificationLogPage>(
    `/notifications/logs?page=${page}&page_size=${pageSize}`,
  )
}

export function useAnnouncements(page = 1, pageSize = 20) {
  return useApi<UpstreamAnnouncementPage>(
    `/announcements?page=${page}&page_size=${pageSize}`,
  )
}

export function useCaptchaConfigs(enabled = true) {
  return useApi<CaptchaConfig[]>(enabled ? "/captcha-configs" : null)
}

export function useSystemConfig() {
  return useApi<SystemConfigResponse>("/settings/config")
}
