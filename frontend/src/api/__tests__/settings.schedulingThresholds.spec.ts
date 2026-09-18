import { describe, expect, it } from 'vitest'
import { SCHEDULING_THRESHOLD_PLATFORMS } from '@/api/admin/settings'

describe('scheduling threshold platforms', () => {
  it('exposes unified cn instead of only legacy kimi/zhipu', () => {
    expect(SCHEDULING_THRESHOLD_PLATFORMS).toContain('openai')
    expect(SCHEDULING_THRESHOLD_PLATFORMS).toContain('anthropic')
    expect(SCHEDULING_THRESHOLD_PLATFORMS).toContain('grok')
    expect(SCHEDULING_THRESHOLD_PLATFORMS).toContain('cn')
    expect(SCHEDULING_THRESHOLD_PLATFORMS).not.toContain('deepseek')
  })
})
