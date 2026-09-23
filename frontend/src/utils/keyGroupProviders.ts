import type { GroupPlatform } from '@/types'

export type KeyGroupProvider = 'anthropic' | 'openai' | 'other'

export const KEY_GROUP_PROVIDERS = ['anthropic', 'openai', 'other'] as const

// Only expose the platforms retained by the custom panel. Legacy records must
// not turn a removed provider back into a selectable group.
const PROVIDER_BY_PLATFORM: Partial<Record<GroupPlatform, KeyGroupProvider>> = {
  anthropic: 'anthropic',
  openai: 'openai',
  grok: 'other',
  composite: 'other'
}

export function getKeyGroupProvider(platform: GroupPlatform): KeyGroupProvider | null {
  return PROVIDER_BY_PLATFORM[platform] ?? null
}

export const KEY_GROUP_PROVIDER_ICONS: Record<KeyGroupProvider, GroupPlatform[]> = {
  anthropic: ['anthropic'],
  openai: ['openai'],
  other: ['anthropic', 'openai', 'grok']
}
