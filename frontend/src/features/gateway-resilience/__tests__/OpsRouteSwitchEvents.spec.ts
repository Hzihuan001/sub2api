import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import OpsRouteSwitchEvents from '../OpsRouteSwitchEvents.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => key === 'admin.ops.errorLog.routeSwitchToBackup'
      ? `切换至后续优先级分组：${params?.group ?? ''}`
      : key
  })
}))

enableAutoUnmount(afterEach)

describe('OpsRouteSwitchEvents', () => {
  it.each([undefined, 'invalid json', '[]', '[{"message":"old unmarked error"}]'])(
    'does not add a switch section for old or invalid diagnostics %j',
    upstreamErrors => {
      const wrapper = mount(OpsRouteSwitchEvents, { props: { upstreamErrors } })
      expect(wrapper.find('[data-testid="route-switch-events"]').exists()).toBe(false)
    }
  )

  it('shows each marked event with its exact account, original message and upstream status', () => {
    const wrapper = mount(OpsRouteSwitchEvents, { props: { upstreamErrors: JSON.stringify([
      { message: 'unmarked error', account_name: 'not the cause', upstream_status_code: 500 },
      { route_switch_group: 'GPT-企业', account_name: 'Account A', upstream_status_code: 503, status_code: 502, message: 'A overloaded' },
      { route_switch_group: 'DeepSeek', account_id: 48, upstream_status_code: 429, message: 'B rate limited' },
      { message: 'last event did not switch', upstream_status_code: 504 }
    ]) } })
    const events = wrapper.findAll('[data-testid="route-switch-event"]')
    expect(events).toHaveLength(2)
    expect(events[0].text()).toContain('#2')
    expect(events[0].text()).toContain('切换至后续优先级分组：GPT-企业')
    expect(events[0].text()).toContain('503')
    expect(events[0].text()).toContain('Account A')
    expect(events[0].get('[data-testid="route-switch-event-message"]').text()).toBe('A overloaded')
    expect(events[1].text()).toContain('#3')
    expect(events[1].text()).toContain('切换至后续优先级分组：DeepSeek')
    expect(events[1].text()).toContain('48')
    expect(wrapper.text()).not.toContain('unmarked error')
    expect(wrapper.text()).not.toContain('not the cause')
    expect(wrapper.text()).not.toContain('last event did not switch')
  })

  it('updates when another log is opened and safely renders untrusted strings as text', async () => {
    const unsafe = '<img src=x onerror=alert(1)>'
    const wrapper = mount(OpsRouteSwitchEvents, { props: { upstreamErrors: JSON.stringify([
      { route_switch_group: unsafe, account_name: unsafe, message: unsafe }
    ]) } })
    expect(wrapper.text()).toContain(unsafe)
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.text()).toContain('—')
    await wrapper.setProps({ upstreamErrors: '[]' })
    expect(wrapper.find('[data-testid="route-switch-events"]').exists()).toBe(false)
  })
})
