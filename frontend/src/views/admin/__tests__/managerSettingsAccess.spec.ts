import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '../SettingsView.vue'), 'utf8')

describe('manager settings access', () => {
  it('keeps email settings visible while backup stays super-admin only', () => {
    expect(source).toContain('<div v-show="activeTab === \'email\'" class="space-y-6">')
    expect(source).toContain('allSettingsTabs.filter((tab) => tab.key !== "backup")')
    expect(source).not.toContain('tab.key !== "email" && tab.key !== "backup"')
  })
})
