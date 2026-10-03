<template>
  <div class="space-y-6">
    <!-- 英雄区：角色展示 + 欢迎信息 (仅独立页面显示) -->
    <PlazaHeroSection v-if="!embedded" />

    <!-- 页头(独立形态下展示标题;后台形态 AppHeader 已有页面标题) -->
    <div v-if="!embedded" class="space-y-2">
      <h1 class="bg-gradient-to-r from-gray-900 to-gray-700 bg-clip-text text-3xl font-bold tracking-tight text-transparent dark:from-white dark:to-gray-300 sm:text-4xl">{{ t('modelPlaza.title') }}</h1>
      <p class="text-sm leading-relaxed text-gray-600 dark:text-dark-300">{{ t('modelPlaza.description') }}</p>
    </div>

    <!-- 全局价格说明(管理员配置,Markdown) -->
    <div
      v-if="descriptionHtml"
      class="plaza-description rounded-2xl border border-primary-100/60 bg-gradient-to-br from-primary-50/50 to-white px-6 py-5 text-sm shadow-sm dark:border-primary-900/30 dark:from-primary-950/20 dark:to-dark-800/50"
      v-html="descriptionHtml"
    ></div>

    <!-- 未登录提示 -->
    <div
      v-if="!isAuthenticated"
      class="flex items-center gap-2 rounded-xl border border-accent-200/60 bg-accent-50/50 px-4 py-3 text-xs text-accent-700 dark:border-accent-800/40 dark:bg-accent-900/20 dark:text-accent-300"
    >
      <Icon name="infoCircle" size="xs" class="h-4 w-4 flex-shrink-0" />
      <span>{{ t('modelPlaza.anonymousHint') }}</span>
    </div>

    <!-- 加载/错误/空 -->
    <div v-if="loading" class="flex min-h-[280px] items-center justify-center">
      <div class="relative">
        <div class="h-10 w-10 animate-spin rounded-full border-3 border-primary-200/40 border-t-primary-600 dark:border-primary-900/40 dark:border-t-primary-400"></div>
        <div class="absolute inset-0 h-10 w-10 animate-pulse rounded-full bg-primary-500/10"></div>
      </div>
    </div>
    <div
      v-else-if="error"
      class="rounded-2xl border border-red-200/80 bg-gradient-to-br from-red-50 to-red-100/50 px-6 py-10 text-center text-sm font-medium text-red-700 shadow-sm dark:border-red-500/30 dark:from-red-950/30 dark:to-red-900/20 dark:text-red-300"
    >
      {{ t('modelPlaza.loadFailed') }}
    </div>
    <template v-else>
      <!-- 筛选区:平台 → 分组 → 倍率 -->
      <PlazaFilterBar
        :platforms="platforms"
        :groups="groupOptions"
        :rates="rates"
        :platform="selectedPlatform"
        :group-id="selectedGroupId"
        :rate="selectedRate"
        :search="searchQuery"
        @update:platform="selectedPlatform = $event"
        @update:group-id="selectedGroupId = $event"
        @update:rate="selectedRate = $event"
        @update:search="searchQuery = $event"
      />

      <!-- 分组分节的模型清单(默认按生效倍率升序) -->
      <div v-if="filteredGroups.length > 0" class="space-y-6">
        <PlazaGroupSection v-for="g in filteredGroups" :key="g.id" :group="g" />
      </div>
      <div
        v-else
        class="rounded-2xl border border-dashed border-gray-300 bg-gray-50/50 px-6 py-16 text-center text-sm text-gray-500 dark:border-dark-600 dark:bg-dark-900/30 dark:text-dark-400"
      >
        {{ searchActive ? t('modelPlaza.noSearchResult') : t('modelPlaza.empty') }}
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import Icon from '@/components/icons/Icon.vue'
import PlazaFilterBar from './PlazaFilterBar.vue'
import PlazaGroupSection from './PlazaGroupSection.vue'
import PlazaHeroSection from './PlazaHeroSection.vue'
import type { ModelPlazaGroup, ModelPlazaResponse } from '@/api/modelPlaza'
import { useAuthStore } from '@/stores/auth'

const props = defineProps<{
  response: ModelPlazaResponse | null
  loading: boolean
  error?: boolean
  /** 后台内嵌形态(AppLayout 内):隐藏页头。 */
  embedded?: boolean
}>()

const { t } = useI18n()
const authStore = useAuthStore()
const isAuthenticated = computed(() => authStore.isAuthenticated)

const selectedPlatform = ref<string>('all')
const selectedGroupId = ref<number | 'all'>('all')
const selectedRate = ref<number | 'all'>('all')
const searchQuery = ref('')

const searchActive = computed(() => searchQuery.value.trim() !== '')

const descriptionHtml = computed(() => {
  const md = props.response?.description?.trim()
  if (!md) return ''
  return DOMPurify.sanitize(marked.parse(md) as string)
})

/** 生效倍率 = 用户专属倍率 ?? 分组默认倍率。 */
function effectiveRate(g: ModelPlazaGroup): number {
  return g.user_rate_multiplier ?? g.rate_multiplier
}

const platforms = computed(() =>
  [...new Set((props.response?.groups ?? []).map((g) => g.platform).filter(Boolean))].sort()
)

const groupOptions = computed(() =>
  (props.response?.groups ?? []).map((g) => ({
    id: g.id,
    name: g.name,
    platform: g.platform,
    rate: effectiveRate(g)
  }))
)

/** 全量生效倍率;当前组合下不可用的项由 FilterBar 置灰而非隐藏。 */
const rates = computed(() =>
  [...new Set((props.response?.groups ?? []).map(effectiveRate))].sort((a, b) => a - b)
)

/** 数据刷新后选中的倍率可能不复存在,重置为全部。 */
watch(rates, (list) => {
  if (selectedRate.value !== 'all' && !list.includes(selectedRate.value)) {
    selectedRate.value = 'all'
  }
})

const filteredGroups = computed(() => {
  let groups = props.response?.groups ?? []
  if (selectedPlatform.value !== 'all') {
    groups = groups.filter((g) => g.platform === selectedPlatform.value)
  }
  if (selectedGroupId.value !== 'all') {
    groups = groups.filter((g) => g.id === selectedGroupId.value)
  }
  if (selectedRate.value !== 'all') {
    groups = groups.filter((g) => effectiveRate(g) === selectedRate.value)
  }
  // 模型名搜索:分组内只留命中的模型,整组无命中则隐藏该分组。
  const q = searchQuery.value.trim().toLowerCase()
  if (q) {
    groups = groups
      .map((g) => ({ ...g, models: g.models.filter((m) => m.name.toLowerCase().includes(q)) }))
      .filter((g) => g.models.length > 0)
  }
  // 过滤掉没有模型的空分组
  groups = groups.filter((g) => g.models && g.models.length > 0)
  // 专属倍率会改变生效值,不能只依赖后端按默认倍率的排序。
  return [...groups].sort(
    (a, b) => effectiveRate(a) - effectiveRate(b) || a.name.localeCompare(b.name)
  )
})
</script>

<style scoped>
.plaza-description {
  line-height: 1.7;
  overflow-wrap: anywhere;
}

.plaza-description :deep(h1),
.plaza-description :deep(h2),
.plaza-description :deep(h3) {
  @apply mb-2 mt-3 font-semibold text-gray-900 first:mt-0 dark:text-white;
}

.plaza-description :deep(p) {
  @apply mb-2 text-gray-700 last:mb-0 dark:text-dark-200;
}

.plaza-description :deep(a) {
  @apply text-primary-600 underline underline-offset-4 hover:text-primary-700 dark:text-primary-300;
}

.plaza-description :deep(ul) {
  @apply mb-2 list-disc pl-5;
}

.plaza-description :deep(ol) {
  @apply mb-2 list-decimal pl-5;
}

.plaza-description :deep(li) {
  @apply mb-0.5 text-gray-700 dark:text-dark-200;
}

.plaza-description :deep(code) {
  @apply rounded bg-gray-100 px-1.5 py-0.5 font-mono text-xs dark:bg-dark-800;
}

.plaza-description :deep(blockquote) {
  @apply my-2 border-l-4 border-gray-300 pl-3 text-gray-600 dark:border-dark-600 dark:text-dark-300;
}
</style>
