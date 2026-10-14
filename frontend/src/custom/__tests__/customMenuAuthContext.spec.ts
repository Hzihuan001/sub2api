import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import Toggle from '@/components/common/Toggle.vue'
import CustomMenuAuthContextField from '../settings/CustomMenuAuthContextField.vue'
import {
  normalizeCustomMenuAuthContext,
  supportsCustomMenuAuthContext,
} from '../settings/customMenuAuthContext'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => `translated:${key}` }),
}))

const mounted: VueWrapper[] = []

function mountField(props: {
  modelValue?: boolean
  url?: string
  readOnly?: boolean
} = {}) {
  // Render the real Toggle too: disabled must reach the actual switch button.
  const wrapper = mount(CustomMenuAuthContextField, { props })
  mounted.push(wrapper)
  return wrapper
}

afterEach(() => {
  mounted.splice(0).forEach((wrapper) => wrapper.unmount())
})

describe('custom menu auth context helpers', () => {
  it.each([
    { value: undefined, expected: true },
    { value: true, expected: true },
    { value: false, expected: false },
  ])('normalizes the stored value $value to $expected', ({ value, expected }) => {
    expect(normalizeCustomMenuAuthContext(value)).toBe(expected)
  })

  it.each([
    { url: undefined, expected: true },
    { url: '', expected: true },
    { url: 'https://example.com/menu', expected: true },
    { url: 'md:', expected: false },
    { url: 'md:# Markdown menu', expected: false },
    { url: 'MD:# Existing case-sensitive behavior', expected: true },
  ])('preserves URL support for $url', ({ url, expected }) => {
    expect(supportsCustomMenuAuthContext(url)).toBe(expected)
  })
})

describe('custom menu auth context real field', () => {
  it('shows the historical enabled default without emitting a write during mount', () => {
    const wrapper = mountField()
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.text()).toContain('translated:admin.settings.customMenu.passAuthContext')
    expect(wrapper.text()).toContain('translated:admin.settings.customMenu.passAuthContextHint')
    expect(wrapper.emitted()).toEqual({})
  })

  it('keeps an explicitly saved false value disabled', () => {
    const wrapper = mountField({ modelValue: false })
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.emitted()).toEqual({})
  })

  it('hides lowercase md: URLs but preserves uppercase MD: visibility', async () => {
    const wrapper = mountField({ url: 'md:# Markdown menu', readOnly: false })
    expect(wrapper.find('[role="switch"]').exists()).toBe(false)
    expect(wrapper.text()).toBe('')
    await wrapper.setProps({ url: 'MD:# Existing case-sensitive behavior' })
    expect(wrapper.find('[role="switch"]').exists()).toBe(true)
    expect(wrapper.emitted()).toEqual({})
  })

  it.each([undefined, true])('denies writes when readOnly is %s, including direct child events', async (readOnly) => {
    const wrapper = mountField({ readOnly })
    const button = wrapper.get('[role="switch"]')
    expect((button.element as HTMLButtonElement).disabled).toBe(true)
    await button.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()

    // A disabled DOM control is not an authorization guard on its own.
    wrapper.getComponent(Toggle).vm.$emit('update:modelValue', false)
    wrapper.getComponent(Toggle).vm.$emit('update:modelValue', true)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it.each([
    { value: undefined, expected: false },
    { value: true, expected: false },
    { value: false, expected: true },
  ])('emits only the updated boolean for a writable value $value', async ({ value, expected }) => {
    const wrapper = mountField({ modelValue: value, url: 'https://example.com/menu', readOnly: false })
    const originalProps = { ...wrapper.props() }
    const button = wrapper.get('[role="switch"]')
    expect((button.element as HTMLButtonElement).disabled).toBe(false)
    await button.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[expected]])
    expect(Object.keys(wrapper.emitted()).filter((name) => name.startsWith('update:'))).toEqual(['update:modelValue'])
    expect(wrapper.props()).toEqual(originalProps)
  })

  it('stops forwarding events immediately when the field becomes read-only', async () => {
    const wrapper = mountField({ modelValue: false, readOnly: false })
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[true]])
    await wrapper.setProps({ readOnly: true })
    wrapper.getComponent(Toggle).vm.$emit('update:modelValue', false)
    expect(wrapper.emitted('update:modelValue')).toEqual([[true]])
    expect((wrapper.get('[role="switch"]').element as HTMLButtonElement).disabled).toBe(true)
  })
})
