import { describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick, ref } from 'vue'
import { mount, type VueWrapper } from '@vue/test-utils'
import type { Group } from '@/types'
import KeyBackupGroupsEditor from '../KeyBackupGroupsEditor.vue'
import { availableBackupGroups, validateKeyRouting } from '../keyRouting'
import { en, zh } from '../messages'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string, values: Record<string, unknown> = {}) =>
    (en[key.replace('custom.keyRouting.', '') as keyof typeof en] ?? key).replace(/\{(\w+)\}/g, (_, name: string) => String(values[name] ?? '')) })
}))

const group = (id: number, overrides: Partial<Group> = {}): Group => ({
  id, name: `Group ${id}`, platform: 'openai', status: 'active', subscription_type: 'standard', rate_multiplier: 1, claude_code_only: false, ...overrides
}) as Group
const groups = [group(1), group(2), group(3, { platform: 'anthropic' }), group(4), group(5), group(6), group(7, { subscription_type: 'subscription' }), group(8, { status: 'inactive' }), group(9, { claude_code_only: true })]
const stubs = {
  GroupOptionItem: { name: 'GroupOptionItem', props: ['name', 'description', 'rateMultiplier', 'userRateMultiplier', 'showCheckmark'], template: '<div><span>{{ name }}</span><span v-if="description">{{ description }}</span></div>' },
  KeyGroupPicker: { name: 'KeyGroupPicker', props: ['show', 'title', 'groups', 'selectedGroupId', 'userGroupRates', 'disabled', 'swapHint'], emits: ['select', 'close'], template: '<div v-if="show" data-test="picker-stub"></div>' }
}
const picker = (wrapper: VueWrapper) => wrapper.findComponent({ name: 'KeyGroupPicker' })
const candidates = (wrapper: VueWrapper) => picker(wrapper).props('groups').map((item: Group) => item.id)
const choose = async (wrapper: VueWrapper, id: number) => { picker(wrapper).vm.$emit('select', id); await nextTick() }
function mountEditor(ids: number[] = [], primaryGroupId: number | null = 1) {
  return mount(KeyBackupGroupsEditor, { props: { modelValue: ids, primaryGroupId, groups }, global: { stubs } })
}

describe('Key backup groups shared picker', () => {
  it('validates active authorized standard non-Claude-Code routes without changing single-group compatibility', () => {
    expect(availableBackupGroups(groups, 1, [4, 2]).map(({ id }) => id)).toEqual([3, 5, 6])
    for (const id of [8, 99]) expect(validateKeyRouting(1, [id], groups)).toBe('groupUnavailable')
    expect(validateKeyRouting(1, [7], groups)).toBe('standardOnly')
    expect(validateKeyRouting(7, [1], groups)).toBe('standardOnly')
    expect(validateKeyRouting(1, [9], groups)).toBe('claudeCodeOnly')
    expect(validateKeyRouting(9, [1], groups)).toBe('claudeCodeOnly')
    expect(validateKeyRouting(1, [1], groups)).toBe('duplicate')
    expect(validateKeyRouting(1, [2, 2], groups)).toBe('duplicate')
    expect(validateKeyRouting(1, [2, 3, 4, 5, 6], groups)).toBe('tooMany')
    expect(validateKeyRouting(null, [2], groups)).toBe('primaryRequired')
    expect(validateKeyRouting(1, [3, 2], groups)).toBeNull()
    expect(validateKeyRouting(7, [], groups)).toBeNull()
    expect(validateKeyRouting(9, [], groups)).toBeNull()
  })

  it.each(['primary-group-name', 'primary-edit'])('opens the searchable picker in one click from %s', async (selector) => {
    const wrapper = mountEditor([2])
    expect(picker(wrapper).props('show')).toBe(false)
    await wrapper.get(`[data-test="${selector}"]`).trigger('click')
    expect(picker(wrapper).props('show')).toBe(true)
    expect(picker(wrapper).props('selectedGroupId')).toBe(1)
    expect(wrapper.find('select').exists()).toBe(false)
    expect(candidates(wrapper)).toEqual([1, 2, 3, 4, 5, 6])
  })

  it.each(['backup-group-name', 'backup-edit'])('opens the same picker in one click from %s', async (selector) => {
    const wrapper = mountEditor([2, 3])
    await wrapper.findAll(`[data-test="${selector}"]`)[1].trigger('click')
    expect(picker(wrapper).props('show')).toBe(true)
    expect(picker(wrapper).props('selectedGroupId')).toBe(3)
    expect(picker(wrapper).props('swapHint')).toBe(true)
  })

  it('opens the shared picker for an empty primary card and allows legacy single-group choices', async () => {
    const wrapper = mountEditor([], null)
    await wrapper.get('[data-test="primary-select"]').trigger('click')
    expect(candidates(wrapper)).toEqual([1, 2, 3, 4, 5, 6, 7, 9])
    await choose(wrapper, 7)
    expect(wrapper.emitted('update:primaryGroupId')?.at(-1)).toEqual([7])
    expect(picker(wrapper).props('show')).toBe(false)
  })

  it('adds a group through one picker click, excluding used and incompatible groups', async () => {
    const wrapper = mountEditor([2])
    expect(wrapper.findAll('[data-test="backup-add"]')).toHaveLength(1)
    await wrapper.get('[data-test="backup-add"]').trigger('click')
    expect(picker(wrapper).props('show')).toBe(true)
    expect(picker(wrapper).props('selectedGroupId')).toBeNull()
    expect(candidates(wrapper)).toEqual([3, 4, 5, 6])
    for (const id of [1, 2, 7, 8, 9, 99]) await choose(wrapper, id)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await choose(wrapper, 3)
    expect(wrapper.emitted('update:modelValue')).toEqual([[[2, 3]]])
    expect(picker(wrapper).props('show')).toBe(false)
  })

  it.each([7, 9])('keeps existing single restricted group %i without offering backups', (id) => {
    const wrapper = mountEditor([], id)
    expect(wrapper.find('[data-test="backup-add"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="backup-error"]').exists()).toBe(false)
    expect(wrapper.find(`[data-test="${id === 7 ? 'backup-subscription-hint' : 'backup-claude-code-hint'}"]`).exists()).toBe(true)
  })

  it('replaces a backup in place with an unused group', async () => {
    const wrapper = mountEditor([2, 3])
    await wrapper.findAll('[data-test="backup-edit"]')[0].trigger('click')
    await choose(wrapper, 4)
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[4, 3]])
    expect(wrapper.emitted('update:primaryGroupId')).toBeUndefined()
  })

  it('safely swaps primary and backup, or two backups, with a real parent v-model', async () => {
    const primary = ref<number | null>(1)
    const backups = ref([2, 3])
    const Parent = defineComponent({ components: { KeyBackupGroupsEditor }, setup: () => ({ primary, backups, groups }), template: '<KeyBackupGroupsEditor v-model="backups" v-model:primary-group-id="primary" :groups="groups" />' })
    const wrapper = mount(Parent, { global: { stubs } })
    await wrapper.get('[data-test="primary-edit"]').trigger('click')
    await choose(wrapper, 3)
    expect(primary.value).toBe(3)
    expect(backups.value).toEqual([2, 1])
    await wrapper.findAll('[data-test="backup-edit"]')[0].trigger('click')
    await choose(wrapper, 1)
    expect(backups.value).toEqual([1, 2])
    await wrapper.findAll('[data-test="backup-edit"]')[0].trigger('click')
    await choose(wrapper, 3)
    expect(primary.value).toBe(1)
    expect(backups.value).toEqual([3, 2])
    expect(new Set([primary.value, ...backups.value]).size).toBe(3)
    expect(validateKeyRouting(primary.value, backups.value, groups)).toBeNull()
    wrapper.unmount()
  })

  it('does not emit any changes when the selected group is chosen again or the picker is closed', async () => {
    const wrapper = mountEditor([2])
    await wrapper.get('[data-test="primary-edit"]').trigger('click')
    await choose(wrapper, 1)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.emitted('update:primaryGroupId')).toBeUndefined()
    await wrapper.get('[data-test="primary-edit"]').trigger('click')
    picker(wrapper).vm.$emit('close')
    await nextTick()
    expect(picker(wrapper).props('show')).toBe(false)
  })

  it('preserves inaccessible selections until explicit repair and never swaps an invalid group into another slot', async () => {
    const wrapper = mountEditor([99, 2])
    expect(wrapper.get('[data-test="backup-error"]').exists()).toBe(true)
    expect(wrapper.get('[data-backup-id="99"]').text()).toContain('Group #99')
    await wrapper.findAll('[data-test="backup-edit"]')[0].trigger('click')
    expect(candidates(wrapper)).not.toContain(1)
    expect(candidates(wrapper)).not.toContain(2)
    expect(wrapper.props('modelValue')).toEqual([99, 2])
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await choose(wrapper, 4)
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[4, 2]])
  })

  it('allows explicitly replacing or removing an unavailable primary without dropping backups', async () => {
    const wrapper = mountEditor([2], 99)
    expect(wrapper.get('[data-test="primary-group-name"]').text()).toContain('Group #99')
    await wrapper.get('[data-test="primary-edit"]').trigger('click')
    expect(candidates(wrapper)).not.toContain(2)
    await choose(wrapper, 4)
    expect(wrapper.emitted('update:primaryGroupId')?.at(-1)).toEqual([4])
    expect(wrapper.props('modelValue')).toEqual([2])
    await wrapper.get('[data-test="primary-remove"]').trigger('click')
    expect(wrapper.emitted('update:primaryGroupId')?.at(-1)).toEqual([2])
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[]])
  })

  it('revalidates a stale picker selection against current permissions and saving state', async () => {
    const wrapper = mountEditor([2])
    await wrapper.get('[data-test="backup-add"]').trigger('click')
    await wrapper.setProps({ groups: groups.filter((item) => item.id !== 3) })
    await choose(wrapper, 3)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.setProps({ disabled: true })
    await choose(wrapper, 4)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('enforces four backups while permitting removal and preserves supplied arrays', async () => {
    const ids = [2, 3, 4, 5]
    const wrapper = mountEditor(ids)
    expect(wrapper.find('[data-test="backup-add"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('limit of 4')
    await wrapper.findAll('[data-test="backup-remove"]')[0].trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[3, 4, 5]])
    expect(ids).toEqual([2, 3, 4, 5])
  })

  it('retains sorting, promotion, and explicit primary removal actions', async () => {
    const wrapper = mountEditor([2, 3, 4])
    await wrapper.findAll('[data-test="backup-up"]')[1].trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[3, 2, 4]])
    await wrapper.findAll('[data-test="backup-up"]')[0].trigger('click')
    expect(wrapper.emitted('update:primaryGroupId')?.at(-1)).toEqual([2])
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[1, 3, 4]])
    await wrapper.get('[data-test="primary-remove"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[3, 4]])
  })

  it('disables card operations while saving and retains actual-group billing wording', async () => {
    const wrapper = mountEditor([2, 3])
    await wrapper.setProps({ disabled: true })
    expect(wrapper.findAll<HTMLButtonElement>('button').every((button) => button.element.disabled)).toBe(true)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.text()).toContain('group that actually handles the request')
    expect(wrapper.text()).not.toContain('stateless text HTTP / SSE')
  })

  it('uses full-width official group rows with real descriptions and preserves zero user rates', async () => {
    const wrapper = mountEditor([2])
    const describedGroups = groups.map((item) => ({ ...item, description: `Real description for ${item.id}` }))
    await wrapper.setProps({ groups: describedGroups, userGroupRates: { 1: 0, 2: 0 } })
    const rows = wrapper.findAllComponents({ name: 'GroupOptionItem' })
    expect(rows).toHaveLength(2)
    expect(rows[0].props()).toMatchObject({ name: 'Group 1', description: 'Real description for 1', rateMultiplier: 1, userRateMultiplier: 0, showCheckmark: false })
    expect(rows[1].props()).toMatchObject({ name: 'Group 2', description: 'Real description for 2', rateMultiplier: 1, userRateMultiplier: 0, showCheckmark: false })
    for (const selector of ['primary-group-name', 'backup-group-name']) {
      expect(wrapper.get(`[data-test="${selector}"]`).classes()).toEqual(expect.arrayContaining(['flex', 'w-full', 'min-w-0']))
    }
    expect(wrapper.text()).toContain('Real description for 1')
    expect(wrapper.text()).toContain('Real description for 2')
    expect(wrapper.find('fieldset').classes()).not.toContain('border')
    expect(wrapper.find('fieldset').classes()).not.toContain('p-3')
    for (const selector of ['primary-edit', 'primary-remove', 'backup-edit', 'backup-remove']) {
      const buttonClasses = wrapper.get(`[data-test="${selector}"]`).classes()
      expect(buttonClasses).toEqual(expect.arrayContaining(['h-6', 'w-6', 'text-gray-400']))
      expect(buttonClasses).not.toContain('btn-secondary')
    }
  })

  it('has matching Chinese and English message keys', () => expect(Object.keys(en).sort()).toEqual(Object.keys(zh).sort()))
})
