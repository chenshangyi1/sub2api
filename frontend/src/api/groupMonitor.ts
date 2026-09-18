import { apiClient } from './client'

export interface UserGroupMonitorStats {
  availability: number
  avg_latency_ms: number
  avg_ttft_ms: number
  cache_rate: number
  probes: number
}

export interface UserGroupMonitorSeriesPoint {
  bucket: string
  availability: number
  avg_ttft_ms: number
  avg_latency_ms: number
  cache_rate: number
  probes: number
}

export interface UserGroupMonitorAccountState {
  status: string
  latency_ms: number
}

export interface UserGroupMonitorRecentRecord {
  status: string
  latency_ms: number
  checked_at: string
  success: number
  failed: number
  cache_rate: number
}

export interface UserGroupMonitor {
  id: number
  group_id: number
  group_name: string
  enabled: boolean
  model_id: string
  last_run_at?: string
  next_run_at?: string
  account_count: number
  healthy_count: number
  failed_count: number
  unknown_count: number
  stats: Record<'1h' | '1d' | '7d', UserGroupMonitorStats>
  series: Record<'1h' | '1d' | '7d', UserGroupMonitorSeriesPoint[]>
  account_states: UserGroupMonitorAccountState[]
  recent: UserGroupMonitorRecentRecord[]
}

export interface UserGroupMonitorListResponse {
  items: UserGroupMonitor[]
  total: number
}

export interface UserGroupAccountStatus {
  account_id: number
  account_name: string
  platform: string
  status: string
  model_id: string
  latency_ms: number
  error_message: string
  checked_at?: string
}

export async function listGroupMonitors(
  params?: { signal?: AbortSignal; window?: '1h' | '1d' | '7d' }
): Promise<UserGroupMonitorListResponse> {
  const { data } = await apiClient.get<UserGroupMonitorListResponse>('/group-monitors', {
    signal: params?.signal,
    params: params?.window ? { window: params.window } : undefined,
  })
  return data
}

export async function fetchGroupMonitorResults(
  id: number,
  params?: { signal?: AbortSignal }
): Promise<UserGroupAccountStatus[]> {
  const { data } = await apiClient.get<UserGroupAccountStatus[]>(`/group-monitors/${id}/results`, {
    signal: params?.signal,
  })
  return data
}
