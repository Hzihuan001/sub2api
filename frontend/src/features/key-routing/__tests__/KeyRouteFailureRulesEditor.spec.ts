import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import KeyRouteFailureRulesEditor from '../KeyRouteFailureRulesEditor.vue'
import { en, zh } from '../messages'
import { COMMON_ERROR_STATUS_CODES } from '@/utils/errorBadges'
import type { KeyRouteFailureRule } from '@/types'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key === 'custom.keyRouting.keyRulesStatusCodeHelp' ? zh.keyRulesStatusCodeHelp : key })
}))

describe('KeyRouteFailureRulesEditor', () => {
  it('describes inheritance from the actually selected group in both languages', () => {
    for (const key of ['rulesHint', 'keyRulesInheritHint', 'keyRulesInheritedHint'] as const) {
      expect(zh[key]).toContain('当前实际选中分组')
      expect(en[key]).toContain('group actually selected for the request')
    }
    expect(zh.keyRulesInheritHint).not.toContain('主分组')
    expect(en.rulesHint).not.toContain('primary group')
    expect(en.keyRulesInheritHint).not.toContain('primary group')
  })

  it('explains error-log status codes individually in the hover help', async () => {
    const policy = { rules: [{ status_codes: [502], threshold: 3, window_seconds: 60, cooldown_seconds: 600 }] }
    const wrapper = mount(KeyRouteFailureRulesEditor, { props: { modelValue: policy } })
    await wrapper.get('[data-test="key-route-rules-toggle-details"]').trigger('click')
    const help = wrapper.get('[data-test="key-route-status-code-help"]')
    expect(help.attributes('title')).toBe(zh.keyRulesStatusCodeHelp)
    expect(help.attributes('aria-label')).toBe(zh.keyRulesStatusCodeHelp)
    for (const messages of [zh, en]) {
      for (const code of [...COMMON_ERROR_STATUS_CODES, 520, 521, 522, 523, 524, 525, 526]) {
        expect(messages.keyRulesStatusCodeHelp).toMatch(new RegExp(`(?:^|\\n)${code} `))
      }
    }
    expect(zh.keyRulesStatusCodeHelp).toContain('上游状态码和客户端状态码可能不同')
    expect(zh.keyRulesStatusCodeHelp).toContain('502 网关错误')
    expect(zh.keyRulesStatusCodeHelp).toContain('503 服务过载或暂时不可用')
    expect(zh.keyRulesStatusCodeHelp).toContain('504 网关等待上游响应超时')
    expect(zh.keyRulesStatusCodeHelp).toContain('529 部分上游使用的服务过载状态码')
  })

  it('starts inherited and seeds the five reference rules only when an empty draft is enabled', async () => {
    const wrapper = mount(KeyRouteFailureRulesEditor, { props: { modelValue: null } })
    expect(wrapper.get('[data-test="key-route-failure-rules-toggle"]').text()).toContain('keyRulesInherited')
    expect(wrapper.find('[data-test="key-route-rules-inherited-hint"]').exists()).toBe(false)
    const detailsToggle = wrapper.get('[data-test="key-route-rules-toggle-details"]')
    expect((detailsToggle.element as HTMLButtonElement).disabled).toBe(false)
    await detailsToggle.trigger('click')
    await wrapper.vm.$nextTick()
    expect(wrapper.get('[data-test="key-route-rules-inherited-hint"]').exists()).toBe(true)
    await wrapper.get('[data-test="key-route-failure-rules-switch"]').trigger('click')
    const emitted = wrapper.emitted('update:modelValue') ?? []
    expect(emitted.at(-1)).toEqual([{ rules: [
      { type: 'ttft', first_content_seconds: 15, threshold: 3, sample_size: 20, cooldown_seconds: 120 },
      { type: 'sla_success_rate', threshold: 90, sample_size: 20, cooldown_seconds: 120 },
      { type: 'consecutive_failures', threshold: 5, cooldown_seconds: 120 },
      { type: 'timeout', threshold: 10, sample_size: 20, cooldown_seconds: 120 },
      { type: 'rate_limit', threshold: 10, sample_size: 20, cooldown_seconds: 120 }
    ] }])
  })

  it('can explicitly disable the key', async () => {
    const policy = { rules: [{ status_codes: [502], threshold: 3, window_seconds: 60, cooldown_seconds: 600 }] }
    const wrapper = mount(KeyRouteFailureRulesEditor, { props: { modelValue: policy } })
    await wrapper.get('[data-test="key-route-failure-rules-switch"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([{ rules: [] }])
    await wrapper.get('[data-test="key-route-rules-toggle-details"]').trigger('click')
    expect(wrapper.get('[data-test="key-route-rules-disabled-hint"]').exists()).toBe(true)
    await wrapper.get('[data-test="key-route-rules-inherit"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([null])
  })

  it('normalizes comma-separated codes and rejects duplicate codes across rules', async () => {
    const policy = { rules: [{ status_codes: [502], threshold: 3, window_seconds: 60, cooldown_seconds: 600 }] }
    const wrapper = mount(KeyRouteFailureRulesEditor, { props: { modelValue: policy } })
    await wrapper.get('[data-test="key-route-rules-toggle-details"]').trigger('click')
    await wrapper.get('[data-test="key-route-rule-add"]').trigger('click')
    await wrapper.get('[data-test="key-route-rule-1-type"]').setValue('status_code')
    const inputs = wrapper.findAll<HTMLInputElement>('input[data-test$="status-codes"]')
    await inputs[1].setValue('502, 503')
    expect(wrapper.text()).toContain('keyRulesDuplicate')
  })

  it('loads legacy status rules without showing unrelated sample fields', async () => {
    const wrapper = mount(KeyRouteFailureRulesEditor, { props: { modelValue: { rules: [{ status_codes: [503], threshold: 5, window_seconds: 60, cooldown_seconds: 600 }] } } })
    await wrapper.get('[data-test="key-route-rules-toggle-details"]').trigger('click')
    expect(wrapper.get<HTMLSelectElement>('[data-test="key-route-rule-0-type"]').element.value).toBe('status_code')
    expect(wrapper.get<HTMLInputElement>('[data-test="key-route-rule-0-status-codes"]').element.value).toBe('503')
    expect(wrapper.find('[data-test="key-route-rule-0-sample_size"]').exists()).toBe(false)
  })

  it('preserves a legacy rule without adding a type field when it is edited and echoed back', async () => {
    const rule = { status_codes: [502], threshold: 5, window_seconds: 60, cooldown_seconds: 600 }
    const wrapper = mount(KeyRouteFailureRulesEditor, { props: { modelValue: { rules: [rule] } } })
    await wrapper.get('[data-test="key-route-rules-toggle-details"]').trigger('click')
    await wrapper.get('[data-test="key-route-rule-0-threshold"]').setValue('7')
    const next = { rules: [{ ...rule, threshold: 7 }] }
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([next])
    await wrapper.setProps({ modelValue: next })
    expect(wrapper.get<HTMLInputElement>('[data-test="key-route-rule-0-threshold"]').element.value).toBe('7')
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).not.toHaveProperty('rules.0.type')
  })

  it('distinguishes inherited rules from explicit disable and describes only implemented failure signals', async () => {
    const wrapper = mount(KeyRouteFailureRulesEditor, { props: { modelValue: null } })
    expect(wrapper.get('[data-test="key-route-failure-rules-toggle"]').text()).toContain('keyRulesInherited')
    await wrapper.setProps({ modelValue: { rules: [] } })
    expect(wrapper.get('[data-test="key-route-failure-rules-toggle"]').text()).toContain('keyRulesDisabled')
    await wrapper.get('[data-test="key-route-rules-toggle-details"]').trigger('click')
    await wrapper.get('[data-test="key-route-rules-inherit"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([null])
    expect(zh.keyRulesInheritedHint).toContain('不是禁用')
    expect(zh.keyRulesDisableHint).toContain('模型')
    expect(zh.keyRulesHint_timeout).toContain('已有上游超时结果')
    expect(zh.keyRulesHint_timeout).toContain('不新增主动首字等待时限')
    expect(zh.keyRulesHint_rate_limit).toContain('上游返回 429')
    expect(zh.keyRulesHint_rate_limit).toContain('不统计本地用户余额、RPM 或 Key 限额')
    expect(en.keyRulesHint_timeout).toContain('existing upstream timeout')
    expect(en.keyRulesHint_rate_limit).toContain('upstream 429')
  })

  it('shows only threshold and cooldown for consecutive failures', async () => {
    const wrapper = mount(KeyRouteFailureRulesEditor, { props: { modelValue: { rules: [{ type: 'consecutive_failures', threshold: 3, cooldown_seconds: 120 }] } } })
    await wrapper.get('[data-test="key-route-rules-toggle-details"]').trigger('click')
    expect(wrapper.find('[data-test="key-route-rule-0-threshold"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="key-route-global-cooldown"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="key-route-rule-0-window_seconds"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="key-route-rule-0-status-codes"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="key-route-rule-0-sample_size"]').exists()).toBe(false)
    expect(zh.keyRulesSection).not.toMatch(/慢响应|TTFT|SLA/)
    expect(zh.keyRulesDescription).not.toMatch(/慢响应|TTFT|SLA/)
  })

  it.each(['timeout', 'rate_limit'] as const)('edits %s rules using a request sample and validates the threshold', async (type) => {
    const wrapper = mount(KeyRouteFailureRulesEditor, { props: { modelValue: { rules: [{ type: 'consecutive_failures', threshold: 3, cooldown_seconds: 120 }] } } })
    await wrapper.get('[data-test="key-route-rules-toggle-details"]').trigger('click')
    await wrapper.get('[data-test="key-route-rule-0-type"]').setValue(type)
    expect(wrapper.get<HTMLInputElement>('[data-test="key-route-rule-0-sample_size"]').element.value).toBe('20')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([{ rules: [{ type, threshold: 3, sample_size: 20, cooldown_seconds: 120 }] }])
    await wrapper.get('[data-test="key-route-rule-0-sample_size"]').setValue('2')
    expect(wrapper.text()).toContain('keyRulesSampleValidation')
    await wrapper.get('[data-test="key-route-rule-0-threshold"]').setValue('2')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('restores defaults for the selected rule type when numeric fields are cleared', async () => {
    const wrapper = mount(KeyRouteFailureRulesEditor, { props: { modelValue: { rules: [{ type: 'timeout', threshold: 7, sample_size: 30, cooldown_seconds: 300 }] } } })
    await wrapper.get('[data-test="key-route-rules-toggle-details"]').trigger('click')
    for (const [field, value] of [['sample_size', '20'], ['threshold', '3']]) {
      const input = wrapper.get<HTMLInputElement>(`[data-test="key-route-rule-0-${field}"]`)
      await input.setValue('')
      await input.trigger('blur')
      expect(input.element.value).toBe(value)
    }
    await wrapper.get('[data-test="key-route-rule-0-type"]').setValue('status_code')
    expect(wrapper.get<HTMLInputElement>('[data-test="key-route-rule-0-window_seconds"]').element.value).toBe('60')
    expect(wrapper.get<HTMLInputElement>('[data-test="key-route-global-cooldown"]').element.value).toBe('300')
    const cooldown = wrapper.get<HTMLInputElement>('[data-test="key-route-global-cooldown"]')
    await cooldown.setValue('')
    await cooldown.trigger('blur')
    expect(cooldown.element.value).toBe('120')
  })

  it('offers each semantic rule only once while allowing additional status rules', async () => {
    const wrapper = mount(KeyRouteFailureRulesEditor, { props: { modelValue: { rules: [{ type: 'consecutive_failures', threshold: 3, cooldown_seconds: 120 }] } } })
    await wrapper.get('[data-test="key-route-rules-toggle-details"]').trigger('click')
    for (let index = 0; index < 6; index++) await wrapper.get('[data-test="key-route-rule-add"]').trigger('click')
    expect(wrapper.findAll<HTMLSelectElement>('select').map((select) => select.element.value)).toEqual(['consecutive_failures', 'ttft', 'sla_success_rate', 'timeout', 'rate_limit', 'status_code', 'status_code'])
    const timeoutType = wrapper.get<HTMLSelectElement>('[data-test="key-route-rule-3-type"]')
    expect(timeoutType.get<HTMLOptionElement>('option[value="consecutive_failures"]').element.disabled).toBe(true)
    expect(timeoutType.get<HTMLOptionElement>('option[value="status_code"]').element.disabled).toBe(false)
    const codes = wrapper.findAll<HTMLInputElement>('input[data-test$="status-codes"]')
    expect(codes.map((input) => input.element.value)).toEqual(['502, 503, 504, 529', ''])
    expect(wrapper.text()).not.toContain('keyRulesDuplicate')
  })

  it('edits TTFT seconds and restores a cleared wait without changing its recent sample', async () => {
    const wrapper = mount(KeyRouteFailureRulesEditor, { props: { modelValue: { rules: [{ type: 'ttft', threshold: 3, sample_size: 20, first_content_seconds: 15, cooldown_seconds: 120 }] } } })
    await wrapper.get('[data-test="key-route-rules-toggle-details"]').trigger('click')
    const wait = wrapper.get<HTMLInputElement>('[data-test="key-route-rule-0-first_content_seconds"]')
    await wait.setValue('4')
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    await wait.setValue('8')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([{ rules: [{ type: 'ttft', first_content_seconds: 8, threshold: 3, sample_size: 20, cooldown_seconds: 120 }] }])
    await wait.setValue('')
    await wait.trigger('blur')
    expect(wait.element.value).toBe('15')
    expect(wrapper.get<HTMLInputElement>('[data-test="key-route-rule-0-sample_size"]').element.value).toBe('20')
    expect(wrapper.text()).toContain('keyRulesTTFTSafety')
  })

  it('uses SLA threshold as a percentage, not a failure count bounded by sample size', async () => {
    const wrapper = mount(KeyRouteFailureRulesEditor, { props: { modelValue: { rules: [{ type: 'sla_success_rate', threshold: 90, sample_size: 20, cooldown_seconds: 120 }] } } })
    await wrapper.get('[data-test="key-route-rules-toggle-details"]').trigger('click')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="key-route-rule-0-first_content_seconds"]').exists()).toBe(false)
    await wrapper.get('[data-test="key-route-rule-0-threshold"]').setValue('95')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([{ rules: [{ type: 'sla_success_rate', threshold: 95, sample_size: 20, cooldown_seconds: 120 }] }])
    await wrapper.get('[data-test="key-route-rule-0-threshold"]').setValue('101')
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
  })

  it('uses one shared cooldown without overwriting legacy mixed values on open, removal, or type changes', async () => {
    const rules: KeyRouteFailureRule[] = [
      { type: 'consecutive_failures', threshold: 3, cooldown_seconds: 120 },
      { type: 'timeout', threshold: 4, sample_size: 20, cooldown_seconds: 600 }
    ]
    const wrapper = mount(KeyRouteFailureRulesEditor, { props: { modelValue: { rules } } })
    await wrapper.get('[data-test="key-route-rules-toggle-details"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.get('[data-test="key-route-cooldown-mixed"]').exists()).toBe(true)
    const cooldown = wrapper.get<HTMLInputElement>('[data-test="key-route-global-cooldown"]')
    expect(cooldown.element.value).toBe('')
    await cooldown.trigger('blur')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.get('[data-test="key-route-rule-1-type"]').setValue('rate_limit')
    expect((wrapper.emitted('update:modelValue')?.at(-1)?.[0] as { rules: KeyRouteFailureRule[] }).rules.map((rule) => rule.cooldown_seconds)).toEqual([120, 600])
    await cooldown.setValue('240')
    expect((wrapper.emitted('update:modelValue')?.at(-1)?.[0] as { rules: KeyRouteFailureRule[] }).rules.map((rule) => rule.cooldown_seconds)).toEqual([240, 240])
    expect(wrapper.find('[data-test="key-route-cooldown-mixed"]').exists()).toBe(false)
    await wrapper.get('[data-test="key-route-rule-add"]').trigger('click')
    expect((wrapper.emitted('update:modelValue')?.at(-1)?.[0] as { rules: KeyRouteFailureRule[] }).rules.every((rule) => rule.cooldown_seconds === 240)).toBe(true)
  })

  it('caps the editor at twenty rules', async () => {
    const rules: KeyRouteFailureRule[] = Array.from({ length: 20 }, (_, index) => ({ type: 'status_code', status_codes: [500 + index], threshold: 3, window_seconds: 60, cooldown_seconds: 600 }))
    const wrapper = mount(KeyRouteFailureRulesEditor, { props: { modelValue: { rules } } })
    await wrapper.get('[data-test="key-route-rules-toggle-details"]').trigger('click')
    expect(wrapper.get<HTMLButtonElement>('[data-test="key-route-rule-add"]').element.disabled).toBe(true)
    await wrapper.get('[data-test="key-route-rule-remove-19"]').trigger('click')
    expect(wrapper.get<HTMLButtonElement>('[data-test="key-route-rule-add"]').element.disabled).toBe(false)
  })

  it('removes the clicked rule rather than the last rule', async () => {
    const rules: KeyRouteFailureRule[] = [
      { type: 'status_code', status_codes: [502], threshold: 3, window_seconds: 60, cooldown_seconds: 600 },
      { type: 'status_code', status_codes: [503], threshold: 4, window_seconds: 90, cooldown_seconds: 700 },
      { type: 'status_code', status_codes: [504], threshold: 5, window_seconds: 120, cooldown_seconds: 800 }
    ]
    const wrapper = mount(KeyRouteFailureRulesEditor, { props: { modelValue: { rules } } })
    await wrapper.get('[data-test="key-route-rules-toggle-details"]').trigger('click')

    await wrapper.get('[data-test="key-route-rule-remove-1"]').trigger('click')

    const emitted = wrapper.emitted('update:modelValue')?.at(-1)?.[0] as { rules: KeyRouteFailureRule[] }
    expect(emitted.rules.map((rule) => rule.status_codes)).toEqual([[502], [504]])
    expect(wrapper.get<HTMLInputElement>('[data-test="key-route-rule-0-status-codes"]').element.value).toBe('502')
    expect(wrapper.get<HTMLInputElement>('[data-test="key-route-rule-1-status-codes"]').element.value).toBe('504')
    expect(wrapper.find<HTMLInputElement>('[data-test="key-route-rule-1-status-codes"]').element.value).not.toBe('503')
  })

  it('explicitly disables failover when its last custom rule is removed', async () => {
    const wrapper = mount(KeyRouteFailureRulesEditor, { props: { modelValue: { rules: [{ type: 'consecutive_failures', threshold: 3, cooldown_seconds: 120 }] } } })
    await wrapper.get('[data-test="key-route-rules-toggle-details"]').trigger('click')
    await wrapper.get('[data-test="key-route-rule-remove-0"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([{ rules: [] }])
    expect(wrapper.get('[data-test="key-route-failure-rules-switch"]').attributes('aria-checked')).toBe('false')
  })

  it('does not discard a partial status-code input when the parent echoes its structured value', async () => {
    const wrapper = mount(KeyRouteFailureRulesEditor, { props: { modelValue: { rules: [{ status_codes: [502], threshold: 3, window_seconds: 60, cooldown_seconds: 600 }] } } })
    await wrapper.get('[data-test="key-route-rules-toggle-details"]').trigger('click')
    await wrapper.get('[data-test="key-route-rule-0-status-codes"]').setValue('not-yet-valid')
    const emitted = wrapper.emitted('update:modelValue')?.at(-1)?.[0]
    await wrapper.setProps({ modelValue: emitted as { rules: KeyRouteFailureRule[] } })
    expect(wrapper.get<HTMLInputElement>('[data-test="key-route-rule-0-status-codes"]').element.value).toBe('not-yet-valid')
  })
})
