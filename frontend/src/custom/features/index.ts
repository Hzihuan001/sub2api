import { customFeatureRegistry } from '../registry'
import { imageStudioFeature } from './imageStudio'
import { promptAuditFeature } from './promptAudit'
import { operatorRoleFeature } from './operatorRole'
import { usageExtrasFeature } from './usageExtras'
import { brandingFeature } from './branding'
import { gatewayResilienceFeature } from './gatewayResilience'
import { keyRoutingFeature } from './keyRouting'

/** Register built-in custom features once during application bootstrap. */
export function registerBuiltInCustomFeatures(): void {
  if (!customFeatureRegistry.has(keyRoutingFeature.id)) {
    customFeatureRegistry.register(keyRoutingFeature)
  }
  if (!customFeatureRegistry.has(gatewayResilienceFeature.id)) {
    customFeatureRegistry.register(gatewayResilienceFeature)
  }
  if (!customFeatureRegistry.has(brandingFeature.id)) {
    customFeatureRegistry.register(brandingFeature)
  }
  if (!customFeatureRegistry.has(promptAuditFeature.id)) {
    customFeatureRegistry.register(promptAuditFeature)
  }
  if (!customFeatureRegistry.has(imageStudioFeature.id)) {
    customFeatureRegistry.register(imageStudioFeature)
  }
  if (!customFeatureRegistry.has(operatorRoleFeature.id)) {
    customFeatureRegistry.register(operatorRoleFeature)
  }
  if (!customFeatureRegistry.has(usageExtrasFeature.id)) {
    customFeatureRegistry.register(usageExtrasFeature)
  }
}

registerBuiltInCustomFeatures()

export { promptAuditFeature }
export { imageStudioFeature }
export { operatorRoleFeature }
export { usageExtrasFeature }
export { brandingFeature }
export { gatewayResilienceFeature }
export { keyRoutingFeature }
