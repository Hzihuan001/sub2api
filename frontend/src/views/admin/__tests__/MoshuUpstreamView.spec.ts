import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import MoshuUpstreamView from '../MoshuUpstreamView.vue'

const mocks = vi.hoisted(() => ({
  selected: true,
  authorized: true,
  models: [] as string[],
  auth: { isSuperAdmin: true },
  enroll: vi.fn(async (payload: { base_url: string; enrollment_code: string }) => ({ ...payload })),
  configureProduct: vi.fn(async () => ({})),
  showError: vi.fn()
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (s: string) => s }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError: mocks.showError }) }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('@/components/admin/account/AccountTestModal.vue', () => ({ default: { props: ['show', 'account'], template: '<div data-test="upstream-account-test">{{ show ? account?.name : "" }}</div>' } }))
vi.mock('@/api/admin', () => ({
  accountsAPI: { getById: async (id: number) => ({ id, name: '真实上游账号', platform: 'openai', type: 'apikey', status: 'active' }) },
  groupsAPI: { getAllIncludingInactive: async () => [{ id: 5, name: '我的销售名' }] },
  moshuResellerAPI: {
    status: async () => ({ enabled: true, connected: true, connection: { base_url: 'https://main.example', reseller_name: 'L1', catalog_version: 1 }, products: [{ id: 3, display_name: '上游名称', platform: 'openai', authorized: mocks.authorized, selected: mocks.selected, local_group_id: 5, local_account_id: 2, cost_rate_multiplier: 1, sales_rate_multiplier: 1.7, models: mocks.models }] }),
    profits: async () => ({ items: [] }),
    enroll: mocks.enroll,
    configureProduct: mocks.configureProduct
  }
}))

describe('reseller configuration', () => {
  beforeEach(() => { vi.clearAllMocks(); mocks.auth.isSuperAdmin = true; mocks.selected = true; mocks.authorized = true; mocks.models = [] })
  it('allows same-site reconnect while connected and preserves the sales name and price', async () => {
    const wrapper = mount(MoshuUpstreamView)
    await flushPromises()
    expect(wrapper.text()).not.toContain('admin.moshuUpstream.protocolSubtitle')
    expect(wrapper.text()).not.toContain('https://main.example')
    expect(wrapper.text()).not.toContain('common.refresh')
    expect(wrapper.text()).not.toContain('admin.moshuUpstream.localConfigured')
    expect(wrapper.text()).not.toContain('admin.moshuUpstream.profitTitle')
    expect(wrapper.text()).not.toContain('admin.moshuUpstream.legacyTitle')
    expect(wrapper.text()).not.toContain('admin.moshuUpstream.syncSettlements')
    expect(wrapper.text()).toContain('admin.moshuUpstream.saleActive')
    expect(wrapper.text()).toContain('admin.moshuUpstream.allModels')
    expect(wrapper.text()).not.toContain('0 models')
    await wrapper.findAll('button').find(b => b.text() === 'admin.accounts.testConnection')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="upstream-account-test"]').text()).toBe('真实上游账号')
    expect(wrapper.text()).toContain('重新授权 / 计费账号换绑后同步 Key')
    expect(wrapper.findAll('input').some(i => i.element.value === '我的销售名')).toBe(true)
    expect(wrapper.find('input[type="number"]').element.value).toBe('1.7')
    await wrapper.find('input[placeholder="输入一次性授权码"]').setValue('new-enrollment')
    await wrapper.findAll('button').find(b => b.text() === '重新授权并同步')!.trigger('click')
    await flushPromises()
    expect(mocks.enroll).toHaveBeenCalledTimes(1)
    expect(mocks.enroll).toHaveBeenCalledWith({ base_url: 'https://main.example', enrollment_code: 'new-enrollment' })
    expect(wrapper.find('input[placeholder="输入一次性授权码"]').element.value).toBe('')
    expect(mocks.showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('does not expose reconnection or batch controls to non-superadmin', async () => {
    mocks.auth.isSuperAdmin = false
    const wrapper = mount(MoshuUpstreamView)
    await flushPromises()
    expect(wrapper.text()).not.toContain('重新授权并同步')
    expect(wrapper.text()).not.toContain('批量启用/保存')
    wrapper.unmount()
  })
  it('does not render revoked upstream products', async () => {
    mocks.authorized = false
    const wrapper = mount(MoshuUpstreamView)
    await flushPromises()
    expect(wrapper.text()).not.toContain('上游名称')
    expect(wrapper.text()).not.toContain('admin.accounts.testConnection')
    wrapper.unmount()
  })
  it('reloads recovery pointers after failure without losing the entered price', async () => {
    const wrapper = mount(MoshuUpstreamView)
    await flushPromises()
    await wrapper.find('input[type="number"]').setValue('0.01')
    mocks.configureProduct.mockImplementationOnce(async () => { mocks.selected = false; throw new Error('retry binding') })
    await wrapper.findAll('button').find(b => b.text() === 'common.save')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('admin.moshuUpstream.savedResources')
    expect(wrapper.text()).toContain('admin.accounts.testConnection')
    expect(wrapper.find('input[type="number"]').element.value).toBe('0.01')
    expect(mocks.showError).toHaveBeenCalledWith('retry binding')
    wrapper.unmount()
  })
  it.each(['single', 'batch'])('submits zero pricing through %s save without a cost override', async (mode) => {
    const confirmation = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const wrapper = mount(MoshuUpstreamView)
    await flushPromises()
    await wrapper.find('input[type="number"]').setValue('0')
    if (mode === 'batch') {
      await wrapper.find('article input[type="checkbox"]').setValue(true)
      await wrapper.findAll('button').find(b => b.text().startsWith('批量启用/保存'))!.trigger('click')
    } else {
      await wrapper.findAll('button').find(b => b.text() === 'common.save')!.trigger('click')
    }
    await flushPromises()
    expect(mocks.configureProduct).toHaveBeenCalledWith(3, { selected: true, sales_name: '我的销售名', sales_multiplier: 0 })
    expect(mocks.showError).not.toHaveBeenCalled()
    wrapper.unmount()
    confirmation.mockRestore()
  })
})
