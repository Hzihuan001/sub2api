import type { CustomFeatureManifest } from '../registry'

/** Group-owned editor only; this feature adds no route or standalone menu. */
export const gatewayResilienceFeature: CustomFeatureManifest = {
  id: 'gateway-resilience',
  i18nNamespace: 'custom.gatewayResilience',
  messages: {
    zh: async () => {
      const { zh, prefixedMessages } = await import('@/features/gateway-resilience/messages')
      return prefixedMessages(zh)
    },
    en: async () => {
      const { en, prefixedMessages } = await import('@/features/gateway-resilience/messages')
      return prefixedMessages(en)
    }
  }
}
