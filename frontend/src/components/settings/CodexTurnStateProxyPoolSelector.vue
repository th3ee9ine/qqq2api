<template>
  <div class="proxy-pool-selector" data-testid="codex-turn-state-proxy-pool">
    <div class="proxy-pool-toolbar">
      <div class="relative min-w-0 flex-1">
        <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
        <input
          v-model="search"
          type="search"
          class="input w-full pl-9 text-sm"
          :placeholder="t('admin.codexTurnState.proxyPool.search')"
          :aria-label="t('admin.codexTurnState.proxyPool.search')"
          :disabled="disabled"
          data-testid="codex-turn-state-proxy-search"
        />
      </div>
      <span class="selected-count">{{ t('admin.codexTurnState.proxyPool.selectedCount', { count: selectedIds.length }) }}</span>
    </div>

    <div class="proxy-pool-actions">
      <label class="inline-flex cursor-pointer items-center gap-2 text-xs font-medium text-gray-600 dark:text-gray-300">
        <input
          ref="selectAllInput"
          type="checkbox"
          class="proxy-checkbox"
          :checked="allVisibleSelected"
          :disabled="disabled || filteredProxies.length === 0 || (selectionLimitReached && !allVisibleSelected)"
          data-testid="codex-turn-state-proxy-select-visible"
          @change="toggleVisible"
        />
        {{ t('admin.codexTurnState.proxyPool.selectVisible') }}
      </label>
      <button
        type="button"
        class="text-xs font-medium text-primary-700 hover:underline disabled:cursor-not-allowed disabled:text-gray-400 disabled:no-underline dark:text-primary-400"
        :disabled="disabled || selectedIds.length === 0"
        data-testid="codex-turn-state-proxy-clear"
        @click="clearSelection"
      >
        {{ t('admin.codexTurnState.proxyPool.clear') }}
      </button>
    </div>

    <div v-if="loading" class="proxy-pool-empty" role="status">
      <Icon name="refresh" size="sm" class="animate-spin" />
      {{ t('common.loading') }}
    </div>
    <div v-else-if="proxies.length === 0" class="proxy-pool-empty">
      {{ t('admin.codexTurnState.proxyPool.empty') }}
    </div>
    <div v-else-if="filteredProxies.length === 0" class="proxy-pool-empty">
      {{ t('admin.codexTurnState.proxyPool.noMatches') }}
    </div>
    <div v-else class="proxy-pool-list">
      <label
        v-for="proxy in filteredProxies"
        :key="proxy.id"
        class="proxy-option"
        :class="{ 'is-selected': selectedSet.has(proxy.id), 'is-disabled': disabled || (!selectedSet.has(proxy.id) && selectionLimitReached) }"
      >
        <input
          type="checkbox"
          class="proxy-checkbox"
          :checked="selectedSet.has(proxy.id)"
          :value="proxy.id"
          :disabled="disabled || (!selectedSet.has(proxy.id) && selectionLimitReached)"
          :data-testid="`codex-turn-state-proxy-${proxy.id}`"
          @change="toggleProxy(proxy.id)"
        />
        <span class="min-w-0 flex-1">
          <span class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
            <span class="truncate text-sm font-medium text-gray-800 dark:text-gray-100" :title="proxy.name">{{ proxy.name }}</span>
            <span class="status-badge" :class="`status-${proxy.status}`">{{ statusLabel(proxy.status) }}</span>
          </span>
          <span class="mt-1 block break-all font-mono text-[11px] text-gray-500 dark:text-gray-400">
            {{ proxyEndpoint(proxy) }}
          </span>
          <span v-if="proxyLocation(proxy)" class="mt-1 block text-[11px] text-gray-400 dark:text-gray-500">
            {{ proxyLocation(proxy) }}
          </span>
        </span>
      </label>
    </div>
    <p class="proxy-pool-limit" :class="{ 'is-reached': selectionLimitReached }">
      {{ selectedIds.length === 0
        ? t('admin.codexTurnState.proxyPool.compatiblePool')
        : t('admin.codexTurnState.proxyPool.limit', { count: maxSelection }) }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { Proxy } from '@/types'

const props = withDefaults(defineProps<{
  modelValue: number[]
  proxies: Proxy[]
  disabled?: boolean
  loading?: boolean
}>(), {
  disabled: false,
  loading: false,
})

const emit = defineEmits<{
  'update:modelValue': [value: number[]]
}>()

const { t } = useI18n()
const maxSelection = 256
const search = ref('')
const selectAllInput = ref<HTMLInputElement | null>(null)

const selectedIds = computed(() => {
  const seen = new Set<number>()
  const result: number[] = []
  for (const id of props.modelValue) {
    if (!Number.isInteger(id) || id <= 0 || seen.has(id)) continue
    seen.add(id)
    result.push(id)
  }
  return result
})
const selectedSet = computed(() => new Set(selectedIds.value))

const filteredProxies = computed(() => {
  const query = search.value.trim().toLowerCase()
  if (!query) return props.proxies
  return props.proxies.filter((proxy) => [
    proxy.id,
    proxy.name,
    proxy.protocol,
    proxy.host,
    proxy.port,
    proxy.status,
    proxy.country,
    proxy.region,
    proxy.city,
  ].filter((value) => value !== null && value !== undefined).join(' ').toLowerCase().includes(query))
})

const visibleSelectedCount = computed(() => filteredProxies.value.filter((proxy) => selectedSet.value.has(proxy.id)).length)
const allVisibleSelected = computed(() => filteredProxies.value.length > 0 && visibleSelectedCount.value === filteredProxies.value.length)
const selectionLimitReached = computed(() => selectedIds.value.length >= maxSelection)

watchEffect(() => {
  if (selectAllInput.value) {
    selectAllInput.value.indeterminate = visibleSelectedCount.value > 0 && !allVisibleSelected.value
  }
})

function toggleProxy(id: number) {
  const next = [...selectedIds.value]
  const index = next.indexOf(id)
  if (index >= 0) next.splice(index, 1)
  else if (next.length < maxSelection) next.push(id)
  emit('update:modelValue', next)
}

function toggleVisible() {
  const visibleIds = new Set(filteredProxies.value.map((proxy) => proxy.id))
  if (allVisibleSelected.value) {
    emit('update:modelValue', selectedIds.value.filter((id) => !visibleIds.has(id)))
    return
  }
  const next = [...selectedIds.value]
  const existing = new Set(next)
  for (const proxy of filteredProxies.value) {
    if (next.length >= maxSelection) break
    if (!existing.has(proxy.id)) next.push(proxy.id)
  }
  emit('update:modelValue', next)
}

function clearSelection() {
  emit('update:modelValue', [])
}

function proxyEndpoint(proxy: Proxy): string {
  return `${proxy.protocol}://${proxy.host}:${proxy.port}`
}

function proxyLocation(proxy: Proxy): string {
  return [proxy.country, proxy.region, proxy.city].filter(Boolean).join(' / ')
}

function statusLabel(status: Proxy['status']): string {
  return t(`admin.codexTurnState.proxyPool.status.${status}`)
}
</script>

<style scoped>
.proxy-pool-selector {
  @apply overflow-hidden rounded-lg border border-gray-200 dark:border-dark-600;
}
.proxy-pool-toolbar {
  @apply flex flex-wrap items-center gap-3 border-b border-gray-100 p-3 dark:border-dark-700;
}
.selected-count {
  @apply shrink-0 text-xs font-medium tabular-nums text-primary-700 dark:text-primary-400;
}
.proxy-pool-actions {
  @apply flex items-center justify-between gap-3 border-b border-gray-100 bg-gray-50/60 px-4 py-2.5 dark:border-dark-700 dark:bg-dark-800/40;
}
.proxy-pool-list {
  @apply max-h-72 divide-y divide-gray-100 overflow-y-auto dark:divide-dark-700;
  scrollbar-width: thin;
}
.proxy-option {
  @apply flex cursor-pointer items-start gap-3 px-4 py-3 transition-colors hover:bg-gray-50 dark:hover:bg-dark-800/60;
}
.proxy-option.is-selected {
  @apply bg-primary-50/50 dark:bg-primary-500/5;
}
.proxy-option.is-disabled {
  @apply cursor-not-allowed opacity-60;
}
.proxy-checkbox {
  @apply mt-0.5 h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 disabled:cursor-not-allowed dark:border-dark-500 dark:bg-dark-800;
}
.proxy-pool-empty {
  @apply flex min-h-28 items-center justify-center gap-2 px-5 py-8 text-center text-xs text-gray-500 dark:text-gray-400;
}
.proxy-pool-limit {
  @apply border-t border-gray-100 px-4 py-2 text-[11px] leading-5 text-gray-400 dark:border-dark-700 dark:text-gray-500;
}
.proxy-pool-limit.is-reached {
  @apply text-amber-700 dark:text-amber-400;
}
.status-badge {
  @apply shrink-0 rounded-md px-1.5 py-0.5 text-[10px] font-medium;
}
.status-active {
  @apply bg-emerald-50 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-400;
}
.status-inactive {
  @apply bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300;
}
.status-expired {
  @apply bg-red-50 text-red-700 dark:bg-red-500/10 dark:text-red-400;
}
</style>
