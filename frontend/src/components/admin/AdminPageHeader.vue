<template>
  <header class="admin-page-header">
    <div class="min-w-0 flex-1">
      <p v-if="eyebrow" class="admin-eyebrow">{{ eyebrow }}</p>
      <h1 class="admin-page-title">{{ title }}</h1>
      <p v-if="description" class="admin-page-description">{{ description }}</p>
      <slot />
    </div>
    <div v-if="$slots.actions" class="admin-page-actions">
      <slot name="actions" />
    </div>
  </header>
</template>

<script setup lang="ts">
import './admin-workspace.css'

defineProps<{
  eyebrow?: string
  title: string
  description?: string
}>()
</script>
