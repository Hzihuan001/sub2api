import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import UserApiKeysModal from '../UserApiKeysModal.vue'
import type { AdminUser } from '@/types'

const { getKeys, getGroups, createKey, deleteKey, updateScope, copyToClipboard } = vi.hoisted(() => ({
  getKeys: vi.fn(),
  getGroups: vi.fn(),
  createKey: vi.fn(),
  deleteKey: vi.fn(),
  updateScope: vi.fn(),
  copyToClipboard: vi.fn().mockResolvedValue(true)
}))
vi.mock('@/api/admin', () => ({ adminAPI: {
  users: { getUserApiKeys: getKeys, createUserApiKey: createKey },
  groups: { getAll: getGroups },
  apiKeys: { updateApiKeyGroup: vi.fn(), deleteApiKey: deleteKey, updateApiKeyRouteScope: updateScope },
} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard }) }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => {
  getKeys.mockReset()
  getGroups.mockReset().mockResolvedValue([])
  createKey.mockReset()
  deleteKey.mockReset()
  updateScope.mockReset()
  copyToClipboard.mockClear()
  vi.spyOn(console, 'error').mockImplementation(() => {})
})
afterEach(() => vi.restoreAllMocks())
function deferred() {
  let resolve!: (value: unknown) => void
  let reject!: (error: Error) => void
  const promise = new Promise((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
const user = (id: number) => ({ id, email: `user${id}@example.com`, username: `user${id}` }) as AdminUser
const keys = (id: number, name: string) => ({ items: [{ id, user_id: 1, name, key: 'sk-example-key-value-for-tests', status: 'active', created_at: '2026-09-20', group_id: null }] })
const editorStub = { name: 'AdminUserApiKeyEditor', props: ['apiKey', 'user', 'groups'], emits: ['saved', 'close'], template: '<div data-test="admin-editor" />' }
async function open() {
  const wrapper = mount(UserApiKeysModal, {
    props: { show: false, user: user(1) },
    global: { stubs: {
      BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
      ConfirmDialog: { props: ['show'], emits: ['confirm', 'cancel'], template: '<div v-if="show"><button data-test="confirm-delete" @click="$emit(\'confirm\')">confirm</button><button data-test="cancel-delete" @click="$emit(\'cancel\')">cancel</button></div>' },
      GroupBadge: true, GroupOptionItem: true,
      AdminUserApiKeyEditor: editorStub,
    } },
  })
  await wrapper.setProps({ show: true })
  return wrapper
}
async function switchUser(wrapper: Awaited<ReturnType<typeof open>>) {
  await wrapper.setProps({ show: false })
  await wrapper.setProps({ show: true, user: user(2) })
}

describe('user API key loading', () => {
  it('keeps legacy routed keys unchanged until an explicit admin upgrade', async () => {
    const key = { ...keys(1, 'routed').items[0], backup_group_ids: [2], key_route_scope: 'group_v1' }
    getKeys.mockResolvedValue({ items: [key] })
    updateScope.mockResolvedValue({ api_key: { ...key, key_route_scope: 'account_v2', key_route_revision: 91 } })
    const wrapper = await open(); await flushPromises()
    expect(updateScope).not.toHaveBeenCalled()
    await wrapper.get('[data-test="key-route-scope-upgrade"]').trigger('click')
    await flushPromises()
    expect(updateScope).toHaveBeenCalledWith(1, 'account_v2')
    expect(wrapper.find('[data-test="key-route-scope-upgrade"]').exists()).toBe(false)
  })

  it('disables account-scope conversion in read-only mode', async () => {
    getKeys.mockResolvedValue({ items: [{ ...keys(1, 'routed').items[0], backup_group_ids: [2] }] })
    const wrapper = await open(); await flushPromises()
    await wrapper.setProps({ readOnly: true })
    const upgrade = wrapper.get('[data-test="key-route-scope-upgrade"]')
    expect(upgrade.attributes('disabled')).toBeDefined()
    await upgrade.trigger('click')
    expect(updateScope).not.toHaveBeenCalled()
  })

  it('does not display the previous user keys when the next load fails', async () => {
    getKeys.mockResolvedValueOnce(keys(1, 'first-user-key')).mockRejectedValueOnce(new Error('unavailable'))
    const wrapper = await open(); await flushPromises()
    expect(wrapper.text()).toContain('first-user-key')
    await switchUser(wrapper); await flushPromises()
    expect(wrapper.text()).toContain('user2@example.com')
    expect(wrapper.text()).not.toContain('first-user-key')
  })

  it('does not replace current keys with a late previous response', async () => {
    const old = deferred()
    getKeys.mockReturnValueOnce(old.promise).mockResolvedValueOnce(keys(2, 'current-user-key'))
    const wrapper = await open()
    await switchUser(wrapper); await flushPromises()
    old.resolve(keys(1, 'old-user-key')); await flushPromises()
    expect(wrapper.text()).toContain('current-user-key')
    expect(wrapper.text()).not.toContain('old-user-key')
  })

  it('keeps the current request loading when an obsolete request fails', async () => {
    const old = deferred(); const current = deferred()
    getKeys.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const wrapper = await open()
    await switchUser(wrapper)
    old.reject(new Error('obsolete')); await flushPromises()
    expect(wrapper.find('.animate-spin').exists()).toBe(true)
    current.resolve(keys(2, 'current-user-key')); await flushPromises()
    expect(wrapper.text()).toContain('current-user-key')
    expect(wrapper.find('.animate-spin').exists()).toBe(false)
  })

  it('loads keys when the selected user changes while the dialog is open', async () => {
    getKeys.mockResolvedValueOnce(keys(1, 'first-user-key')).mockResolvedValueOnce(keys(2, 'second-user-key'))
    const wrapper = await open(); await flushPromises()
    await wrapper.setProps({ user: user(2) }); await flushPromises()
    expect(getKeys).toHaveBeenLastCalledWith(2)
    expect(wrapper.text()).toContain('second-user-key')
    expect(wrapper.text()).not.toContain('first-user-key')
  })
})

describe('user API key actions', () => {
  it('shows existing backups and protects their route from quick group changes', async () => {
    getKeys.mockResolvedValue({ items: [{ ...keys(1, 'routed-key').items[0], group_id: 1, backup_group_ids: [2, 3] }] })
    const wrapper = await open(); await flushPromises()
    expect(wrapper.get('[data-test="admin-key-backups"]').text()).toContain('custom.keyRouting.summary')
    expect(wrapper.get('[data-test="admin-key-backups"]').text()).not.toContain('custom.keyRouting.adminEditHint')
    expect(wrapper.find('[data-test="quick-change-key-group"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="edit-user-api-key"]').exists()).toBe(true)
  })

  it('opens the full editor for a routed key and applies the saved key', async () => {
    const key = { ...keys(1, 'routed-key').items[0], group_id: 1, backup_group_ids: [2, 3] }
    getKeys.mockResolvedValue({ items: [key] })
    const wrapper = await open(); await flushPromises()
    await wrapper.get('[data-test="edit-user-api-key"]').trigger('click'); await flushPromises()
    const editor = wrapper.findComponent(editorStub)
    expect(editor.props('apiKey')).toEqual(key)
    expect(editor.props('user').id).toBe(1)
    editor.vm.$emit('saved', { ...key, name: 'updated-key', group_id: 3, backup_group_ids: [1, 2] })
    editor.vm.$emit('close'); await flushPromises()
    expect(wrapper.text()).toContain('updated-key')
    expect(wrapper.find('[data-test="admin-editor"]').exists()).toBe(false)
  })

  it('allows editing single-group keys too', async () => {
    getKeys.mockResolvedValue(keys(1, 'single-key'))
    const wrapper = await open(); await flushPromises()
    await wrapper.get('[data-test="edit-user-api-key"]').trigger('click'); await flushPromises()
    expect(wrapper.findComponent(editorStub).props('apiKey').name).toBe('single-key')
  })

  it('closes the previous user editor when the selected user changes', async () => {
    getKeys.mockResolvedValueOnce(keys(1, 'first')).mockResolvedValueOnce({ items: [{ ...keys(2, 'second').items[0], user_id: 2 }] })
    const wrapper = await open(); await flushPromises()
    await wrapper.get('[data-test="edit-user-api-key"]').trigger('click'); await flushPromises()
    const oldEditor = wrapper.findComponent(editorStub)
    await wrapper.setProps({ user: user(2) }); await flushPromises()
    expect(wrapper.find('[data-test="admin-editor"]').exists()).toBe(false)
    oldEditor.vm.$emit('saved', { ...keys(2, 'stale-update').items[0], user_id: 1 })
    await flushPromises()
    expect(wrapper.text()).toContain('second')
    expect(wrapper.text()).not.toContain('stale-update')
  })

  it('does not replace the current groups with a late previous user load', async () => {
    const oldGroups = deferred()
    getGroups.mockReturnValueOnce(oldGroups.promise).mockResolvedValueOnce([{ id: 2, name: 'current-group' }])
    getKeys.mockResolvedValueOnce(keys(1, 'first')).mockResolvedValueOnce({ items: [{ ...keys(2, 'second').items[0], user_id: 2 }] })
    const wrapper = await open(); await flushPromises()
    await wrapper.setProps({ user: user(2) }); await flushPromises()
    oldGroups.resolve([{ id: 1, name: 'old-group' }]); await flushPromises()
    await wrapper.get('[data-test="edit-user-api-key"]').trigger('click'); await flushPromises()
    expect(wrapper.findComponent(editorStub).props('groups')).toEqual([{ id: 2, name: 'current-group' }])
  })

  it('unmounts a full editor when management becomes read-only', async () => {
    getKeys.mockResolvedValue(keys(1, 'editable'))
    const wrapper = await open(); await flushPromises()
    await wrapper.get('[data-test="edit-user-api-key"]').trigger('click'); await flushPromises()
    await wrapper.setProps({ readOnly: true })
    expect(wrapper.find('[data-test="admin-editor"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="edit-user-api-key"]').exists()).toBe(false)
  })

  it('creates a key for the selected user', async () => {
    getKeys.mockResolvedValue(keys(1, 'existing-key'))
    const created = { ...keys(2, 'created-key').items[0], user_id: 1 }
    createKey.mockResolvedValue(created)
    const wrapper = await open(); await flushPromises()

    await wrapper.find('[data-test="create-user-api-key"]').trigger('click')
    await wrapper.find('[data-test="create-user-api-key-name"]').setValue('new key')
    await wrapper.find('[data-test="create-user-api-key-form"]').trigger('submit')
    await flushPromises()

    expect(createKey).toHaveBeenCalledWith(1, { name: 'new key', group_id: null })
    expect(wrapper.text()).toContain('created-key')
  })

  it('does not insert a previous user key when its creation completes after switching users', async () => {
    const oldCreate = deferred()
    getKeys.mockResolvedValueOnce(keys(1, 'first-user-key')).mockResolvedValueOnce({ items: [{ ...keys(2, 'current-user-key').items[0], user_id: 2 }] })
    createKey.mockReturnValueOnce(oldCreate.promise)
    const wrapper = await open(); await flushPromises()
    await wrapper.get('[data-test="create-user-api-key"]').trigger('click')
    await wrapper.get('[data-test="create-user-api-key-name"]').setValue('first-user-new-key')
    await wrapper.get('[data-test="create-user-api-key-form"]').trigger('submit')
    expect(createKey).toHaveBeenCalledWith(1, { name: 'first-user-new-key', group_id: null })
    await switchUser(wrapper); await flushPromises()
    oldCreate.resolve({ ...keys(3, 'late-first-user-key').items[0], user_id: 1 }); await flushPromises()
    expect(wrapper.text()).toContain('current-user-key')
    expect(wrapper.text()).not.toContain('late-first-user-key')
    expect(wrapper.get('[data-test="create-user-api-key"]').attributes('disabled')).toBeUndefined()
  })

  it('can reopen editing after an obsolete group load completes while closed and read-only', async () => {
    const oldGroups = deferred()
    getGroups.mockReturnValueOnce(oldGroups.promise).mockResolvedValueOnce([{ id: 2, name: 'current-group' }])
    getKeys.mockResolvedValue(keys(1, 'current-key'))
    const wrapper = await open(); await flushPromises()
    expect(wrapper.get('[data-test="edit-user-api-key"]').attributes('disabled')).toBeDefined()
    await wrapper.setProps({ show: false, readOnly: true })
    oldGroups.resolve([{ id: 1, name: 'obsolete-group' }]); await flushPromises()
    await wrapper.setProps({ show: true }); await flushPromises()
    expect(wrapper.find('[data-test="edit-user-api-key"]').exists()).toBe(false)
    await wrapper.setProps({ readOnly: false })
    expect(wrapper.get('[data-test="edit-user-api-key"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-test="edit-user-api-key"]').trigger('click'); await flushPromises()
    expect(wrapper.findComponent(editorStub).props('groups')).toEqual([{ id: 2, name: 'current-group' }])
    expect(getGroups).toHaveBeenCalledTimes(2)
  })

  it('copies the full API key value', async () => {
    getKeys.mockResolvedValue(keys(1, 'copy-key'))
    const wrapper = await open(); await flushPromises()

    await wrapper.find('[data-test="copy-user-api-key"]').trigger('click')

    expect(copyToClipboard).toHaveBeenCalledWith('sk-example-key-value-for-tests', 'admin.users.apiKeyCopied')
  })

  it('deletes a key after confirmation', async () => {
    getKeys.mockResolvedValue(keys(1, 'delete-key'))
    deleteKey.mockResolvedValue({ message: 'deleted' })
    const wrapper = await open(); await flushPromises()

    await wrapper.find('[data-test="delete-user-api-key"]').trigger('click')
    await wrapper.find('[data-test="confirm-delete"]').trigger('click')
    await flushPromises()

    expect(deleteKey).toHaveBeenCalledWith(1)
    expect(wrapper.text()).not.toContain('delete-key')
  })

  it('keeps copy available but hides mutations in read-only mode', async () => {
    getKeys.mockResolvedValue(keys(1, 'read-only-key'))
    const wrapper = mount(UserApiKeysModal, {
      props: { show: false, user: user(1), readOnly: true },
      global: { stubs: {
        BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
        ConfirmDialog: true,
        GroupBadge: true, GroupOptionItem: true,
      } },
    })
    await wrapper.setProps({ show: true })
    await flushPromises()

    expect(wrapper.find('[data-test="copy-user-api-key"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="create-user-api-key"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="delete-user-api-key"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="edit-user-api-key"]').exists()).toBe(false)
  })
})
