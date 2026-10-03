import { customFeatureRegistry } from '../registry'
import { promptAuditFeature } from './promptAudit'

/** Register built-in custom features once during application bootstrap. */
export function registerBuiltInCustomFeatures(): void {
  if (!customFeatureRegistry.has(promptAuditFeature.id)) {
    customFeatureRegistry.register(promptAuditFeature)
  }
}

registerBuiltInCustomFeatures()

export { promptAuditFeature }

