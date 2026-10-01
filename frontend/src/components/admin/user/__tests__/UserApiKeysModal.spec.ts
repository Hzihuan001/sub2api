import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import UserApiKeysModal from '../UserApiKeysModal.vue'
import type { AdminUser } from '@/types'

const { getKeys, createKey, deleteKey, copyToClipboard } = vi.hoisted(() => ({
  getKeys: vi.fn(),
  createKey: vi.fn(),
  deleteKey: vi.fn(),
  copyToClipboard: vi.fn().mockResolvedValue(true)
}))
vi.mock('@/api/admin', () => ({ adminAPI: {
  users: { getUserApiKeys: getKeys, createUserApiKey: createKey },
  groups: { getAll: vi.fn().mockResolvedValue([]) },
  apiKeys: { updateApiKeyGroup: vi.fn(), deleteApiKey: deleteKey },
} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard }) }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => {
  getKeys.mockReset()
  createKey.mockReset()
  deleteKey.mockReset()
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
const keys = (id: number, name: string) => ({ items: [{ id, name, key: 'sk-example-key-value-for-tests', status: 'active', created_at: '2026-09-20', group_id: null }] })
async function open() {
  const wrapper = mount(UserApiKeysModal, {
    props: { show: false, user: user(1) },
    global: { stubs: {
      BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
      ConfirmDialog: { props: ['show'], emits: ['confirm', 'cancel'], template: '<div v-if="show"><button data-test="confirm-delete" @click="$emit(\'confirm\')">confirm</button><button data-test="cancel-delete" @click="$emit(\'cancel\')">cancel</button></div>' },
      GroupBadge: true, GroupOptionItem: true,
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
  })
})
