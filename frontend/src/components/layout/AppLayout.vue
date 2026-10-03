<template>
  <div
    class="relative min-h-screen"
    :class="isConsoleSignal ? 'console-signal' : 'bg-transparent'"
  >
    <!-- Background Decoration -->
    <div v-if="!isConsoleSignal" class="pointer-events-none fixed inset-0 z-0 bg-mesh-gradient opacity-15"></div>

    <!-- Global Character Display (全屏角色展示，铺到侧栏下方) -->
    <GlobalCharacterDisplay />

    <!-- Sidebar -->
    <AppSidebar />

    <!-- Main Content Area：左侧不透明阅读区，人物只从右侧透出 -->
    <div
      class="app-main-surface relative min-h-screen transition-all duration-300"
      :class="[sidebarCollapsed ? 'lg:ml-[72px]' : 'lg:ml-64', { 'signal-frame': isConsoleSignal }]"
    >
      <!-- Header -->
      <AppHeader />

      <!-- Main Content -->
      <main :class="[flushContent ? 'p-0' : 'p-4 md:p-6 lg:p-8', { 'signal-main': isConsoleSignal }]">
        <slot />
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import '@/styles/onboarding.css'
import '@/styles/console-signal.css'
import { computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { useOnboardingTour } from '@/composables/useOnboardingTour'
import { useOnboardingStore } from '@/stores/onboarding'
import { useConsoleSignal } from '@/composables/useConsoleSignal'
import AppSidebar from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'
import GlobalCharacterDisplay from '@/components/common/GlobalCharacterDisplay.vue'

const appStore = useAppStore()
const authStore = useAuthStore()
const route = useRoute()
const { isConsoleSignal } = useConsoleSignal()
const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
const isAdmin = computed(() => authStore.user?.role === 'admin')
const flushContent = computed(() => Boolean(route.meta.flushContent))

const { replayTour } = useOnboardingTour({
  storageKey: isAdmin.value ? 'admin_guide' : 'user_guide',
  autoStart: true
})

const onboardingStore = useOnboardingStore()

onMounted(() => {
  onboardingStore.setReplayCallback(replayTour)
})

defineExpose({ replayTour })
</script>

<style scoped>
.app-main-surface {
  background:
    linear-gradient(
      90deg,
      color-mix(in srgb, var(--ds-bg, #f8fafc) 92%, transparent) 0%,
      color-mix(in srgb, var(--ds-bg, #f8fafc) 68%, transparent) 58%,
      color-mix(in srgb, var(--ds-bg, #f8fafc) 28%, transparent) 100%
    );
}
</style>
