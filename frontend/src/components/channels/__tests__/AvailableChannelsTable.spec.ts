import { createPinia } from 'pinia'
import { shallowMount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import AvailableChannelsTable from '../AvailableChannelsTable.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

describe('AvailableChannelsTable', () => {
  it('exposes the table-wrapper class required by TablePageLayout scrolling', () => {
    const wrapper = shallowMount(AvailableChannelsTable, {
      global: { plugins: [createPinia()] },
      props: {
        columns: {
          name: 'Name',
          description: 'Description',
          platform: 'Platform',
          groups: 'Groups',
          supportedModels: 'Supported models'
        },
        rows: [],
        loading: false,
        pricingKeyPrefix: 'pricing',
        noPricingLabel: 'No pricing',
        noModelsLabel: 'No models',
        emptyLabel: 'Empty',
        userGroupRates: {}
      }
    })

    expect(wrapper.classes()).toContain('table-wrapper')
  })
})
