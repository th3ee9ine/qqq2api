<template>
  <dl class="admin-overview" :aria-busy="loading">
    <div v-for="item in items" :key="item.label" class="admin-overview-item">
      <dt class="admin-overview-label">{{ item.label }}</dt>
      <dd class="admin-overview-value" :class="`admin-overview-value--${item.tone || 'default'}`">
        <span v-if="loading" class="inline-block h-7 w-12 animate-pulse rounded-md bg-gray-100 dark:bg-dark-700" aria-hidden="true"></span>
        <span v-else>{{ item.value }}</span>
      </dd>
      <dd v-if="item.hint" class="admin-overview-hint">{{ item.hint }}</dd>
    </div>
  </dl>
</template>

<script setup lang="ts">
import './admin-workspace.css'

withDefaults(defineProps<{
  items: Array<{
    label: string
    value: string | number
    hint?: string
    tone?: 'default' | 'positive' | 'warning'
  }>
  loading?: boolean
}>(), { loading: false })
</script>
