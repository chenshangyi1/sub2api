import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppLayout.vue')
const componentSource = readFileSync(componentPath, 'utf8')

describe('AppLayout flush content', () => {
  it('drops main padding when the route asks for a full-bleed page', () => {
    expect(componentSource).toContain("Boolean(route.meta.flushContent)")
    expect(componentSource).toContain("flushContent ? 'p-0' : 'p-4 md:p-6 lg:p-8'")
  })

  it('offsets the main pane by the same 16rem as the expanded sidebar', () => {
    expect(componentSource).toContain("sidebarCollapsed ? 'lg:ml-[72px]' : 'lg:ml-64'")
  })
})
