<template>
  <details class="header-guide">
    <summary class="header-guide-summary">
      <span class="header-guide-heading"><Icon name="infoCircle" size="sm" /><span>请求头用途与适用条件</span></span>
      <span class="header-guide-summary-meta"><span>{{ loading ? '同步中' : `${details.length} 项说明` }}</span><Icon name="chevronRight" size="sm" class="header-guide-chevron" /></span>
    </summary>
    <div class="header-guide-content">
      <p v-if="loading" class="header-guide-empty" role="status">正在同步当前账号与接口的请求头说明…</p>
      <p v-else-if="!details.length" class="header-guide-empty">本次后端未返回请求头说明，现有 Headers 保持不变。</p>
      <template v-else>
        <p class="header-guide-note">按当前 JSON 预览标记配置状态，不代表请求已发送。条件头仅在对应场景生效；连通性测试仍由服务端构造请求。</p>
        <ul class="header-guide-list" aria-label="请求头用途说明">
          <li v-for="detail in details" :key="detail.name" class="header-guide-item" :data-header="detail.name">
            <div class="header-guide-item-title">
              <code>{{ detail.name }}</code>
              <span class="header-requirement" :class="`header-requirement-${detail.requirement}`">{{ requirementLabels[detail.requirement] }}</span>
              <span class="header-presence" :class="`header-presence-${presence(detail).tone}`"><span aria-hidden="true"></span>{{ presence(detail).label }}</span>
            </div>
            <p class="header-guide-purpose">{{ detail.purpose }}</p>
            <dl class="header-guide-context">
              <template v-if="detail.condition"><dt>适用</dt><dd>{{ detail.condition }}</dd></template>
              <template v-if="detail.source"><dt>来源</dt><dd>{{ detail.source }}</dd></template>
            </dl>
          </li>
        </ul>
      </template>
    </div>
  </details>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import type { UpstreamHeaderDetail } from '@/api/admin/debugWorkbench'

const props = defineProps<{ details: UpstreamHeaderDetail[]; headers: string; loading?: boolean }>()
const requirementLabels = { required: '必要', recommended: '建议', conditional: '按条件', transport: '自动' } as const
const currentHeaderNames = computed(() => {
  try {
    const value: unknown = JSON.parse(props.headers)
    if (!value || typeof value !== 'object' || Array.isArray(value)) return null
    if (Object.values(value).some((entry) => typeof entry !== 'string')) return null
    return new Set(Object.keys(value).map((name) => name.toLowerCase()))
  } catch { return null }
})

function presence(detail: UpstreamHeaderDetail) {
  if (detail.requirement === 'transport') return { label: '传输层', tone: 'automatic' }
  if (!currentHeaderNames.value) return { label: '待校验', tone: 'pending' }
  if (!currentHeaderNames.value.has(detail.name.toLowerCase())) return { label: '未配置', tone: 'absent' }
  return { label: detail.default_included ? '默认包含' : '已配置', tone: 'included' }
}
</script>

<style scoped>
.header-guide { margin-top: 18px; border: 1px solid var(--wb-border); border-radius: 8px; background: var(--wb-subtle); }
.header-guide-summary { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 13px 14px; list-style: none; cursor: pointer; }
.header-guide-summary::-webkit-details-marker { display: none; }
.header-guide-summary:focus-visible { outline: 2px solid var(--wb-accent); outline-offset: 3px; border-radius: 8px; }
.header-guide-heading, .header-guide-summary-meta { display: flex; align-items: center; gap: 8px; }
.header-guide-heading { color: var(--wb-text); font-size: 12px; font-weight: 600; }
.header-guide-heading > svg { color: var(--wb-accent); flex-shrink: 0; }
.header-guide-summary-meta { color: var(--wb-muted); font-size: 11px; white-space: nowrap; }
.header-guide-chevron { transition: transform .15s; }
.header-guide[open] .header-guide-chevron { transform: rotate(90deg); }
.header-guide-content { padding: 0 14px 2px; }
.header-guide-note, .header-guide-empty { margin: 0 0 14px; color: var(--wb-muted); font-size: 11px; line-height: 1.8; }
.header-guide-list { max-height: 420px; overflow: auto; scrollbar-width: thin; scrollbar-color: var(--wb-border) transparent; list-style: none; margin: 0; padding: 0; }
.header-guide-item { padding: 14px 0; border-top: 1px solid var(--wb-border); }
.header-guide-item-title { display: flex; align-items: center; flex-wrap: wrap; gap: 7px; }
.header-guide-item-title code { color: var(--wb-text); font: 600 11px/1.6 ui-monospace, SFMono-Regular, Menlo, monospace; overflow-wrap: anywhere; }
.header-requirement { padding: 1px 5px; border-radius: 4px; font-size: 10px; line-height: 1.7; white-space: nowrap; }
.header-requirement-required, .header-requirement-recommended { color: var(--wb-accent); background: var(--wb-accent-soft); }
.header-requirement-conditional, .header-requirement-transport { color: var(--wb-muted); border: 1px solid var(--wb-border); }
.header-presence { display: inline-flex; align-items: center; gap: 5px; margin-left: auto; color: var(--wb-muted); font-size: 10px; white-space: nowrap; }
.header-presence > span { width: 4px; height: 4px; border-radius: 50%; background: currentColor; }
.header-presence-included { color: var(--wb-accent); }
.header-presence-pending { color: var(--wb-warning); }
.header-guide-purpose { margin: 6px 0 5px; color: var(--wb-text); font-size: 12px; line-height: 1.7; overflow-wrap: anywhere; }
.header-guide-context { display: grid; grid-template-columns: auto minmax(0, 1fr); column-gap: 9px; row-gap: 3px; margin: 0; color: var(--wb-muted); font-size: 11px; line-height: 1.7; }
.header-guide-context dt { font-weight: 500; }
.header-guide-context dd { margin: 0; overflow-wrap: anywhere; }
@media (max-width: 600px) { .header-guide-summary { gap: 8px; padding-inline: 10px; } .header-guide-content { padding-inline: 10px; } .header-guide-heading { gap: 5px; } .header-guide-list { max-height: 360px; } }
@media (prefers-reduced-motion: reduce) { .header-guide-chevron { transition: none; } }
</style>
