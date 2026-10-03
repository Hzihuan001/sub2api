import { customFeatureRegistry } from '../registry'
import { imageStudioFeature } from './imageStudio'
import { promptAuditFeature } from './promptAudit'

/** Register built-in custom features once during application bootstrap. */
export function registerBuiltInCustomFeatures(): void {
  if (!customFeatureRegistry.has(promptAuditFeature.id)) {
    customFeatureRegistry.register(promptAuditFeature)
  }
  if (!customFeatureRegistry.has(imageStudioFeature.id)) {
    customFeatureRegistry.register(imageStudioFeature)
  }
}

registerBuiltInCustomFeatures()

export { promptAuditFeature }
export { imageStudioFeature }
