import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppSidebar.vue')
const componentSource = readFileSync(componentPath, 'utf8')
const stylePath = resolve(dirname(fileURLToPath(import.meta.url)), '../../../style.css')
const styleSource = readFileSync(stylePath, 'utf8')

describe('AppSidebar custom SVG styles', () => {
  it('does not override uploaded SVG fill or stroke colors', () => {
    expect(componentSource).toContain('.sidebar-svg-icon {')
    expect(componentSource).toContain('color: currentColor;')
    expect(componentSource).toContain('display: block;')
    expect(componentSource).not.toContain('stroke: currentColor;')
    expect(componentSource).not.toContain('fill: none;')
  })
})

describe('AppSidebar scroll position persistence', () => {
  it('binds a template ref to the sidebar nav element', () => {
    expect(componentSource).toContain('ref="sidebarNavRef"')
    expect(componentSource).toContain('sidebar-nav')
  })

  it('declares sidebarNavRef in script setup', () => {
    expect(componentSource).toContain("const sidebarNavRef = ref<HTMLElement | null>(null)")
  })

  it('saves scroll position on beforeUnmount', () => {
    expect(componentSource).toContain('onBeforeUnmount')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('sidebarNavRef.value.scrollTop')
  })

  it('restores scroll position on mount', () => {
    expect(componentSource).toContain('onMounted')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('nextTick')
  })
})

describe('AppSidebar header styles', () => {
  it('does not clip the version badge dropdown', () => {
    const sidebarHeaderBlockMatch = styleSource.match(/\.sidebar-header\s*\{[\s\S]*?\n {2}\}/)
    const sidebarBrandBlockMatch = componentSource.match(/\.sidebar-brand\s*\{[\s\S]*?\n\}/)

    expect(sidebarHeaderBlockMatch).not.toBeNull()
    expect(sidebarBrandBlockMatch).not.toBeNull()
    expect(sidebarHeaderBlockMatch?.[0]).not.toContain('@apply overflow-hidden;')
    expect(sidebarBrandBlockMatch?.[0]).not.toContain('overflow: hidden;')
  })
})

describe('AppSidebar user navigation', () => {
  it('hides the legacy subscriptions entry while keeping the route available', () => {
    expect(componentSource).not.toContain("{ path: '/subscriptions'")
  })

  it('exposes model plaza in the mobile sidebar menu', () => {
    expect(componentSource).toContain("path: '/model-plaza'")
    expect(componentSource).toContain("t('nav.modelPlaza')")
    expect(componentSource).toContain('flagModelPlaza')
  })

  it('does not keep the compact-home toggle', () => {
    expect(componentSource).not.toContain('data-testid="sidebar-home-layout"')
    expect(componentSource).not.toContain('toggleHomeLayout')
    expect(componentSource).not.toContain("t('nav.useCompactHome')")
    expect(componentSource).not.toContain('homeLayoutPreference')
  })

  it('keeps Infinite Canvas and batch image without Image Studio', () => {
    expect(componentSource).not.toContain("path: '/image-studio'")
    expect(componentSource).not.toContain("t('nav.imageStudio')")
    expect(componentSource).toContain("path: '/infinite-canvas'")
    expect(componentSource).toContain("path: '/batch-image'")
    expect(componentSource.indexOf("path: '/infinite-canvas'")).toBeLessThan(componentSource.indexOf("path: '/batch-image'"))
  })

  it('includes Infinite Canvas next to API keys', () => {
    expect(componentSource).toContain("path: '/infinite-canvas'")
    expect(componentSource).toContain("t('nav.infiniteCanvas')")
    expect(componentSource.indexOf("path: '/keys'")).toBeLessThan(componentSource.indexOf("path: '/infinite-canvas'"))
  })

  it('hides the IP allowlist page from user navigation', () => {
    expect(componentSource).not.toContain("path: '/ip-allowlist'")
    expect(componentSource).not.toContain("t('nav.ipAllowlist')")
  })

  it('renders the character-safe BrandLogo instead of a clipped square', () => {
    expect(componentSource).toContain('BrandLogo')
    expect(componentSource).not.toContain("siteLogo || '/logo.png'")
    expect(componentSource).not.toContain('h-10 w-10 overflow-hidden')
  })

  it('keeps the expanded sidebar at 16rem so it does not overlap the main pane', () => {
    expect(componentSource).toContain("sidebarCollapsed ? 'w-[76px]' : 'w-64'")
    expect(componentSource).not.toContain("'w-[17.5rem]'")
  })
})
