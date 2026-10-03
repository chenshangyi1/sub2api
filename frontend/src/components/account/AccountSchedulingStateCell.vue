<template>
  <div
    class="flex min-w-[9rem] max-w-[16rem] flex-col gap-1 text-xs"
    :title="t('admin.accounts.schedulingState.hint')"
    aria-live="polite"
  >
    <span :class="['inline-flex w-fit rounded-full px-2 py-0.5 font-medium', colors[state.state]]">
      {{ t(`admin.accounts.schedulingState.${state.state}`) }}
    </span>
    <span v-if="state.until" class="tabular-nums text-gray-500 dark:text-gray-400">
      {{ t('admin.accounts.schedulingState.until', { time: formatDateTime(new Date(state.until)) }) }}
    </span>
    <span v-if="reasons" class="break-words text-gray-500 dark:text-gray-400">{{ reasons }}</span>
    <span v-if="state.detail" class="break-words text-gray-500 dark:text-gray-400" :title="state.detail">
      {{ state.detail }}
    </span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useNow } from '@vueuse/core'
import type { Account } from '@/types'
import { accountSchedulingState } from '@/utils/accountSchedulingState'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{ account: Account }>()
const { t } = useI18n()
const now = useNow({ interval: 1000 })
const state = computed(() => accountSchedulingState(props.account, now.value.getTime()))
const reasons = computed(() => state.value.reasons.map(reason => t(`admin.accounts.schedulingState.${reason}`)).join(' / '))
const colors = {
  paused: 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300',
  inactive: 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300',
  cooldown: 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300',
  eligible: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300',
  unknown: 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300',
}
</script>
