/** Canonicalize the system-wide model scope; never silently broaden invalid input. */
export function normalizeCodexTurnStateModels(raw: string): string {
  const encoder = new TextEncoder()
  const seen = new Set<string>()
  for (const part of raw.trim().split(/[,\r\n]/)) {
    const model = part.trim().toLowerCase()
    if (!model) continue
    // Keep the same model-character whitelist as the backend; '*' by itself
    // is a valid all-model prefix, but wildcards elsewhere are not accepted.
    if (!/^[a-z0-9_.:/-]*\*?$/.test(model)) {
      throw new Error('invalid_models')
    }
    seen.add(model)
  }
  const value = [...seen].join(',')
  if (encoder.encode(value).length > 1024) throw new Error('invalid_models')
  return value
}
