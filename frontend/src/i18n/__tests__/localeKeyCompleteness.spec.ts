import { beforeAll, describe, expect, it } from 'vitest'
import { createCustomFeatureRegistry, loadCustomLocaleMessages, type CustomLocaleMessages } from '@/custom'

import en from '../locales/en'
import zh from '../locales/zh'

type LocaleValue = Record<string, unknown>

function flattenLeafKeys(value: unknown, prefix = ''): string[] {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    return prefix ? [prefix] : []
  }

  return Object.entries(value as LocaleValue).flatMap(([key, child]) => {
    const path = prefix ? `${prefix}.${key}` : key
    return flattenLeafKeys(child, path)
  })
}

function collectStaticSourceKeys(source: string): string[] {
  const keys = new Set<string>()

  // Covers useI18n().t(), the global $t(), and direct i18n.t() calls in both
  // TypeScript and Vue script/template source. Dynamic suffixes are checked by
  // the runtime type of the value and cannot be proven from source text alone.
  const translationCalls = /(?:\bi18n\.t|\$t|\bt)\s*\(\s*(['"])([^'"\r\n]+)\1/g
  for (const match of source.matchAll(translationCalls)) {
    const key = match[2]
    if (!key.endsWith('.')) {
      keys.add(key)
    }
  }

  // Router metadata and i18n-t are key references without a t() call.
  const keyReferences = /(?:keypath|titleKey|descriptionKey)\s*[:=]\s*(['"])([^'"\r\n]+)\1/g
  for (const match of source.matchAll(keyReferences)) {
    keys.add(match[2])
  }

  const i18nTKeypaths = /<i18n-t\b[^>]*\bkeypath\s*=\s*(['"])([^'"\r\n]+)\1/gi
  for (const match of source.matchAll(i18nTKeypaths)) {
    keys.add(match[2])
  }

  return [...keys]
}

function sourceKeys(): string[] {
  const sourceFiles = import.meta.glob('../../**/*.{ts,vue}', {
    query: '?raw',
    import: 'default',
    eager: true
  }) as Record<string, string>

  return Object.entries(sourceFiles)
    .filter(([path]) => !path.includes('/__tests__/') && !/\.(spec|test)\.ts$/.test(path))
    .flatMap(([, source]) => collectStaticSourceKeys(source))
}

function missingKeys(usedKeys: string[], availableKeys: Set<string>): string[] {
  return usedKeys.filter((key) => !availableKeys.has(key)).sort()
}

describe('locale key completeness', () => {
  let messages: { en: CustomLocaleMessages; zh: CustomLocaleMessages }
  let enKeys: Set<string>
  let zhKeys: Set<string>
  const usedKeys = [...new Set(sourceKeys())].sort()

  beforeAll(async () => {
    // Match the production locale loader, including registered feature additions
    // and validated overrides. Missing feature translations remain gate failures.
    const [english, chinese] = await Promise.all([
      loadCustomLocaleMessages('en', en),
      loadCustomLocaleMessages('zh', zh)
    ])
    messages = { en: english, zh: chinese }
    enKeys = new Set(flattenLeafKeys(english))
    zhKeys = new Set(flattenLeafKeys(chinese))
  })

  it('keeps English and Chinese locale schemas identical', () => {
    expect([...enKeys].filter((key) => !zhKeys.has(key)).sort()).toEqual([])
    expect([...zhKeys].filter((key) => !enKeys.has(key)).sort()).toEqual([])
  })

  it('contains a non-empty message for every locale leaf', () => {
    for (const [locale, localeMessages] of Object.entries(messages)) {
      const emptyKeys = flattenLeafKeys(localeMessages).filter((key) => {
        let current: unknown = localeMessages
        for (const segment of key.split('.')) {
          current = (current as LocaleValue)[segment]
        }
        return typeof current !== 'string' || current.trim() === ''
      })
      expect(emptyKeys, `${locale} has empty or non-string messages`).toEqual([])
    }
  })

  it('contains every statically referenced production key', () => {
    expect(missingKeys(usedKeys, enKeys), 'English locale is missing referenced keys').toEqual([])
    expect(missingKeys(usedKeys, zhKeys), 'Chinese locale is missing referenced keys').toEqual([])
  })

  it('still detects missing feature messages and missing locale contributions', async () => {
    const registry = createCustomFeatureRegistry()
    registry.register({
      id: 'locale-completeness-probe',
      i18nNamespace: 'custom.localeCompleteness',
      messages: { en: async () => ({ 'custom.localeCompleteness.title': 'Feature title' }) }
    })
    const probeKeys = collectStaticSourceKeys("t('custom.localeCompleteness.title'); t('custom.localeCompleteness.missing')")
    const english = await loadCustomLocaleMessages('en', en, registry)
    const chinese = await loadCustomLocaleMessages('zh', zh, registry)
    expect(missingKeys(probeKeys, new Set(flattenLeafKeys(english)))).toEqual(['custom.localeCompleteness.missing'])
    expect(missingKeys(probeKeys, new Set(flattenLeafKeys(chinese)))).toEqual([
      'custom.localeCompleteness.missing',
      'custom.localeCompleteness.title'
    ])
  })
})
