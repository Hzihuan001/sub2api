import { describe, expect, it } from 'vitest'
import { createCustomFeatureRegistry, type CustomFeatureManifest } from '../registry'
import { loadCustomLocaleMessages } from '../messages'

const host = { host: { title: 'Official title', untouched: 'Official value' } }
function registryFor(overrides: Partial<CustomFeatureManifest> = {}) {
  const registry = createCustomFeatureRegistry()
  registry.register({
    id: 'probe', i18nNamespace: 'custom.probe',
    messages: { en: async () => ({ 'custom.probe.title': 'Feature title' }) },
    ...overrides
  })
  return registry
}

describe('custom locale contribution contract', () => {
  it('keeps an empty registry and absent locale contributions identical to the host', async () => {
    expect(await loadCustomLocaleMessages('en', host, createCustomFeatureRegistry())).toEqual(host)
    expect(await loadCustomLocaleMessages('zh', host, registryFor())).toEqual(host)
  })

  it('loads declared namespaced additions without mutating the source locale', async () => {
    const result = await loadCustomLocaleMessages('en', host, registryFor())
    expect(result).toEqual({ ...host, custom: { probe: { title: 'Feature title' } } })
    expect(host).toEqual({ host: { title: 'Official title', untouched: 'Official value' } })
  })

  it('permits only the exact declared existing leaf override', async () => {
    const registry = registryFor({
      messageOverrides: ['host.title'],
      messages: { en: async () => ({ 'host.title': 'Explicit override' }) }
    })
    expect(await loadCustomLocaleMessages('en', host, registry)).toEqual({ host: { title: 'Explicit override', untouched: 'Official value' } })
    expect(host.host.title).toBe('Official title')
  })

  it.each([
    { key: 'host.title', overrides: [], error: 'Undeclared custom message override' },
    { key: 'host.title', overrides: ['host.*'], error: 'Undeclared custom message override' },
    { key: 'host.deleted', overrides: ['host.deleted'], error: 'override target not found' },
    { key: 'outside.title', overrides: [], error: 'outside declared namespace' },
    { key: 'custom.probe.__proto__.polluted', overrides: [], error: 'Invalid custom message key' },
  ])('rejects invalid contribution $key without publishing partial changes', async ({ key, overrides, error }) => {
    const registry = registryFor({ messageOverrides: overrides, messages: { en: async () => ({
      'custom.probe.good': 'Would otherwise be added', [key]: 'Rejected'
    }) } })
    await expect(loadCustomLocaleMessages('en', host, registry)).rejects.toThrow(error)
    expect(host).toEqual({ host: { title: 'Official title', untouched: 'Official value' } })
    expect(({} as Record<string, unknown>).polluted).toBeUndefined()
  })

  it('rejects collisions between features even if both declare the same override', async () => {
    const registry = registryFor({ messageOverrides: ['host.title'], messages: { en: async () => ({ 'host.title': 'One' }) } })
    registry.register({ id: 'second', messageOverrides: ['host.title'], messages: { en: async () => ({ 'host.title': 'Two' }) } })
    await expect(loadCustomLocaleMessages('en', host, registry)).rejects.toThrow('Duplicate custom message: host.title')
  })

  it('rejects parent/child namespace clobbering', async () => {
    const registry = registryFor({ messages: { en: async () => ({
      'custom.probe.title': 'Leaf', 'custom.probe.title.child': 'Invalid child'
    }) } })
    await expect(loadCustomLocaleMessages('en', host, registry)).rejects.toThrow('conflicts with parent')
  })
})
