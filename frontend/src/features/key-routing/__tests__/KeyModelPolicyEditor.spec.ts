import { describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import type { KeyModelPolicy } from '@/types'
import KeyModelPolicyEditor from '../KeyModelPolicyEditor.vue'
import { en } from '../messages'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values: Record<string, unknown> = {}) =>
  (en[key.replace('custom.keyRouting.', '') as keyof typeof en] ?? key).replace(/\{(\w+)\}/g, (_, name: string) => String(values[name] ?? '')) }) }))

function mountEditor(initial: KeyModelPolicy | null = null, models = ['gpt-a', 'gpt-b', 'kimi-a']) {
  const policy = ref<KeyModelPolicy | null>(initial)
  const disabled = ref(false)
  const Parent = defineComponent({ components: { KeyModelPolicyEditor }, setup: () => ({ policy, disabled, models }), template: '<KeyModelPolicyEditor v-model="policy" :models="models" :disabled="disabled" />' })
  return { wrapper: mount(Parent), policy, disabled }
}

describe('KeyModelPolicyEditor', () => {
  it('keeps exact selected IDs after disabling and the real parent null echo', async () => {
    const { wrapper, policy } = mountEditor({ enabled: true, models: ['custom-a', 'GPT-A'] })
    await wrapper.get('[data-test="model-policy-toggle"]').trigger('click')
    expect(policy.value).toBeNull()
    expect(wrapper.find('[data-test="model-policy-search"]').exists()).toBe(false)
    await wrapper.get('[data-test="model-policy-toggle"]').trigger('click')
    expect(policy.value).toEqual({ enabled: true, models: ['custom-a', 'GPT-A'] })
    wrapper.unmount()
  })

  it('shows an explicit validation error for enabled empty lists', async () => {
    const { wrapper, policy } = mountEditor()
    await wrapper.get('[data-test="model-policy-toggle"]').trigger('click')
    expect(policy.value).toEqual({ enabled: true, models: [] })
    expect(wrapper.get('[role="alert"]').text()).toContain('Select 1–200')
    await wrapper.findAll('[data-test="model-policy-candidate"]')[0].setValue(true)
    expect(policy.value?.models).toEqual(['gpt-a'])
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('searches configured candidates and selects only current results', async () => {
    const { wrapper, policy } = mountEditor({ enabled: true, models: ['custom-a'] }, ['gpt-a', 'gpt-b', 'kimi-a', 'gpt-*', 'gpt-a'])
    await wrapper.get('[data-test="model-policy-search"]').setValue('GPT')
    expect(wrapper.findAll('[data-test="model-policy-candidate"]')).toHaveLength(2)
    await wrapper.get('[data-test="model-policy-select-results"]').trigger('click')
    expect(policy.value?.models).toEqual(['custom-a', 'gpt-a', 'gpt-b'])
    await wrapper.get('[data-test="model-policy-select-results"]').trigger('click')
    expect(policy.value?.models).toEqual(['custom-a', 'gpt-a', 'gpt-b'])
    wrapper.unmount()
  })

  it('manually adds exact IDs, rejects wildcards and reports duplicates', async () => {
    const { wrapper, policy } = mountEditor({ enabled: true, models: ['model-a'] })
    const input = wrapper.get('[data-test="model-policy-manual"]')
    await input.setValue(' model-a ')
    await wrapper.get('[data-test="model-policy-add"]').trigger('click')
    expect(wrapper.get('[role="alert"]').text()).toContain('already selected')
    await input.setValue('model-*')
    await input.trigger('keydown', { key: 'Enter' })
    expect(policy.value?.models).toEqual(['model-a'])
    expect(wrapper.get('[role="alert"]').text()).toContain('without wildcards')
    await input.setValue(' MODEL-A ')
    await input.trigger('keydown', { key: 'Enter' })
    expect(policy.value?.models).toEqual(['model-a', 'MODEL-A'])
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect((input.element as HTMLInputElement).value).toBe('')
    wrapper.unmount()
  })

  it('removes the clicked chip and allows clear then a fresh selection', async () => {
    const { wrapper, policy } = mountEditor({ enabled: true, models: ['model-a', 'model-b', 'model-c'] })
    await wrapper.get('[data-test="model-policy-remove"][data-model="model-b"]').trigger('click')
    expect(policy.value?.models).toEqual(['model-a', 'model-c'])
    await wrapper.get('[data-test="model-policy-clear"]').trigger('click')
    expect(policy.value?.models).toEqual([])
    expect(wrapper.findAll('[data-test="model-policy-chip"]')).toHaveLength(0)
    await wrapper.findAll('[data-test="model-policy-candidate"]')[0].setValue(true)
    expect(policy.value?.models).toEqual(['gpt-a'])
    wrapper.unmount()
  })

  it('does not mutate externally supplied key data and reflects an explicit policy change', async () => {
    const original = { enabled: true, models: ['original'] }
    const { wrapper, policy } = mountEditor(original)
    await wrapper.get('[data-test="model-policy-clear"]').trigger('click')
    expect(original.models).toEqual(['original'])
    policy.value = { enabled: true, models: ['replacement'] }
    await nextTick()
    expect(wrapper.get('[data-test="model-policy-chip"]').text()).toContain('replacement')
    wrapper.unmount()
  })

  it('enforces the 200-model limit atomically when selecting results or adding a model', async () => {
    const models = Array.from({ length: 200 }, (_, index) => `model-${index}`)
    const { wrapper, policy } = mountEditor({ enabled: true, models }, ['another'])
    await wrapper.get('[data-test="model-policy-select-results"]').trigger('click')
    expect(policy.value?.models).toHaveLength(200)
    await wrapper.get('[data-test="model-policy-manual"]').setValue('extra')
    await wrapper.get('[data-test="model-policy-add"]').trigger('click')
    expect(policy.value?.models).toHaveLength(200)
    expect(wrapper.get('[role="alert"]').text()).toContain('Select 1–200')
    wrapper.unmount()
  })

  it('locks every edit action during save', async () => {
    const { wrapper, policy, disabled } = mountEditor({ enabled: true, models: ['model-a'] })
    disabled.value = true
    await nextTick()
    expect(wrapper.findAll<HTMLButtonElement>('button').every((button) => button.element.disabled)).toBe(true)
    expect(wrapper.findAll<HTMLInputElement>('input').every((input) => input.element.disabled)).toBe(true)
    await wrapper.get('[data-test="model-policy-clear"]').trigger('click')
    expect(policy.value?.models).toEqual(['model-a'])
    wrapper.unmount()
  })
})
