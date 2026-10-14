import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import { brandedMessageKeys, loadBrandedMessages } from '../branding/messages'
import { brandingFeature } from '../features/branding'
import { getMessageAtKey, loadCustomLocaleMessages } from '../messages'
import { createCustomFeatureRegistry, type CustomLocaleMessages } from '../registry'

function leaves(messages: CustomLocaleMessages, prefix = ''): [string, string][] {
  return Object.entries(messages).flatMap(([key, value]) => {
    const path = prefix ? `${prefix}.${key}` : key
    return typeof value === 'string' ? [[path, value] as [string, string]] : leaves(value, path)
  })
}

describe.each([['en', en], ['zh', zh]] as const)('explicit brand overlay: %s', (locale, messages) => {
  it('preserves every existing translated value from the former branding behavior', async () => {
    const registry = createCustomFeatureRegistry()
    registry.register(brandingFeature)
    const result = await loadCustomLocaleMessages(locale, messages, registry)
    for (const [key, value] of leaves(messages)) {
      const historical = value.replace(/Sub2API/g, 'Moshu').replace(/(?<![\\/._-])sub2api\b/g, 'moshu').replace(/SUB2API_API_KEY/g, 'MOSHU_API_KEY')
      expect(getMessageAtKey(result, key), key).toBe(historical)
    }
    expect(Object.keys(loadBrandedMessages(messages))).toEqual([...brandedMessageKeys])
  })

  it('does not rewrite new technical URLs, examples, storage ids or unknown display fields', async () => {
    const host = { ...messages, technicalProbe: {
      url: 'https://Sub2API.example/sub2api/v1',
      example: 'export SUB2API_API_KEY=example; sub2api-backups',
      storage: 'sub2api_locale',
      unknownDisplay: 'Sub2API is not an approved display key'
    } }
    const registry = createCustomFeatureRegistry()
    registry.register(brandingFeature)
    const result = await loadCustomLocaleMessages(locale, host, registry)
    expect(result.technicalProbe).toEqual(host.technicalProbe)
    expect(getMessageAtKey(result, 'keys.useKeyModal.grok.codexNote')).toContain('MOSHU_API_KEY')
    expect(getMessageAtKey(result, 'admin.accounts.grok.ttsTextDefault')).toBe('Hello from Moshu account connectivity test.')
  })

  it('fails when an explicitly covered host key disappears', () => {
    const host = { ...messages, setup: {} }
    expect(() => loadBrandedMessages(host)).toThrow('Brand display key not found: setup.title')
  })
})
