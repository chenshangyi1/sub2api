<template>
  <AppLayout>
    <div class="space-y-4">
      <div class="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
            {{ t('admin.groupMonitor.title') }}
          </h2>
          <p class="mt-0.5 text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.groupMonitor.description') }}
          </p>
        </div>
        <div class="flex items-center gap-2">
          <select v-model="healthFilter" class="rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-900">
            <option value="all">{{ t('admin.groupMonitor.filterAll') }}</option>
            <option value="healthy">{{ t('admin.groupMonitor.filterHealthy') }}</option>
            <option value="degraded">{{ t('admin.groupMonitor.filterDegraded') }}</option>
            <option value="unknown">{{ t('admin.groupMonitor.filterUnknown') }}</option>
          </select>
          <button
          type="button"
          class="inline-flex items-center gap-1.5 rounded-lg bg-blue-600 px-3 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
          @click="openCreate"
        >
          {{ t('admin.groupMonitor.createButton') }}
        </button>
        </div>
      </div>

      <section class="grid grid-cols-2 gap-3 sm:grid-cols-5">
        <div v-for="item in summaryItems" :key="item.key" class="rounded-2xl border border-gray-200/80 bg-white/75 p-3 shadow-sm dark:border-gray-700 dark:bg-gray-900/70">
          <div class="text-xs text-gray-400">{{ item.label }}</div>
          <div class="mt-1 text-xl font-bold tabular-nums text-gray-900 dark:text-white">{{ item.value }}</div>
        </div>
      </section>

      <div v-if="loading && monitors.length === 0" class="py-12 text-center text-sm text-gray-400">
        {{ t('admin.groupMonitor.loadError') }}
      </div>

      <div v-else-if="monitors.length === 0" class="rounded-lg border border-dashed border-gray-300 py-12 text-center dark:border-gray-700">
        <p class="text-sm font-medium text-gray-700 dark:text-gray-200">
          {{ t('admin.groupMonitor.noMonitors') }}
        </p>
        <p class="mt-1 text-sm text-gray-400">{{ t('admin.groupMonitor.noMonitorsDesc') }}</p>
      </div>

      <div v-else class="grid grid-cols-1 gap-3 lg:grid-cols-2 xl:grid-cols-3">
        <div
          v-for="m in filteredMonitors"
          :key="m.id"
          class="rounded-lg border border-gray-200 bg-white shadow-sm dark:border-gray-700 dark:bg-gray-900"
        >
          <div class="flex items-start justify-between gap-2 border-b border-gray-100 px-4 py-3 dark:border-gray-800">
            <div class="min-w-0">
              <div class="flex items-center gap-2">
                <span class="truncate font-medium text-gray-900 dark:text-white">{{ m.group_name }}</span>
                <span
                  class="inline-flex items-center rounded-full px-2 py-0.5 text-[11px] font-medium"
                  :class="m.enabled ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300' : 'bg-gray-100 text-gray-500 dark:bg-gray-800 dark:text-gray-400'"
                >
                  {{ m.enabled ? 'ON' : 'OFF' }}
                </span>
              </div>
              <p v-if="m.model_id" class="mt-0.5 text-xs text-gray-400">{{ m.model_id }}</p>
            </div>
            <div class="flex shrink-0 items-center gap-1">
              <button
                type="button"
                class="rounded-md px-2 py-1 text-xs font-medium text-blue-600 hover:bg-blue-50 disabled:opacity-50 dark:text-blue-400 dark:hover:bg-blue-900/30"
                :disabled="runningId === m.id"
                @click="runNow(m)"
              >
                {{ runningId === m.id ? t('admin.groupMonitor.running') : t('admin.groupMonitor.runNow') }}
              </button>
              <button
                type="button"
                class="rounded-md px-2 py-1 text-xs text-gray-500 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-gray-800"
                @click="openEdit(m)"
              >
                {{ t('common.edit') }}
              </button>
              <button
                type="button"
                class="rounded-md px-2 py-1 text-xs text-red-500 hover:bg-red-50 dark:hover:bg-red-900/30"
                @click="askDelete(m)"
              >
                {{ t('admin.groupMonitor.delete') }}
              </button>
            </div>
          </div>

          <button type="button" class="w-full px-4 py-3 text-left" @click="toggleExpand(m)">
            <div class="grid grid-cols-3 gap-2">
              <div>
                <div class="text-xs text-gray-400">{{ t('admin.groupMonitor.accounts') }}</div>
                <div class="mt-0.5 text-lg font-semibold tabular-nums text-gray-900 dark:text-white">{{ m.account_count }}</div>
              </div>
              <div>
                <div class="text-xs text-emerald-600 dark:text-emerald-400">{{ t('admin.groupMonitor.healthy') }}</div>
                <div class="mt-0.5 text-lg font-semibold tabular-nums text-emerald-600 dark:text-emerald-400">{{ m.healthy_count }}</div>
              </div>
              <div>
                <div class="text-xs text-red-500">{{ t('admin.groupMonitor.failed') }}</div>
                <div class="mt-0.5 text-lg font-semibold tabular-nums text-red-500">{{ m.failed_count }}</div>
              </div>
            </div>

            <div class="mt-3 flex h-2 w-full overflow-hidden rounded-full bg-gray-100 dark:bg-gray-800">
              <div class="bg-emerald-500" :style="{ width: healthPct(m) + '%' }"></div>
              <div class="bg-red-400" :style="{ width: failedPct(m) + '%' }"></div>
              <div class="bg-amber-300" :style="{ width: unknownPct(m) + '%' }"></div>
            </div>

            <div class="mt-3 space-y-1 text-xs text-gray-400">
              <div>
                {{ t('admin.groupMonitor.lastCheck') }}：
                {{ m.last_run_at ? formatTime(m.last_run_at) : '—' }}
              </div>
              <div>
                {{ t('admin.groupMonitor.nextCheck') }}：{{ formatTime(m.next_run_at) }}
              </div>
              <div class="text-blue-500">{{ t('admin.groupMonitor.expandHint') }}</div>
            </div>
          </button>

          <div v-if="expandedId === m.id" class="border-t border-gray-100 dark:border-gray-800">
            <div v-if="resultsMap[m.id]?.length" class="divide-y divide-gray-100 dark:divide-gray-800">
              <div
                v-for="acc in resultsMap[m.id]"
                :key="acc.account_id"
                class="flex items-center justify-between gap-2 px-4 py-2"
              >
                <div class="flex min-w-0 items-center gap-2">
                  <span
                    class="h-2 w-2 shrink-0 rounded-full"
                    :class="statusDotClass(acc.status)"
                  />
                  <span class="truncate text-sm text-gray-800 dark:text-gray-200">{{ acc.account_name }}</span>
                  <span class="text-xs text-gray-400">{{ acc.platform }}</span>
                </div>
                <div class="flex shrink-0 items-center gap-2 text-xs">
                  <span :class="statusTextClass(acc.status)">
                    {{ t(`admin.groupMonitor.status.${acc.status}`) }}
                  </span>
                  <span class="tabular-nums text-gray-400">{{ acc.latency_ms }}ms</span>
                </div>
              </div>
            </div>
            <div v-else class="px-4 py-3 text-center text-xs text-gray-400">
              {{ t('admin.groupMonitor.runNow') }}
            </div>
          </div>
        </div>
      </div>
    </div>

    <div v-if="showDialog" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div class="w-full max-w-md rounded-lg bg-white p-5 shadow-xl dark:bg-gray-900">
        <h3 class="text-base font-semibold text-gray-900 dark:text-white">
          {{ editing ? t('admin.groupMonitor.editTitle') : t('admin.groupMonitor.createTitle') }}
        </h3>
        <form class="mt-4 space-y-4" @submit.prevent="save">
          <div v-if="!editing">
            <div class="flex items-center justify-between">
              <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('admin.groupMonitor.form.group') }}
              </label>
              <button
                type="button"
                class="text-xs text-blue-600 hover:underline dark:text-blue-400"
                @click="toggleSelectAllGroups"
              >
                {{ allGroupsSelected ? t('admin.groupMonitor.form.unselectAll') : t('admin.groupMonitor.form.selectAll') }}
              </button>
            </div>
            <div class="mt-2 max-h-60 space-y-1 overflow-y-auto rounded-md border border-gray-300 p-2 dark:border-gray-700">
              <label
                v-for="g in groups"
                :key="g.id"
                class="flex cursor-pointer items-center gap-2 rounded px-2 py-1 text-sm hover:bg-gray-50 dark:hover:bg-gray-800"
              >
                <input
                  v-model="form.group_ids"
                  type="checkbox"
                  :value="g.id"
                  class="rounded border-gray-300"
                />
                <span class="text-gray-800 dark:text-gray-200">{{ g.name }}</span>
              </label>
            </div>
            <p class="mt-1 text-xs text-gray-400">
              {{ t('admin.groupMonitor.form.groupHint', { count: form.group_ids.length }) }}
            </p>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.groupMonitor.presetTitle') }}</label>
            <div class="mt-2 grid grid-cols-3 gap-2">
              <button v-for="option in presetOptions" :key="option.value" type="button" class="rounded-xl border px-2 py-2 text-xs font-medium transition" :class="selectedPreset === option.value ? 'border-blue-500 bg-blue-50 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300' : 'border-gray-200 text-gray-600 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800'" @click="selectPreset(option.value)">
                <span class="block">{{ option.label }}</span>
                <span class="mt-0.5 block text-[10px] opacity-70">{{ option.hint }}</span>
              </button>
            </div>
            <p class="mt-2 text-xs text-gray-400">{{ t('admin.groupMonitor.presetHint') }}</p>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.groupMonitor.form.intervalMinutes') }}
            </label>
            <input
              v-model.number="form.interval_minutes"
              type="number"
              min="5"
              max="1440"
              class="mt-1 w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-800"
            />
            <p class="mt-1 text-xs text-gray-400">{{ t('admin.groupMonitor.form.intervalHint') }}</p>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.groupMonitor.form.modelId') }}
            </label>
            <input
              v-model="form.model_id"
              type="text"
              class="mt-1 w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-800"
            />
            <p class="mt-1 text-xs text-gray-400">{{ t('admin.groupMonitor.form.modelHint') }}</p>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.groupMonitor.form.maxOutputTokens') }}
            </label>
            <input
              v-model.number="form.max_output_tokens"
              type="number"
              min="1"
              max="256"
              class="mt-1 w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-800"
            />
          </div>

          <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
            <input v-model="form.auto_recover" type="checkbox" class="rounded" />
            {{ t('admin.groupMonitor.form.autoRecover') }}
          </label>

          <div class="flex justify-end gap-2 pt-2">
            <button
              type="button"
              class="rounded-md px-3 py-2 text-sm text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800"
              @click="closeDialog"
            >
              {{ t('common.cancel') }}
            </button>
            <button
              type="submit"
              class="rounded-md bg-blue-600 px-3 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
              :disabled="saving"
            >
              {{ t('common.save') }}
            </button>
          </div>
        </form>
      </div>
    </div>

    <div v-if="deleting" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div class="w-full max-w-sm rounded-lg bg-white p-5 shadow-xl dark:bg-gray-900">
        <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.groupMonitor.delete') }}</h3>
        <p class="mt-2 text-sm text-gray-500">
          {{ t('admin.groupMonitor.deleteConfirm', { name: deleting.group_name }) }}
        </p>
        <div class="mt-4 flex justify-end gap-2">
          <button
            type="button"
            class="rounded-md px-3 py-2 text-sm text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800"
            @click="deleting = null"
          >
            {{ t('common.cancel') }}
          </button>
          <button
            type="button"
            class="rounded-md bg-red-600 px-3 py-2 text-sm font-medium text-white hover:bg-red-700"
            @click="confirmDelete"
          >
            {{ t('common.delete') }}
          </button>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { adminAPI } from '@/api/admin'
import type { GroupMonitor, GroupMonitorAccountStatus } from '@/api/admin/groupMonitor'
import AppLayout from '@/components/layout/AppLayout.vue'
import { applyMonitorPreset, filterMonitors, summarizeMonitors, validateMonitorForm, type MonitorHealthFilter, type MonitorPreset } from '@/features/group-monitor/monitorWorkbench'

interface GroupOption {
  id: number
  name: string
}

const { t } = useI18n()
const appStore = useAppStore()

const monitors = ref<GroupMonitor[]>([])
const groups = ref<GroupOption[]>([])
const loading = ref(false)
const runningId = ref<number | null>(null)
const expandedId = ref<number | null>(null)
const resultsMap = ref<Record<number, GroupMonitorAccountStatus[]>>({})
const showDialog = ref(false)
const editing = ref<GroupMonitor | null>(null)
const saving = ref(false)
const deleting = ref<GroupMonitor | null>(null)
const healthFilter = ref<MonitorHealthFilter>('all')
const selectedPreset = ref<MonitorPreset>('balanced')
const summary = computed(() => summarizeMonitors(monitors.value))
const filteredMonitors = computed(() => filterMonitors(monitors.value, healthFilter.value))
const summaryItems = computed(() => [
  { key: 'total', label: t('admin.groupMonitor.summaryTotal'), value: summary.value.total },
  { key: 'enabled', label: t('admin.groupMonitor.summaryRunning'), value: summary.value.enabled },
  { key: 'healthy', label: t('admin.groupMonitor.summaryHealthy'), value: summary.value.healthy },
  { key: 'degraded', label: t('admin.groupMonitor.summaryDegraded'), value: summary.value.degraded },
  { key: 'unknown', label: t('admin.groupMonitor.summaryUnknown'), value: summary.value.unknown },
])
const presetOptions = computed(() => [
  { value: 'fast' as MonitorPreset, label: t('admin.groupMonitor.presetFast'), hint: t('admin.groupMonitor.presetFastHint') },
  { value: 'balanced' as MonitorPreset, label: t('admin.groupMonitor.presetBalanced'), hint: t('admin.groupMonitor.presetBalancedHint') },
  { value: 'economy' as MonitorPreset, label: t('admin.groupMonitor.presetEconomy'), hint: t('admin.groupMonitor.presetEconomyHint') },
])

const form = ref({
  group_ids: [] as number[],
  interval_minutes: 30,
  model_id: '',
  auto_recover: false,
  max_output_tokens: 16
})

const allGroupsSelected = computed(() => {
  return groups.value.length > 0 && form.value.group_ids.length === groups.value.length
})

function toggleSelectAllGroups() {
  if (allGroupsSelected.value) {
    form.value.group_ids = []
  } else {
    form.value.group_ids = groups.value.map((g) => g.id)
  }
}

function statusDotClass(status: string): string {
  if (status === 'success') return 'bg-emerald-500'
  if (status === 'failed') return 'bg-red-500'
  return 'bg-gray-300 dark:bg-gray-600'
}

function statusTextClass(status: string): string {
  if (status === 'success') return 'text-emerald-600 dark:text-emerald-400'
  if (status === 'failed') return 'text-red-500'
  return 'text-gray-400'
}

function formatTime(iso: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return isNaN(d.getTime()) ? iso : d.toLocaleString()
}

function pctOf(m: GroupMonitor, kind: 'healthy' | 'failed' | 'unknown'): number {
  const total = m.healthy_count + m.failed_count + m.unknown_count
  if (total === 0) return 0
  const value = kind === 'healthy' ? m.healthy_count : kind === 'failed' ? m.failed_count : m.unknown_count
  return (value / total) * 100
}

function healthPct(m: GroupMonitor): number {
  return pctOf(m, 'healthy')
}
function failedPct(m: GroupMonitor): number {
  return pctOf(m, 'failed')
}
function unknownPct(m: GroupMonitor): number {
  return pctOf(m, 'unknown')
}

async function reload() {
  loading.value = true
  try {
    const res = await adminAPI.groupMonitor.list({ page: 1, page_size: 100 })
    monitors.value = res.items ?? []
  } catch (err) {
    appStore.showError(extractApiErrorMessage(err, t('admin.groupMonitor.loadError')))
  } finally {
    loading.value = false
  }
}

async function loadGroups() {
  try {
    groups.value = await adminAPI.groups.getAll()
  } catch {
    groups.value = []
  }
}

function selectPreset(preset: MonitorPreset) {
  selectedPreset.value = preset
  form.value = applyMonitorPreset(form.value, preset)
}

function openCreate() {
  editing.value = null
  selectedPreset.value = 'balanced'
  form.value = applyMonitorPreset({ group_ids: [], interval_minutes: 30, model_id: '', auto_recover: false, max_output_tokens: 16 }, 'balanced')
  showDialog.value = true
}

function openEdit(m: GroupMonitor) {
  editing.value = m
  form.value = {
    group_ids: [m.group_id],
    interval_minutes: m.interval_minutes,
    model_id: m.model_id,
    auto_recover: m.auto_recover,
    max_output_tokens: m.max_output_tokens
  }
  showDialog.value = true
}

function closeDialog() {
  showDialog.value = false
  editing.value = null
}

async function save() {
  const validationError = validateMonitorForm(form.value)
  if (validationError === 'group_required') {
    appStore.showError(t('admin.groupMonitor.form.groupRequired'))
    return
  }
  if (validationError === 'interval_invalid' || validationError === 'max_output_tokens_invalid') {
    appStore.showError('Check interval and output token limits')
    return
  }
  saving.value = true
  try {
    if (editing.value) {
      await adminAPI.groupMonitor.update(editing.value.id, {
        interval_minutes: form.value.interval_minutes,
        model_id: form.value.model_id,
        auto_recover: form.value.auto_recover,
        max_output_tokens: form.value.max_output_tokens
      })
      appStore.showSuccess(t('admin.groupMonitor.updateSuccess'))
    } else {
      if (form.value.group_ids.length === 0) {
        appStore.showError(t('admin.groupMonitor.form.groupRequired'))
        return
      }
      const res = await adminAPI.groupMonitor.batchCreate({
        group_ids: form.value.group_ids,
        interval_minutes: form.value.interval_minutes,
        model_id: form.value.model_id,
        auto_recover: form.value.auto_recover,
        max_output_tokens: form.value.max_output_tokens
      })
      const createdCount = res.created?.length ?? 0
      const skipped = res.skipped ?? 0
      appStore.showSuccess(
        t('admin.groupMonitor.batchCreateSuccess', { created: createdCount, skipped })
      )
    }
    closeDialog()
    await reload()
  } catch (err) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  } finally {
    saving.value = false
  }
}

async function runNow(m: GroupMonitor) {
  if (runningId.value !== null) return
  runningId.value = m.id
  try {
    const results = await adminAPI.groupMonitor.run(m.id)
    resultsMap.value[m.id] = results
    expandedId.value = m.id
    appStore.showSuccess(t('admin.groupMonitor.runSuccess'))
    await reload()
  } catch (err) {
    appStore.showError(extractApiErrorMessage(err, t('admin.groupMonitor.runFailed')))
  } finally {
    runningId.value = null
  }
}

async function toggleExpand(m: GroupMonitor) {
  if (expandedId.value === m.id) {
    expandedId.value = null
    return
  }
  expandedId.value = m.id
  try {
    resultsMap.value[m.id] = await adminAPI.groupMonitor.listResults(m.id)
  } catch {
    resultsMap.value[m.id] = []
  }
}

function askDelete(m: GroupMonitor) {
  deleting.value = m
}

async function confirmDelete() {
  if (!deleting.value) return
  try {
    await adminAPI.groupMonitor.delete(deleting.value.id)
    appStore.showSuccess(t('admin.groupMonitor.deleteSuccess'))
    deleting.value = null
    await reload()
  } catch (err) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  }
}

onMounted(() => {
  void reload()
  void loadGroups()
})
</script>
