import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import i18n, { initI18n } from './i18n'
import { useAppStore } from '@/stores/app'
import { updateFavicon } from '@/utils/branding'
import { isIOSDevice } from '@/utils/device'
import { initTheme } from '@/composables/useCharacterTheme'
import { readLocalStorage } from '@/utils/safeStorage'
import './style.css'
import './styles/design-system.css'
import './styles/macos-liquid-glass.css'
import './styles/console-redesign.css'

function initIOSViewportZoomFix() {
  // iOS Safari 在输入框字号小于 16px 时聚焦会自动放大页面，且失焦后不会恢复。
  // 限制 maximum-scale 可阻止该行为；iOS 10+ 用户仍可双指手动缩放，不影响可访问性。
  // 仅在 iOS 设备上注入，避免影响 Android Chrome 的手动缩放能力。
  if (!isIOSDevice()) return

  const viewport = document.querySelector('meta[name="viewport"]')
  if (!viewport) return

  const content = viewport.getAttribute('content') || ''
  if (/maximum-scale/i.test(content)) return
  viewport.setAttribute('content', `${content}, maximum-scale=1.0`)
}

function initThemeClass() {
  const savedTheme = readLocalStorage('theme')
  let prefersDark = false
  try {
    prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
  } catch {
    prefersDark = false
  }
  const shouldUseDark = savedTheme === 'dark' || (!savedTheme && prefersDark)
  document.documentElement.classList.toggle('dark', shouldUseDark)
  document.documentElement.style.colorScheme = shouldUseDark ? 'dark' : 'light'
}

function initChunkReloadGuard() {
  window.addEventListener('unhandledrejection', (event) => {
    const msg = (event.reason as Error)?.message || String(event.reason)
    if (/Failed to fetch dynamically imported module|Loading chunk|error loading dynamically imported module|Importing a module script failed/i.test(msg)) {
      const key = 'chunk_reload_attempted_app'
      const last = Number(sessionStorage.getItem(key) || 0)
      if (!last || Date.now() - last > 10000) {
        sessionStorage.setItem(key, String(Date.now()))
        window.location.reload()
      }
    }
  })
}

async function bootstrap() {
  // Keep this assignment: it forces a new hashed entry chunk after 0.1.295
  // served compressed JS without Content-Type, which browsers refuse as modules.
  document.documentElement.dataset.spaBuild = '0.1.329'

  // Apply theme class globally before app mount to keep all routes consistent.
  try {
    initThemeClass()
  } catch (error) {
    console.error(error)
  }
  initIOSViewportZoomFix()
  initChunkReloadGuard()

  // 初始化角色动态主题系统
  try {
    initTheme()
  } catch (error) {
    console.error(error)
  }

  const app = createApp(App)
  const pinia = createPinia()
  app.use(pinia)

  app.config.errorHandler = (err) => {
    const msg = (err as Error)?.message || String(err)
    if (/Failed to fetch dynamically imported module|Loading chunk|error loading dynamically imported module|Importing a module script failed/i.test(msg)) {
      const key = 'chunk_reload_attempted_app'
      const last = Number(sessionStorage.getItem(key) || 0)
      if (!last || Date.now() - last > 10000) {
        sessionStorage.setItem(key, String(Date.now()))
        window.location.reload()
        return
      }
    }
    console.error(err)
  }

  // Initialize settings from injected config BEFORE mounting (prevents flash)
  // This must happen after pinia is installed but before router and i18n
  const appStore = useAppStore()
  appStore.initFromInjectedConfig()

  // Set document title immediately after config is loaded
  if (appStore.siteName && appStore.siteName !== 'Sub2API') {
    document.title = `${appStore.siteName} - AI API Gateway`
  }
  updateFavicon(appStore.siteLogo)

  try {
    await initI18n()
  } catch (error) {
    console.error('initI18n failed, mounting without locale messages', error)
  }

  app.use(router)
  app.use(i18n)

  // 等待路由器完成初始导航后再挂载，避免竞态条件导致的空白渲染
  try {
    await router.isReady()
  } catch (error) {
    console.error(error)
  }
  app.mount('#app')
}

bootstrap().catch((error) => {
  console.error('bootstrap failed', error)
})
