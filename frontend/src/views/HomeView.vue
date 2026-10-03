<template>
  <div v-if="homeContent" class="relative min-h-[100dvh]">
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      class="h-[100dvh] w-full border-0"
      allowfullscreen
    ></iframe>
    <div v-else v-html="homeContent"></div>
  </div>

  <div v-else class="gateway-shell terminal-container min-h-[100dvh]">
    <div class="gateway-grid pointer-events-none absolute inset-0" aria-hidden="true"></div>

    <header class="glass-header relative z-30">
      <nav class="glass-nav mx-auto flex h-16 max-w-7xl items-center justify-between px-4 sm:px-6 lg:px-8" aria-label="Primary">
        <a href="#top" class="brand-link flex min-w-0 items-center gap-3" :aria-label="siteName">
          <BrandLogo :src="siteLogo" :alt="siteName" size="sm" />
          <span class="brand-name truncate text-sm font-bold text-gray-950 dark:text-white">{{ siteName }}</span>
        </a>

        <div class="flex shrink-0 items-center gap-1 sm:gap-2">
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="icon-button"
            :title="t('home.viewDocs')"
          >
            <Icon name="book" size="md" />
          </a>
          <LocaleSwitcher />
          <router-link
            v-if="showModelPlazaEntry"
            to="/model-plaza"
            class="flex h-10 shrink-0 items-center gap-1.5 rounded-lg px-2.5 text-sm font-medium text-gray-500 hover:bg-gray-100 hover:text-gray-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
            :title="t('nav.modelPlaza')"
          >
            <Icon name="grid" size="md" />
            <span class="hidden sm:inline">{{ t('nav.modelPlaza') }}</span>
          </router-link>
          <button
            type="button"
            class="icon-button"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            @click="toggleTheme"
          >
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>
          <router-link
            :to="isAuthenticated ? dashboardPath : '/login'"
            class="header-action"
            :aria-label="isAuthenticated ? t('home.dashboard') : t('home.login')"
          >
            <Icon :name="isAuthenticated ? 'grid' : 'login'" size="sm" />
            <span>{{ isAuthenticated ? t('home.dashboard') : t('home.login') }}</span>
          </router-link>
        </div>
      </nav>
    </header>

    <main id="top" class="relative z-10">
      <section class="hero-layout mx-auto grid max-w-7xl gap-10 px-4 py-12 sm:px-6 sm:py-16 lg:grid-cols-[0.82fr_1.18fr] lg:items-center lg:gap-16 lg:px-8 lg:py-20">
        <div class="hero-copy max-w-2xl">
          <div class="brand-kicker mb-5">
            <span class="status-dot" aria-hidden="true"></span>
            {{ t('home.heroKicker') }}
          </div>
          <h1 class="hero-title text-4xl font-black leading-[1.04] text-gray-950 dark:text-white md:text-5xl">
            {{ siteName }}
          </h1>
          <p class="hero-subtitle mt-5 max-w-xl text-2xl font-semibold leading-tight text-gray-800 dark:text-gray-100">
            {{ t('home.heroSubtitle') }}
          </p>
          <p class="hero-description mt-4 max-w-xl text-base leading-relaxed text-gray-600 dark:text-dark-300">
            {{ t('home.heroDescription') }}
          </p>

          <div class="hero-actions mt-8 flex flex-col gap-3 sm:flex-row">
            <router-link :to="isAuthenticated ? dashboardPath : '/login'" class="primary-action">
              <Icon :name="isAuthenticated ? 'grid' : 'key'" size="sm" />
              {{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}
              <Icon name="arrowRight" size="sm" />
            </router-link>
            <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer" class="secondary-action">
              <Icon name="book" size="sm" />
              {{ t('home.viewDocs') }}
            </a>
          </div>

          <div class="feature-strip mt-8 flex flex-wrap gap-x-5 gap-y-3 text-sm text-gray-600 dark:text-dark-300">
            <span v-for="tag in featureTags" :key="tag" class="inline-flex items-center gap-2">
              <Icon name="checkCircle" size="sm" class="text-teal-700 dark:text-teal-300" />
              {{ tag }}
            </span>
          </div>
        </div>

        <section class="quickstart-panel flow-surface" :aria-labelledby="quickstartTitleId">
          <div class="flex items-start justify-between gap-4 border-b border-gray-900/10 px-4 py-4 dark:border-white/10 sm:px-5">
            <div>
              <div class="flex items-center gap-2">
                <Icon name="terminal" size="sm" class="text-teal-700 dark:text-teal-300" />
                <h2 :id="quickstartTitleId" class="text-sm font-bold text-gray-950 dark:text-white">
                  {{ t('home.quickstart.title') }}
                </h2>
              </div>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('home.quickstart.description') }}</p>
            </div>
            <span class="live-status">
              <span class="status-dot" aria-hidden="true"></span>
              {{ t('home.quickstart.live') }}
            </span>
          </div>

          <div class="overflow-x-auto border-b border-gray-900/10 px-2 py-2 dark:border-white/10" role="tablist" :aria-label="t('home.quickstart.protocols')">
            <div class="protocol-tabs">
              <button
                v-for="protocol in protocols"
                :key="protocol.id"
                type="button"
                role="tab"
                :aria-selected="selectedProtocolId === protocol.id"
                :class="['protocol-tab', { 'protocol-tab-active': selectedProtocolId === protocol.id }]"
                @click="selectedProtocolId = protocol.id"
              >
                {{ protocol.label }}
              </button>
            </div>
          </div>

          <div class="flex min-w-0 items-center gap-3 border-b border-gray-900/10 px-4 py-3 text-xs dark:border-white/10 sm:px-5">
            <span class="method-badge">POST</span>
            <code class="min-w-0 truncate font-mono text-gray-700 dark:text-dark-200">{{ selectedProtocol.endpoint }}</code>
            <span class="ml-auto hidden items-center gap-1 text-gray-400 sm:inline-flex">
              <span class="status-dot" aria-hidden="true"></span>
              200 OK
            </span>
          </div>

          <div class="code-section">
            <div class="mb-3 flex items-center justify-between gap-3">
              <span class="code-label">{{ t('home.quickstart.request') }}</span>
              <button type="button" class="copy-button" @click="copyCommand">
                <Icon :name="copyState === 'copied' ? 'check' : 'copy'" size="xs" />
                {{ copyButtonLabel }}
              </button>
            </div>
            <pre class="request-code"><code>{{ selectedProtocol.command }}</code></pre>
          </div>

          <div class="response-section border-t border-gray-900/10 dark:border-white/10">
            <span class="code-label">{{ t('home.quickstart.response') }}</span>
            <pre class="response-block"><code>{{ selectedProtocol.response }}</code></pre>
            <div class="response-metrics">
              <span>168 ms</span>
              <span>31 tokens</span>
              <span>$0.00093</span>
            </div>
          </div>
          <span class="sr-only" aria-live="polite">{{ copyAnnouncement }}</span>
        </section>
      </section>

      <!-- Stats band -->
      <section class="stats-band reveal" aria-label="Key metrics">
        <div class="mx-auto grid max-w-7xl grid-cols-2 gap-6 px-4 sm:px-6 lg:grid-cols-4 lg:px-8">
          <div v-for="s in statItems" :key="s.label" class="stat-cell">
            <div class="stat-value">{{ s.value }}</div>
            <div class="stat-label">{{ s.label }}</div>
          </div>
        </div>
      </section>

      <!-- Features -->
      <section class="section-block reveal" aria-labelledby="features-title">
        <div class="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
          <h2 id="features-title" class="section-title">{{ t('home.solutions.title') }}</h2>
          <p class="section-subtitle">{{ t('home.solutions.subtitle') }}</p>
          <div class="mt-10 grid gap-5 md:grid-cols-3">
            <div v-for="(f, i) in featureCards" :key="f.title" class="feature-card-v2" :style="{ transitionDelay: `${i * 80}ms` }">
              <div class="feature-icon"><Icon :name="(f.icon as 'key' | 'shield' | 'chart')" size="md" /></div>
              <h3 class="feature-title">{{ f.title }}</h3>
              <p class="feature-desc">{{ f.desc }}</p>
            </div>
          </div>
        </div>
      </section>

      <!-- Steps -->
      <section class="section-block reveal" aria-labelledby="steps-title">
        <div class="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
          <h2 id="steps-title" class="section-title">{{ t('home.steps.title') }}</h2>
          <div class="mt-10 grid gap-5 md:grid-cols-3">
            <div v-for="(s, i) in stepItems" :key="s.title" class="step-card">
              <div class="step-num">{{ i + 1 }}</div>
              <h3 class="feature-title">{{ s.title }}</h3>
              <p class="feature-desc">{{ s.desc }}</p>
            </div>
          </div>
        </div>
      </section>

      <!-- Comparison -->
      <section class="section-block reveal" aria-labelledby="comparison-title">
        <div class="mx-auto max-w-5xl px-4 sm:px-6 lg:px-8">
          <h2 id="comparison-title" class="section-title">{{ t('home.comparison.title') }}</h2>
          <div class="comparison-table">
            <div class="ct-row ct-head">
              <span>{{ t('home.comparison.headers.feature') }}</span>
              <span>{{ t('home.comparison.headers.official') }}</span>
              <span class="ct-us">{{ t('home.comparison.headers.us') }}</span>
            </div>
            <div v-for="row in comparisonRows" :key="row.feature" class="ct-row">
              <span class="ct-feature">{{ row.feature }}</span>
              <span class="ct-official">{{ row.official }}</span>
              <span class="ct-us">{{ row.us }}</span>
            </div>
          </div>
        </div>
      </section>

      <!-- Providers -->
      <section class="section-block reveal" aria-labelledby="providers-title">
        <div class="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
          <h2 id="providers-title" class="section-title">{{ t('home.providers.title') }}</h2>
          <p class="section-subtitle">{{ t('home.providers.description') }}</p>
          <div class="mt-8 flex flex-wrap justify-center gap-3">
            <span v-for="p in providerChips" :key="p" class="provider-chip">
              <Icon name="sparkles" size="sm" />
              {{ p }}
            </span>
          </div>
        </div>
      </section>

      <!-- CTA -->
      <section class="cta-band reveal" aria-labelledby="cta-title">
        <div class="mx-auto max-w-3xl px-4 py-16 text-center sm:px-6">
          <h2 id="cta-title" class="cta-title">{{ t('home.cta.title') }}</h2>
          <p class="cta-desc">{{ t('home.cta.description') }}</p>
          <router-link :to="isAuthenticated ? dashboardPath : '/register'" class="primary-action cta-button">
            {{ isAuthenticated ? t('home.goToDashboard') : t('home.cta.button') }}
            <Icon name="arrowRight" size="sm" />
          </router-link>
        </div>
      </section>

      <section class="service-band border-y border-gray-900/10 dark:border-white/10" aria-label="Service information">
        <div class="mx-auto flex max-w-7xl flex-col items-start gap-3 px-4 py-4 text-sm text-gray-700 dark:text-dark-200 sm:flex-row sm:items-center sm:gap-6 sm:px-6 lg:px-8">
          <span class="inline-flex items-center gap-2 font-semibold">
            <Icon name="users" size="sm" class="text-teal-700 dark:text-teal-300" />
            {{ t('home.qqGroup', { number: qqGroupNumber }) }}
          </span>
          <span class="inline-flex items-center gap-2">
            <Icon name="globe" size="sm" class="text-violet-700 dark:text-violet-300" />
            {{ t('home.mainlandServiceNotice') }}
          </span>
        </div>
      </section>
    </main>

    <footer class="relative z-10 px-4 py-7 sm:px-6 lg:px-8">
      <div class="mx-auto flex max-w-7xl flex-col gap-3 text-sm text-gray-500 dark:text-dark-400 sm:flex-row sm:items-center sm:justify-between">
        <p>&copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}</p>
        <a :href="githubUrl" target="_blank" rel="noopener noreferrer" class="footer-link">GitHub</a>
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import BrandLogo from '@/components/common/BrandLogo.vue'
import Icon from '@/components/icons/Icon.vue'
import { sanitizeUrl } from '@/utils/url'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import { readLocalStorage, writeLocalStorage } from '@/utils/safeStorage'

type ProtocolId = 'chat' | 'responses' | 'claude' | 'gemini'

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const docUrl = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || ''))
const homeContent = computed(() => (appStore.cachedPublicSettings?.home_content || '').trim())
const isHomeContentUrl = computed(() => /^https?:\/\//.test(homeContent.value.trim()))
const modelPlazaEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.modelPlaza))

const isDark = ref(document.documentElement.classList.contains('dark'))
const selectedProtocolId = ref<ProtocolId>('responses')
const copyState = ref<'idle' | 'copied' | 'error'>('idle')
let copyResetTimer: ReturnType<typeof setTimeout> | undefined

const githubUrl = 'https://github.com/Wei-Shaw/sub2api'
const qqGroupNumber = '964834219'
const quickstartTitleId = 'gateway-quickstart-title'

const isAuthenticated = computed(() => authStore.isAuthenticated)
const modelPlazaRequiresAuth = computed(
  () => appStore.cachedPublicSettings?.model_plaza_require_auth === true,
)
const showModelPlazaEntry = computed(
  () => modelPlazaEnabled.value && (isAuthenticated.value || !modelPlazaRequiresAuth.value),
)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => isAdmin.value ? '/admin/dashboard' : '/dashboard')
const currentYear = computed(() => new Date().getFullYear())
const featureTags = computed(() => [
  t('home.tags.subscriptionToApi'),
  t('home.tags.stickySession'),
  t('home.tags.realtimeBilling')
])

const statItems = computed(() => [
  { value: '200+', label: t('home.stats.models') },
  { value: '4', label: t('home.stats.protocols') },
  { value: '99.9%', label: t('home.stats.uptime') },
  { value: '0.0001', label: t('home.stats.billing') }
])

const featureCards = computed(() => [
  { icon: 'key', title: t('home.features.unifiedGateway'), desc: t('home.features.unifiedGatewayDesc') },
  { icon: 'shield', title: t('home.features.multiAccount'), desc: t('home.features.multiAccountDesc') },
  { icon: 'chart', title: t('home.features.balanceQuota'), desc: t('home.features.balanceQuotaDesc') }
])

const stepItems = computed(() => [
  { title: t('home.steps.s1.title'), desc: t('home.steps.s1.desc') },
  { title: t('home.steps.s2.title'), desc: t('home.steps.s2.desc') },
  { title: t('home.steps.s3.title'), desc: t('home.steps.s3.desc') }
])

const comparisonRows = computed(() => [
  { feature: t('home.comparison.items.pricing.feature'), official: t('home.comparison.items.pricing.official'), us: t('home.comparison.items.pricing.us') },
  { feature: t('home.comparison.items.models.feature'), official: t('home.comparison.items.models.official'), us: t('home.comparison.items.models.us') },
  { feature: t('home.comparison.items.management.feature'), official: t('home.comparison.items.management.official'), us: t('home.comparison.items.management.us') },
  { feature: t('home.comparison.items.stability.feature'), official: t('home.comparison.items.stability.official'), us: t('home.comparison.items.stability.us') },
  { feature: t('home.comparison.items.control.feature'), official: t('home.comparison.items.control.official'), us: t('home.comparison.items.control.us') }
])

const providerChips = computed(() => [
  'Claude', 'GPT', 'Gemini', 'Grok', 'GLM', 'Kimi', 'DeepSeek', t('home.providers.more')
])

let revealObserver: IntersectionObserver | null = null

function initReveal() {
  if (!('IntersectionObserver' in window)) {
    document.querySelectorAll('.reveal').forEach((el) => el.classList.add('revealed'))
    return
  }
  revealObserver = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (entry.isIntersecting) {
          entry.target.classList.add('revealed')
          revealObserver?.unobserve(entry.target)
        }
      }
    },
    { threshold: 0.12 }
  )
  document.querySelectorAll('.reveal').forEach((el) => revealObserver?.observe(el))
}

const gatewayOrigin = computed(() => {
  const configured = appStore.cachedPublicSettings?.api_base_url || appStore.apiBaseUrl || window.location.origin
  return configured.replace(/\/+$/, '').replace(/\/v1$/, '')
})

const protocols = computed(() => [
  {
    id: 'chat' as const,
    label: 'Chat',
    endpoint: '/v1/chat/completions',
    command: `curl -X POST "${gatewayOrigin.value}/v1/chat/completions" \\
  -H "Authorization: Bearer sk-••••" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"gpt-5","messages":[{"role":"user","content":"Hello"}]}'`,
    response: '{\n  "choices": [{ "message": { "role": "assistant", "content": "Ready." } }]\n}'
  },
  {
    id: 'responses' as const,
    label: 'Responses',
    endpoint: '/v1/responses',
    command: `curl -X POST "${gatewayOrigin.value}/v1/responses" \\
  -H "Authorization: Bearer sk-••••" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"gpt-5","input":"Hello"}'`,
    response: '{\n  "output": [{ "type": "message", "content": [{ "type": "output_text" }] }]\n}'
  },
  {
    id: 'claude' as const,
    label: 'Claude',
    endpoint: '/v1/messages',
    command: `curl -X POST "${gatewayOrigin.value}/v1/messages" \\
  -H "x-api-key: sk-••••" \\
  -H "anthropic-version: 2023-06-01" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"claude-sonnet","max_tokens":1024,"messages":[{"role":"user","content":"Hello"}]}'`,
    response: '{\n  "content": [{ "type": "text", "text": "Message routed." }]\n}'
  },
  {
    id: 'gemini' as const,
    label: 'Gemini',
    endpoint: '/v1beta/models/gemini-pro:generateContent',
    command: `curl -X POST "${gatewayOrigin.value}/v1beta/models/gemini-pro:generateContent" \\
  -H "x-goog-api-key: sk-••••" \\
  -H "Content-Type: application/json" \\
  -d '{"contents":[{"parts":[{"text":"Hello"}]}]}'`,
    response: '{\n  "candidates": [{ "content": { "parts": [{ "text": "Response ready." }] } }]\n}'
  }
])

const selectedProtocol = computed(() => protocols.value.find((protocol) => protocol.id === selectedProtocolId.value) || protocols.value[0])
const copyButtonLabel = computed(() => {
  if (copyState.value === 'copied') return t('home.quickstart.copied')
  if (copyState.value === 'error') return t('home.quickstart.copyFailed')
  return t('home.quickstart.copy')
})
const copyAnnouncement = computed(() => copyState.value === 'copied' ? t('home.quickstart.copySuccess') : '')

async function copyCommand() {
  try {
    await navigator.clipboard.writeText(selectedProtocol.value.command)
    copyState.value = 'copied'
  } catch {
    copyState.value = 'error'
  }
  if (copyResetTimer) clearTimeout(copyResetTimer)
  copyResetTimer = setTimeout(() => {
    copyState.value = 'idle'
  }, 1800)
}

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  writeLocalStorage('theme', isDark.value ? 'dark' : 'light')
  // Force paint so porcelain/dark tokens apply immediately
  document.documentElement.style.colorScheme = isDark.value ? 'dark' : 'light'
}

function initTheme() {
  const savedTheme = readLocalStorage('theme')
  let prefersDark = false
  try {
    prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
  } catch {
    prefersDark = false
  }
  if (savedTheme === 'dark' || (!savedTheme && prefersDark)) {
    isDark.value = true
    document.documentElement.classList.add('dark')
  }
}

onMounted(() => {
  initTheme()
  authStore.checkAuth()
  if (!appStore.publicSettingsLoaded) appStore.fetchPublicSettings()
  initReveal()
})

onBeforeUnmount(() => {
  if (copyResetTimer) clearTimeout(copyResetTimer)
  if (revealObserver) {
    revealObserver.disconnect()
    revealObserver = null
  }
})
</script>

<style scoped>
.gateway-shell {
  position: relative;
  isolation: isolate;
  overflow: hidden;
  color: var(--console-ink, #000000);
  letter-spacing: 0;
  background:
    radial-gradient(ellipse 80% 60% at 85% 10%, rgba(15, 118, 110, 0.14), transparent 50%),
    radial-gradient(ellipse 50% 40% at 10% 80%, rgba(124, 58, 237, 0.08), transparent 45%),
    radial-gradient(ellipse 40% 30% at 70% 90%, rgba(8, 145, 178, 0.08), transparent 40%),
    linear-gradient(180deg, #faf9f7 0%, #f3f1ec 100%);
  font-family: "Site PingFang UI", "PingFang SC", "Noto Sans SC", "Microsoft YaHei", sans-serif;
}

:global(html.dark .gateway-shell) {
  color: #f0f4f1;
  background:
    radial-gradient(ellipse 80% 60% at 85% 10%, rgba(94, 234, 212, 0.12), transparent 50%),
    radial-gradient(ellipse 50% 40% at 10% 80%, rgba(196, 181, 253, 0.08), transparent 45%),
    linear-gradient(180deg, #141816 0%, #1c221f 100%);
}

.gateway-grid {
  inset: 0;
  opacity: 1;
  background-image:
    linear-gradient(rgba(15, 118, 110, 0.05) 1px, transparent 1px),
    linear-gradient(90deg, rgba(15, 118, 110, 0.05) 1px, transparent 1px);
  background-size: 56px 56px;
  mask-image: radial-gradient(ellipse 90% 70% at 50% 30%, black 20%, transparent 75%);
}

:global(html.dark .gateway-grid) {
  background-image:
    linear-gradient(rgba(94, 234, 212, 0.04) 1px, transparent 1px),
    linear-gradient(90deg, rgba(94, 234, 212, 0.04) 1px, transparent 1px);
}

.glass-header {
  padding: 14px 14px 0;
}

.glass-nav {
  width: 100%;
  max-width: 80rem;
  border: 1px solid rgba(231, 227, 219, 0.9);
  border-radius: 1rem;
  background: rgba(255, 255, 255, 0.82);
  box-shadow: 0 1px 2px rgba(28, 25, 23, 0.04), 0 8px 24px rgba(28, 25, 23, 0.04);
  backdrop-filter: blur(16px) saturate(140%);
}

:global(html.dark .glass-nav) {
  border-color: rgba(44, 53, 48, 0.9);
  background: rgba(28, 34, 31, 0.86);
}

.brand-link {
  color: inherit;
}

.brand-logo-frame {
  border: 1px solid rgba(231, 227, 219, 0.95);
  border-radius: 0.75rem;
  background: #fff;
  box-shadow: 0 4px 14px rgba(15, 118, 110, 0.1);
}

.brand-name {
  letter-spacing: 0;
}

.hero-layout {
  min-height: clamp(590px, calc(100dvh - 138px), 690px);
  padding-top: clamp(52px, 7vw, 88px);
  padding-bottom: clamp(44px, 6vw, 72px);
}

.hero-copy {
  animation: none;
}

.hero-title {
  color: var(--ink-strong);
  letter-spacing: 0;
  text-shadow: none;
}

.hero-subtitle {
  color: var(--ink-strong);
}

.hero-description {
  color: var(--ink-muted);
}

:global(html.dark .hero-title) {
  color: var(--ink-strong);
  text-shadow: none;
}

:global(html.dark .hero-subtitle) {
  color: var(--ink-strong);
}

:global(html.dark .hero-description) {
  color: var(--ink-muted);
}

.brand-kicker {
  display: inline-flex;
  align-items: center;
  gap: 9px;
  border: 1px solid rgba(15, 118, 110, 0.22);
  border-radius: 999px;
  padding: 6px 12px;
  color: #0f766e;
  font-size: 12px;
  font-weight: 750;
  letter-spacing: 0.02em;
  background: linear-gradient(135deg, rgba(236, 253, 248, 0.95), rgba(240, 249, 255, 0.9));
  box-shadow: 0 1px 0 rgba(255, 255, 255, 0.8);
}

:global(html.dark .brand-kicker) {
  border-color: rgba(94, 234, 212, 0.28);
  color: #5eead4;
  background: linear-gradient(135deg, rgba(19, 78, 74, 0.7), rgba(12, 74, 110, 0.45));
}

.hero-actions,
.feature-strip {
  animation: none;
}

.icon-button {
  display: inline-flex;
  height: 36px;
  width: 36px;
  align-items: center;
  justify-content: center;
  border: 1px solid transparent;
  border-radius: 0.75rem;
  color: var(--ink-muted);
  transition: background-color 180ms ease, border-color 180ms ease, color 180ms ease, transform 180ms ease;
}

.icon-button:hover {
  border-color: rgba(15, 118, 110, 0.2);
  background: #ecfdf8;
  color: #0f766e;
  box-shadow: 0 4px 12px rgba(15, 118, 110, 0.1);
}

.icon-button:active,
.header-action:active,
.primary-action:active,
.secondary-action:active,
.copy-button:active {
  transform: scale(0.98);
}

.icon-button:focus-visible,
.header-action:focus-visible,
.primary-action:focus-visible,
.secondary-action:focus-visible,
.copy-button:focus-visible,
.protocol-tab:focus-visible,
.footer-link:focus-visible {
  outline: 2px solid #0f766e;
  outline-offset: 3px;
}

.header-action,
.primary-action,
.secondary-action {
  display: inline-flex;
  min-height: 40px;
  align-items: center;
  justify-content: center;
  gap: 8px;
  border-radius: 0.875rem;
  font-size: 14px;
  font-weight: 700;
  transition: background-color 180ms ease, border-color 180ms ease, color 180ms ease, box-shadow 180ms ease, transform 180ms ease;
}

.header-action {
  min-height: 36px;
  padding: 0 14px;
  border: 1px solid transparent;
  background: var(--ds-primary);
  color: var(--ds-on-primary);
  box-shadow: var(--ds-shadow-sm);
}

.primary-action {
  padding: 0 20px;
  min-height: 46px;
  border: 1px solid transparent;
  background: var(--ds-primary);
  color: var(--ds-on-primary);
  box-shadow: var(--ds-shadow-md);
}

.primary-action:hover,
.header-action:hover {
  background: var(--ds-primary-hover);
  box-shadow: var(--ds-shadow-md);
}

.secondary-action {
  border: 1px solid #e7e3db;
  padding: 0 18px;
  color: var(--console-ink, #000000);
  background: color-mix(in srgb, var(--console-surface, #fff) 90%, transparent);
  box-shadow: 0 1px 2px rgba(28, 25, 23, 0.04);
}

.secondary-action:hover {
  border-color: rgba(15, 118, 110, 0.35);
  color: #0f766e;
  background: #ecfdf8;
}

.status-dot {
  display: inline-block;
  width: 6px;
  height: 6px;
  flex: 0 0 auto;
  border-radius: 50%;
  background: #15803d;
  box-shadow: 0 0 0 4px rgba(21, 128, 61, 0.12);
  animation: pulse-dot 2.4s ease-in-out infinite;
}

.quickstart-panel {
  position: relative;
  min-width: 0;
  overflow: hidden;
  border: 1px solid #e7e3db;
  border-radius: 1.35rem;
  background: rgba(255, 255, 255, 0.92);
  box-shadow: 0 1px 2px rgba(28, 25, 23, 0.04), 0 18px 40px rgba(28, 25, 23, 0.06);
  backdrop-filter: blur(14px) saturate(130%);
  animation: panel-enter 700ms 120ms cubic-bezier(0.16, 1, 0.3, 1) both;
}

.quickstart-panel::before {
  content: "";
  position: absolute;
  inset: 0 0 auto 0;
  height: 3px;
  background: var(--ds-primary);
  opacity: 1;
}

.live-status {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 8px;
  border: 1px solid rgba(21, 128, 61, 0.22);
  border-radius: 999px;
  padding: 5px 10px;
  font-size: 11px;
  font-weight: 700;
  color: #15803d;
  background: #f0fdf4;
}

.protocol-tabs {
  display: grid;
  min-width: 340px;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 4px;
  border: 1px solid #e7e3db;
  border-radius: 0.875rem;
  padding: 3px;
  background: #f3f1ec;
}

.protocol-tab {
  height: 34px;
  border-radius: 0.65rem;
  padding: 0 8px;
  font-size: 12px;
  font-weight: 700;
  color: #6b6560;
  transition: color 160ms ease, background-color 160ms ease, box-shadow 160ms ease;
}

.protocol-tab:hover {
  color: #0f766e;
  background: #ecfdf8;
}

.protocol-tab-active {
  color: #115e59;
  background: #fff;
  box-shadow: 0 2px 8px rgba(15, 118, 110, 0.1);
}

.method-badge {
  border-radius: 0.5rem;
  padding: 3px 6px;
  font-family: ui-monospace, monospace;
  font-size: 10px;
  font-weight: 800;
  color: #c2410c;
  background: #fff7ed;
}

.code-section,
.response-section {
  padding: 16px 18px;
}

.code-label {
  font-size: 11px;
  font-weight: 800;
  text-transform: uppercase;
  color: var(--ink-muted);
  letter-spacing: 0;
}

.copy-button {
  display: inline-flex;
  min-height: 30px;
  align-items: center;
  gap: 6px;
  border: 1px solid var(--glass-border);
  border-radius: 0.65rem;
  padding: 0 9px;
  font-size: 11px;
  font-weight: 700;
  color: var(--ink-muted);
  background: var(--glass-surface-strong);
  box-shadow: var(--glass-highlight);
  transition: background-color 150ms ease, color 150ms ease, transform 150ms ease;
}

.copy-button:hover {
  color: var(--accent-operational);
  background: var(--surface-subtle);
}

.request-code,
.response-block {
  overflow-x: auto;
  white-space: pre;
  font-family: ui-monospace, "Cascadia Code", monospace;
  font-size: 12px;
  line-height: 1.7;
}

.request-code {
  min-height: 128px;
  border: 1px solid rgba(94, 234, 212, 0.14);
  border-radius: 1rem;
  padding: 14px;
  color: #ccfbf1;
  background: linear-gradient(145deg, #134e4a, #0c1f1d 72%);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.06), 0 12px 26px rgba(4, 47, 46, 0.22);
}

.response-block {
  min-height: 64px;
  margin-top: 10px;
  color: #0e7490;
}

.response-metrics {
  display: flex;
  gap: 14px;
  margin-top: 10px;
  font-family: ui-monospace, monospace;
  font-size: 10px;
  color: #6b6560;
}

.service-band {
  border-color: #e7e3db;
  background: rgba(255, 255, 255, 0.72);
}

.footer-link {
  transition: color 160ms ease;
}

.footer-link:hover {
  color: #0f766e;
}

:global(html.dark .brand-logo-frame) {
  border-color: #2c3530;
  background: #1c221f;
}

:global(html.dark .icon-button) { color: #a3b0a8; }
:global(html.dark .icon-button:hover) { border-color: #2c3530; background: #134e4a; color: #5eead4; }
:global(html.dark .header-action),
:global(html.dark .primary-action) {
  border-color: transparent;
  background: var(--ds-primary);
  color: var(--ds-on-primary);
  box-shadow: var(--ds-shadow-md);
}
:global(html.dark .header-action:hover),
:global(html.dark .primary-action:hover) {
  background: var(--ds-primary-hover);
}
:global(html.dark .secondary-action) {
  border-color: #2c3530;
  color: #f0f4f1;
  background: #1c221f;
}
:global(html.dark .secondary-action:hover) {
  border-color: rgba(94, 234, 212, 0.35);
  color: #5eead4;
  background: #134e4a;
}
:global(html.dark .quickstart-panel) {
  border-color: #2c3530;
  background: rgba(28, 34, 31, 0.92);
  box-shadow: 0 18px 40px rgba(0, 0, 0, 0.28);
}
:global(html.dark .live-status) {
  border-color: rgba(134, 239, 172, 0.28);
  color: #86efac;
  background: rgba(20, 83, 45, 0.55);
}
:global(html.dark .protocol-tabs) {
  border-color: #2c3530;
  background: #171d1a;
}
:global(html.dark .protocol-tab) { color: #a3b0a8; }
:global(html.dark .protocol-tab:hover) { color: #5eead4; background: #134e4a; }
:global(html.dark .protocol-tab-active) {
  color: #99f6e4;
  background: #1c221f;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.25);
}
:global(html.dark .copy-button) {
  border-color: #2c3530;
  color: #a3b0a8;
  background: #1c221f;
}
:global(html.dark .copy-button:hover) {
  color: #5eead4;
  background: #134e4a;
}
:global(html.dark .request-code) {
  color: #ccfbf1;
  background: linear-gradient(145deg, #134e4a, #0c1f1d 72%);
}
:global(html.dark .response-block) { color: #67e8f9; }
:global(html.dark .service-band) {
  border-color: #2c3530;
  background: rgba(28, 34, 31, 0.78);
}
:global(html.dark .footer-link:hover) { color: #5eead4; }

@keyframes content-enter {
  from {
    opacity: 0;
    transform: translate3d(0, 18px, 0);
  }
  to {
    opacity: 1;
    transform: translate3d(0, 0, 0);
  }
}

@keyframes panel-enter {
  from {
    opacity: 0;
    transform: translate3d(0, 24px, 0) scale(0.985);
  }
  to {
    opacity: 1;
    transform: translate3d(0, 0, 0) scale(1);
  }
}

@keyframes pulse-dot {
  0%, 100% { box-shadow: 0 0 0 4px rgba(21, 128, 61, 0.12); }
  50% { box-shadow: 0 0 0 7px rgba(21, 128, 61, 0.06); }
}

@media (max-width: 639px) {
  .gateway-grid {
    background-size: 40px 40px;
  }

  .glass-header {
    padding-top: 0;
  }

  .glass-nav {
    width: 100%;
    padding-right: 10px;
    padding-left: 10px;
  }

  .hero-layout {
    min-height: auto;
    padding-top: 42px;
    padding-bottom: 48px;
  }

  .header-action {
    width: 36px;
    padding: 0;
  }

  .header-action span {
    display: none;
  }

  .primary-action,
  .secondary-action {
    width: 100%;
  }

  .code-section,
  .response-section {
    padding: 14px;
  }

  .request-code {
    min-height: 148px;
    font-size: 11px;
  }

  .glass-nav,
  .quickstart-panel {
    backdrop-filter: none;
  }
}

@media (prefers-reduced-motion: reduce) {
  .gateway-grid {
    animation: none;
  }

  *,
  *::before,
  *::after {
    scroll-behavior: auto !important;
    transition-duration: 0.01ms !important;
    animation-duration: 0.01ms !important;
  }
}

/* ============ Landing sections v2 ============ */
.reveal {
  opacity: 0;
  transform: translateY(24px);
  transition: opacity 0.6s ease, transform 0.6s cubic-bezier(0.2, 0.7, 0.3, 1);
}
.reveal.revealed {
  opacity: 1;
  transform: translateY(0);
}

.section-block {
  padding: 56px 0;
}
.section-title {
  text-align: center;
  font-size: 1.75rem;
  font-weight: 800;
  letter-spacing: -0.02em;
  color: var(--ds-ink);
}
.section-subtitle {
  margin-top: 8px;
  text-align: center;
  font-size: 0.95rem;
  color: var(--ds-ink-muted);
}

.stats-band {
  padding: 36px 0;
  border-top: 1px solid var(--ds-border);
  border-bottom: 1px solid var(--ds-border);
  background: var(--ds-surface);
}
.stat-cell {
  text-align: center;
}
.stat-value {
  font-size: 2rem;
  font-weight: 800;
  letter-spacing: -0.03em;
  font-variant-numeric: tabular-nums;
  color: var(--ds-primary);
}
.stat-label {
  margin-top: 4px;
  font-size: 0.8rem;
  color: var(--ds-ink-muted);
}

.feature-card-v2 {
  border: 1px solid var(--ds-border);
  border-radius: var(--ds-radius-lg);
  background: var(--ds-surface);
  padding: 24px;
  box-shadow: var(--ds-shadow-xs);
  transition: box-shadow 0.2s ease, border-color 0.2s ease, transform 0.2s ease;
}
.feature-card-v2:hover {
  border-color: var(--ds-border-strong);
  box-shadow: var(--ds-shadow-md);
  transform: translateY(-2px);
}
.feature-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 44px;
  height: 44px;
  border-radius: 12px;
  background: var(--ds-primary-soft);
  color: var(--ds-primary);
}
.feature-title {
  margin-top: 14px;
  font-size: 1.05rem;
  font-weight: 700;
  color: var(--ds-ink);
}
.feature-desc {
  margin-top: 6px;
  font-size: 0.875rem;
  line-height: 1.6;
  color: var(--ds-ink-muted);
}

.step-card {
  position: relative;
  border: 1px solid var(--ds-border);
  border-radius: var(--ds-radius-lg);
  background: var(--ds-surface);
  padding: 24px;
  box-shadow: var(--ds-shadow-xs);
}
.step-num {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  border-radius: 999px;
  background: var(--ds-primary);
  color: var(--ds-on-primary);
  font-weight: 800;
  font-variant-numeric: tabular-nums;
}

.comparison-table {
  border: 1px solid var(--ds-border);
  border-radius: var(--ds-radius-lg);
  overflow: hidden;
  background: var(--ds-surface);
  box-shadow: var(--ds-shadow-xs);
}
.ct-row {
  display: grid;
  grid-template-columns: 1.1fr 1.4fr 1.4fr;
  gap: 12px;
  padding: 14px 18px;
  font-size: 0.875rem;
  border-top: 1px solid var(--ds-divider);
}
.ct-row.ct-head {
  border-top: 0;
  background: var(--ds-surface-muted);
  font-weight: 700;
  color: var(--ds-ink);
}
.ct-feature {
  font-weight: 600;
  color: var(--ds-ink);
}
.ct-official {
  color: var(--ds-ink-muted);
}
.ct-us {
  color: var(--ds-primary);
  font-weight: 600;
}

.provider-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border: 1px solid var(--ds-border);
  border-radius: 999px;
  background: var(--ds-surface);
  padding: 8px 16px;
  font-size: 0.875rem;
  font-weight: 600;
  color: var(--ds-ink);
  box-shadow: var(--ds-shadow-xs);
  transition: border-color 0.2s ease, box-shadow 0.2s ease, transform 0.2s ease;
}
.provider-chip:hover {
  border-color: var(--ds-primary);
  color: var(--ds-primary);
  transform: translateY(-2px);
  box-shadow: var(--ds-shadow-sm);
}

.cta-band {
  background: var(--ds-primary-soft);
  border-top: 1px solid var(--ds-border);
}
.cta-title {
  font-size: 1.9rem;
  font-weight: 800;
  letter-spacing: -0.02em;
  color: var(--ds-ink);
}
.cta-desc {
  margin-top: 10px;
  font-size: 0.95rem;
  color: var(--ds-ink-muted);
}
.cta-button {
  display: inline-flex;
  margin-top: 26px;
}

@media (max-width: 767px) {
  .ct-row {
    grid-template-columns: 1fr;
    gap: 4px;
  }
  .section-block {
    padding: 40px 0;
  }
}

</style>
