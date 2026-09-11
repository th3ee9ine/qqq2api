<template>
  <div class="json-viewer" :class="{ 'is-wrapped': wrap }">
    <div class="json-tools"><span>JSON <span class="json-lines">{{ lines.length }} 行</span></span><button type="button" :aria-pressed="wrap" @click="wrap = !wrap">{{ wrap ? '取消换行' : '自动换行' }}</button></div>
    <div class="json-scroll" tabindex="0" role="region" :aria-label="label">
      <pre><code><span v-for="(line, index) in lines" :key="index" class="code-line"><span class="line-number" aria-hidden="true">{{ index + 1 }}</span><span class="line-content"><span v-for="(token, tokenIndex) in line" :key="tokenIndex" :class="token.type && `token-${token.type}`">{{ token.text }}</span></span></span></code></pre>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'

const props = defineProps<{ value: string; label: string }>()
const wrap = ref(false)
// Render every token as text, never HTML: upstream strings are untrusted.
const lines = computed(() => props.value.split('\n').map((line) => {
  const tokens: { text: string; type: string }[] = []
  const pattern = /"(?:\\.|[^"\\])*"(?=\s*:)|"(?:\\.|[^"\\])*"|\b(?:true|false|null)\b|-?\b\d+(?:\.\d+)?(?:[eE][+-]?\d+)?\b/g
  let cursor = 0
  for (const match of line.matchAll(pattern)) {
    const start = match.index ?? 0
    if (start > cursor) tokens.push({ text: line.slice(cursor, start), type: '' })
    const text = match[0]
    const type = text.startsWith('"') ? /^\s*:/.test(line.slice(start + text.length)) ? 'key' : 'string' : /^(true|false|null)$/.test(text) ? 'literal' : 'number'
    tokens.push({ text, type })
    cursor = start + text.length
  }
  if (cursor < line.length) tokens.push({ text: line.slice(cursor), type: '' })
  return tokens
}))
</script>

<style scoped>
.json-viewer { min-width: 0; display: flex; flex: 1; flex-direction: column; color: #c4cede; background: #111827; }
.json-tools { display: flex; align-items: center; justify-content: space-between; padding: 12px 22px 4px; color: #a0aec2; font: 12px/1.5 ui-monospace, SFMono-Regular, Menlo, monospace; }
.json-lines { padding-left: 12px; color: #8c9ab0; }
.json-tools button { padding: 4px 8px; border-radius: 4px; font-family: inherit; transition: background .15s; }
.json-tools button:hover, .json-tools button[aria-pressed='true'] { background: #263246; color: #e7efff; }
.json-tools button:focus-visible, .json-scroll:focus-visible { outline: 2px solid #8faeff; outline-offset: -2px; }
.json-scroll { min-width: 0; overflow: auto; max-height: 575px; flex: 1; scrollbar-color: #3b4a62 #111827; scrollbar-width: thin; }
pre { margin: 0; padding: 14px 0 28px; font: 13px/1.9 ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; tab-size: 2; }
.code-line { display: flex; min-width: max-content; padding-right: 22px; }
.code-line:hover { background: #ffffff04; }
.line-number { width: 52px; min-width: 52px; padding-right: 18px; text-align: right; user-select: none; color: #7f8ca4; }
.line-content { display: block; min-width: 0; }
.token-key { color: #a8c5ff; }
.token-string { color: #a5d8b5; }
.token-literal { color: #c7b8ff; }
.token-number { color: #f2c28e; }
.is-wrapped pre { white-space: pre-wrap; overflow-wrap: anywhere; }
.is-wrapped .code-line { min-width: 0; }
@media (max-width: 600px) { .json-scroll { max-height: 420px; } .line-number { width: 36px; min-width: 36px; padding-right: 10px; } .json-tools { padding-inline: 12px; } }
</style>
