import type { CustomFeatureManifest } from '../registry'
import { brandedMessageKeys, loadBrandedMessages } from '../branding/messages'

export const brandingFeature: CustomFeatureManifest = {
  id: 'branding',
  i18nNamespace: 'custom.branding',
  messageOverrides: brandedMessageKeys,
  messages: {
    en: async (host) => loadBrandedMessages(host),
    zh: async (host) => loadBrandedMessages(host)
  }
}
