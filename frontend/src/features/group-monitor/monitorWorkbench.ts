export type MonitorPreset = 'fast' | 'balanced' | 'economy'
export type MonitorHealthFilter = 'all' | 'healthy' | 'degraded' | 'unknown'

export interface MonitorPresetConfig { interval_minutes: number; max_output_tokens: number; auto_recover: boolean }
export interface MonitorCardLike { enabled: boolean; healthy_count: number; failed_count: number; unknown_count: number }
export interface MonitorSummary { total: number; enabled: number; healthy: number; degraded: number; unknown: number }
export interface MonitorFormLike { group_ids: number[]; interval_minutes: number; model_id: string; auto_recover: boolean; max_output_tokens: number }

export const MONITOR_PRESETS: Record<MonitorPreset, MonitorPresetConfig> = {
  fast: { interval_minutes: 10, max_output_tokens: 32, auto_recover: true },
  balanced: { interval_minutes: 30, max_output_tokens: 16, auto_recover: true },
  economy: { interval_minutes: 120, max_output_tokens: 8, auto_recover: false },
}
export function applyMonitorPreset(form: MonitorFormLike, preset: MonitorPreset): MonitorFormLike { return { ...form, ...MONITOR_PRESETS[preset] } }
export function monitorHealth(monitor: MonitorCardLike): Exclude<MonitorHealthFilter, 'all'> {
  const total = monitor.healthy_count + monitor.failed_count + monitor.unknown_count
  if (total === 0 || monitor.unknown_count === total) return 'unknown'
  if (monitor.failed_count > 0) return 'degraded'
  return 'healthy'
}
export function filterMonitors<T extends MonitorCardLike>(monitors: T[], filter: MonitorHealthFilter): T[] {
  if (filter === 'all') return monitors
  return monitors.filter((monitor) => monitorHealth(monitor) === filter)
}
export function summarizeMonitors(monitors: MonitorCardLike[]): MonitorSummary {
  return monitors.reduce<MonitorSummary>((summary, monitor) => {
    summary.total += 1
    if (monitor.enabled) summary.enabled += 1
    const health = monitorHealth(monitor)
    if (health === 'healthy') summary.healthy += 1
    else if (health === 'degraded') summary.degraded += 1
    else summary.unknown += 1
    return summary
  }, { total: 0, enabled: 0, healthy: 0, degraded: 0, unknown: 0 })
}
export function validateMonitorForm(form: MonitorFormLike): string | null {
  if (form.group_ids.length === 0) return 'group_required'
  if (!Number.isInteger(form.interval_minutes) || form.interval_minutes < 5 || form.interval_minutes > 1440) return 'interval_invalid'
  if (!Number.isInteger(form.max_output_tokens) || form.max_output_tokens < 1 || form.max_output_tokens > 256) return 'max_output_tokens_invalid'
  return null
}
