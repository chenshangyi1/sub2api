import { sanitizeUrl } from '@/utils/url'

export const defaultBrandLogo = '/logo.png'
const legacyDefaultLogos = new Set(['/logo.svg', '/logo-v2.svg', '/logo.png'])

export function resolveBrandLogo(logoUrl = ''): string {
  const sanitizedLogoUrl = sanitizeUrl(logoUrl, {
    allowRelative: true,
    allowDataUrl: true,
  })
  if (!sanitizedLogoUrl || legacyDefaultLogos.has(sanitizedLogoUrl)) {
    return defaultBrandLogo
  }
  return sanitizedLogoUrl
}

export function updateFavicon(logoUrl: string): void {
  const sanitizedLogoUrl = resolveBrandLogo(logoUrl)
  if (!sanitizedLogoUrl) {
    return
  }

  let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
  if (!link) {
    link = document.createElement('link')
    link.rel = 'icon'
    document.head.appendChild(link)
  }

  link.type = sanitizedLogoUrl.endsWith('.svg') ? 'image/svg+xml' : 'image/x-icon'
  link.href = sanitizedLogoUrl
}
