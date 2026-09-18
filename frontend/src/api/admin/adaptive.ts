/**
 * Admin Adaptive Group Pool API
 * Parent group → ordered leaf memberships (full replacement on PUT).
 */

import { apiClient } from '../client'

export interface AdaptiveLeafMember {
  leaf_group_id: number
  enabled: boolean
  sort_order: number
}

export interface AdaptivePool {
  parent_group_id: number
  platform: string
  enabled: boolean
  allow_hybrid: boolean
  config_generation: number
  members: AdaptiveLeafMember[]
}

export interface AdaptivePoolListResponse {
  items: AdaptivePool[]
  total: number
}

export interface PutAdaptivePoolRequest {
  enabled: boolean
  allow_hybrid: boolean
  members: AdaptiveLeafMember[]
}

export async function list(options?: { signal?: AbortSignal }): Promise<AdaptivePoolListResponse> {
  const { data } = await apiClient.get<AdaptivePoolListResponse>('/admin/adaptive-groups', {
    signal: options?.signal
  })
  return data
}

export async function getByParentId(parentGroupId: number): Promise<AdaptivePool> {
  const { data } = await apiClient.get<AdaptivePool>(`/admin/adaptive-groups/${parentGroupId}`)
  return data
}

export async function put(
  parentGroupId: number,
  request: PutAdaptivePoolRequest
): Promise<AdaptivePool> {
  const { data } = await apiClient.put<AdaptivePool>(
    `/admin/adaptive-groups/${parentGroupId}`,
    request
  )
  return data
}

export async function remove(
  parentGroupId: number
): Promise<{ deleted: boolean; parent_group_id: number }> {
  const { data } = await apiClient.delete<{ deleted: boolean; parent_group_id: number }>(
    `/admin/adaptive-groups/${parentGroupId}`
  )
  return data
}

/** Per-tier parameters configured by admin */
export interface AntiStallTierParams {
  buffer_tokens: number
  drip_tokens_per_second: number
  upstream_max_retry: number
  low_buffer_tokens: number
  max_drip_seconds: number
  max_leaf_switches: number
}

/**
 * Admin Anti-Stall PRO config.
 * Users pick off/basic/pro/ultra on their API key; params come from here.
 */
export interface AntiStallAdminConfig {
  module_enabled: boolean
  basic: AntiStallTierParams
  pro: AntiStallTierParams
  ultra: AntiStallTierParams
}

/** @deprecated legacy flat shape — use AntiStallAdminConfig */
export interface AntiStallProSettings {
  enabled: boolean
  buffer_tokens: number
  drip_tokens_per_second: number
  upstream_max_retry: number
  low_buffer_tokens: number
  max_drip_seconds?: number
  max_leaf_switches?: number
}

export const defaultTierParams = (tier: 'basic' | 'pro' | 'ultra'): AntiStallTierParams => {
  if (tier === 'pro') {
    return {
      buffer_tokens: 48,
      drip_tokens_per_second: 1,
      upstream_max_retry: 3,
      low_buffer_tokens: 4,
      max_drip_seconds: 45,
      max_leaf_switches: 4
    }
  }
  if (tier === 'ultra') {
    return {
      buffer_tokens: 96,
      drip_tokens_per_second: 1,
      upstream_max_retry: 4,
      low_buffer_tokens: 6,
      max_drip_seconds: 60,
      max_leaf_switches: 5
    }
  }
  return {
    buffer_tokens: 32,
    drip_tokens_per_second: 1,
    upstream_max_retry: 3,
    low_buffer_tokens: 4,
    max_drip_seconds: 30,
    max_leaf_switches: 3
  }
}

export function defaultAntiStallAdminConfig(): AntiStallAdminConfig {
  return {
    module_enabled: true,
    basic: defaultTierParams('basic'),
    pro: defaultTierParams('pro'),
    ultra: defaultTierParams('ultra')
  }
}

export async function getAntiStallPro(): Promise<AntiStallAdminConfig> {
  const { data } = await apiClient.get<AntiStallAdminConfig>('/admin/anti-stall-pro')
  return data
}

export async function putAntiStallPro(settings: AntiStallAdminConfig): Promise<AntiStallAdminConfig> {
  const { data } = await apiClient.put<AntiStallAdminConfig>('/admin/anti-stall-pro', settings)
  return data
}

const adaptiveAPI = {
  list,
  getByParentId,
  put,
  delete: remove,
  getAntiStallPro,
  putAntiStallPro
}

export default adaptiveAPI
