import { cpSync, existsSync, mkdirSync, rmSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const vendorRoot = resolve(frontendRoot, 'vendor/infinite-canvas')
const dest = resolve(frontendRoot, 'public/canvas')

if (!existsSync(resolve(vendorRoot, 'index.html'))) {
  console.error('missing vendored infinite-canvas at', vendorRoot)
  process.exit(1)
}

rmSync(dest, { recursive: true, force: true })
mkdirSync(dest, { recursive: true })
cpSync(vendorRoot, dest, { recursive: true })
console.log('copied infinite canvas to', dest)
