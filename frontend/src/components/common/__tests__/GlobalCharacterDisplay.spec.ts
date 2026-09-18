import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const dir = dirname(fileURLToPath(import.meta.url))
const source = readFileSync(resolve(dir, '../GlobalCharacterDisplay.vue'), 'utf8')
const tokens = readFileSync(resolve(dir, '../../../styles/design-system.css'), 'utf8')
const layout = readFileSync(resolve(dir, '../../layout/AppLayout.vue'), 'utf8')

describe('character wallpaper contrast', () => {
  it('pins a larger portrait to the right instead of covering the whole console', () => {
    expect(source).toContain('width: min(52vw, 720px);')
    expect(source).toContain('object-fit: contain;')
    expect(source).toContain('object-position: right bottom;')
    expect(source).not.toContain('width: min(42vw, 560px);')
    expect(source).not.toContain('object-position: 22% 16%;')
  })

  it('lets the portrait show through without washing out controls', () => {
    expect(tokens).toContain('var(--ds-surface) 62%')
    expect(tokens).not.toContain('var(--ds-surface) 88%')
    expect(layout).toContain('app-main-surface')
    expect(layout).toContain('color-mix(in srgb, var(--ds-bg, #f8fafc) 28%, transparent) 100%')
    expect(source).not.toContain('html.compact-home-active')
  })
})
