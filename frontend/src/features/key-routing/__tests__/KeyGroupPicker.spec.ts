import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import type { Group } from '@/types'
import KeyGroupPicker from '../KeyGroupPicker.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))
const groups = [
  { id: 1, name: 'GPT Enterprise', description: 'Fast text service', platform: 'openai', status: 'active', subscription_type: 'standard', rate_multiplier: 0.2 },
  { id: 2, name: 'Kimi', description: '长上下文', platform: 'kimi', status: 'active', subscription_type: 'standard', rate_multiplier: 0.3 }
] as Group[]
const mounted: VueWrapper[] = []
afterEach(() => { mounted.splice(0).forEach((wrapper) => wrapper.unmount()); document.body.innerHTML = '' })
function mountPicker(show = true) {
  const wrapper = mount(KeyGroupPicker, {
    attachTo: document.body,
    props: { show, title: 'Choose group', groups, selectedGroupId: 2, userGroupRates: { 1: 0.1 } },
    global: { stubs: {
      BaseDialog: { name: 'BaseDialog', props: ['show', 'title', 'zIndex'], emits: ['close'], template: '<div v-if="show"><button data-test="close-picker" @click="$emit(\'close\')">Close</button><slot /></div>' },
      GroupOptionItem: { name: 'GroupOptionItem', props: ['name', 'description', 'selected', 'userRateMultiplier', 'rateMultiplier'], template: '<span>{{ name }} {{ description }}</span>' }
    } }
  })
  mounted.push(wrapper)
  return wrapper
}

describe('KeyGroupPicker', () => {
  it('renders actual group names, descriptions, selected state, and applicable prices', () => {
    const wrapper = mountPicker()
    expect(wrapper.findAll('[role="option"]')).toHaveLength(2)
    expect(wrapper.get('[data-group-id="2"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.findAllComponents({ name: 'GroupOptionItem' })[0].props()).toMatchObject({ name: 'GPT Enterprise', rateMultiplier: 0.2, userRateMultiplier: 0.1 })
    expect(wrapper.findComponent({ name: 'BaseDialog' }).props('zIndex')).toBe(75)
    expect(wrapper.text()).not.toMatch(/全部型号|All models/)
  })

  it.each([['gPt', '1'], ['长上下文', '2'], ['openai', '1']])('searches real group metadata using %s', async (query, id) => {
    const wrapper = mountPicker()
    await wrapper.get('[data-test="key-group-picker-search"]').setValue(query)
    expect(wrapper.findAll('[role="option"]').map((option) => option.attributes('data-group-id'))).toEqual([id])
  })

  it('shows an empty search state and resets search on the next open', async () => {
    const wrapper = mountPicker()
    await wrapper.get('[data-test="key-group-picker-search"]').setValue('No match')
    expect(wrapper.get('[data-test="key-group-picker-empty"]').text()).toContain('pickerNoResults')
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    expect(wrapper.get<HTMLInputElement>('[data-test="key-group-picker-search"]').element.value).toBe('')
    expect(wrapper.findAll('[role="option"]')).toHaveLength(2)
  })

  it('emits selection once and does not mutate supplied groups', async () => {
    const wrapper = mountPicker()
    await wrapper.get('[data-group-id="1"]').trigger('click')
    expect(wrapper.emitted('select')).toEqual([[1]])
    expect(groups.map((group) => group.id)).toEqual([1, 2])
    await wrapper.get('[data-test="close-picker"]').trigger('click')
    expect(wrapper.emitted('close')).toEqual([[]])
  })

  it('does not emit selections while disabled or hidden', async () => {
    const wrapper = mountPicker()
    await wrapper.setProps({ disabled: true })
    await wrapper.get('[data-group-id="1"]').trigger('click')
    expect(wrapper.emitted('select')).toBeUndefined()
    await wrapper.setProps({ show: false })
    expect(wrapper.find('[data-test="key-group-picker"]').exists()).toBe(false)
  })

  it('focuses search after the real dialog and closes only the picker on the first Escape', async () => {
    const outerShow = ref(true)
    const pickerShow = ref(false)
    const draft = ref('Unsaved key name')
    const Parent = defineComponent({
      components: { BaseDialog, KeyGroupPicker },
      setup: () => ({ outerShow, pickerShow, draft, groups }),
      template: '<BaseDialog :show="outerShow" title="Edit key" @close="outerShow = false"><input v-model="draft" data-test="key-name-draft" /><button data-test="open-picker" @click="pickerShow = true">Choose group</button><KeyGroupPicker :show="pickerShow" title="Choose group" :groups="groups" @close="pickerShow = false" /></BaseDialog>'
    })
    const wrapper = mount(Parent, { attachTo: document.body, global: { stubs: { GroupOptionItem: true, Icon: true } } })
    mounted.push(wrapper)
    await flushPromises()
    document.querySelector<HTMLButtonElement>('[data-test="open-picker"]')!.click()
    await flushPromises()

    expect(pickerShow.value).toBe(true)
    expect(document.activeElement).toBe(document.querySelector('[data-test="key-group-picker-search"]'))
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(pickerShow.value).toBe(false)
    expect(outerShow.value).toBe(true)
    expect(draft.value).toBe('Unsaved key name')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(outerShow.value).toBe(false)
  })
})
