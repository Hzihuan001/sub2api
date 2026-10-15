import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { Group } from '@/types'
import KeyBackupGroupsEditor from '../KeyBackupGroupsEditor.vue'
import { availableBackupGroups, validateKeyRouting } from '../keyRouting'
import { en, zh } from '../messages'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({
    t: (key: string, values: Record<string, unknown> = {}) =>
      (en[key.replace('custom.keyRouting.', '') as keyof typeof en] ?? key)
        .replace(/\{(\w+)\}/g, (_, name: string) => String(values[name] ?? ''))
  })
}))

const group = (id: number, overrides: Partial<Group> = {}): Group => ({
  id, name: `Group ${id}`, platform: 'openai', status: 'active', subscription_type: 'standard',
  rate_multiplier: 1, claude_code_only: false, ...overrides
}) as Group
const groups = [group(1), group(2), group(3, { platform: 'anthropic' }), group(4), group(5), group(6), group(7, { subscription_type: 'subscription' }), group(8, { status: 'inactive' }), group(9, { claude_code_only: true })]

function mountEditor(ids: number[] = [], primaryGroupId: number | null = 1) {
  return mount(KeyBackupGroupsEditor, {
    props: { modelValue: ids, primaryGroupId, groups },
    global: {
      stubs: {
        GroupBadge: { props: ['name'], template: '<span>{{ name }}</span>' },
        GroupOptionItem: true,
        Select: { name: 'Select', props: ['modelValue', 'options', 'disabled'], emits: ['update:modelValue'], template: '<div />' }
      }
    }
  })
}

describe('Key backup groups', () => {
  it('offers only accessible active standard non-Claude-Code-only groups, excluding the primary and existing backups', () => {
    expect(availableBackupGroups(groups, 1, [4, 2]).map(({ id }) => id)).toEqual([3, 5, 6])
    expect(validateKeyRouting(1, [99], groups)).toBe('groupUnavailable')
    expect(validateKeyRouting(1, [8], groups)).toBe('groupUnavailable')
    expect(validateKeyRouting(1, [7], groups)).toBe('standardOnly')
    expect(validateKeyRouting(7, [1], groups)).toBe('standardOnly')
    expect(validateKeyRouting(1, [9], groups)).toBe('claudeCodeOnly')
    expect(validateKeyRouting(9, [1], groups)).toBe('claudeCodeOnly')
    expect(validateKeyRouting(1, [1], groups)).toBe('duplicate')
    expect(validateKeyRouting(1, [2, 2], groups)).toBe('duplicate')
    expect(validateKeyRouting(1, [2, 3, 4, 5, 6], groups)).toBe('tooMany')
    expect(validateKeyRouting(null, [2], groups)).toBe('primaryRequired')
    expect(validateKeyRouting(1, [3, 2], groups)).toBeNull()
  })

  it('keeps legacy single subscription groups valid', () => {
    expect(validateKeyRouting(7, [], groups)).toBeNull()
    const wrapper = mountEditor([], 7)
    expect(wrapper.get('[data-test="backup-subscription-hint"]').text()).toContain('cannot be combined')
    expect(wrapper.find('[data-test="backup-select"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="backup-error"]').exists()).toBe(false)
  })

  it('keeps a single Claude Code-only group valid without allowing backup additions', () => {
    expect(validateKeyRouting(9, [], groups)).toBeNull()
    const wrapper = mountEditor([], 9)
    expect(wrapper.get('[data-test="backup-claude-code-hint"]').text()).toContain('cannot have backup groups')
    expect(wrapper.find('[data-test="backup-select"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="backup-error"]').exists()).toBe(false)
  })

  it.each([1, 2])('preserves existing routes when group %i becomes Claude Code-only and requires explicit repair', async (changedGroupId) => {
    const ids = [2, 3]
    const wrapper = mountEditor(ids)
    const updatedGroups = groups.map((item) => item.id === changedGroupId ? { ...item, claude_code_only: true } : item)
    await wrapper.setProps({ groups: updatedGroups })
    expect(wrapper.get('[data-test="backup-error"]').text()).toContain('Replace that group or clear all backup groups before saving')
    expect(wrapper.props('modelValue')).toEqual([2, 3])
    expect(wrapper.findAll('[data-backup-id]').map((row) => row.attributes('data-backup-id'))).toEqual(['2', '3'])
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(validateKeyRouting(1, ids, updatedGroups)).toBe('claudeCodeOnly')
    if (changedGroupId === 1) {
      expect(wrapper.find('[data-test="backup-select"]').exists()).toBe(false)
      await wrapper.setProps({ primaryGroupId: 4 })
    } else {
      await wrapper.findAll('[data-test="backup-remove"]')[0].trigger('click')
      expect(wrapper.emitted('update:modelValue')).toEqual([[[3]]])
      await wrapper.setProps({ modelValue: [3] })
    }
    expect(wrapper.find('[data-test="backup-error"]').exists()).toBe(false)
    expect(ids).toEqual([2, 3])
  })

  it('rejects switching an existing route to a Claude Code-only primary until backups are cleared', async () => {
    const wrapper = mountEditor([2])
    const select = wrapper.findComponent({ name: 'Select' })
    await wrapper.setProps({ primaryGroupId: 9 })
    expect(wrapper.get('[data-test="backup-error"]').text()).toContain('Claude Code-only group')
    expect(wrapper.find('[data-test="backup-select"]').exists()).toBe(false)
    select.vm.$emit('update:modelValue', 3)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.get('[data-test="backup-remove"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[[]]])
    await wrapper.setProps({ modelValue: [] })
    expect(wrapper.find('[data-test="backup-error"]').exists()).toBe(false)
  })

  it('adds groups from another platform in selection order and rejects unlisted IDs', async () => {
    const wrapper = mountEditor([2])
    const select = wrapper.findComponent({ name: 'Select' })
    select.vm.$emit('update:modelValue', 3)
    expect(wrapper.emitted('update:modelValue')).toEqual([[[2, 3]]])
    await wrapper.setProps({ modelValue: [2, 3] })
    for (const id of [1, 2, 3, 7, 8, 9, 99]) select.vm.$emit('update:modelValue', id)
    expect(wrapper.emitted('update:modelValue')).toHaveLength(1)
  })

  it('moves and removes backups without mutating the supplied route', async () => {
    const ids = [2, 3, 4]
    const wrapper = mountEditor(ids)
    expect(wrapper.findAll<HTMLButtonElement>('[data-test="backup-up"]')[0].element.disabled).toBe(true)
    expect(wrapper.findAll<HTMLButtonElement>('[data-test="backup-down"]')[2].element.disabled).toBe(true)
    await wrapper.findAll('[data-test="backup-up"]')[1].trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[3, 2, 4]])
    await wrapper.setProps({ modelValue: [3, 2, 4] })
    await wrapper.findAll('[data-test="backup-down"]')[0].trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[2, 3, 4]])
    await wrapper.findAll('[data-test="backup-remove"]')[1].trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[3, 4]])
    expect(ids).toEqual([2, 3, 4])
  })

  it('enforces four backups and still permits removal', async () => {
    const wrapper = mountEditor([2, 3, 4, 5])
    expect(wrapper.find('[data-test="backup-select"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('limit of 4')
    await wrapper.findAll('[data-test="backup-remove"]')[0].trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[[3, 4, 5]]])
  })

  it('preserves unavailable selections for explicit removal and clearing', async () => {
    const wrapper = mountEditor([99])
    expect(wrapper.text()).toContain('Group #99 (unavailable)')
    expect(wrapper.get('[data-test="backup-error"]').text()).toContain('unavailable or inactive')
    await wrapper.get('[data-test="backup-remove"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[[]]])
  })

  it('does not silently change backups after changing the primary to a selected backup', async () => {
    const wrapper = mountEditor([2, 3])
    await wrapper.setProps({ primaryGroupId: 2 })
    expect(wrapper.get('[data-test="backup-error"]').text()).toContain('must all be different')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.props('modelValue')).toEqual([2, 3])
  })

  it('disables every edit while saving and explains actual-group billing and supported scope', async () => {
    const wrapper = mountEditor([2, 3])
    await wrapper.setProps({ disabled: true })
    wrapper.findComponent({ name: 'Select' }).vm.$emit('update:modelValue', 4)
    expect(wrapper.findAll<HTMLButtonElement>('button').every((button) => button.element.disabled)).toBe(true)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.text()).toContain('group that actually handles the request')
    expect(wrapper.text()).toContain('stateless text HTTP / SSE')
  })

  it('has matching English and Chinese message keys', () => {
    expect(Object.keys(en).sort()).toEqual(Object.keys(zh).sort())
  })
})
