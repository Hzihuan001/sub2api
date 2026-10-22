import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'

import type { ApiKey } from '@/types'
import { keysAPI } from '@/api'
import KeysView from '../KeysView.vue'

const {
  listKeys,
  updateKey,
  getPublicSettings,
  getDashboardApiKeysUsage,
  getAvailableGroups,
  getUserGroupRates,
  showError,
  showSuccess,
  copyToClipboard,
  isCurrentStep,
  nextStep,
} = vi.hoisted(() => ({
  listKeys: vi.fn(),
  updateKey: vi.fn(),
  getPublicSettings: vi.fn(),
  getDashboardApiKeysUsage: vi.fn(),
  getAvailableGroups: vi.fn(),
  getUserGroupRates: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  copyToClipboard: vi.fn(),
  isCurrentStep: vi.fn(),
  nextStep: vi.fn(),
}))

const messages: Record<string, string> = {
  'common.actions': 'Actions',
  'common.name': 'Name',
  'common.refresh': 'Refresh',
  'common.status': 'Status',
  'keys.apiKey': 'API Key',
  'keys.allGroups': 'All Groups',
  'keys.allStatus': 'All Status',
  'keys.columnSettings': 'Column Settings',
  'keys.createKey': 'Create API Key',
  'keys.created': 'Created',
  'keys.expiresAt': 'Expires',
  'keys.group': 'Group',
  'keys.id': 'ID',
  'keys.currentConcurrency': 'Current Concurrency',
  'keys.lastUsedAt': 'Last Used',
  'keys.lastUsedIP': 'Last Used IP',
  'keys.rateLimitColumn': 'Rate Limit',
  'keys.searchPlaceholder': 'Search name or key...',
  'keys.status.active': 'Active',
  'keys.status.expired': 'Expired',
  'keys.status.inactive': 'Inactive',
  'keys.status.quota_exhausted': 'Quota exhausted',
  'keys.usage': 'Usage',
}

vi.mock('@/api', () => ({
  keysAPI: {
    list: listKeys,
    create: vi.fn(),
    update: updateKey,
    delete: vi.fn(),
    toggleStatus: vi.fn(),
  },
  authAPI: {
    getPublicSettings,
  },
  usageAPI: {
    getDashboardApiKeysUsage,
  },
  userGroupsAPI: {
    getAvailable: getAvailableGroups,
    getUserGroupRates,
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({
    isCurrentStep,
    nextStep,
  }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard,
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

const createApiKey = (): ApiKey => ({
  id: 1,
  user_id: 1,
  key: 'sk-test-key',
  name: 'test-key',
  group_id: null,
  status: 'active',
  ip_whitelist: [],
  ip_blacklist: [],
  last_used_at: null,
  last_used_ip: null,
  quota: 0,
  quota_used: 0,
  expires_at: null,
  created_at: '2026-06-27T00:00:00Z',
  updated_at: '2026-06-27T00:00:00Z',
  current_concurrency: 3,
  rate_limit_5h: 0,
  rate_limit_1d: 0,
  rate_limit_7d: 0,
  usage_5h: 0,
  usage_1d: 0,
  usage_7d: 0,
  window_5h_start: null,
  window_1d_start: null,
  window_7d_start: null,
  reset_5h_at: null,
  reset_1d_at: null,
  reset_7d_at: null,
})

const AppLayoutStub = {
  template: '<div><slot /></div>',
}

const TablePageLayoutStub = {
  template: `
    <div>
      <slot name="filters" />
      <slot name="actions" />
      <slot name="table" />
      <slot name="pagination" />
    </div>
  `,
}

const DataTableStub = {
  name: 'DataTable',
  props: { columns: Array, data: Array, selectedKeys: Array, selectable: Boolean },
  emits: ['sort', 'update:selectedKeys'],
  template: `
    <div>
      <div data-test="columns">{{ columns.map((col) => col.key).join(',') }}</div>
      <div data-test="columns-meta">{{ JSON.stringify(columns.map((col) => ({ key: col.key, sortable: !!col.sortable }))) }}</div>
      <button data-test="sort-current-concurrency" @click="$emit('sort', 'current_concurrency', 'asc')">
        Sort Current Concurrency
      </button>
      <div v-for="row in data" :key="row.id">
        <div
          v-if="columns.some((col) => col.key === 'id')"
          data-test="key-id"
        >
          <slot name="cell-id" :value="row.id" :row="row" />
        </div>
        <slot name="cell-name" :value="row.name" :row="row" />
        <slot name="cell-group" :row="row" />
        <slot name="cell-actions" :row="row" />
        <div data-test="current-concurrency">
          <slot name="cell-current_concurrency" :value="row.current_concurrency" :row="row" />
        </div>
        <div
          v-if="columns.some((col) => col.key === 'last_used_ip')"
          data-test="last-used-ip"
        >
          <slot name="cell-last_used_ip" :value="row.last_used_ip" :row="row" />
        </div>
      </div>
      <slot name="empty" />
    </div>
  `,
}

const SelectStub = {
  name: 'Select',
  props: ['modelValue', 'options'],
  emits: ['update:modelValue'],
  template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"></select>',
}

const SearchInputStub = {
  name: 'SearchInput',
  props: ['modelValue'],
  emits: ['update:modelValue', 'search'],
  template: '<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
}

const PaginationStub = {
  name: 'Pagination',
  props: ['page', 'total', 'pageSize'],
  emits: ['update:page', 'update:pageSize'],
  template: `
    <div>
      <button data-test="page-size-50" @click="$emit('update:pageSize', 50)">50</button>
    </div>
  `,
}

const IconStub = {
  props: ['name'],
  template: '<span data-test="icon">{{ name }}</span>',
}

const mountView = async () => {
  const wrapper = mount(KeysView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: PaginationStub,
        BaseDialog: {
          name: 'BaseDialog',
          props: ['show', 'title', 'closeOnEscape', 'showCloseButton'],
          emits: ['close'],
          template: '<div v-if="show" role="dialog"><button data-test="close-dialog" @click="$emit(\'close\')">Close</button><slot /><slot name="footer" /></div>',
        },
        ConfirmDialog: true,
        EmptyState: true,
        Select: SelectStub,
        SearchInput: SearchInputStub,
        Icon: IconStub,
        UseKeyModal: true,
        BulkEditKeysModal: true,
        EndpointPopover: true,
        GroupBadge: true,
        GroupOptionItem: true,
        Teleport: true,
      },
    },
  })
  await flushPromises()
  await nextTick()
  return wrapper
}

const visibleColumnKeys = (wrapper: VueWrapper) =>
  wrapper.get('[data-test="columns"]').text().split(',').filter(Boolean)

const visibleColumnMeta = (wrapper: VueWrapper): Array<{ key: string; sortable: boolean }> =>
  JSON.parse(wrapper.get('[data-test="columns-meta"]').text())

const getButtonByText = (wrapper: VueWrapper, text: string) => {
  const button = wrapper.findAll('button').find((item) => item.text().includes(text))
  if (!button) {
    throw new Error(`Button not found: ${text}`)
  }
  return button
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

describe('user KeysView column settings', () => {
  beforeEach(() => {
    localStorage.clear()

    listKeys.mockReset()
    updateKey.mockReset()
    vi.mocked(keysAPI.create).mockReset()
    getPublicSettings.mockReset()
    getDashboardApiKeysUsage.mockReset()
    getAvailableGroups.mockReset()
    getUserGroupRates.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    copyToClipboard.mockReset()
    isCurrentStep.mockReset()
    nextStep.mockReset()

    listKeys.mockResolvedValue({
      items: [createApiKey()],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    getPublicSettings.mockResolvedValue({})
    getDashboardApiKeysUsage.mockResolvedValue({ stats: {} })
    getAvailableGroups.mockResolvedValue([])
    getUserGroupRates.mockResolvedValue({})
    isCurrentStep.mockReturnValue(false)
  })

  it.each([
    { initialStatus: 'quota_exhausted', status: 'active', formStatus: 'active' },
    { initialStatus: 'inactive', status: 'inactive', formStatus: 'inactive' },
    { initialStatus: 'active', status: 'active', formStatus: 'inactive' },
  ] as const)('syncs quota reset from $initialStatus to $status with form status $formStatus', async ({ initialStatus, status, formStatus }) => {
    getAvailableGroups.mockResolvedValue([{ id: 1, name: 'Group 1', platform: 'openai', status: 'active', subscription_type: 'standard', rate_multiplier: 1 }])
    const key: ApiKey = {
      ...createApiKey(), group_id: 1, quota: 10, quota_used: 10,
      status: initialStatus,
    }
    listKeys.mockResolvedValueOnce({ items: [key], total: 1, page: 1, page_size: 20, pages: 1 })
    updateKey.mockResolvedValue({ ...key, status, quota_used: 0 })
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    await wrapper.get('[data-tour="key-form-name"]').setValue('Unsaved name')
    const statusSelect = wrapper.findAllComponents({ name: 'Select' })
      .find((select) => select.props('options').length === 2 &&
        select.props('options')[0].value === 'active')!
    statusSelect.vm.$emit('update:modelValue', 'inactive')
    await wrapper.get('button[title="keys.resetQuotaUsed"]').trigger('click')
    const confirmation = wrapper.findAllComponents({ name: 'ConfirmDialog' })
      .find((dialog) => dialog.props('title') === 'keys.resetQuotaTitle')!
    confirmation.vm.$emit('confirm')
    await flushPromises()

    expect(updateKey).toHaveBeenNthCalledWith(1, key.id, { reset_quota: true })
    expect(wrapper.findComponent({ name: 'DataTable' }).props('data')[0])
      .toMatchObject({ status, quota_used: 0 })
    expect(statusSelect.props('modelValue')).toBe(formStatus)
    expect((wrapper.get('[data-tour="key-form-name"]').element as HTMLInputElement).value)
      .toBe('Unsaved name')

    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    expect(updateKey).toHaveBeenNthCalledWith(2, key.id, expect.objectContaining({ name: 'Unsaved name', status: formStatus }))
    wrapper.unmount()
  })

  it('uses the default API key columns with low-frequency columns hidden', async () => {
    const wrapper = await mountView()

    expect(visibleColumnKeys(wrapper)).toEqual([
      'name',
      'key',
      'group',
      'current_concurrency',
      'usage',
      'expires_at',
      'status',
      'created_at',
      'actions',
    ])
    expect(visibleColumnKeys(wrapper)).not.toContain('rate_limit')
    expect(visibleColumnKeys(wrapper)).not.toContain('last_used_at')
    expect(visibleColumnKeys(wrapper)).not.toContain('last_used_ip')
    expect(visibleColumnKeys(wrapper)).not.toContain('id')
  })

  it('opens bulk editing with only selected visible keys', async () => {
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    expect(table.props('selectable')).toBe(true)
    table.vm.$emit('update:selectedKeys', [1, 99])
    await nextTick()
    await wrapper.get('[data-test="bulk-edit-keys"]').trigger('click')
    const modal = wrapper.findComponent({ name: 'BulkEditKeysModal' })
    expect(modal.props('show')).toBe(true)
    expect(modal.props('selectedKeys').map((key: ApiKey) => key.id)).toEqual([1])
    wrapper.unmount()
  })

  it.each(['filter', 'page size', 'sort'])('clears selection on %s changes', async (change) => {
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    table.vm.$emit('update:selectedKeys', [1])
    await nextTick()
    if (change === 'filter') {
      wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('search')
    } else if (change === 'page size') {
      await wrapper.get('[data-test="page-size-50"]').trigger('click')
    } else {
      table.vm.$emit('sort', 'created_at', 'asc')
    }
    await flushPromises()
    expect(table.props('selectedKeys')).toEqual([])
    expect(wrapper.find('[data-test="bulk-edit-keys"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('removes successful keys from the selection and refreshes the table', async () => {
    listKeys.mockResolvedValue({
      items: [createApiKey(), { ...createApiKey(), id: 2, name: 'Second' }],
      total: 2, pages: 1
    })
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    table.vm.$emit('update:selectedKeys', [1, 2])
    await nextTick()
    await wrapper.get('[data-test="bulk-edit-keys"]').trigger('click')
    wrapper.findComponent({ name: 'BulkEditKeysModal' }).vm.$emit('updated', [1])
    await flushPromises()
    expect(listKeys).toHaveBeenCalledTimes(2)
    expect(table.props('selectedKeys')).toEqual([2])
    wrapper.unmount()
  })

  it('drops keys that are no longer visible after a refresh', async () => {
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    table.vm.$emit('update:selectedKeys', [1])
    await nextTick()
    listKeys.mockResolvedValue({ items: [], total: 0, pages: 0 })
    await wrapper.get('button[title="Refresh"]').trigger('click')
    await flushPromises()
    expect(table.props('selectedKeys')).toEqual([])
    wrapper.unmount()
  })

  it('shows a hidden column when toggled and persists the preference', async () => {
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await getButtonByText(wrapper, 'Rate Limit').trigger('click')
    await nextTick()

    expect(visibleColumnKeys(wrapper)).toContain('rate_limit')
    expect(localStorage.getItem('api-key-hidden-columns')).toBe(
      JSON.stringify(['id', 'last_used_at', 'last_used_ip'])
    )
    expect(localStorage.getItem('api-key-column-settings-version')).toBe('3')
  })

  it('shows the API key ID column when toggled', async () => {
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await getButtonByText(wrapper, 'ID').trigger('click')
    await nextTick()

    expect(visibleColumnKeys(wrapper)).toContain('id')
    expect(wrapper.get('[data-test="key-id"]').text()).toBe('#1')
    expect(visibleColumnMeta(wrapper).find((column) => column.key === 'id')?.sortable).toBe(true)
  })

  it('shows the last used IP column when toggled', async () => {
    listKeys.mockResolvedValueOnce({
      items: [{ ...createApiKey(), last_used_ip: '203.0.113.10' }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await getButtonByText(wrapper, 'Last Used IP').trigger('click')
    await nextTick()

    expect(visibleColumnKeys(wrapper)).toContain('last_used_ip')
    expect(wrapper.get('[data-test="last-used-ip"]').text()).toBe('203.0.113.10')
  })

  it('restores column preferences from localStorage on mount', async () => {
    localStorage.setItem('api-key-hidden-columns', JSON.stringify(['group', 'created_at']))
    localStorage.setItem('api-key-column-settings-version', '1')

    const wrapper = await mountView()

    expect(visibleColumnKeys(wrapper)).toEqual([
      'name',
      'key',
      'current_concurrency',
      'usage',
      'rate_limit',
      'expires_at',
      'status',
      'last_used_at',
      'actions',
    ])
    expect(localStorage.getItem('api-key-hidden-columns')).toBe(
      JSON.stringify(['group', 'created_at', 'last_used_ip', 'id'])
    )
    expect(localStorage.getItem('api-key-column-settings-version')).toBe('3')
  })

  it('does not include always-visible columns in the toggleable menu', async () => {
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await nextTick()

    const columnMenuText = wrapper.text()
    expect(columnMenuText).toContain('API Key')
    expect(columnMenuText).toContain('ID')
    expect(columnMenuText).toContain('Current Concurrency')
    expect(columnMenuText).toContain('Rate Limit')
    expect(columnMenuText).toContain('Last Used IP')
    expect(columnMenuText).not.toContain('Name')
    expect(columnMenuText).not.toContain('Actions')
  })

  it('renders the current concurrency value', async () => {
    const wrapper = await mountView()

    expect(wrapper.get('[data-test="current-concurrency"]').text()).toBe('3')
  })

  it('marks current concurrency as sortable', async () => {
    const wrapper = await mountView()

    const currentConcurrencyColumn = visibleColumnMeta(wrapper).find(
      (column) => column.key === 'current_concurrency'
    )
    expect(currentConcurrencyColumn?.sortable).toBe(true)
  })

  it('keeps filters and selected page size when sorting by current concurrency', async () => {
    getAvailableGroups.mockResolvedValue([{ id: 42, name: 'OpenAI' }])
    const wrapper = await mountView()

    await wrapper.get('[data-test="page-size-50"]').trigger('click')
    await flushPromises()

    await wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('update:modelValue', 'target')
    await wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('search')
    await flushPromises()

    const selects = wrapper.findAllComponents({ name: 'Select' })
    await selects[0].vm.$emit('update:modelValue', 42)
    await flushPromises()
    await selects[1].vm.$emit('update:modelValue', 'active')
    await flushPromises()

    listKeys.mockClear()

    await wrapper.get('[data-test="sort-current-concurrency"]').trigger('click')
    await flushPromises()

    expect(listKeys).toHaveBeenLastCalledWith(
      1,
      50,
      {
        search: 'target',
        status: 'active',
        group_id: 42,
        sort_by: 'current_concurrency',
        sort_order: 'asc',
      },
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
  })

  describe('key editor save locking', () => {
    const routeGroups = [1, 2, 3].map((id) => ({ id, name: `Group ${id}`, platform: 'openai', status: 'active', subscription_type: 'standard', rate_multiplier: 1 }))

    beforeEach(() => getAvailableGroups.mockResolvedValue(routeGroups))

    it.each(['create', 'edit'] as const)('submits a pending %s only once even if the form emits submit repeatedly', async (mode) => {
      const key = { ...createApiKey(), group_id: 1 }
      listKeys.mockResolvedValue({ items: [key], total: 1, pages: 1 })
      const pending = deferred<ApiKey>()
      if (mode === 'edit') updateKey.mockReturnValueOnce(pending.promise)
      else vi.mocked(keysAPI.create).mockReturnValueOnce(pending.promise)
      const wrapper = await mountView()
      if (mode === 'edit') await getButtonByText(wrapper, 'common.edit').trigger('click')
      else {
        await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
        await wrapper.get('[data-tour="key-form-name"]').setValue('Pending creation')
        wrapper.findComponent({ name: 'KeyBackupGroupsEditor' }).vm.$emit('update:modelValue', [1])
        await nextTick()
      }

      await wrapper.get('#key-form').trigger('submit')
      await wrapper.get('#key-form').trigger('submit')
      expect(mode === 'edit' ? updateKey : keysAPI.create).toHaveBeenCalledTimes(1)
      expect(wrapper.get<HTMLButtonElement>('[data-tour="key-form-submit"]').element.disabled).toBe(true)
      pending.resolve(key)
      await flushPromises()
      expect(wrapper.find('#key-form').exists()).toBe(false)
      wrapper.unmount()
    })

    it('locks the current key while saving so close or another edit cannot replace the pending draft', async () => {
      const first = { ...createApiKey(), name: 'Key A', group_id: 1 }
      const second = { ...createApiKey(), id: 2, name: 'Key B', group_id: 2 }
      listKeys.mockResolvedValue({ items: [first, second], total: 2, pages: 1 })
      const pending = deferred<ApiKey>()
      updateKey.mockReturnValueOnce(pending.promise)
      const wrapper = await mountView()
      const editButtons = () => wrapper.findAll('button').filter((button) => button.text().includes('common.edit'))
      await editButtons()[0].trigger('click')
      await wrapper.get('[data-tour="key-form-name"]').setValue('A pending draft')
      await wrapper.get('#key-form').trigger('submit')

      await getButtonByText(wrapper, 'common.cancel').trigger('click')
      await wrapper.get('[data-test="close-dialog"]').trigger('click')
      const dialog = wrapper.findAllComponents({ name: 'BaseDialog' }).find((component) => component.find('#key-form').exists())!
      expect(dialog.props('closeOnEscape')).toBe(false)
      expect(dialog.props('showCloseButton')).toBe(false)
      await editButtons()[1].trigger('click')
      await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
      expect(wrapper.get<HTMLInputElement>('[data-tour="key-form-name"]').element.value).toBe('A pending draft')
      expect(updateKey).toHaveBeenCalledWith(1, expect.objectContaining({ name: 'A pending draft' }))

      pending.resolve({ ...first, name: 'A pending draft' })
      await flushPromises()
      expect(wrapper.find('#key-form').exists()).toBe(false)
      await editButtons()[1].trigger('click')
      await wrapper.get('[data-tour="key-form-name"]').setValue('B new draft')
      await flushPromises()
      expect(wrapper.get<HTMLInputElement>('[data-tour="key-form-name"]').element.value).toBe('B new draft')
      wrapper.unmount()
    })

    it('keeps the edited name and routing policy after a failed save and unlocks the form for retry', async () => {
      const key = { ...createApiKey(), group_id: 1, backup_group_ids: [2] }
      listKeys.mockResolvedValue({ items: [key], total: 1, pages: 1 })
      const pending = deferred<ApiKey>()
      updateKey.mockReturnValueOnce(pending.promise)
      const wrapper = await mountView()
      await getButtonByText(wrapper, 'common.edit').trigger('click')
      await wrapper.get('[data-tour="key-form-name"]').setValue('Keep this draft')
      const policy = { rules: [{ type: 'consecutive_failures', threshold: 7, cooldown_seconds: 360 }] }
      wrapper.findComponent({ name: 'KeyRouteFailureRulesEditor' }).vm.$emit('update:modelValue', policy)
      await wrapper.get('#key-form').trigger('submit')
      pending.reject({ response: { data: { detail: 'Temporary save failure' } } })
      await flushPromises()

      expect(showError).toHaveBeenCalledWith('Temporary save failure')
      expect(wrapper.get<HTMLInputElement>('[data-tour="key-form-name"]').element.value).toBe('Keep this draft')
      expect(wrapper.findComponent({ name: 'KeyRouteFailureRulesEditor' }).props('modelValue')).toEqual(policy)
      expect(wrapper.get<HTMLButtonElement>('[data-tour="key-form-submit"]').element.disabled).toBe(false)
      updateKey.mockResolvedValueOnce(key)
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(updateKey).toHaveBeenCalledTimes(2)
      expect(updateKey).toHaveBeenLastCalledWith(1, expect.objectContaining({ name: 'Keep this draft', key_route_failure_policy: policy }))
      expect(wrapper.find('#key-form').exists()).toBe(false)
      wrapper.unmount()
    })
  })

  describe('priority route integration', () => {
    const routeGroups = [1, 2, 3, 4].map((id) => ({ id, name: `Group ${id}`, platform: 'openai', status: 'active', subscription_type: 'standard', rate_multiplier: 1 }))

    beforeEach(() => getAvailableGroups.mockResolvedValue(routeGroups))

    it('round-trips exact model restrictions, retains failed drafts, and explicitly clears restrictions', async () => {
      const original = { enabled: true, models: ['original-model'] }
      const key = { ...createApiKey(), group_id: 1, backup_group_ids: [2], key_model_policy: original }
      listKeys.mockResolvedValue({ items: [key], total: 1, pages: 1 })
      const wrapper = await mountView()
      await getButtonByText(wrapper, 'common.edit').trigger('click')
      const editor = wrapper.findComponent({ name: 'KeyModelPolicyEditor' })
      expect(editor.props('modelValue')).toEqual(original)
      editor.vm.$emit('update:modelValue', { enabled: true, models: [' custom-a ', 'custom-a', 'CUSTOM-A'] })
      await nextTick()
      expect(original.models).toEqual(['original-model'])
      updateKey.mockRejectedValueOnce({ response: { data: { detail: 'Temporary save failure' } } })
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(editor.props('modelValue').models).toEqual([' custom-a ', 'custom-a', 'CUSTOM-A'])
      expect(editor.props('disabled')).toBe(false)
      expect(wrapper.find('#key-form').exists()).toBe(true)

      const savedPolicy = { enabled: true, models: ['custom-a', 'CUSTOM-A'] }
      const saved = { ...key, key_model_policy: savedPolicy }
      updateKey.mockResolvedValue(saved)
      listKeys.mockResolvedValue({ items: [saved], total: 1, pages: 1 })
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(updateKey).toHaveBeenLastCalledWith(key.id, expect.objectContaining({ key_model_policy: savedPolicy }))
      await getButtonByText(wrapper, 'common.edit').trigger('click')
      const reopened = wrapper.findComponent({ name: 'KeyModelPolicyEditor' })
      expect(reopened.props('modelValue')).toEqual(savedPolicy)
      reopened.vm.$emit('update:modelValue', null)
      await nextTick()
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(updateKey).toHaveBeenLastCalledWith(key.id, expect.objectContaining({ key_model_policy: null }))
      wrapper.unmount()
    })

    it('blocks enabled empty or wildcard model restrictions before saving', async () => {
      listKeys.mockResolvedValue({ items: [{ ...createApiKey(), group_id: 1 }], total: 1, pages: 1 })
      const wrapper = await mountView()
      await getButtonByText(wrapper, 'common.edit').trigger('click')
      const editor = wrapper.findComponent({ name: 'KeyModelPolicyEditor' })
      for (const models of [[], ['gpt-*']]) {
        editor.vm.$emit('update:modelValue', { enabled: true, models })
        await nextTick()
        await wrapper.get('#key-form').trigger('submit')
        expect(updateKey).not.toHaveBeenCalled()
        expect(showError).toHaveBeenLastCalledWith('custom.keyRouting.modelPolicyValidation')
      }
      wrapper.unmount()
    })

    it('loads existing order, preserves it on edit, and explicitly clears it on removal', async () => {
      listKeys.mockResolvedValue({ items: [{ ...createApiKey(), group_id: 1, backup_group_ids: [3, 2] }], total: 1, pages: 1 })
      const wrapper = await mountView()
      await wrapper.get('[data-test="edit-key-backups"]').trigger('click')
      const editor = wrapper.findComponent({ name: 'KeyBackupGroupsEditor' })
      expect(editor.props('modelValue')).toEqual([1, 3, 2])
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(updateKey).toHaveBeenLastCalledWith(1, expect.objectContaining({ backup_group_ids: [3, 2] }))
      await wrapper.get('[data-test="edit-key-backups"]').trigger('click')
      wrapper.findComponent({ name: 'KeyBackupGroupsEditor' }).vm.$emit('update:modelValue', [1])
      await nextTick()
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(updateKey).toHaveBeenLastCalledWith(1, expect.objectContaining({ backup_group_ids: [] }))
      wrapper.unmount()
    })

    it('opens complete editing for a routed key when its first-priority group is clicked', async () => {
      listKeys.mockResolvedValue({ items: [{ ...createApiKey(), group_id: 1, backup_group_ids: [2] }], total: 1, pages: 1 })
      const wrapper = await mountView()
      await wrapper.get('button[title="keys.clickToChangeGroup"]').trigger('click')
      expect(wrapper.find('#key-form').exists()).toBe(true)
      expect(wrapper.findComponent({ name: 'KeyBackupGroupsEditor' }).props('modelValue')).toEqual([1, 2])
      expect(updateKey).not.toHaveBeenCalled()
      wrapper.unmount()
    })

    it('preserves priority order and rejects malformed duplicate routes', async () => {
      listKeys.mockResolvedValue({ items: [{ ...createApiKey(), group_id: 1, backup_group_ids: [3, 2] }], total: 1, pages: 1 })
      const wrapper = await mountView()
      await getButtonByText(wrapper, 'common.edit').trigger('click')
      const editor = wrapper.findComponent({ name: 'KeyBackupGroupsEditor' })
      editor.vm.$emit('update:modelValue', [2, 3, 2])
      await nextTick()
      await wrapper.get('#key-form').trigger('submit')
      expect(updateKey).not.toHaveBeenCalled()
      expect(showError).toHaveBeenCalledWith('custom.keyRouting.duplicate')
      expect(wrapper.findComponent({ name: 'KeyBackupGroupsEditor' }).props('modelValue')).toEqual([2, 3, 2])

      editor.vm.$emit('update:modelValue', [4, 3, 2])
      await nextTick()
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(updateKey).toHaveBeenCalledWith(1, expect.objectContaining({ group_id: 4, backup_group_ids: [3, 2] }))
      wrapper.unmount()
    })

    it.each([
      { backups: [99], error: 'groupUnavailable' },
      { backups: [2, 2], error: 'duplicate' },
      { backups: [2, 3, 4, 5, 6], error: 'tooMany' }
    ])('preserves an invalid existing route $backups until explicitly repaired', async ({ backups, error }) => {
      listKeys.mockResolvedValue({ items: [{ ...createApiKey(), group_id: 1, backup_group_ids: backups }], total: 1, pages: 1 })
      const wrapper = await mountView()
      await getButtonByText(wrapper, 'common.edit').trigger('click')
      await wrapper.get('[data-tour="key-form-name"]').setValue('Unsaved name')
      await wrapper.get('#key-form').trigger('submit')
      expect(updateKey).not.toHaveBeenCalled()
      expect(showError).toHaveBeenCalledWith(`custom.keyRouting.${error}`)
      expect(wrapper.findComponent({ name: 'KeyBackupGroupsEditor' }).props('modelValue')).toEqual([1, ...backups])

      wrapper.findComponent({ name: 'KeyBackupGroupsEditor' }).vm.$emit('update:modelValue', [1])
      await nextTick()
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(updateKey).toHaveBeenCalledWith(1, expect.objectContaining({ name: 'Unsaved name', backup_group_ids: [] }))
      wrapper.unmount()
    })

    it('submits create order and resets backups when reopening the form', async () => {
      const wrapper = await mountView()
      await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
      await wrapper.get('[data-tour="key-form-name"]').setValue('Routed key')
      wrapper.findComponent({ name: 'KeyBackupGroupsEditor' }).vm.$emit('update:modelValue', [1, 3, 2])
      await nextTick()
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(vi.mocked(keysAPI.create).mock.calls[0][8]).toEqual([3, 2])
      await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
      expect(wrapper.findComponent({ name: 'KeyBackupGroupsEditor' }).props('modelValue')).toEqual([])
      wrapper.unmount()
    })

    it('rejects subscription combinations but keeps single subscription editing valid', async () => {
      getAvailableGroups.mockResolvedValue([{ ...routeGroups[0], subscription_type: 'subscription' }, routeGroups[1]])
      listKeys.mockResolvedValue({ items: [{ ...createApiKey(), group_id: 1 }], total: 1, pages: 1 })
      const wrapper = await mountView()
      await getButtonByText(wrapper, 'common.edit').trigger('click')
      const editor = wrapper.findComponent({ name: 'KeyBackupGroupsEditor' })
      editor.vm.$emit('update:modelValue', [2, 1])
      await nextTick()
      await wrapper.get('#key-form').trigger('submit')
      expect(updateKey).not.toHaveBeenCalled()
      expect(showError).toHaveBeenCalledWith('custom.keyRouting.standardOnly')
      editor.vm.$emit('update:modelValue', [1])
      await nextTick()
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(updateKey).toHaveBeenCalledWith(1, expect.objectContaining({ group_id: 1, backup_group_ids: [] }))
      wrapper.unmount()
    })
  })

  describe('advanced settings integration', () => {
    const advancedKey = (): ApiKey => ({
      ...createApiKey(),
      group_id: 1,
      ip_whitelist: ['203.0.113.10', '198.51.100.0/24'],
      ip_blacklist: ['192.0.2.5'],
      quota: 25.5,
      rate_limit_5h: 2.5,
      rate_limit_1d: 7.5,
      rate_limit_7d: 30,
      expires_at: '2030-01-02T03:04:00.000Z',
    })

    beforeEach(() => {
      getAvailableGroups.mockResolvedValue([{ id: 1, name: 'Group 1', platform: 'openai', status: 'active', subscription_type: 'standard', rate_multiplier: 1 }])
      updateKey.mockResolvedValue(advancedKey())
    })

    it('starts create with advanced settings collapsed and retains drafts across folding', async () => {
      const wrapper = await mountView()
      await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
      const toggle = wrapper.get('[data-test="key-advanced-settings-toggle"]')
      const content = wrapper.get('[data-test="key-advanced-settings-content"]')
      const quota = wrapper.get<HTMLInputElement>('[placeholder="keys.quotaAmountPlaceholder"]')
      const originalInput = quota.element
      expect(toggle.attributes('aria-expanded')).toBe('false')
      expect(content.attributes('style')).toContain('display: none')
      expect(quota.element.value).toBe('')

      await toggle.trigger('click')
      await quota.setValue('12.5')
      await toggle.trigger('click')
      expect(content.attributes('style')).toContain('display: none')
      expect(quota.element).toBe(originalInput)
      await toggle.trigger('click')
      expect(quota.element.value).toBe('12.5')

      await wrapper.get('[data-tour="key-form-name"]').setValue('Quota draft')
      wrapper.findComponent({ name: 'KeyBackupGroupsEditor' }).vm.$emit('update:modelValue', [1])
      await nextTick()
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(vi.mocked(keysAPI.create).mock.calls[0][5]).toBe(12.5)
      wrapper.unmount()
    })

    it('preserves existing IP, quota, rate and expiration values when saved without expanding', async () => {
      const key = advancedKey()
      listKeys.mockResolvedValue({ items: [key], total: 1, pages: 1 })
      const wrapper = await mountView()
      await getButtonByText(wrapper, 'common.edit').trigger('click')
      expect(wrapper.get('[data-test="key-advanced-settings-toggle"]').attributes('aria-expanded')).toBe('false')
      expect(wrapper.get('[data-test="key-advanced-settings-content"]').attributes('style')).toContain('display: none')
      expect(wrapper.get<HTMLTextAreaElement>('[placeholder="keys.ipWhitelistPlaceholder"]').element.value).toBe(key.ip_whitelist.join('\n'))
      expect(wrapper.get<HTMLTextAreaElement>('[placeholder="keys.ipBlacklistPlaceholder"]').element.value).toBe(key.ip_blacklist.join('\n'))
      expect(wrapper.get<HTMLInputElement>('[placeholder="keys.quotaAmountPlaceholder"]').element.value).toBe('25.5')
      expect(wrapper.findAll<HTMLInputElement>('input[placeholder="0"]').map((input) => input.element.value)).toEqual(['2.5', '7.5', '30'])
      expect(new Date(wrapper.get<HTMLInputElement>('input[type="datetime-local"]').element.value).toISOString()).toBe(key.expires_at)

      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(updateKey).toHaveBeenCalledWith(key.id, expect.objectContaining({
        ip_whitelist: key.ip_whitelist,
        ip_blacklist: key.ip_blacklist,
        quota: key.quota,
        rate_limit_5h: key.rate_limit_5h,
        rate_limit_1d: key.rate_limit_1d,
        rate_limit_7d: key.rate_limit_7d,
        expires_at: key.expires_at,
      }))
      wrapper.unmount()
    })

    it('allows expanded advanced fields to be edited and serializes the retained values after folding', async () => {
      const key = advancedKey()
      listKeys.mockResolvedValue({ items: [key], total: 1, pages: 1 })
      const wrapper = await mountView()
      await getButtonByText(wrapper, 'common.edit').trigger('click')
      const toggle = wrapper.get('[data-test="key-advanced-settings-toggle"]')
      await toggle.trigger('click')
      expect(toggle.attributes('aria-expanded')).toBe('true')
      await wrapper.get('[placeholder="keys.ipWhitelistPlaceholder"]').setValue(' 203.0.113.11 \n\n198.51.100.0/25')
      await wrapper.get('[placeholder="keys.ipBlacklistPlaceholder"]').setValue('192.0.2.7')
      await wrapper.get('[placeholder="keys.quotaAmountPlaceholder"]').setValue('48.25')
      const rateInputs = wrapper.findAll<HTMLInputElement>('input[placeholder="0"]')
      await rateInputs[0].setValue('4.5')
      await rateInputs[1].setValue('10')
      await rateInputs[2].setValue('50')
      const expiration = '2030-03-04T12:34'
      await wrapper.get('input[type="datetime-local"]').setValue(expiration)
      await toggle.trigger('click')
      await toggle.trigger('click')
      expect(wrapper.get<HTMLInputElement>('[placeholder="keys.quotaAmountPlaceholder"]').element.value).toBe('48.25')
      expect(wrapper.findAll<HTMLInputElement>('input[placeholder="0"]').map((input) => input.element.value)).toEqual(['4.5', '10', '50'])

      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(updateKey).toHaveBeenCalledWith(key.id, expect.objectContaining({
        ip_whitelist: ['203.0.113.11', '198.51.100.0/25'],
        ip_blacklist: ['192.0.2.7'],
        quota: 48.25,
        rate_limit_5h: 4.5,
        rate_limit_1d: 10,
        rate_limit_7d: 50,
        expires_at: new Date(expiration).toISOString(),
      }))
      wrapper.unmount()
    })
  })

  describe('Wanmo-style service group editor', () => {
    const routeGroups = [1, 2, 3, 4].map((id) => ({ id, name: `Group ${id}`, platform: 'openai', status: 'active', subscription_type: 'standard', rate_multiplier: 1 }))

    beforeEach(() => getAvailableGroups.mockResolvedValue(routeGroups))

    it('does not silently upgrade an existing key and submits an explicit user conversion', async () => {
      listKeys.mockResolvedValue({ items: [{ ...createApiKey(), group_id: 1, backup_group_ids: [2] }], total: 1, pages: 1 })
      const wrapper = await mountView()
      await getButtonByText(wrapper, 'common.edit').trigger('click')
      expect(wrapper.find('[data-test="key-route-scope-upgrade"]').exists()).toBe(true)
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(updateKey.mock.calls[0][1]).not.toHaveProperty('key_route_scope')
      await getButtonByText(wrapper, 'common.edit').trigger('click')
      await wrapper.get('[data-test="key-route-scope-upgrade"]').trigger('click')
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(updateKey).toHaveBeenLastCalledWith(1, expect.objectContaining({ key_route_scope: 'account_v2' }))
      expect(updateKey.mock.calls.at(-1)?.[1]).not.toHaveProperty('key_route_revision')
      wrapper.unmount()
    })

    it('defaults new routed keys to account scope but leaves single-group creates on the legacy path', async () => {
      const wrapper = await mountView()
      await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
      await wrapper.get('[data-tour="key-form-name"]').setValue('New route')
      const editor = wrapper.findComponent({ name: 'KeyBackupGroupsEditor' })
      editor.vm.$emit('update:modelValue', [1, 2])
      await nextTick()
      expect(wrapper.find('[data-test="key-route-scope-upgrade"]').exists()).toBe(false)
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(vi.mocked(keysAPI.create).mock.calls.at(-1)?.[11]).toBe('account_v2')
      await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
      await wrapper.get('[data-tour="key-form-name"]').setValue('Single route')
      wrapper.findComponent({ name: 'KeyBackupGroupsEditor' }).vm.$emit('update:modelValue', [1])
      await nextTick()
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(vi.mocked(keysAPI.create).mock.calls.at(-1)?.[11]).toBe('group_v1')
      wrapper.unmount()
    })

    it('uses one priority-list editor for create and allows selecting its first group', async () => {
      const wrapper = await mountView()
      await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
      const editor = wrapper.findComponent({ name: 'KeyBackupGroupsEditor' })
      expect(editor.exists()).toBe(true)
      editor.vm.$emit('update:modelValue', [2])
      await nextTick()
      expect(editor.props('modelValue')).toEqual([2])
      expect(wrapper.find('[data-tour="key-form-provider"]').exists()).toBe(false)
      expect(wrapper.find('[data-tour="key-form-group"]').exists()).toBe(false)
      wrapper.unmount()
    })

    it('shows route rules only when multiple priority groups are configured', async () => {
      const wrapper = await mountView()
      await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
      const editor = wrapper.findComponent({ name: 'KeyBackupGroupsEditor' })
      expect(wrapper.find('[data-test="key-route-failure-rules"]').exists()).toBe(false)

      editor.vm.$emit('update:modelValue', [1])
      await nextTick()
      expect(wrapper.find('[data-test="key-route-failure-rules"]').exists()).toBe(false)

      editor.vm.$emit('update:modelValue', [1, 2])
      await nextTick()
      expect(wrapper.find('[data-test="key-route-failure-rules"]').exists()).toBe(true)

      await wrapper.get('[data-test="route-remove-1"]').trigger('click')
      expect(wrapper.find('[data-test="key-route-failure-rules"]').exists()).toBe(false)
      wrapper.unmount()
    })

    it('shows automatic failover for an existing routed key but not for a single-group key', async () => {
      listKeys.mockResolvedValue({ items: [{ ...createApiKey(), group_id: 1, backup_group_ids: [2] }], total: 1, pages: 1 })
      const wrapper = await mountView()
      await getButtonByText(wrapper, 'common.edit').trigger('click')
      expect(wrapper.find('[data-test="key-route-failure-rules"]').exists()).toBe(true)

      await wrapper.get('[data-test="route-remove-1"]').trigger('click')
      expect(wrapper.find('[data-test="key-route-failure-rules"]').exists()).toBe(false)
      wrapper.unmount()

      listKeys.mockResolvedValue({ items: [{ ...createApiKey(), group_id: 1 }], total: 1, pages: 1 })
      const singleGroupWrapper = await mountView()
      await getButtonByText(singleGroupWrapper, 'common.edit').trigger('click')
      expect(singleGroupWrapper.find('[data-test="key-route-failure-rules"]').exists()).toBe(false)
      singleGroupWrapper.unmount()
    })

    it.each(['create', 'edit'])('saves %s with no policy after removing the last backup despite an invalid hidden draft', async (mode) => {
      if (mode === 'edit') {
        listKeys.mockResolvedValue({ items: [{ ...createApiKey(), group_id: 1, backup_group_ids: [2] }], total: 1, pages: 1 })
      }
      const wrapper = await mountView()
      if (mode === 'edit') {
        await getButtonByText(wrapper, 'common.edit').trigger('click')
      } else {
        await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
        await wrapper.get('[data-tour="key-form-name"]').setValue('Single-group key')
        const editor = wrapper.findComponent({ name: 'KeyBackupGroupsEditor' })
        editor.vm.$emit('update:modelValue', [1, 2])
        await nextTick()
      }
      wrapper.findComponent({ name: 'KeyRouteFailureRulesEditor' }).vm.$emit('update:modelValue', {
        rules: [{ status_codes: [], threshold: 0, window_seconds: 60, cooldown_seconds: 600 }]
      })
      await nextTick()
      await wrapper.get('[data-test="route-remove-1"]').trigger('click')
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()

      expect(showError).not.toHaveBeenCalled()
      if (mode === 'edit') {
        expect(updateKey).toHaveBeenCalledWith(1, expect.objectContaining({ backup_group_ids: [], key_route_failure_policy: null }))
      } else {
        expect(vi.mocked(keysAPI.create).mock.calls[0][8]).toEqual([])
        expect(vi.mocked(keysAPI.create).mock.calls[0][9]).toBeNull()
      }
      wrapper.unmount()
    })

    it('keeps the unsaved policy draft when the last backup is removed and added again', async () => {
      const wrapper = await mountView()
      await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
      const editor = wrapper.findComponent({ name: 'KeyBackupGroupsEditor' })
      editor.vm.$emit('update:modelValue', [1, 2])
      await nextTick()
      const policyDraft = { rules: [{ status_codes: [], threshold: 7, window_seconds: 120, cooldown_seconds: 300 }] }
      wrapper.findComponent({ name: 'KeyRouteFailureRulesEditor' }).vm.$emit('update:modelValue', policyDraft)
      await nextTick()

      await wrapper.get('[data-test="route-remove-0"]').trigger('click')
      expect(wrapper.find('[data-test="key-route-failure-rules"]').exists()).toBe(false)
      editor.vm.$emit('update:modelValue', [3, 1])
      await nextTick()
      expect(wrapper.findComponent({ name: 'KeyRouteFailureRulesEditor' }).props('modelValue')).toEqual(policyDraft)
      wrapper.unmount()
    })

    it('submits primary and backup order from the same editor', async () => {
      const wrapper = await mountView()
      await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
      await wrapper.get('[data-tour="key-form-name"]').setValue('Routed key')
      const editor = wrapper.findComponent({ name: 'KeyBackupGroupsEditor' })
      editor.vm.$emit('update:modelValue', [1, 3, 2])
      await nextTick()
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(vi.mocked(keysAPI.create).mock.calls[0].slice(0, 2)).toEqual(['Routed key', 1])
      expect(vi.mocked(keysAPI.create).mock.calls[0][8]).toEqual([3, 2])
      wrapper.unmount()
    })

    it('allows the first priority to be changed without exposing a separate provider selector', async () => {
      listKeys.mockResolvedValue({ items: [{ ...createApiKey(), group_id: 1, backup_group_ids: [2] }], total: 1, pages: 1 })
      const wrapper = await mountView()
      await getButtonByText(wrapper, 'common.edit').trigger('click')
      const editor = wrapper.findComponent({ name: 'KeyBackupGroupsEditor' })
      editor.vm.$emit('update:modelValue', [4, 2])
      await nextTick()
      expect(editor.props('modelValue')).toEqual([4, 2])
      expect(wrapper.find('[data-tour="key-form-provider"]').exists()).toBe(false)
      expect(wrapper.find('[data-tour="key-form-group"]').exists()).toBe(false)
      wrapper.unmount()
    })
  })
})
