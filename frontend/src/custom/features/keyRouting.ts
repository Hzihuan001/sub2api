import type { CustomFeatureManifest } from '../registry'

/** Key-owned editor; contributes translations without a separate route or menu. */
export const keyRoutingFeature: CustomFeatureManifest = {
  id: 'key-routing',
  i18nNamespace: 'custom.keyRouting',
  messages: {
    zh: async () => {
      const { zh, prefixedMessages } = await import('@/features/key-routing/messages')
      return prefixedMessages(zh)
    },
    en: async () => {
      const { en, prefixedMessages } = await import('@/features/key-routing/messages')
      return prefixedMessages(en)
    }
  }
}
