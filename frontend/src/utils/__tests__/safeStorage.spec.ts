import { afterEach, describe, expect, it, vi } from 'vitest'
import { readLocalStorage, writeLocalStorage } from '../safeStorage'

describe('safeStorage', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.removeItem('theme')
    localStorage.removeItem('sub2api_locale')
  })

  it('returns stored values', () => {
    localStorage.setItem('theme', 'dark')
    expect(readLocalStorage('theme')).toBe('dark')
  })

  it('returns null when localStorage throws', () => {
    vi.stubGlobal('localStorage', {
      getItem() {
        throw new Error('Access is denied')
      },
      setItem() {
        throw new Error('Access is denied')
      },
    })
    expect(readLocalStorage('theme')).toBeNull()
    expect(() => writeLocalStorage('theme', 'dark')).not.toThrow()
  })
})
