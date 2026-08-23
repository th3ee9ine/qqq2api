const openaiModels = [
  'gpt-5.6', 'gpt-5.6-sol', 'gpt-5.6-terra', 'gpt-5.6-luna',
  'gpt-5.5', 'gpt-5.4', 'gpt-5.4-mini', 'gpt-5.4-2026-03-05',
  'gpt-5.3-codex-spark', 'codex-auto-review',
  'gpt-5.2', 'gpt-5.2-2025-12-11', 'gpt-5.2-chat-latest',
  'gpt-5.2-pro', 'gpt-5.2-pro-2025-12-11',
  'gpt-4.1', 'gpt-4o', 'gpt-4o-mini', 'gpt-4o-audio-preview',
  'gpt-4o-realtime-preview', 'o1', 'o3',
  'gpt-image-1', 'gpt-image-1.5', 'gpt-image-2'
]

export const claudeModels = [
  'claude-3-5-sonnet-20241022', 'claude-3-5-sonnet-20240620',
  'claude-3-5-haiku-20241022', 'claude-3-7-sonnet-20250219',
  'claude-sonnet-4-20250514', 'claude-opus-4-20250514',
  'claude-opus-4-1-20250805', 'claude-sonnet-4-5-20250929',
  'claude-haiku-4-5-20251001', 'claude-opus-4-5-20251101',
  'claude-opus-4-6', 'claude-opus-4-7', 'claude-opus-4-8',
  'claude-opus-5', 'claude-sonnet-4-6', 'claude-sonnet-5', 'claude-fable-5'
]

export const allModels = [...openaiModels, ...claudeModels]
  .filter((model, index, models) => models.indexOf(model) === index)
  .map(model => ({ value: model, label: model }))

const anthropicPresetMappings = [
  { label: 'Fable 5', from: 'claude-fable-5', to: 'claude-fable-5', color: 'bg-rose-100 text-rose-700' },
  { label: 'Sonnet 5', from: 'claude-sonnet-5', to: 'claude-sonnet-5', color: 'bg-indigo-100 text-indigo-700' },
  { label: 'Sonnet 4.6', from: 'claude-sonnet-4-6', to: 'claude-sonnet-4-6', color: 'bg-blue-100 text-blue-700' },
  { label: 'Opus 4.6', from: 'claude-opus-4-6', to: 'claude-opus-4-6', color: 'bg-purple-100 text-purple-700' },
  { label: 'Opus 4.8', from: 'claude-opus-4-8', to: 'claude-opus-4-8', color: 'bg-purple-100 text-purple-700' },
  { label: 'Opus 5', from: 'claude-opus-5', to: 'claude-opus-5', color: 'bg-purple-100 text-purple-700' }
]

const openaiPresetMappings = [
  { label: 'GPT-5.6', from: 'gpt-5.6', to: 'gpt-5.6', color: 'bg-amber-100 text-amber-700' },
  { label: 'GPT-5.4', from: 'gpt-5.4', to: 'gpt-5.4', color: 'bg-rose-100 text-rose-700' },
  { label: 'GPT-5.2', from: 'gpt-5.2', to: 'gpt-5.2', color: 'bg-red-100 text-red-700' },
  { label: 'GPT-4o', from: 'gpt-4o', to: 'gpt-4o', color: 'bg-green-100 text-green-700' },
  { label: 'GPT-4.1', from: 'gpt-4.1', to: 'gpt-4.1', color: 'bg-indigo-100 text-indigo-700' },
  { label: 'Opus→5.4', from: 'claude-opus-4-6', to: 'gpt-5.4', color: 'bg-purple-100 text-purple-700' },
  { label: 'Sonnet→5.4', from: 'claude-sonnet-4-6', to: 'gpt-5.4', color: 'bg-blue-100 text-blue-700' }
]

const bedrockPresetMappings = [
  { label: 'Fable 5', from: 'claude-fable-5', to: 'anthropic.claude-fable-5', color: 'bg-rose-100 text-rose-700' },
  { label: 'Opus 4.6', from: 'claude-opus-4-6', to: 'us.anthropic.claude-opus-4-6-v1', color: 'bg-pink-100 text-pink-700' },
  { label: 'Opus 4.8', from: 'claude-opus-4-8', to: 'us.anthropic.claude-opus-4-8-v1', color: 'bg-pink-100 text-pink-700' },
  { label: 'Sonnet 4.6', from: 'claude-sonnet-4-6', to: 'us.anthropic.claude-sonnet-4-6', color: 'bg-cyan-100 text-cyan-700' }
]

export const commonErrorCodes = [
  { value: 401, label: 'Unauthorized' },
  { value: 403, label: 'Forbidden' },
  { value: 429, label: 'Rate Limit' },
  { value: 500, label: 'Server Error' },
  { value: 502, label: 'Bad Gateway' },
  { value: 503, label: 'Unavailable' },
  { value: 529, label: 'Overloaded' }
]

export function getModelsByPlatform(platform: string): string[] {
  if (platform === 'openai') return openaiModels
  if (platform === 'anthropic' || platform === 'claude' || platform === 'bedrock') return claudeModels
  return []
}

export function getPresetMappingsByPlatform(platform: string) {
  if (platform === 'openai') return openaiPresetMappings
  if (platform === 'bedrock') return bedrockPresetMappings
  if (platform === 'anthropic' || platform === 'claude') return anthropicPresetMappings
  return []
}

export function isValidWildcardPattern(pattern: string): boolean {
  const starIndex = pattern.indexOf('*')
  if (starIndex === -1) return true
  return starIndex === pattern.length - 1 && pattern.lastIndexOf('*') === starIndex
}

export type ModelRestrictionMode = 'whitelist' | 'mapping' | 'combined'

export interface ModelMappingEntry {
  from: string
  to: string
}

export function splitModelMappingObject(
  modelMapping?: Record<string, unknown> | null
): { allowedModels: string[]; modelMappings: ModelMappingEntry[] } {
  const allowedModels: string[] = []
  const modelMappings: ModelMappingEntry[] = []
  if (!modelMapping || typeof modelMapping !== 'object') return { allowedModels, modelMappings }
  for (const [rawFrom, rawTo] of Object.entries(modelMapping)) {
    if (typeof rawTo !== 'string') continue
    const from = rawFrom.trim()
    const to = rawTo.trim()
    if (!from || !to) continue
    if (from === to) allowedModels.push(from)
    else modelMappings.push({ from, to })
  }
  return { allowedModels, modelMappings }
}

export function buildModelMappingObject(
  mode: ModelRestrictionMode,
  allowedModels: string[],
  modelMappings: ModelMappingEntry[]
): Record<string, string> | null {
  const mapping: Record<string, string> = {}
  if (mode === 'whitelist' || mode === 'combined') {
    for (const rawModel of allowedModels) {
      const model = rawModel.trim()
      if (model && !model.includes('*')) mapping[model] = model
    }
  }
  if (mode === 'mapping' || mode === 'combined') {
    for (const row of modelMappings) {
      const from = row.from.trim()
      const to = row.to.trim()
      if (!from || !to || !isValidWildcardPattern(from) || to.includes('*')) continue
      mapping[from] = to
    }
  }
  return Object.keys(mapping).length > 0 ? mapping : null
}
