import {
  customFeatureRegistry,
  type CustomFeatureRegistry,
  type CustomLocale,
  type CustomLocaleMessages
} from './registry'

function keySegments(key: string): string[] {
  const segments = key.split('.')
  if (segments.some((part) => !part || ['__proto__', 'prototype', 'constructor'].includes(part))) {
    throw new Error(`Invalid custom message key: ${key}`)
  }
  return segments
}

export function getMessageAtKey(messages: Readonly<CustomLocaleMessages>, key: string): string | CustomLocaleMessages | undefined {
  let current: string | Readonly<CustomLocaleMessages> | undefined = messages
  for (const segment of keySegments(key)) {
    if (!current || typeof current !== 'object' || !Object.prototype.hasOwnProperty.call(current, segment)) return undefined
    current = current[segment]
  }
  return current
}

function copyMessages(messages: Readonly<CustomLocaleMessages>): CustomLocaleMessages {
  return Object.fromEntries(Object.entries(messages).map(([key, value]) => [
    key, typeof value === 'string' ? value : copyMessages(value)
  ]))
}

/** Merge a complete locale before publication; rejected features cannot partially update the host. */
export async function loadCustomLocaleMessages(
  locale: CustomLocale,
  hostMessages: Readonly<CustomLocaleMessages>,
  registry: CustomFeatureRegistry = customFeatureRegistry
): Promise<CustomLocaleMessages> {
  const merged = copyMessages(hostMessages)
  const claimedKeys = new Set<string>()
  for (const feature of registry.features()) {
    const loader = feature.messages?.[locale]
    if (!loader) continue
    const additions = await loader(copyMessages(hostMessages))
    const overrides = new Set(feature.messageOverrides ?? [])
    for (const [key, value] of Object.entries(additions)) {
      const segments = keySegments(key)
      if (typeof value !== 'string' || !value.trim()) throw new Error(`Empty custom message: ${key}`)
      if (claimedKeys.has(key)) throw new Error(`Duplicate custom message: ${key}`)
      const existing = getMessageAtKey(hostMessages, key)
      if (existing !== undefined) {
        if (typeof existing !== 'string' || !overrides.has(key)) throw new Error(`Undeclared custom message override: ${key}`)
      } else {
        if (overrides.has(key)) throw new Error(`Custom message override target not found: ${key}`)
        if (!feature.i18nNamespace || !key.startsWith(`${feature.i18nNamespace}.`)) {
          throw new Error(`Custom message outside declared namespace: ${key}`)
        }
      }
      let parent = merged
      for (const segment of segments.slice(0, -1)) {
        if (!Object.prototype.hasOwnProperty.call(parent, segment)) parent[segment] = {}
        if (typeof parent[segment] === 'string') throw new Error(`Custom message conflicts with parent: ${key}`)
        parent = (parent[segment] ??= {}) as CustomLocaleMessages
      }
      const leaf = segments[segments.length - 1]
      if (Object.prototype.hasOwnProperty.call(parent, leaf) && typeof parent[leaf] !== 'string') throw new Error(`Custom message conflicts with namespace: ${key}`)
      parent[leaf] = value
      claimedKeys.add(key)
    }
  }
  return merged
}
