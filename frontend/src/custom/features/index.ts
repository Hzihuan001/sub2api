import { customFeatureRegistry } from '../registry'
import { imageStudioFeature } from './imageStudio'
import { promptAuditFeature } from './promptAudit'
import { operatorRoleFeature } from './operatorRole'
import { usageExtrasFeature } from './usageExtras'

/** Register built-in custom features once during application bootstrap. */
export function registerBuiltInCustomFeatures(): void {
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
