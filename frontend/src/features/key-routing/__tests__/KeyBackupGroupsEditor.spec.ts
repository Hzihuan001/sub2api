import { describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick, ref } from 'vue'
import { mount, type VueWrapper } from '@vue/test-utils'
import type { Group } from '@/types'
import KeyBackupGroupsEditor from '../KeyBackupGroupsEditor.vue'
import { availableKeyRouteGroups, keyRouteFromAPI, keyRouteToAPI, MAX_ROUTE_GROUPS, moveRouteGroup, validateOrderedKeyRouting } from '../keyRouting'
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
function mountEditor(ids: number[] = []) {
  return mount(KeyBackupGroupsEditor, { props: { modelValue: ids, groups }, global: { stubs } })
}

describe('ordered API-key group list', () => {
  it('validates the complete ordered route and preserves single-group restrictions', () => {
    expect(validateOrderedKeyRouting([1, 2, 3], groups)).toBeNull()
    for (const id of [8, 99]) expect(validateOrderedKeyRouting([1, id], groups)).toBe('groupUnavailable')
    expect(validateOrderedKeyRouting([1, 7], groups)).toBe('standardOnly')
    expect(validateOrderedKeyRouting([1, 9], groups)).toBe('claudeCodeOnly')
    expect(validateOrderedKeyRouting([1, 1], groups)).toBe('duplicate')
    expect(validateOrderedKeyRouting([1, 2, 3, 4, 5, 6], groups)).toBe('tooMany')
    expect(validateOrderedKeyRouting([7], groups)).toBeNull()
    expect(validateOrderedKeyRouting([9], groups)).toBeNull()
  })

  it('maps the priority list to the compatible API fields without losing order', () => {
    expect(keyRouteFromAPI(1, [3, 2])).toEqual([1, 3, 2])
    expect(keyRouteFromAPI(null, [3, 2])).toEqual([3, 2])
    expect(keyRouteToAPI([4, 2, 3])).toEqual({ groupId: 4, backupGroupIds: [2, 3] })
    expect(keyRouteToAPI([])).toEqual({ groupId: null, backupGroupIds: [] })
  })

  it('moves any row in the priority order and does not mutate the source', () => {
    const route = [1, 2, 3]
    expect(moveRouteGroup(route, 1, -1)).toEqual([2, 1, 3])
    expect(moveRouteGroup(route, 0, 1)).toEqual([2, 1, 3])
    expect(moveRouteGroup(route, 0, -1)).toEqual([1, 2, 3])
    expect(route).toEqual([1, 2, 3])
  })

  it('shows the same priority controls on every group and promotes any row when moved up', async () => {
    const route = ref([1, 2, 3])
    const Parent = defineComponent({ components: { KeyBackupGroupsEditor }, setup: () => ({ route, groups }), template: '<KeyBackupGroupsEditor v-model="route" :groups="groups" />' })
    const wrapper = mount(Parent, { global: { stubs } })
    expect(wrapper.findAll('[data-route-group-id]')).toHaveLength(3)
    expect(wrapper.findAll('[data-test^="route-up-"]')).toHaveLength(3)
    expect(wrapper.findAll('[data-test^="route-down-"]')).toHaveLength(3)
    expect(wrapper.findAll('[data-test^="route-edit-"]')).toHaveLength(3)
    expect(wrapper.findAll('[data-test^="route-remove-"]')).toHaveLength(3)
    expect(wrapper.text()).toContain('Priority 1')
    expect(wrapper.text()).toContain('Priority 2')
    expect(wrapper.text()).toContain('Priority 3')
    await wrapper.get('[data-test="route-up-1"]').trigger('click')
    expect(route.value).toEqual([2, 1, 3])
    await wrapper.get('[data-test="route-up-2"]').trigger('click')
    expect(route.value).toEqual([2, 3, 1])
    await wrapper.get('[data-test="route-up-1"]').trigger('click')
    expect(route.value).toEqual([3, 2, 1])
    wrapper.unmount()
  })

  it('adds groups through the shared picker and excludes already selected or incompatible groups', async () => {
    const wrapper = mountEditor([1])
    await wrapper.get('[data-test="route-add"]').trigger('click')
    expect(picker(wrapper).props('show')).toBe(true)
    expect(picker(wrapper).props('selectedGroupId')).toBeNull()
    expect(candidates(wrapper)).toEqual([2, 3, 4, 5, 6])
    for (const id of [1, 7, 8, 9, 99]) await choose(wrapper, id)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await choose(wrapper, 3)
    expect(wrapper.emitted('update:modelValue')).toEqual([[[1, 3]]])
    expect(picker(wrapper).props('show')).toBe(false)
  })

  it('allows a selected group to be promoted by choosing it from another row', async () => {
    const wrapper = mountEditor([1, 2, 3])
    await wrapper.get('[data-test="route-edit-2"]').trigger('click')
    expect(picker(wrapper).props('swapHint')).toBe(true)
    expect(candidates(wrapper)).toEqual([1, 2, 3, 4, 5, 6])
    await choose(wrapper, 1)
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[3, 2, 1]])
  })

  it('replaces or removes the exact row that was selected', async () => {
    const wrapper = mountEditor([1, 2, 3])
    await wrapper.get('[data-test="route-edit-1"]').trigger('click')
    await choose(wrapper, 4)
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[1, 4, 3]])
    await wrapper.get('[data-test="route-remove-1"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[1, 3]])
  })

  it('lets a restricted single group remain but prevents combining it with other groups', async () => {
    for (const id of [7, 9]) {
      const wrapper = mountEditor([id])
      expect(wrapper.find('[data-test="route-add"]').exists()).toBe(false)
      expect(wrapper.find('[data-test="route-error"]').exists()).toBe(false)
      expect(wrapper.find('[data-test="route-subscription-hint"], [data-test="route-claude-code-hint"]').exists()).toBe(true)
      wrapper.unmount()
    }
  })

  it('preserves unavailable saved rows until the user explicitly repairs them', async () => {
    const wrapper = mountEditor([99, 2])
    expect(wrapper.get('[data-test="route-error"]').exists()).toBe(true)
    expect(wrapper.get('[data-route-group-id="99"]').text()).toContain('Group #99')
    await wrapper.get('[data-test="route-edit-0"]').trigger('click')
    expect(candidates(wrapper)).not.toContain(2)
    expect(wrapper.props('modelValue')).toEqual([99, 2])
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await choose(wrapper, 4)
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[4, 2]])
  })

  it('revalidates a stale picker selection against current permissions and saving state', async () => {
    const wrapper = mountEditor([1])
    await wrapper.get('[data-test="route-add"]').trigger('click')
    await wrapper.setProps({ groups: groups.filter((item) => item.id !== 3) })
    await choose(wrapper, 3)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.setProps({ disabled: true })
    await choose(wrapper, 4)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('enforces five total groups and preserves arrays on removal', async () => {
    const ids = [1, 2, 3, 4, 5]
    const wrapper = mountEditor(ids)
    expect(wrapper.find('[data-test="route-add"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('up to 5 groups')
    await wrapper.get('[data-test="route-remove-2"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[1, 2, 4, 5]])
    expect(ids).toEqual([1, 2, 3, 4, 5])
  })

  it('disables every row operation while saving and explains actual-group billing', async () => {
    const wrapper = mountEditor([1, 2])
    await wrapper.setProps({ disabled: true })
    expect(wrapper.findAll<HTMLButtonElement>('button').every((button) => button.element.disabled)).toBe(true)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.text()).toContain('group that actually handles the request')
    expect(wrapper.text()).not.toContain('primary group')
    expect(wrapper.text()).not.toContain('backup group')
  })

  it('uses full-width group rows with descriptions and preserves zero user rates', async () => {
    const wrapper = mountEditor([1, 2])
    const describedGroups = groups.map((item) => ({ ...item, description: `Real description for ${item.id}` }))
    await wrapper.setProps({ groups: describedGroups, userGroupRates: { 1: 0, 2: 0 } })
    const rows = wrapper.findAllComponents({ name: 'GroupOptionItem' })
    expect(rows).toHaveLength(2)
    expect(rows[0].props()).toMatchObject({ name: 'Group 1', description: 'Real description for 1', rateMultiplier: 1, userRateMultiplier: 0, showCheckmark: false })
    expect(rows[1].props()).toMatchObject({ name: 'Group 2', description: 'Real description for 2', rateMultiplier: 1, userRateMultiplier: 0, showCheckmark: false })
    for (const selector of ['route-group-name-0', 'route-group-name-1']) {
      expect(wrapper.get(`[data-test="${selector}"]`).classes()).toEqual(expect.arrayContaining(['flex', 'w-full', 'min-w-0']))
    }
    expect(wrapper.text()).toContain('Real description for 1')
    expect(wrapper.text()).toContain('Real description for 2')
  })

  it('offers no more than five ordered groups', () => {
    expect(MAX_ROUTE_GROUPS).toBe(5)
    expect(availableKeyRouteGroups(groups, [1], null).map(({ id }) => id)).toEqual([2, 3, 4, 5, 6])
  })

  it('has matching Chinese and English message keys', () => expect(Object.keys(en).sort()).toEqual(Object.keys(zh).sort()))
})
