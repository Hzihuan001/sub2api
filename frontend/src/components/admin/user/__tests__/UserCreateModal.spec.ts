import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import UserCreateModal from '../UserCreateModal.vue'

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isAdmin: true, isSuperAdmin: false })
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: { users: { create: vi.fn() } }
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))

describe('UserCreateModal roles', () => {
  it('lets an ordinary administrator create a peer manager but not a super admin', () => {
    const wrapper = mount(UserCreateModal, {
      props: { show: true },
      global: {
        stubs: {
          BaseDialog: {
            props: ['show', 'title'],
            template: '<div v-if="show"><slot /><slot name="footer" /></div>'
          },
          Icon: true,
          TotpStepUpDialog: true
        }
      }
    })

    const roles = wrapper.findAll('option').map((option) => option.attributes('value'))
    expect(roles).toEqual(['user', 'manager'])
  })
})
