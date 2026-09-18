/**
 * Admin Group Monitor API endpoints
 * 分组级渠道监控：一个分组 = 一个监控配置，自动检测组内所有账号健康。
 */

import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'

export interface GroupMonitor {
  id: number
  group_id: number
  group_name: string
  enabled: boolean
  interval_minutes: number
  model_id: string
  auto_recover: boolean
  max_output_tokens: number
  last_run_at: string | null
  next_run_at: string
  created_at: string
  updated_at: string
  account_count: number
  healthy_count: number
  failed_count: number
  unknown_count: number
}

export interface GroupMonitorAccountStatus {
  account_id: number
  account_name: string
  platform: string
  status: 'success' | 'failed' | 'unknown'
  model_id: string
  latency_ms: number
  error_message: string
  checked_at: string
}

export interface CreateGroupMonitorRequest {
  group_id: number
  enabled?: boolean
  interval_minutes?: number
  model_id?: string
  auto_recover?: boolean
  max_output_tokens?: number
}

export interface BatchCreateGroupMonitorRequest {
  group_ids: number[]
  enabled?: boolean
  interval_minutes?: number
  model_id?: string
  auto_recover?: boolean
  max_output_tokens?: number
}

export interface BatchCreateGroupMonitorResult {
  created: number[]
  skipped: number
}

export interface UpdateGroupMonitorRequest {
  enabled?: boolean
  interval_minutes?: number
  model_id?: string
  auto_recover?: boolean
  max_output_tokens?: number
}

export async function list(params?: {
  page?: number
  page_size?: number
  enabled?: boolean
  search?: string
}): Promise<PaginatedResponse<GroupMonitor>> {
  const { data } = await apiClient.get<PaginatedResponse<GroupMonitor>>(
    '/admin/group-monitors',
    { params }
  )
  return data
}

export async function create(req: CreateGroupMonitorRequest): Promise<GroupMonitor> {
  const { data } = await apiClient.post<GroupMonitor>('/admin/group-monitors', req)
  return data
}

export async function batchCreate(
  req: BatchCreateGroupMonitorRequest
): Promise<BatchCreateGroupMonitorResult> {
  const { data } = await apiClient.post<BatchCreateGroupMonitorResult>(
    '/admin/group-monitors/batch',
    req
  )
  return data
}

export async function update(id: number, req: UpdateGroupMonitorRequest): Promise<GroupMonitor> {
  const { data } = await apiClient.put<GroupMonitor>(`/admin/group-monitors/${id}`, req)
  return data
}

export async function remove(id: number): Promise<void> {
  await apiClient.delete(`/admin/group-monitors/${id}`)
}

export async function run(id: number): Promise<GroupMonitorAccountStatus[]> {
  const { data } = await apiClient.post<GroupMonitorAccountStatus[]>(
    `/admin/group-monitors/${id}/run`,
    undefined,
    { timeout: 180000 }
  )
  return data ?? []
}

export async function listResults(id: number): Promise<GroupMonitorAccountStatus[]> {
  const { data } = await apiClient.get<GroupMonitorAccountStatus[]>(
    `/admin/group-monitors/${id}/results`
  )
  return data ?? []
}

export const groupMonitorAPI = {
  list,
  create,
  batchCreate,
  update,
  delete: remove,
  run,
  listResults
}

export default groupMonitorAPI
