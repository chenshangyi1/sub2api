import { beforeEach, describe, expect, it } from 'vitest'
import { resolveBrandLogo, updateFavicon } from '@/utils/branding'

describe('resolveBrandLogo', () => {
  it('uses the shipped png for empty or legacy placeholders', () => {
    expect(resolveBrandLogo('')).toBe('/logo.png')
    expect(resolveBrandLogo('/logo.svg')).toBe('/logo.png')
    expect(resolveBrandLogo('/logo-v2.svg')).toBe('/logo.png')
  })

  it('keeps a custom character portrait', () => {
    expect(resolveBrandLogo('/characters/character-purple-cat.jpg')).toBe('/characters/character-purple-cat.jpg')
  })
})

describe('updateFavicon', () => {
  beforeEach(() => {
    document.head.innerHTML = '<link rel="icon" href="/logo.png">'
  })

  it('replaces the default favicon with the configured logo', () => {
    updateFavicon('https://example.com/custom-logo.png')

    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    expect(link?.href).toBe('https://example.com/custom-logo.png')
  })

  it('ignores unsafe logo URLs', () => {
    updateFavicon('javascript:alert(1)')

    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    expect(link?.getAttribute('href')).toBe('/logo.png')
  })
})
