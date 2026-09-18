import type { Account } from '@/types'

export type SchedulingState = 'paused' | 'inactive' | 'cooldown' | 'eligible' | 'unknown'
type SchedulingAccount = Partial<Pick<Account,
  'status' | 'schedulable' | 'temp_unschedulable_until' | 'temp_unschedulable_reason' |
  'overload_until' | 'rate_limit_reset_at' | 'expires_at'>>

export function accountSchedulingState(account: SchedulingAccount, now: number) {
  const blocks = [
    { kind: 'temporary', until: account.temp_unschedulable_until },
    { kind: 'overload', until: account.overload_until },
    { kind: 'rateLimit', until: account.rate_limit_reset_at },
  ].flatMap(({ kind, until }) => {
    const timestamp = until ? Date.parse(until) : NaN
    return Number.isFinite(timestamp) && timestamp > now ? [{ kind, timestamp }] : []
  })
  const expiry = account.expires_at && account.expires_at > 0 ? account.expires_at * 1000 : NaN
  let state: SchedulingState
  if (account.schedulable === false) state = 'paused'
  else if ((account.status && account.status !== 'active') || expiry <= now) state = 'inactive'
  else if (blocks.length) state = 'cooldown'
  else if (account.status === 'active' && account.schedulable === true) state = 'eligible'
  else state = 'unknown'

  const hasTemporaryBlock = blocks.some(block => block.kind === 'temporary')
  const detail = hasTemporaryBlock ? account.temp_unschedulable_reason?.trim() || null : null

  return {
    state,
    reasons: blocks.map(block => block.kind),
    // All account-wide blocks must expire before scheduling can resume.
    until: blocks.length ? Math.max(...blocks.map(block => block.timestamp)) : null,
    detail,
  }
}
