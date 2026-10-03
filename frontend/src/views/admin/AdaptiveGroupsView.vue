<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center gap-3">
          <div class="flex-1 sm:max-w-72">
            <input
              v-model="searchQuery"
              type="text"
              :placeholder="t('admin.adaptive.searchPlaceholder')"
              class="input"
            />
          </div>
          <Select
            v-model="statusFilter"
            :options="statusFilterOptions"
            class="w-36"
          />

          <div class="flex flex-1 flex-wrap items-center justify-end gap-2">
            <button
              type="button"
              class="btn btn-secondary"
              :title="t('admin.antiStall.openSettings')"
              data-test="open-anti-stall-settings"
              @click="showAntiStallDialog = true"
            >
              {{ t('admin.antiStall.openSettings') }}
              <span
                :class="[
                  'ml-1.5 inline-flex rounded-full px-1.5 py-0.5 text-[10px] font-semibold uppercase',
                  antiStall.module_enabled
                    ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
                    : 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-dark-300'
                ]"
              >
                {{ antiStall.module_enabled ? t('admin.antiStall.moduleOn') : t('admin.antiStall.moduleOff') }}
              </span>
            </button>
            <button
              type="button"
              class="btn btn-secondary"
              :disabled="loading"
              :title="t('common.refresh')"
              @click="loadPools"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <button type="button" class="btn btn-primary" @click="openCreateDialog">
              <Icon name="plus" size="md" class="mr-1" />
              {{ t('admin.adaptive.configurePool') }}
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <!-- Adaptive pools table only — Anti-Stall lives in a dialog so it cannot
             clip this table inside the fixed-height overflow-hidden layout. -->
        <div
          v-if="!loading && pools.length === 0"
          class="rounded-xl border border-dashed border-gray-200 px-6 py-12 text-center dark:border-dark-600"
        >
          <p class="text-base font-medium text-gray-900 dark:text-white">
            {{ t('admin.adaptive.noPoolsYet') }}
          </p>
          <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">
            {{ t('admin.adaptive.createFirstPool') }}
          </p>
          <button type="button" class="btn btn-primary mt-4" @click="openCreateDialog">
            <Icon name="plus" size="md" class="mr-1" />
            {{ t('admin.adaptive.configurePool') }}
          </button>
        </div>

        <DataTable
          v-else
          :columns="columns"
          :data="filteredPools"
          :loading="loading"
        >
          <template #cell-parent="{ row }">
            <div class="min-w-0">
              <div class="font-medium text-gray-900 dark:text-white">
                {{ groupLabel(row.parent_group_id) }}
              </div>
              <div class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
                #{{ row.parent_group_id }} · {{ t('admin.adaptive.platformLabel') }} {{ row.platform || '—' }}
                <span
                  v-if="poolPlatforms(row).length > 1"
                  class="ml-1 rounded bg-emerald-100 px-1.5 py-0.5 text-[10px] font-semibold uppercase text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300"
                >
                  {{ te('admin.adaptive.mixedPlatforms') ? t('admin.adaptive.mixedPlatforms') : '跨平台' }} · {{ poolPlatforms(row).join(' / ') }}
                </span>
              </div>
            </div>
          </template>

          <template #cell-enabled="{ value }">
            <span :class="['badge', value ? 'badge-success' : 'badge-gray']">
              {{ value ? t('admin.adaptive.statusEnabled') : t('admin.adaptive.statusDisabled') }}
            </span>
          </template>

          <template #cell-members="{ row }">
            <div class="flex flex-wrap items-center gap-1.5">
              <template v-if="row.members?.length">
                <span
                  v-for="(member, index) in orderedMembers(row.members)"
                  :key="`${row.parent_group_id}-${member.leaf_group_id}`"
                  :class="[
                    'inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs font-medium',
                    member.enabled
                      ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300'
                      : 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-dark-300'
                  ]"
                  :title="member.enabled ? t('admin.adaptive.leafEnabled') : t('admin.adaptive.leafDisabled')"
                >
                  <span class="opacity-60">{{ index + 1 }}.</span>
                  {{ groupLabel(member.leaf_group_id) }}
                  <span class="opacity-50">{{ groupPlatform(member.leaf_group_id) }}</span>
                </span>
              </template>
              <span v-else class="text-sm text-gray-400">{{ t('admin.adaptive.noMembers') }}</span>
            </div>
          </template>

          <template #cell-config_generation="{ value }">
            <span class="font-mono text-sm text-gray-600 dark:text-gray-300">{{ value }}</span>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex items-center space-x-1">
              <button
                type="button"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-dark-600 dark:hover:text-gray-300"
                :title="t('common.edit')"
                @click="openEditDialog(row)"
              >
                <Icon name="edit" size="sm" />
              </button>
              <button
                type="button"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
                :title="t('common.delete')"
                @click="openDeleteDialog(row)"
              >
                <Icon name="trash" size="sm" />
              </button>
            </div>
          </template>
        </DataTable>
      </template>
    </TablePageLayout>

    <!-- Anti-Stall PRO settings (dialog — keeps Adaptive pool table fully visible) -->
    <BaseDialog
      :show="showAntiStallDialog"
      :title="t('admin.antiStall.title')"
      width="wide"
      @close="showAntiStallDialog = false"
    >
      <div class="space-y-4">
        <p class="text-sm text-gray-500 dark:text-dark-400">
          {{ t('admin.antiStall.description') }}
        </p>

        <label class="inline-flex items-center gap-2 text-sm text-gray-700 dark:text-gray-200">
          <input
            v-model="antiStall.module_enabled"
            type="checkbox"
            class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
          {{ t('admin.antiStall.moduleEnabled') }}
        </label>
        <p class="text-xs text-gray-500 dark:text-dark-400">
          {{ t('admin.antiStall.moduleHint') }}
        </p>

        <div class="grid gap-4 lg:grid-cols-3">
          <div
            v-for="tier in antiStallTiers"
            :key="tier"
            class="rounded-lg border border-gray-100 bg-gray-50 p-3 dark:border-dark-600 dark:bg-dark-900/40"
          >
            <h4 class="mb-3 text-sm font-semibold uppercase tracking-wide text-gray-800 dark:text-gray-100">
              {{ t(`admin.antiStall.tier.${tier}`) }}
            </h4>
            <div class="space-y-3">
              <div>
                <label class="input-label">{{ t('admin.antiStall.bufferTokens') }}</label>
                <input
                  v-model.number="antiStall[tier].buffer_tokens"
                  type="number"
                  min="1"
                  max="256"
                  class="input w-full"
                />
              </div>
              <div>
                <label class="input-label">{{ t('admin.antiStall.dripRate') }}</label>
                <input
                  v-model.number="antiStall[tier].drip_tokens_per_second"
                  type="number"
                  min="1"
                  max="20"
                  class="input w-full"
                />
              </div>
              <div>
                <label class="input-label">{{ t('admin.antiStall.upstreamMaxRetry') }}</label>
                <input
                  v-model.number="antiStall[tier].upstream_max_retry"
                  type="number"
                  min="1"
                  max="10"
                  class="input w-full"
                />
              </div>
              <div>
                <label class="input-label">{{ t('admin.antiStall.lowBufferTokens') }}</label>
                <input
                  v-model.number="antiStall[tier].low_buffer_tokens"
                  type="number"
                  min="0"
                  max="256"
                  class="input w-full"
                />
              </div>
              <div>
                <label class="input-label">{{ t('admin.antiStall.maxDripSeconds') }}</label>
                <input
                  v-model.number="antiStall[tier].max_drip_seconds"
                  type="number"
                  min="5"
                  max="300"
                  class="input w-full"
                />
              </div>
              <div>
                <label class="input-label">{{ t('admin.antiStall.maxLeafSwitches') }}</label>
                <input
                  v-model.number="antiStall[tier].max_leaf_switches"
                  type="number"
                  min="1"
                  max="10"
                  class="input w-full"
                />
              </div>
            </div>
          </div>
        </div>
      </div>

      <template #footer>
        <div class="flex justify-end gap-2">
          <button type="button" class="btn btn-secondary" :disabled="antiStallSaving" @click="showAntiStallDialog = false">
            {{ t('common.cancel') }}
          </button>
          <button
            type="button"
            class="btn btn-primary"
            :disabled="antiStallSaving"
            @click="saveAntiStall"
          >
            {{ antiStallSaving ? t('common.saving') : t('admin.antiStall.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Create / Edit Dialog -->
    <BaseDialog
      :show="showEditor"
      :title="isEditing ? t('admin.adaptive.editPool') : t('admin.adaptive.configurePool')"
      width="wide"
      @close="closeEditor"
    >
      <form id="adaptive-pool-form" class="space-y-5" @submit.prevent="handleSave">
        <p class="rounded-lg bg-blue-50 px-3 py-2 text-sm text-blue-800 dark:bg-blue-950/40 dark:text-blue-200">
          {{ t('admin.adaptive.hint') }}
        </p>

        <div>
          <label class="input-label">{{ t('admin.adaptive.parentGroup') }}</label>
          <Select
            v-model="form.parentGroupId"
            :options="parentGroupOptions"
            :disabled="isEditing || groupsLoading"
            class="w-full"
          />
          <p v-if="selectedParent" class="mt-1 text-xs text-gray-500 dark:text-dark-400">
            {{ t('admin.adaptive.platformLabel') }}: {{ selectedParent.platform }}
            · #{{ selectedParent.id }}
          </p>
        </div>

        <div class="flex flex-col gap-3">
          <div class="flex items-center gap-3">
            <label class="inline-flex cursor-pointer items-center gap-2">
              <input v-model="form.enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
              <span class="text-sm font-medium text-gray-800 dark:text-gray-100">
                {{ t('admin.adaptive.enabled') }}
              </span>
            </label>
            <span class="text-xs text-gray-500 dark:text-dark-400">
              {{ t('admin.adaptive.enabledHelp') }}
            </span>
          </div>
          <div class="flex items-center gap-3">
            <label class="inline-flex cursor-pointer items-center gap-2">
              <input v-model="form.allowHybrid" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
              <span class="text-sm font-medium text-gray-800 dark:text-gray-100">
                {{ t('admin.adaptive.allowHybrid') }}
              </span>
            </label>
            <span class="text-xs text-gray-500 dark:text-dark-400">
              {{ t('admin.adaptive.allowHybridHelp') }}
            </span>
          </div>
        </div>

        <div>
          <div class="mb-2 flex items-center justify-between gap-2">
            <label class="input-label mb-0">{{ t('admin.adaptive.leafMembers') }}</label>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="!form.parentGroupId || leafOptionsAvailable.length === 0"
              @click="addLeafRow"
            >
              <Icon name="plus" size="sm" class="mr-1" />
              {{ t('admin.adaptive.addLeaf') }}
            </button>
          </div>

          <div v-if="form.members.length === 0" class="rounded-lg border border-dashed border-gray-200 px-4 py-6 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-dark-400">
            {{ t('admin.adaptive.noMembersHint') }}
          </div>

          <div v-else class="space-y-2">
            <div
              v-for="(member, index) in form.members"
              :key="member._key"
              class="flex flex-wrap items-center gap-2 rounded-lg border border-gray-200 bg-gray-50/80 p-3 dark:border-dark-600 dark:bg-dark-800/50"
            >
              <span class="w-6 shrink-0 text-center text-xs font-semibold text-gray-400">
                {{ index + 1 }}
              </span>

              <Select
                v-model="member.leaf_group_id"
                :options="leafSelectOptions(index)"
                class="min-w-[12rem] flex-1"
              />

              <label class="inline-flex items-center gap-1.5 text-sm text-gray-700 dark:text-gray-200">
                <input v-model="member.enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
                {{ t('admin.adaptive.leafEnabled') }}
              </label>

              <div class="flex items-center gap-1">
                <button
                  type="button"
                  class="rounded-md p-1.5 text-gray-500 hover:bg-white hover:text-gray-800 disabled:opacity-40 dark:hover:bg-dark-700 dark:hover:text-gray-200"
                  :disabled="index === 0"
                  :title="t('admin.adaptive.moveUp')"
                  @click="moveMember(index, -1)"
                >
                  <Icon name="chevronUp" size="sm" />
                </button>
                <button
                  type="button"
                  class="rounded-md p-1.5 text-gray-500 hover:bg-white hover:text-gray-800 disabled:opacity-40 dark:hover:bg-dark-700 dark:hover:text-gray-200"
                  :disabled="index === form.members.length - 1"
                  :title="t('admin.adaptive.moveDown')"
                  @click="moveMember(index, 1)"
                >
                  <Icon name="chevronDown" size="sm" />
                </button>
                <button
                  type="button"
                  class="rounded-md p-1.5 text-gray-500 hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
                  :title="t('common.delete')"
                  @click="removeMember(index)"
                >
                  <Icon name="trash" size="sm" />
                </button>
              </div>
            </div>
          </div>

          <p class="mt-2 text-xs text-gray-500 dark:text-dark-400">
            {{ t('admin.adaptive.leafOrderHelp') }}
          </p>
        </div>
      </form>

      <template #footer>
        <div class="flex justify-end gap-2">
          <button type="button" class="btn btn-secondary" :disabled="saving" @click="closeEditor">
            {{ t('common.cancel') }}
          </button>
          <button
            type="submit"
            form="adaptive-pool-form"
            class="btn btn-primary"
            :disabled="saving || !canSave"
          >
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('admin.adaptive.deletePool')"
      :message="deleteConfirmMessage"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      danger
      @confirm="confirmDelete"
      @cancel="showDeleteDialog = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import type { AdaptiveLeafMember, AdaptivePool, AntiStallAdminConfig } from '@/api/admin/adaptive'
import {
  defaultAntiStallAdminConfig,
  getAntiStallPro,
  putAntiStallPro
} from '@/api/admin/adaptive'
import type { AdminGroup } from '@/types'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'

const { t, te } = useI18n()
const appStore = useAppStore()

interface FormMember {
  _key: string
  leaf_group_id: number | ''
  enabled: boolean
}

const pools = ref<AdaptivePool[]>([])
const groups = ref<AdminGroup[]>([])
const loading = ref(false)
const groupsLoading = ref(false)
const saving = ref(false)
const searchQuery = ref('')
const statusFilter = ref('')

const antiStallSaving = ref(false)
const showAntiStallDialog = ref(false)
const antiStallTiers = ['basic', 'pro', 'ultra'] as const
const antiStall = reactive<AntiStallAdminConfig>(defaultAntiStallAdminConfig())

const showEditor = ref(false)
const isEditing = ref(false)
const showDeleteDialog = ref(false)
const deletingPool = ref<AdaptivePool | null>(null)

let memberKeySeq = 0
const nextMemberKey = () => `m-${++memberKeySeq}`

const form = reactive({
  parentGroupId: '' as number | '',
  enabled: true,
  allowHybrid: true,
  members: [] as FormMember[]
})

const statusFilterOptions = computed(() => [
  { value: '', label: t('admin.adaptive.allStatus') },
  { value: 'enabled', label: t('admin.adaptive.statusEnabled') },
  { value: 'disabled', label: t('admin.adaptive.statusDisabled') }
])

const columns = computed<Column[]>(() => [
  { key: 'parent', label: t('admin.adaptive.columns.parent') },
  { key: 'enabled', label: t('admin.adaptive.columns.status') },
  { key: 'members', label: t('admin.adaptive.columns.members') },
  { key: 'config_generation', label: t('admin.adaptive.columns.generation') },
  { key: 'actions', label: t('admin.adaptive.columns.actions') }
])

const groupById = computed(() => {
  const map = new Map<number, AdminGroup>()
  for (const g of groups.value) {
    map.set(g.id, g)
  }
  return map
})

const adaptiveParentIds = computed(() => new Set(pools.value.map((p) => p.parent_group_id)))

function groupLabel(id: number): string {
  const g = groupById.value.get(id)
  return g ? g.name : t('admin.adaptive.unknownGroup', { id })
}

function groupPlatform(id: number): string {
  return groupById.value.get(id)?.platform || '—'
}

function poolPlatforms(row: AdaptivePool): string[] {
  const plats = new Set<string>()
  for (const member of row.members || []) {
    const platform = groupById.value.get(member.leaf_group_id)?.platform
    if (platform) plats.add(platform)
  }
  return [...plats]
}

function orderedMembers(members: AdaptiveLeafMember[]): AdaptiveLeafMember[] {
  return [...members].sort((a, b) => a.sort_order - b.sort_order || a.leaf_group_id - b.leaf_group_id)
}

const filteredPools = computed(() => {
  let list = pools.value
  if (statusFilter.value === 'enabled') {
    list = list.filter((p) => p.enabled)
  } else if (statusFilter.value === 'disabled') {
    list = list.filter((p) => !p.enabled)
  }
  const q = searchQuery.value.trim().toLowerCase()
  if (!q) return list
  return list.filter((p) => {
    const parentName = groupLabel(p.parent_group_id).toLowerCase()
    const leafNames = (p.members || []).map((m) => groupLabel(m.leaf_group_id).toLowerCase()).join(' ')
    return (
      String(p.parent_group_id).includes(q) ||
      parentName.includes(q) ||
      (p.platform || '').toLowerCase().includes(q) ||
      leafNames.includes(q) ||
      (p.members || []).some((m) => String(m.leaf_group_id).includes(q))
    )
  })
})

const selectedParent = computed(() => {
  if (form.parentGroupId === '' || form.parentGroupId == null) return null
  return groupById.value.get(Number(form.parentGroupId)) ?? null
})

const parentGroupOptions = computed(() => {
  const options: { value: number | ''; label: string }[] = [
    { value: '', label: t('admin.adaptive.selectParent') }
  ]
  for (const g of groups.value) {
    // When editing, always include current parent; when creating, skip groups already configured as parents
    if (!isEditing.value && adaptiveParentIds.value.has(g.id)) continue
    if (isEditing.value && form.parentGroupId !== g.id && adaptiveParentIds.value.has(g.id)) continue
    options.push({
      value: g.id,
      label: `#${g.id} ${g.name} (${g.platform}${g.status !== 'active' ? ', ' + g.status : ''})`
    })
  }
  return options
})

const leafCandidates = computed(() => {
  const parent = selectedParent.value
  if (!parent) return [] as AdminGroup[]
  return groups.value.filter((g) => {
    if (g.id === parent.id) return false
    // Parent platform is identity only — leaves may mix openai/anthropic/gemini.
    if (adaptiveParentIds.value.has(g.id) && form.parentGroupId !== g.id) return false
    return true
  })
})

const leafOptionsAvailable = computed(() => {
  const used = new Set(
    form.members
      .map((m) => (m.leaf_group_id === '' ? null : Number(m.leaf_group_id)))
      .filter((id): id is number => id != null && !Number.isNaN(id))
  )
  return leafCandidates.value.filter((g) => !used.has(g.id))
})

function leafSelectOptions(rowIndex: number) {
  const currentId = form.members[rowIndex]?.leaf_group_id
  const options: { value: number | ''; label: string }[] = [
    { value: '', label: t('admin.adaptive.selectLeaf') }
  ]
  for (const g of leafCandidates.value) {
    const takenByOther = form.members.some(
      (m, i) => i !== rowIndex && m.leaf_group_id !== '' && Number(m.leaf_group_id) === g.id
    )
    if (takenByOther) continue
    options.push({
      value: g.id,
      label: `#${g.id} ${g.name} (${g.platform}${g.status !== 'active' ? ', ' + g.status : ''})`
    })
  }
  // Preserve stale leaf id if group list no longer has it
  if (currentId !== '' && currentId != null && !options.some((o) => o.value === currentId)) {
    options.push({
      value: Number(currentId),
      label: groupLabel(Number(currentId))
    })
  }
  return options
}

const canSave = computed(() => {
  if (form.parentGroupId === '' || form.parentGroupId == null) return false
  if (form.members.some((m) => m.leaf_group_id === '' || m.leaf_group_id == null)) return false
  if (form.enabled) {
    if (form.members.length === 0) return false
    if (!form.members.some((m) => m.enabled)) return false
  }
  return true
})

// Keep mixed-platform leaves when the parent changes; only drop self-parent.
watch(
  () => form.parentGroupId,
  () => {
    if (!showEditor.value || isEditing.value) return
    const parentID = Number(form.parentGroupId)
    form.members = form.members.filter(
      (m) => m.leaf_group_id === '' || Number(m.leaf_group_id) !== parentID
    )
  }
)

const deleteConfirmMessage = computed(() => {
  if (!deletingPool.value) return ''
  return t('admin.adaptive.deletePoolConfirm', {
    name: groupLabel(deletingPool.value.parent_group_id),
    id: deletingPool.value.parent_group_id
  })
})

function extractErrorMessage(error: unknown, fallback: string): string {
  if (error && typeof error === 'object') {
    const e = error as Record<string, unknown>
    if (typeof e.message === 'string' && e.message) return e.message
    const resp = e.response as { data?: { message?: string; detail?: string } } | undefined
    if (resp?.data?.message) return resp.data.message
    if (resp?.data?.detail) return resp.data.detail
  }
  return fallback
}

let abortController: AbortController | null = null

async function loadPools() {
  if (abortController) abortController.abort()
  const current = new AbortController()
  abortController = current
  loading.value = true
  try {
    const response = await adminAPI.adaptive.list({ signal: current.signal })
    if (current.signal.aborted || abortController !== current) return
    pools.value = response.items || []
  } catch (error: unknown) {
    if (
      current.signal.aborted ||
      abortController !== current ||
      (error as { name?: string; code?: string })?.name === 'AbortError' ||
      (error as { code?: string })?.code === 'ERR_CANCELED'
    ) {
      return
    }
    appStore.showError(extractErrorMessage(error, t('admin.adaptive.failedToLoad')))
    console.error('Error loading adaptive pools:', error)
  } finally {
    if (abortController === current) {
      loading.value = false
      abortController = null
    }
  }
}

async function loadGroups() {
  groupsLoading.value = true
  try {
    groups.value = await adminAPI.groups.getAllIncludingInactive()
  } catch (error: unknown) {
    appStore.showError(extractErrorMessage(error, t('admin.adaptive.failedToLoadGroups')))
    console.error('Error loading groups:', error)
  } finally {
    groupsLoading.value = false
  }
}

function applyAntiStallConfig(settings: AntiStallAdminConfig) {
  const defaults = defaultAntiStallAdminConfig()
  antiStall.module_enabled = !!settings.module_enabled
  for (const tier of antiStallTiers) {
    const src = settings[tier] ?? defaults[tier]
    antiStall[tier] = {
      buffer_tokens: src.buffer_tokens ?? defaults[tier].buffer_tokens,
      drip_tokens_per_second: src.drip_tokens_per_second ?? defaults[tier].drip_tokens_per_second,
      upstream_max_retry: src.upstream_max_retry ?? defaults[tier].upstream_max_retry,
      low_buffer_tokens: src.low_buffer_tokens ?? defaults[tier].low_buffer_tokens,
      max_drip_seconds: src.max_drip_seconds ?? defaults[tier].max_drip_seconds,
      max_leaf_switches: src.max_leaf_switches ?? defaults[tier].max_leaf_switches
    }
  }
}

async function loadAntiStall() {
  try {
    const settings = await getAntiStallPro()
    applyAntiStallConfig(settings)
  } catch (error: unknown) {
    console.error('Error loading Anti-Stall PRO settings:', error)
  }
}

async function saveAntiStall() {
  antiStallSaving.value = true
  try {
    const payload: AntiStallAdminConfig = {
      module_enabled: antiStall.module_enabled,
      basic: { ...antiStall.basic },
      pro: { ...antiStall.pro },
      ultra: { ...antiStall.ultra }
    }
    const saved = await putAntiStallPro(payload)
    applyAntiStallConfig(saved)
    appStore.showSuccess(t('admin.antiStall.saved'))
    showAntiStallDialog.value = false
  } catch (error: unknown) {
    appStore.showError(extractErrorMessage(error, t('admin.antiStall.failedToSave')))
  } finally {
    antiStallSaving.value = false
  }
}

function resetForm() {
  form.parentGroupId = ''
  form.enabled = true
  form.allowHybrid = true
  form.members = []
}

function openCreateDialog() {
  isEditing.value = false
  resetForm()
  showEditor.value = true
}

function openEditDialog(pool: AdaptivePool) {
  isEditing.value = true
  form.parentGroupId = pool.parent_group_id
  form.enabled = pool.enabled
  form.allowHybrid = pool.allow_hybrid !== false
  form.members = orderedMembers(pool.members || []).map((m) => ({
    _key: nextMemberKey(),
    leaf_group_id: m.leaf_group_id,
    enabled: m.enabled
  }))
  showEditor.value = true
}

function closeEditor() {
  showEditor.value = false
  isEditing.value = false
  resetForm()
}

function addLeafRow() {
  const next = leafOptionsAvailable.value[0]
  form.members.push({
    _key: nextMemberKey(),
    leaf_group_id: next ? next.id : '',
    enabled: true
  })
}

function removeMember(index: number) {
  form.members.splice(index, 1)
}

function moveMember(index: number, delta: number) {
  const target = index + delta
  if (target < 0 || target >= form.members.length) return
  const copy = form.members.slice()
  const [item] = copy.splice(index, 1)
  copy.splice(target, 0, item)
  form.members = copy
}

function buildPayloadMembers(): AdaptiveLeafMember[] {
  return form.members.map((m, index) => ({
    leaf_group_id: Number(m.leaf_group_id),
    enabled: m.enabled,
    sort_order: (index + 1) * 10
  }))
}

async function handleSave() {
  if (!canSave.value || form.parentGroupId === '') return
  saving.value = true
  try {
    const members = buildPayloadMembers()
    const mixed = new Set(
      members
        .map((m) => groupById.value.get(m.leaf_group_id)?.platform)
        .filter((platform) => platform)
    ).size > 1
    await adminAPI.adaptive.put(Number(form.parentGroupId), {
      enabled: form.enabled,
      allow_hybrid: form.allowHybrid || mixed,
      members
    })
    appStore.showSuccess(
      isEditing.value ? t('admin.adaptive.poolUpdated') : t('admin.adaptive.poolSaved')
    )
    closeEditor()
    await loadPools()
  } catch (error: unknown) {
    appStore.showError(extractErrorMessage(error, t('admin.adaptive.failedToSave')))
  } finally {
    saving.value = false
  }
}

function openDeleteDialog(pool: AdaptivePool) {
  deletingPool.value = pool
  showDeleteDialog.value = true
}

async function confirmDelete() {
  if (!deletingPool.value) return
  try {
    await adminAPI.adaptive.delete(deletingPool.value.parent_group_id)
    appStore.showSuccess(t('admin.adaptive.poolDeleted'))
    showDeleteDialog.value = false
    deletingPool.value = null
    await loadPools()
  } catch (error: unknown) {
    appStore.showError(extractErrorMessage(error, t('admin.adaptive.failedToDelete')))
  }
}

onMounted(async () => {
  await Promise.all([loadGroups(), loadPools(), loadAntiStall()])
})

onUnmounted(() => {
  abortController?.abort()
})
</script>
