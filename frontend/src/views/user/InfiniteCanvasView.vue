<template>
  <AppLayout>
    <div class="infinite-canvas-page relative flex h-[calc(100dvh-4rem)] flex-col overflow-hidden">
      <iframe
        data-testid="infinite-canvas-frame"
        class="h-full min-h-0 w-full flex-1 border-0 bg-white dark:bg-dark-900"
        :src="frameSrc"
        allow="clipboard-read; clipboard-write; fullscreen"
        allowfullscreen
        :title="t('infiniteCanvas.title')"
      ></iframe>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { detectTheme } from '@/utils/embedded-url'
import { buildInfiniteCanvasFrameSrc } from '@/utils/infiniteCanvas'

const { t, locale } = useI18n()

function canvasLang(value: string): string {
  return value.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en'
}

const frameSrc = computed(() => buildInfiniteCanvasFrameSrc({
  origin: window.location.origin,
  theme: detectTheme(),
  lang: canvasLang(String(locale.value || '')),
}))
</script>
