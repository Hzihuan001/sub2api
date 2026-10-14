import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { CustomFeatureRegistry } from '../registry'

let registry: CustomFeatureRegistry
beforeEach(async () => {
  vi.resetModules()
  // Match vite.config.ts: the production runtime-only build enables its CSP-safe JIT.
  vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true)
  localStorage.clear()
  localStorage.setItem('sub2api_locale', 'en')
  registry = (await import('@/custom')).customFeatureRegistry
})
afterEach(() => {
  registry.unregister('locale-host-probe')
  vi.unstubAllGlobals()
})

describe('real i18n host message loading', () => {
  it('loads registered additions and explicit overrides through the real locale loader', async () => {
    registry.register({
      id: 'locale-host-probe', i18nNamespace: 'custom.localeProbe', messageOverrides: ['common.save'],
      messages: { en: async () => ({
        'custom.localeProbe.title': 'Registered message',
        'custom.localeProbe.echo': 'URL: {url}',
        'common.save': 'Explicit save override'
      }) }
    })
    const { i18n, initI18n } = await import('@/i18n')
    await initI18n()
    expect(i18n.global.t('custom.localeProbe.title')).toBe('Registered message')
    expect(i18n.global.t('common.save')).toBe('Explicit save override')
    expect(i18n.global.t('setup.title')).toBe('Moshu Setup')
    expect(i18n.global.t('custom.localeProbe.echo', { url: 'https://Sub2API.example/sub2api' })).toBe('URL: https://Sub2API.example/sub2api')
    expect(localStorage.getItem('sub2api_locale')).toBe('en')
    expect(localStorage.getItem('moshu_locale')).toBeNull()
  })

  it('rejects an undeclared override without marking a failed locale as loaded', async () => {
    registry.register({ id: 'locale-host-probe', messages: { en: async () => ({ 'common.save': 'Rejected' }) } })
    const { i18n, loadLocaleMessages } = await import('@/i18n')
    await expect(loadLocaleMessages('en')).rejects.toThrow('Undeclared custom message override: common.save')
    expect(i18n.global.te('setup.title')).toBe(false)
    registry.unregister('locale-host-probe')
    await loadLocaleMessages('en')
    expect(i18n.global.t('common.save')).toBe('Save')
    expect(i18n.global.t('setup.title')).toBe('Moshu Setup')
  })
})
