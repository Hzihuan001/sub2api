import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import MoshuUpstreamView from '../MoshuUpstreamView.vue'

const mocks = vi.hoisted(() => ({
  selected: true,
  authorized: true,
  models: [] as string[],
  localAccountID: 2 as number | undefined,
  costOverride: undefined as number | undefined,
  setProductCost: vi.fn(async () => ({})),
  auth: { isSuperAdmin: true },
  enroll: vi.fn(async (payload: { base_url: string; enrollment_code: string }) => ({ ...payload })),
  configureProduct: vi.fn(async () => ({})),
  ensureTestAccount: vi.fn(async () => ({ account_id: 42 })),
  getAccountByID: vi.fn(async (id: number) => ({ id, name: '真实上游账号', platform: 'openai', type: 'apikey', status: 'active' })),
  probeUpstreamModels: vi.fn(async () => ({ models: ['model-a', 'model-b', 'model-b'] })),
  balance: vi.fn(async () => ({ balance: 88.5, frozen_balance: 3.25, warning: false })),
  showError: vi.fn()
  ,reloadAfterEnrollment: vi.fn()
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (s: string, params?: { count?: number }) => params?.count == null ? s : `${s}:${params.count}` }) }))
vi.mock('@/utils/format', () => ({ formatCurrency: (value: number) => `$${value.toFixed(2)}` }))
vi.mock('@/utils/resellerRefresh', () => ({ reloadAfterResellerEnrollment: mocks.reloadAfterEnrollment }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError: mocks.showError }) }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('@/components/admin/account/AccountTestModal.vue', () => ({ default: { props: ['show', 'account'], template: '<div data-test="upstream-account-test">{{ show ? account?.name : "" }}</div>' } }))
vi.mock('@/api/admin', () => ({
  accountsAPI: { getById: mocks.getAccountByID, probeUpstreamModels: mocks.probeUpstreamModels },
  groupsAPI: { getAllIncludingInactive: async () => [{ id: 5, name: '我的销售名' }] },
  moshuResellerAPI: {
    status: async () => ({ enabled: true, connected: true, connection: { base_url: 'https://main.example', reseller_name: 'L1', catalog_version: 1 }, products: [{ id: 3, display_name: '上游名称', platform: 'openai', authorized: mocks.authorized, selected: mocks.selected, local_group_id: 5, local_account_id: mocks.localAccountID, cost_rate_multiplier: 1, cost_rate_override: mocks.costOverride, sales_rate_multiplier: 1.7, capacity: 23, models: mocks.models }] }),
    balance: mocks.balance,
    profits: async () => ({ items: [] }),
    enroll: mocks.enroll,
    configureProduct: mocks.configureProduct,
    setProductCost: mocks.setProductCost,
    ensureTestAccount: mocks.ensureTestAccount
  }
}))

describe('reseller configuration', () => {
  beforeEach(() => { mocks.costOverride = undefined })
  it.each([0, 0.1234])('ignores legacy cost override %s and keeps retail editing', async (rate) => {
    mocks.costOverride = rate
    mocks.auth.isSuperAdmin = false
    mocks.selected = false
    const wrapper = mount(MoshuUpstreamView)
    await flushPromises()
    expect(wrapper.find('[data-test="cost-multiplier"]').exists()).toBe(false)
    expect(wrapper.get('article').text()).toContain('admin.moshuUpstream.costMultiplier 1')
    expect(wrapper.get('article input[type="number"]').element.value).toBe('1.7')
    expect(mocks.setProductCost).not.toHaveBeenCalled()
    expect(mocks.configureProduct).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  beforeEach(() => { vi.clearAllMocks(); mocks.auth.isSuperAdmin = true; mocks.selected = true; mocks.authorized = true; mocks.models = []; mocks.localAccountID = 2; mocks.probeUpstreamModels.mockResolvedValue({ models: ['model-a', 'model-b', 'model-b'] }) })
  it('allows same-site reconnect while connected and preserves the sales name and price', async () => {
    const wrapper = mount(MoshuUpstreamView)
    await flushPromises()
    expect(wrapper.get('article').element.closest('template')).toBeNull()
    expect(wrapper.text()).not.toContain('admin.moshuUpstream.protocolSubtitle')
    expect(wrapper.text()).not.toContain('https://main.example')
    expect(wrapper.text()).not.toContain('common.refresh')
    expect(wrapper.text()).not.toContain('admin.moshuUpstream.localConfigured')
    expect(wrapper.text()).not.toContain('admin.moshuUpstream.profitTitle')
    expect(wrapper.text()).not.toContain('admin.moshuUpstream.legacyTitle')
    expect(wrapper.text()).not.toContain('admin.moshuUpstream.syncSettlements')
    expect(wrapper.text()).not.toContain('admin.moshuUpstream.title')
    expect(wrapper.text()).toContain('admin.moshuUpstream.saleActive')
    expect(wrapper.text()).toContain('admin.moshuUpstream.modelCount:2')
    expect(wrapper.text()).toContain('admin.moshuUpstream.accountBalance')
    expect(wrapper.text()).toContain('$88.50')
    expect(wrapper.text()).toContain('$3.25')
    expect(wrapper.text()).toContain('$85.25')
    expect(mocks.balance).toHaveBeenCalledTimes(1)
    expect(mocks.probeUpstreamModels).toHaveBeenCalledWith(2)
    await wrapper.findAll('button').find(b => b.text() === 'admin.accounts.testConnection')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="upstream-account-test"]').text()).toBe('真实上游账号')
    expect(wrapper.text()).toContain('重新授权 / 计费账号换绑后同步 Key')
    expect(wrapper.text()).toContain('本站用户、余额、API Key、历史日志和系统设置继续保留')
    expect(wrapper.text()).not.toContain('仅支持同一主站、同一代理商重新授权')
    expect(wrapper.findAll('input').some(i => i.element.value === '我的销售名')).toBe(true)
    expect(wrapper.find('input[type="number"]').element.value).toBe('1.7')
    expect(wrapper.findAll('input[type="number"]')[1].element.value).toBe('23')
    await wrapper.find('input[placeholder="输入一次性授权码"]').setValue('new-enrollment')
    await wrapper.findAll('button').find(b => b.text() === '重新授权并同步')!.trigger('click')
    await flushPromises()
    expect(mocks.enroll).toHaveBeenCalledTimes(1)
    expect(mocks.reloadAfterEnrollment).toHaveBeenCalledTimes(1)
    expect(mocks.enroll).toHaveBeenCalledWith({ base_url: 'https://main.example', enrollment_code: 'new-enrollment' })
    expect(wrapper.find('input[placeholder="输入一次性授权码"]').element.value).toBe('')
    expect(mocks.showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('hides the model suffix when live discovery fails', async () => {
    mocks.probeUpstreamModels.mockRejectedValueOnce(new Error('model endpoint unavailable'))
    const wrapper = mount(MoshuUpstreamView)
    await flushPromises()
    expect(wrapper.text()).not.toContain('admin.moshuUpstream.modelCount')
    expect(mocks.showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('keeps the current station and authorization code on enrollment rejection', async () => {
    mocks.enroll.mockRejectedValueOnce(new Error('不支持切换代理商'))
    const wrapper = mount(MoshuUpstreamView)
    await flushPromises()
    await wrapper.find('input[placeholder="输入一次性授权码"]').setValue('another-tenant')
    await wrapper.findAll('button').find(b => b.text() === '重新授权并同步')!.trigger('click')
    await flushPromises()
    expect(mocks.reloadAfterEnrollment).not.toHaveBeenCalled()
    expect(wrapper.find('input[placeholder="输入一次性授权码"]').element.value).toBe('another-tenant')
    expect(wrapper.text()).toContain('上游名称')
    expect(mocks.showError).toHaveBeenCalledWith('不支持切换代理商')
    wrapper.unmount()
  })
  it('offers the standard connection test for a product without an existing local account', async () => {
    mocks.localAccountID = undefined
    mocks.selected = false
    const wrapper = mount(MoshuUpstreamView)
    await flushPromises()
    const testButton = wrapper.findAll('button').find(b => b.text() === 'admin.accounts.testConnection')
    expect(testButton).toBeDefined()
    await testButton!.trigger('click')
    await flushPromises()
    expect(mocks.ensureTestAccount).toHaveBeenCalledWith(3)
    expect(mocks.getAccountByID).toHaveBeenCalledWith(42)
    expect(wrapper.get('[data-test="upstream-account-test"]').text()).toBe('真实上游账号')
    wrapper.unmount()
  })
  it('allows manager channel controls but hides reconnection and key rotation', async () => {
    mocks.auth.isSuperAdmin = false
    const wrapper = mount(MoshuUpstreamView)
    await flushPromises()
    expect(wrapper.text()).not.toContain('重新授权并同步')
    expect(wrapper.text()).toContain('批量启用/保存')
    expect(wrapper.text()).toContain('上游名称')
    expect(wrapper.text()).toContain('admin.accounts.testConnection')
    expect(wrapper.text()).toContain('$85.25')
    expect(wrapper.text()).not.toContain('admin.moshuUpstream.rotate')
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
    expect(mocks.configureProduct).toHaveBeenCalledWith(3, { selected: true, sales_name: '我的销售名', sales_multiplier: 0, capacity: 23 })
    expect(mocks.showError).not.toHaveBeenCalled()
    wrapper.unmount()
    confirmation.mockRestore()
  })
})
