import { afterEach, describe, expect, it } from 'vitest'
import { initI18n, localeLoaders, resetLoadedLocalesForTests } from '../index'

const originalEn = localeLoaders.en
const originalZh = localeLoaders.zh

afterEach(() => {
  localeLoaders.en = originalEn
  localeLoaders.zh = originalZh
  resetLoadedLocalesForTests()
  document.documentElement.removeAttribute('lang')
})

describe('initI18n boot resilience', () => {
  it('resolves and still sets lang when the preferred locale chunk rejects', async () => {
    localeLoaders.zh = async () => {
      throw new TypeError('Failed to fetch dynamically imported module')
    }
    localeLoaders.en = originalEn

    await expect(initI18n({ timeoutMs: 4000, preferredLocale: 'zh' })).resolves.toBeUndefined()
    expect(document.documentElement.getAttribute('lang')).toBe('en')
  })

  it('resolves when every locale loader hangs past the timeout', async () => {
    localeLoaders.zh = () => new Promise(() => undefined)
    localeLoaders.en = () => new Promise(() => undefined)

    const started = Date.now()
    await expect(initI18n({ timeoutMs: 50, preferredLocale: 'zh' })).resolves.toBeUndefined()
    expect(Date.now() - started).toBeLessThan(1500)
  })
})
