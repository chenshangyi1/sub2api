import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppHeader.vue')
const componentSource = readFileSync(componentPath, 'utf8')

describe('AppHeader model plaza', () => {
  it('keeps the model plaza entry visible on small screens', () => {
    expect(componentSource).toContain("path: '/model-plaza'")
    const plazaLinkClass = componentSource.match(
      /<!-- Model Plaza Entry -->[\s\S]*?class="([^"]+)"/,
    )?.[1]
    expect(plazaLinkClass).toBeTruthy()
    expect(plazaLinkClass).not.toMatch(/(?:^|\s)hidden(?:\s|$)/)
    expect(plazaLinkClass).toMatch(/(?:^|\s)flex(?:\s|$)/)
  })
})
