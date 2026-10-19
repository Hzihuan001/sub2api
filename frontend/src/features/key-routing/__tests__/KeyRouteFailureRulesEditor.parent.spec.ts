import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick, ref } from 'vue'
import { mount, type VueWrapper } from '@vue/test-utils'
import KeyRouteFailureRulesEditor from '../KeyRouteFailureRulesEditor.vue'
import type { KeyRouteFailurePolicy } from '@/types'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const mounted: VueWrapper[] = []

afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount()
  document.body.innerHTML = ''
})

function initialPolicy(): KeyRouteFailurePolicy {
  return { rules: [
    { type: 'consecutive_failures', threshold: 5, cooldown_seconds: 120 },
    { type: 'timeout', threshold: 6, sample_size: 20, cooldown_seconds: 240 },
    { type: 'rate_limit', threshold: 7, sample_size: 30, cooldown_seconds: 360 }
  ] }
}

async function mountParent(value = initialPolicy()) {
  const policy = ref<KeyRouteFailurePolicy | null>(value)
  const Parent = defineComponent({
    components: { KeyRouteFailureRulesEditor },
    setup: () => ({ policy }),
    template: '<KeyRouteFailureRulesEditor v-model="policy" />'
  })
  const wrapper = mount(Parent, { attachTo: document.body })
  mounted.push(wrapper)
  await wrapper.get('[data-test="key-route-rules-toggle-details"]').trigger('click')
  return { wrapper, policy }
}

const removeButtons = (wrapper: VueWrapper) => wrapper.findAll<HTMLButtonElement>('[data-test^="key-route-rule-remove-"]')

describe('KeyRouteFailureRulesEditor real parent v-model interaction', () => {
  it.each([0, 1, 2])('removes clicked card %i after a focused input is restored on blur and echoed by its parent', async (index) => {
    const { wrapper, policy } = await mountParent()
    const ids = removeButtons(wrapper).map((button) => button.attributes('data-rule-id'))
    const originalButton = removeButtons(wrapper)[index].element
    const input = wrapper.get<HTMLInputElement>(`[data-test="key-route-rule-${index}-threshold"]`)

    input.element.focus()
    await input.setValue('')
    await input.trigger('blur')
    await nextTick()

    // A normal v-model echo must neither replace the focused card nor change
    // the rule identity between blur and the subsequent button click.
    expect(removeButtons(wrapper).map((button) => button.attributes('data-rule-id'))).toEqual(ids)
    expect(removeButtons(wrapper)[index].element).toBe(originalButton)
    const expected = JSON.parse(JSON.stringify(policy.value)) as KeyRouteFailurePolicy
    expected.rules.splice(index, 1)

    await removeButtons(wrapper)[index].trigger('mousedown')
    await removeButtons(wrapper)[index].trigger('mouseup')
    await removeButtons(wrapper)[index].trigger('click')
    await nextTick()

    expect(policy.value).toEqual(expected)
    expect(removeButtons(wrapper).map((button) => button.attributes('data-rule-id'))).toEqual(ids.filter((_, position) => position !== index))
    expect(wrapper.findAll<HTMLSelectElement>('select').map((select) => select.element.value)).toEqual(expected.rules.map((rule) => rule.type))
    expect(policy.value?.rules.map((rule) => rule.cooldown_seconds)).toEqual(expected.rules.map((rule) => rule.cooldown_seconds))
  })

  it('keeps the remaining card identities and values through consecutive deletion and another edit', async () => {
    const { wrapper, policy } = await mountParent()
    const lastId = removeButtons(wrapper)[2].attributes('data-rule-id')
    await removeButtons(wrapper)[1].trigger('click')
    await removeButtons(wrapper)[0].trigger('click')

    expect(policy.value?.rules).toEqual([initialPolicy().rules[2]])
    expect(removeButtons(wrapper)[0].attributes('data-rule-id')).toBe(lastId)
    await wrapper.get('[data-test="key-route-rule-0-threshold"]').setValue('9')
    await wrapper.get('[data-test="key-route-global-cooldown"]').setValue('900')
    expect(policy.value?.rules).toEqual([{ ...initialPolicy().rules[2], threshold: 9, cooldown_seconds: 900 }])
    expect(removeButtons(wrapper)[0].attributes('data-rule-id')).toBe(lastId)
  })

  it('prevents delete mousedown from blurring an empty sample field before the click can remove its card', async () => {
    const { wrapper, policy } = await mountParent()
    const input = wrapper.get<HTMLInputElement>('[data-test="key-route-rule-1-sample_size"]')
    input.element.focus()
    await input.setValue('')
    expect(document.activeElement).toBe(input.element)
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    const remove = removeButtons(wrapper)[1]
    const event = new MouseEvent('mousedown', { bubbles: true, cancelable: true })

    // jsdom cannot model layout movement, but the regression's prerequisite
    // is observable: the button must cancel the browser's focus-changing
    // mousedown default. Otherwise blur restores the default field value,
    // removes its validation line, and can move the button before mouseup.
    remove.element.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(input.element)
    expect(input.element.value).toBe('')
    await remove.trigger('mouseup')
    await remove.trigger('click')

    expect(policy.value?.rules).toEqual([initialPolicy().rules[0], initialPolicy().rules[2]])
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('restores all custom values and card identities after switching off, a parent empty-policy echo, and switching on', async () => {
    const { wrapper, policy } = await mountParent()
    await wrapper.get('[data-test="key-route-rule-0-threshold"]').setValue('9')
    await wrapper.get('[data-test="key-route-rule-1-sample_size"]').setValue('40')
    await wrapper.get('[data-test="key-route-global-cooldown"]').setValue('900')
    const expected = JSON.parse(JSON.stringify(policy.value)) as KeyRouteFailurePolicy
    const ids = removeButtons(wrapper).map((button) => button.attributes('data-rule-id'))

    await wrapper.get('[data-test="key-route-failure-rules-switch"]').trigger('click')
    expect(policy.value).toEqual({ rules: [] })
    expect(removeButtons(wrapper)).toHaveLength(0)
    expect(wrapper.get('[data-test="key-route-failure-rules-switch"]').attributes('aria-checked')).toBe('false')
    // Exercise a fresh parent object in addition to the automatic v-model
    // echo. Neither should erase the locally retained disabled draft.
    policy.value = { rules: [] }
    await nextTick()
    await wrapper.get('[data-test="key-route-failure-rules-switch"]').trigger('click')

    expect(policy.value).toEqual(expected)
    expect(removeButtons(wrapper).map((button) => button.attributes('data-rule-id'))).toEqual(ids)
    expect(wrapper.get<HTMLInputElement>('[data-test="key-route-rule-0-threshold"]').element.value).toBe('9')
    expect(wrapper.get<HTMLInputElement>('[data-test="key-route-rule-1-sample_size"]').element.value).toBe('40')
    expect(wrapper.get<HTMLInputElement>('[data-test="key-route-global-cooldown"]').element.value).toBe('900')
  })

  it('does not resurrect deleted rules when every rule is removed and new rules are added', async () => {
    const { wrapper, policy } = await mountParent()
    const removedIds = removeButtons(wrapper).map((button) => button.attributes('data-rule-id'))
    for (let count = 0; count < 3; count++) await removeButtons(wrapper)[0].trigger('click')
    expect(policy.value).toEqual({ rules: [] })
    expect(removeButtons(wrapper)).toHaveLength(0)
    expect(wrapper.get('[data-test="key-route-failure-rules-switch"]').attributes('aria-checked')).toBe('false')

    await wrapper.get('[data-test="key-route-failure-rules-switch"]').trigger('click')
    await wrapper.get('[data-test="key-route-rule-add"]').trigger('click')
    expect(policy.value).toEqual({ rules: [
      { type: 'ttft', first_content_seconds: 15, threshold: 3, sample_size: 20, cooldown_seconds: 120 },
      { type: 'sla_success_rate', threshold: 90, sample_size: 20, cooldown_seconds: 120 },
      { type: 'consecutive_failures', threshold: 5, cooldown_seconds: 120 },
      { type: 'timeout', threshold: 10, sample_size: 20, cooldown_seconds: 120 },
      { type: 'rate_limit', threshold: 10, sample_size: 20, cooldown_seconds: 120 },
      { type: 'status_code', status_codes: [502, 503, 504, 529], threshold: 3, window_seconds: 60, cooldown_seconds: 120 }
    ] })
    expect(removeButtons(wrapper).every((button) => !removedIds.includes(button.attributes('data-rule-id')))).toBe(true)
    await removeButtons(wrapper)[0].trigger('click')
    expect(policy.value?.rules.map((rule) => rule.type)).toEqual(['sla_success_rate', 'consecutive_failures', 'timeout', 'rate_limit', 'status_code'])
  })

  it.each(['key-route-rule-add', 'key-route-rules-inherit', 'key-route-rules-toggle-details', 'key-route-failure-rules-switch'])('keeps %s actionable while an empty focused field would change layout on blur', async (testId) => {
    const { wrapper } = await mountParent()
    const input = wrapper.get<HTMLInputElement>('[data-test="key-route-rule-1-sample_size"]')
    input.element.focus()
    await input.setValue('')
    const button = wrapper.get(`[data-test="${testId}"]`)
    const event = new MouseEvent('mousedown', { bubbles: true, cancelable: true })
    button.element.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(input.element)
    await button.trigger('click')
    if (testId === 'key-route-rule-add') expect(removeButtons(wrapper)).toHaveLength(4)
    if (testId === 'key-route-rules-inherit') expect(wrapper.find('[data-test="key-route-rules-inherited-hint"]').exists()).toBe(true)
    if (testId === 'key-route-rules-toggle-details') expect(removeButtons(wrapper)).toHaveLength(0)
    if (testId === 'key-route-failure-rules-switch') expect(wrapper.get('[data-test="key-route-failure-rules-switch"]').attributes('aria-checked')).toBe('false')
  })

  it('shows an invalid field alert only on the card containing that field', async () => {
    const { wrapper } = await mountParent()
    const input = wrapper.get('[data-test="key-route-rule-1-sample_size"]')
    await input.setValue('')

    expect(wrapper.findAll('[data-rule]').map((card) => card.find('[role="alert"]').exists())).toEqual([false, true, false])
    expect(wrapper.findAll('[role="alert"]')).toHaveLength(1)

    await input.trigger('blur')
    expect(wrapper.findAll('[role="alert"]')).toHaveLength(0)
  })

  it('shows duplicate status-code alerts only on the two participating cards, not a third valid card', async () => {
    const value: KeyRouteFailurePolicy = { rules: [
      { type: 'status_code', status_codes: [502], threshold: 5, window_seconds: 60, cooldown_seconds: 120 },
      { type: 'status_code', status_codes: [503], threshold: 6, window_seconds: 90, cooldown_seconds: 240 },
      { type: 'status_code', status_codes: [504], threshold: 7, window_seconds: 120, cooldown_seconds: 360 }
    ] }
    const { wrapper } = await mountParent(value)
    await wrapper.get('[data-test="key-route-rule-1-status-codes"]').setValue('502, 503')

    const cards = wrapper.findAll('[data-rule]')
    expect(cards.map((card) => card.find('[role="alert"]').exists())).toEqual([true, true, false])
    expect(cards[0].get('[role="alert"]').text()).toContain('keyRulesDuplicate')
    expect(cards[1].get('[role="alert"]').text()).toContain('keyRulesDuplicate')
    await wrapper.get('[data-test="key-route-rule-1-status-codes"]').setValue('503')
    expect(wrapper.findAll('[role="alert"]')).toHaveLength(0)
  })

  it('preserves status-card identities when comma normalization blurs before deleting a non-final card', async () => {
    const value: KeyRouteFailurePolicy = { rules: [
      { type: 'status_code', status_codes: [502], threshold: 5, window_seconds: 60, cooldown_seconds: 120 },
      { type: 'status_code', status_codes: [503], threshold: 6, window_seconds: 90, cooldown_seconds: 240 },
      { type: 'status_code', status_codes: [504], threshold: 7, window_seconds: 120, cooldown_seconds: 360 }
    ] }
    const { wrapper, policy } = await mountParent(value)
    const ids = removeButtons(wrapper).map((button) => button.attributes('data-rule-id'))
    const input = wrapper.get<HTMLInputElement>('[data-test="key-route-rule-1-status-codes"]')
    input.element.focus()
    await input.setValue('503， 529')
    await removeButtons(wrapper)[1].trigger('mousedown')
    await input.trigger('blur')
    await nextTick()
    expect(input.element.value).toBe('503, 529')
    expect(removeButtons(wrapper).map((button) => button.attributes('data-rule-id'))).toEqual(ids)
    await removeButtons(wrapper)[1].trigger('mouseup')
    await removeButtons(wrapper)[1].trigger('click')
    expect(policy.value?.rules).toEqual([value.rules[0], value.rules[2]])
    expect(wrapper.findAll<HTMLInputElement>('input[data-test$="status-codes"]').map((field) => field.element.value)).toEqual(['502', '504'])
  })
})
