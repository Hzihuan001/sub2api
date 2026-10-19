import type { KeyModelPolicy } from '@/types'

export const MAX_KEY_MODEL_IDS = 200
export const MAX_KEY_MODEL_ID_LENGTH = 200

export function validateKeyModelPolicy(policy?: KeyModelPolicy | null): boolean {
  if (!policy?.enabled) return true
  return policy.models.length > 0 && policy.models.length <= MAX_KEY_MODEL_IDS && policy.models.every((raw) => {
    const model = raw.trim()
    return model.length > 0 && [...model].length <= MAX_KEY_MODEL_ID_LENGTH && !model.includes('*') && [...model].every((character) => {
      const codePoint = character.codePointAt(0)!
      return codePoint >= 32 && (codePoint < 127 || codePoint > 159)
    })
  })
}

export function cloneKeyModelPolicy(policy?: KeyModelPolicy | null): KeyModelPolicy | null {
  return policy?.enabled ? { enabled: true, models: [...policy.models] } : null
}

export function normalizeKeyModelPolicy(policy?: KeyModelPolicy | null): KeyModelPolicy | null {
  return policy?.enabled ? { enabled: true, models: [...new Set(policy.models.map((model) => model.trim()))] } : null
}
