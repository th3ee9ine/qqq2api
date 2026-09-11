<template>
  <details class="header-changes" :open="changes.some(change => change.action === 'filtered' || change.action === 'rewritten')">
    <summary><span>实际请求头处理</span><span class="change-summary">{{ changes.length }} 项 <span aria-hidden="true">↗</span></span></summary>
    <div v-if="!changes.length" class="changes-empty">尚无实际上游请求头记录。</div>
    <template v-else>
      <div class="change-legend"><span v-for="(label, key) in actionLabels" :key="key" :class="`change-${key}`">{{ label }} {{ changes.filter(change => change.action === key).length }}</span></div>
      <div class="change-list">
        <article v-for="change in changes" :key="change.name" class="header-change" :data-header-change="change.name">
          <div class="change-title"><code>{{ change.name }}</code><span :class="`change-badge change-${change.action}`">{{ actionLabels[change.action] }}</span></div>
          <p>{{ change.reason }}</p>
          <dl><template v-if="change.input_values?.length"><dt>输入</dt><dd>{{ change.input_values.join(', ') }}</dd></template><template v-if="change.output_values?.length"><dt>实际</dt><dd>{{ change.output_values.join(', ') }}</dd></template></dl>
        </article>
      </div>
    </template>
  </details>
</template>

<script setup lang="ts">
import type { DebugHeaderChange } from '@/api/admin/debugWorkbench'
defineProps<{ changes: DebugHeaderChange[] }>()
const actionLabels = { preserved: '保留', rewritten: '重写', generated: '生成', filtered: '过滤' }
</script>

<style scoped>
.header-changes { border-top: 1px solid var(--wb-border); color: var(--wb-text); }
summary { display: flex; justify-content: space-between; align-items: center; gap: 12px; padding: 16px 22px; cursor: pointer; list-style: none; font-size: 13px; font-weight: 600; }
summary::-webkit-details-marker { display: none; }
summary:focus-visible { outline: 2px solid var(--wb-accent); outline-offset: -4px; }
.change-summary { display: flex; gap: 12px; font: 12px/1.6 ui-monospace, monospace; color: var(--wb-muted); }
.change-legend { display: flex; gap: 14px; flex-wrap: wrap; padding: 0 22px 14px; font-size: 12px; }
.change-list { max-height: 360px; overflow: auto; padding: 0 22px 6px; }
.header-change { padding: 13px 0; border-top: 1px solid var(--wb-border); }
.change-title { display: flex; align-items: start; justify-content: space-between; gap: 12px; }
.change-title code { overflow-wrap: anywhere; font-size: 12px; }
.change-badge { flex-shrink: 0; padding: 1px 5px; border-radius: 3px; background: var(--wb-subtle); font-size: 11px; }
.change-preserved { color: #21835f; }
.change-rewritten { color: var(--wb-accent); }
.change-generated { color: var(--wb-muted); }
.change-filtered { color: var(--wb-warning); }
p { margin: 5px 0; font-size: 12px; color: var(--wb-muted); }
dl { display: grid; grid-template-columns: 28px minmax(0, 1fr); gap: 3px 8px; font: 11px/1.7 ui-monospace, monospace; }
dt { color: var(--wb-muted); } dd { white-space: pre-wrap; overflow-wrap: anywhere; }
.changes-empty { padding: 0 22px 16px; font-size: 12px; color: var(--wb-muted); }
:global(.dark) .change-preserved { color: #80d6b3; }
</style>
