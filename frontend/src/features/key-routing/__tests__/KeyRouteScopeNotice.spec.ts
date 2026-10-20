import { describe, expect, it, vi } from 'vitest'
import { defineComponent, ref } from 'vue'
import { mount } from '@vue/test-utils'
import type { KeyRouteScope } from '@/types'
import KeyRouteScopeNotice from '../KeyRouteScopeNotice.vue'
import { en } from '../messages'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => en[key.replace('custom.keyRouting.', '') as keyof typeof en] ?? key }) }))

describe('explicit account route scope', () => {
  it('keeps a legacy key unchanged until the user explicitly upgrades it', async () => {
    const scope = ref<KeyRouteScope>('group_v1')
    const Parent = defineComponent({ components: { KeyRouteScopeNotice }, setup: () => ({ scope }), template: '<KeyRouteScopeNotice v-model="scope" />' })
    const wrapper = mount(Parent)
    expect(scope.value).toBe('group_v1')
    expect(wrapper.text()).toContain('will not upgrade it automatically')
    await wrapper.get('[data-test="key-route-scope-upgrade"]').trigger('click')
    expect(scope.value).toBe('account_v2')
    expect(wrapper.find('[data-test="key-route-scope-upgrade"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('same group are tried first')
    wrapper.unmount()
  })

  it('explains the new-key default and prevents disabled upgrades', async () => {
    const wrapper = mount(KeyRouteScopeNotice, { props: { modelValue: 'account_v2', newKey: true } })
    expect(wrapper.text()).toContain('by default')
    await wrapper.setProps({ modelValue: 'group_v1', disabled: true })
    await wrapper.get('[data-test="key-route-scope-upgrade"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    wrapper.unmount()
  })
})
