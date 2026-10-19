import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick, onMounted, ref } from 'vue'
import { mount, type VueWrapper } from '@vue/test-utils'
import KeyAdvancedSettings from '../KeyAdvancedSettings.vue'
import { en, zh } from '../messages'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key === 'custom.keyRouting.advancedSettings' ? zh.advancedSettings : key })
}))

const mounted: VueWrapper[] = []

afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount()
  document.body.innerHTML = ''
})

function mountSettings(props: { summary?: string; disabled?: boolean } = {}) {
  const wrapper = mount(KeyAdvancedSettings, { props, slots: { default: '<input value="initial" />' } })
  mounted.push(wrapper)
  return wrapper
}

describe('KeyAdvancedSettings', () => {
  it('starts collapsed while keeping the supplied controls mounted', () => {
    const wrapper = mountSettings()
    const toggle = wrapper.get('[data-test="key-advanced-settings-toggle"]')
    const content = wrapper.get('[data-test="key-advanced-settings-content"]')
    expect(toggle.text()).toContain('高级设置')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    expect(toggle.attributes('aria-controls')).toBe(content.attributes('id'))
    expect(content.attributes('style')).toContain('display: none')
    expect(content.find('input').exists()).toBe(true)
    expect(wrapper.find('[data-test="key-advanced-settings-summary"]').exists()).toBe(false)
  })

  it('expands and collapses from the same header control', async () => {
    const wrapper = mountSettings()
    const toggle = wrapper.get('[data-test="key-advanced-settings-toggle"]')
    const content = wrapper.get('[data-test="key-advanced-settings-content"]')
    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect(content.attributes('style') ?? '').not.toContain('display: none')
    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    expect(content.attributes('style')).toContain('display: none')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('shows the caller summary without deriving or changing any field values', async () => {
    const wrapper = mountSettings({ summary: '无限额度 · 长期有效' })
    expect(wrapper.get('[data-test="key-advanced-settings-summary"]').text()).toBe('无限额度 · 长期有效')
    await wrapper.setProps({ summary: '$10 · 30天' })
    expect(wrapper.get('[data-test="key-advanced-settings-summary"]').attributes('title')).toBe('$10 · 30天')
    expect(wrapper.get<HTMLInputElement>('input').element.value).toBe('initial')
  })

  it('preserves child component drafts and identity through repeated collapse', async () => {
    const childMounted = vi.fn()
    const Field = defineComponent({
      setup() {
        const draft = ref('initial')
        onMounted(childMounted)
        return { draft }
      },
      template: '<input v-model="draft" />'
    })
    const wrapper = mount(KeyAdvancedSettings, { slots: { default: Field } })
    mounted.push(wrapper)
    const toggle = wrapper.get('[data-test="key-advanced-settings-toggle"]')
    const fieldElement = wrapper.get<HTMLInputElement>('input').element
    expect(childMounted).toHaveBeenCalledTimes(1)
    await toggle.trigger('click')
    await wrapper.get('input').setValue('unfinished draft')
    await toggle.trigger('click')
    await toggle.trigger('click')
    expect(wrapper.get<HTMLInputElement>('input').element).toBe(fieldElement)
    expect(wrapper.get<HTMLInputElement>('input').element.value).toBe('unfinished draft')
    expect(childMounted).toHaveBeenCalledTimes(1)
  })

  it('does not toggle while disabled', async () => {
    const wrapper = mountSettings({ disabled: true })
    const toggle = wrapper.get<HTMLButtonElement>('[data-test="key-advanced-settings-toggle"]')
    expect(toggle.element.disabled).toBe(true)
    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('false')
  })

  it('reveals and focuses a hidden native-invalid control without changing validity', async () => {
    const wrapper = mount(KeyAdvancedSettings, {
      attachTo: document.body,
      slots: { default: '<input required value="" />' }
    })
    mounted.push(wrapper)
    const field = wrapper.get<HTMLInputElement>('input').element
    expect(field.checkValidity()).toBe(false)
    await nextTick()
    expect(wrapper.get('[data-test="key-advanced-settings-toggle"]').attributes('aria-expanded')).toBe('true')
    expect(wrapper.get('[data-test="key-advanced-settings-content"]').attributes('style') ?? '').not.toContain('display: none')
    expect(document.activeElement).toBe(field)
    expect(field.validity.valueMissing).toBe(true)
  })

  it('keeps focus on the first invalid control when form validation finds several hidden fields', async () => {
    const form = document.createElement('form')
    document.body.append(form)
    const wrapper = mount(KeyAdvancedSettings, {
      attachTo: form,
      slots: { default: '<input required value="" data-field="first" /><input required value="" data-field="second" />' }
    })
    mounted.push(wrapper)
    expect(form.checkValidity()).toBe(false)
    await nextTick()
    expect(document.activeElement).toBe(wrapper.get('[data-field="first"]').element)
    expect(wrapper.get<HTMLInputElement>('[data-field="second"]').element.validity.valueMissing).toBe(true)
  })

  it('leaves native invalid events alone when the controls are already visible', async () => {
    const wrapper = mountSettings()
    await wrapper.get('[data-test="key-advanced-settings-toggle"]').trigger('click')
    const event = new Event('invalid', { cancelable: true })
    wrapper.get('input').element.dispatchEvent(event)
    await nextTick()
    expect(event.defaultPrevented).toBe(false)
  })

  it('has localized titles in both languages', () => {
    expect(zh.advancedSettings).toBe('高级设置')
    expect(en.advancedSettings).toBe('Advanced settings')
  })
})
