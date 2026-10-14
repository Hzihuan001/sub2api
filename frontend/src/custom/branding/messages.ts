import type { CustomLocaleMessages, CustomMessageMap } from '../registry'
import { getMessageAtKey } from '../messages'

// These are existing public display fields, not a scan of every locale string.
export const brandedDisplayKeys = [
  'setup.title',
  'setup.description',
  'keys.useKeyModal.grok.description',
  'keys.useKeyModal.grok.claudeDescription',
  'keys.useKeyModal.grok.codexDescription',
  'keys.useKeyModal.grok.codexConfigTomlHint',
  'admin.groups.openaiLive.hint',
  'admin.groups.openaiLive.unsupportedMessage',
  'admin.accounts.usageWindowsHint',
  'admin.accounts.upstreamBilling.trustWarning',
  'admin.accounts.usageWindow.grokFreeQuota24hHint',
  'admin.accounts.poolModeInfo',
  'admin.settings.linuxdo.description',
  'admin.settings.dingtalk.description',
  'admin.settings.scheduling.accountSchedulingThresholdsDescription',
  'admin.settings.upstreamBillingProbe.description',
  'admin.settings.site.siteNamePlaceholder',
  'admin.settings.payment.easypayCustomMethodsHint',
  'admin.settings.smtp.fromNamePlaceholder',
  'admin.settings.openaiFastPolicy.userIdsHint',
  'admin.plugins.runtimeNotice',
  'admin.plugins.emptyHint',
  'admin.plugins.currentVersion',
  'admin.plugins.confirmDisable',
  'admin.plugins.confirmUntested',
  'onboarding.admin.welcome.title',
  'onboarding.admin.welcome.description',
  'onboarding.admin.groupManage.description',
  'onboarding.user.welcome.title',
  'onboarding.user.welcome.description'
] as const

// Historical rendered examples already used Moshu. Keep these exact exceptions
// so extraction does not change bucket suggestions or connectivity payloads.
export const brandedCompatibilityExampleKeys = [
  'admin.backup.r2Guide.step1.line2',
  'admin.accounts.grok.ttsTextPlaceholder',
  'admin.accounts.grok.ttsTextDefault'
] as const

// These instructions must match the existing generated MOSHU_API_KEY configs.
// This does not authorize renaming arbitrary environment-variable examples.
export const brandedCodexInstructionKeys = [
  'keys.useKeyModal.grok.codexNote',
  'keys.useKeyModal.grok.codexNoteWindows',
  'keys.useKeyModal.deepseek.codexNote',
  'keys.useKeyModal.minimax.codexNote',
  'keys.useKeyModal.composite.codexNote',
  'keys.useKeyModal.routedCodex.note'
] as const

export const brandedMessageKeys = [
  ...brandedDisplayKeys, ...brandedCompatibilityExampleKeys, ...brandedCodexInstructionKeys
] as const

export function loadBrandedMessages(host: Readonly<CustomLocaleMessages>): CustomMessageMap {
  const result: Record<string, string> = {}
  for (const key of brandedMessageKeys) {
    const value = getMessageAtKey(host, key)
    if (typeof value !== 'string') throw new Error(`Brand display key not found: ${key}`)
    result[key] = (brandedCodexInstructionKeys as readonly string[]).includes(key)
      ? value.split('SUB2API_API_KEY').join('MOSHU_API_KEY')
      : value.split('Sub2API').join('Moshu').split('sub2api').join('moshu')
  }
  return result
}
