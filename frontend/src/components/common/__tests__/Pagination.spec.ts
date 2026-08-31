import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import Pagination from '../Pagination.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

const createWrapper = () => {
  return mount(Pagination, {
    props: {
      total: 500,
      page: 1,
      pageSize: 20,
      showJump: true
    },
    global: {
      stubs: {
        Icon: true,
        Select: true
      }
    }
  })
}

describe('Pagination', () => {
  it('jumps when a number input updates the model with a numeric value', async () => {
    const wrapper = createWrapper()
    const input = wrapper.get('input[type="number"]')

    await input.setValue('7')
    await wrapper.get('button.btn-ghost').trigger('click')

    expect(wrapper.emitted('update:page')).toEqual([[7]])
  })

  it('clamps an out-of-range page when Enter is pressed', async () => {
    const wrapper = createWrapper()
    const input = wrapper.get('input[type="number"]')

    await input.setValue('999')
    await input.trigger('keyup.enter')

    expect(wrapper.emitted('update:page')).toEqual([[25]])
  })
})
