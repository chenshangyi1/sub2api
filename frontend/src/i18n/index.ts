import { createI18n } from 'vue-i18n'
import { readLocalStorage, writeLocalStorage } from '@/utils/safeStorage'

type LocaleCode = 'en' | 'zh'

type LocaleMessages = Record<string, any>

const LOCALE_KEY = 'sub2api_locale'
const DEFAULT_LOCALE: LocaleCode = 'en'
const LOCALE_LOAD_TIMEOUT_MS = 4000

export const localeLoaders: Record<LocaleCode, () => Promise<{ default: LocaleMessages }>> = {
  en: () => import('./locales/en'),
  zh: () => import('./locales/zh')
}

function isLocaleCode(value: string): value is LocaleCode {
  return value === 'en' || value === 'zh'
}

function getBrowserLocale(): LocaleCode {
  try {
    const browserLang = navigator.language.toLowerCase()
    if (browserLang.startsWith('zh')) {
      return 'zh'
    }
  } catch {
    // Ignore environments where navigator.language is unavailable.
  }
  return DEFAULT_LOCALE
}

function getDefaultLocale(): LocaleCode {
  const saved = readLocalStorage(LOCALE_KEY)
  if (saved && isLocaleCode(saved)) {
    return saved
  }
  return getBrowserLocale()
}

function withTimeout<T>(promise: Promise<T>, timeoutMs: number): Promise<T> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      reject(new Error(`locale load timed out after ${timeoutMs}ms`))
    }, timeoutMs)
    promise.then(
      (value) => {
        clearTimeout(timer)
        resolve(value)
      },
      (error) => {
        clearTimeout(timer)
        reject(error)
      }
    )
  })
}

export const i18n = createI18n({
  legacy: false,
  locale: getDefaultLocale(),
  fallbackLocale: DEFAULT_LOCALE,
  messages: {},
  // 禁用 HTML 消息警告 - 引导步骤使用富文本内容（driver.js 支持 HTML）
  // 这些内容是内部定义的，不存在 XSS 风险
  warnHtmlMessage: false
})

const loadedLocales = new Set<LocaleCode>()

export function resetLoadedLocalesForTests(): void {
  loadedLocales.clear()
}

export async function loadLocaleMessages(
  locale: LocaleCode,
  timeoutMs = LOCALE_LOAD_TIMEOUT_MS
): Promise<void> {
  if (loadedLocales.has(locale)) {
    return
  }

  const loader = localeLoaders[locale]
  const module = await withTimeout(loader(), timeoutMs)
  i18n.global.setLocaleMessage(locale, module.default)
  loadedLocales.add(locale)
}

export async function initI18n(options?: {
  timeoutMs?: number
  preferredLocale?: LocaleCode
}): Promise<void> {
  const timeoutMs = options?.timeoutMs ?? LOCALE_LOAD_TIMEOUT_MS
  const preferred = options?.preferredLocale ?? getLocale()
  const preferredLoad = loadLocaleMessages(preferred, timeoutMs)

  if (preferred !== DEFAULT_LOCALE) {
    const fallbackLoad = loadLocaleMessages(DEFAULT_LOCALE, timeoutMs)
    try {
      await preferredLoad
      i18n.global.locale.value = preferred
      document.documentElement.setAttribute('lang', preferred)
      return
    } catch (error) {
      console.error(`Failed to load locale messages for ${preferred}:`, error)
      try {
        await fallbackLoad
        i18n.global.locale.value = DEFAULT_LOCALE
        document.documentElement.setAttribute('lang', DEFAULT_LOCALE)
        return
      } catch (fallbackError) {
        console.error(`Failed to load locale messages for ${DEFAULT_LOCALE}:`, fallbackError)
      }
    }
  } else {
    try {
      await preferredLoad
      i18n.global.locale.value = preferred
      document.documentElement.setAttribute('lang', preferred)
      return
    } catch (error) {
      console.error(`Failed to load locale messages for ${preferred}:`, error)
    }
  }

  document.documentElement.setAttribute('lang', preferred)
}

export async function setLocale(locale: string): Promise<void> {
  if (!isLocaleCode(locale)) {
    return
  }

  await loadLocaleMessages(locale)
  i18n.global.locale.value = locale
  writeLocalStorage(LOCALE_KEY, locale)
  document.documentElement.setAttribute('lang', locale)

  // 同步更新浏览器页签标题，使其跟随语言切换
  const { resolveRouteDocumentTitle } = await import('@/router/title')
  const { default: router } = await import('@/router')
  const { useAppStore } = await import('@/stores/app')
  const { useAuthStore } = await import('@/stores/auth')
  const { useAdminSettingsStore } = await import('@/stores/adminSettings')
  const route = router.currentRoute.value
  const appStore = useAppStore()
  const authStore = useAuthStore()
  const adminSettingsStore = useAdminSettingsStore()
  const customMenuItems = [
    ...(appStore.cachedPublicSettings?.custom_menu_items ?? []),
    ...(authStore.isAdmin ? adminSettingsStore.customMenuItems : []),
  ]
  document.title = resolveRouteDocumentTitle(route, appStore.siteName, customMenuItems)
}

export function getLocale(): LocaleCode {
  const current = i18n.global.locale.value
  return isLocaleCode(current) ? current : DEFAULT_LOCALE
}

export const availableLocales = [
  { code: 'en', name: 'English', flag: '🇺🇸' },
  { code: 'zh', name: '中文', flag: '🇨🇳' }
] as const

export default i18n
