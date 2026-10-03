import { describe, expect, it } from 'vitest'
import { accountSchedulingState } from '../accountSchedulingState'

const now = Date.parse('2026-09-16T12:00:00Z')
const base = { status: 'active' as const, schedulable: true }

describe('accountSchedulingState', () => {
  it('distinguishes unknown from locally eligible', () => {
    expect(accountSchedulingState({}, now).state).toBe('unknown')
    expect(accountSchedulingState(base, now).state).toBe('eligible')
  })
  it('uses the latest of all active cooldowns', () => {
    const state = accountSchedulingState({ ...base,
      temp_unschedulable_until: '2026-09-16T12:01:00Z',
      overload_until: '2026-09-16T12:02:00Z',
      rate_limit_reset_at: '2026-09-16T12:05:00Z',
    }, now)
    expect(state.state).toBe('cooldown')
    expect(state.until).toBe(now + 300_000)
    expect(state.reasons).toEqual(['temporary', 'overload', 'rateLimit'])
  })
  it('ignores invalid and expired timestamps including exact expiry', () => {
    expect(accountSchedulingState({ ...base, temp_unschedulable_until: 'invalid',
      overload_until: new Date(now - 1).toISOString(), rate_limit_reset_at: new Date(now).toISOString(),
    }, now)).toEqual({ state: 'eligible', until: null, reasons: [], detail: null })
  })
  it('does not claim paused or inactive accounts resume after cooldown', () => {
    expect(accountSchedulingState({ ...base, schedulable: false,
      overload_until: new Date(now + 1000).toISOString() }, now).state).toBe('paused')
    expect(accountSchedulingState({ ...base, status: 'error' }, now).state).toBe('inactive')
    expect(accountSchedulingState({ ...base, expires_at: now / 1000 }, now).state).toBe('inactive')
    expect(accountSchedulingState({ ...base, expires_at: 0 }, now).state).toBe('eligible')
  })
  it('recomputes eligibility after time advances', () => {
    const account = { ...base, overload_until: new Date(now + 1000).toISOString() }
    expect(accountSchedulingState(account, now).state).toBe('cooldown')
    expect(accountSchedulingState(account, now + 1001).state).toBe('eligible')
  })
  it('surfaces the live temp-unschedulable reason only while that block is active', () => {
    const until = '2026-09-16T12:01:00Z'
    expect(accountSchedulingState({ ...base, temp_unschedulable_until: until,
      temp_unschedulable_reason: '  upstream 503  ' }, now).detail).toBe('upstream 503')
    expect(accountSchedulingState({ ...base, overload_until: until,
      temp_unschedulable_reason: 'stale reason' }, now).detail).toBeNull()
  })
})
