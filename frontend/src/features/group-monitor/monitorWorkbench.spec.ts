import { describe, expect, it } from 'vitest'
import {
  MONITOR_PRESETS,
  applyMonitorPreset,
  filterMonitors,
  monitorHealth,
  summarizeMonitors,
  validateMonitorForm,
} from './monitorWorkbench'

describe('monitor workbench rules', () => {
  it('provides usable fast, balanced, and economy presets', () => {
    expect(MONITOR_PRESETS.fast.interval_minutes).toBe(10)
    expect(MONITOR_PRESETS.balanced.max_output_tokens).toBe(16)
    expect(MONITOR_PRESETS.economy.auto_recover).toBe(false)
    expect(applyMonitorPreset({ group_ids: [1], interval_minutes: 5, model_id: '', auto_recover: false, max_output_tokens: 1 }, 'fast').interval_minutes).toBe(10)
  })

  it('classifies failed samples as degraded and empty samples as unknown', () => {
    expect(monitorHealth({ enabled: true, healthy_count: 3, failed_count: 0, unknown_count: 0 })).toBe('healthy')
    expect(monitorHealth({ enabled: true, healthy_count: 2, failed_count: 1, unknown_count: 0 })).toBe('degraded')
    expect(monitorHealth({ enabled: true, healthy_count: 0, failed_count: 0, unknown_count: 0 })).toBe('unknown')
  })

  it('filters and summarizes monitors without mutating the source list', () => {
    const monitors = [
      { enabled: true, healthy_count: 3, failed_count: 0, unknown_count: 0 },
      { enabled: false, healthy_count: 1, failed_count: 1, unknown_count: 0 },
      { enabled: true, healthy_count: 0, failed_count: 0, unknown_count: 2 },
    ]
    expect(filterMonitors(monitors, 'degraded')).toHaveLength(1)
    expect(summarizeMonitors(monitors)).toEqual({ total: 3, enabled: 2, healthy: 1, degraded: 1, unknown: 1 })
    expect(monitors).toHaveLength(3)
  })

  it('rejects unusable configurations before saving', () => {
    expect(validateMonitorForm({ group_ids: [], interval_minutes: 30, model_id: '', auto_recover: true, max_output_tokens: 16 })).toBe('group_required')
    expect(validateMonitorForm({ group_ids: [1], interval_minutes: 2, model_id: '', auto_recover: true, max_output_tokens: 16 })).toBe('interval_invalid')
    expect(validateMonitorForm({ group_ids: [1], interval_minutes: 30, model_id: '', auto_recover: true, max_output_tokens: 512 })).toBe('max_output_tokens_invalid')
    expect(validateMonitorForm({ group_ids: [1], interval_minutes: 30, model_id: '', auto_recover: true, max_output_tokens: 16 })).toBeNull()
  })
})

