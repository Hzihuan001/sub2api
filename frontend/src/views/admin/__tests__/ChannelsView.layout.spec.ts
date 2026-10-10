import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'

const { channelsList, getModelDefaultPricing, groupsGetAll, settingsGetWebSearchEmulationConfig, syncPricingModels } = vi.hoisted(() => ({
  channelsList: vi.fn(),
  getModelDefaultPricing: vi.fn(),
  groupsGetAll: vi.fn(),
  settingsGetWebSearchEmulationConfig: vi.fn(),
  syncPricingModels: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      list: vi.fn(),
    },
    channels: {
      create: vi.fn(),
      getModelDefaultPricing,
      list: channelsList,
      remove: vi.fn(),
      syncPricingModels,
      update: vi.fn(),
    },
    groups: {
      getAll: groupsGetAll,
    },
    settings: {
      getWebSearchEmulationConfig: settingsGetWebSearchEmulationConfig,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
  }),
}))

vi.mock('@/utils/apiError', () => ({
  extractApiErrorMessage: (_err: unknown, fallback: string) => fallback,
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (_key: string, paramsOrFallback?: Record<string, unknown> | string, fallback?: string) => {
        if (typeof paramsOrFallback === 'string') return paramsOrFallback
        return fallback ?? _key
      },
    }),
  }
})

import ChannelsView from '../ChannelsView.vue'

const SlotStub = defineComponent({
  setup(_, { slots }) {
    return () => h('div', slots.default?.())
  },
})

const TablePageLayoutStub = defineComponent({
  setup(_, { slots }) {
    return () => h('main', [slots.filters?.(), slots.table?.(), slots.pagination?.()])
  },
})

const DataTableStub = defineComponent({
  props: {
    data: {
      type: Array,
      default: () => [],
    },
  },
  setup(props, { slots }) {
    return () => h('div', {}, props.data.length === 0 ? slots.empty?.() : [])
  },
})

const BaseDialogStub = defineComponent({
  props: {
    show: Boolean,
    title: String,
  },
  setup(props, { slots }) {
    return () => (props.show ? h('section', { class: 'base-dialog-stub' }, [slots.default?.(), slots.footer?.()]) : null)
  },
})

const PricingEntryCardStub = defineComponent({
  props: {
    entry: {
      type: Object,
      required: true,
    },
    platform: String,
  },
  emits: ['update', 'remove'],
  setup(props) {
    return () => {
      const entry = props.entry as { models?: string[], input_price?: number | string | null }
      return h('div', { class: 'pricing-entry-card-stub', 'data-platform': props.platform }, [
        h('span', { class: 'pricing-models' }, entry.models?.join(',') ?? ''),
        h('span', { class: 'pricing-input-price' }, String(entry.input_price ?? '')),
      ])
    }
  },
})

const SelectStub = defineComponent({
  props: {
    modelValue: {
      type: [String, Number, Boolean, null],
      default: '',
    },
    options: {
      type: Array,
      default: () => [],
    },
    placeholder: String,
  },
  emits: ['update:modelValue', 'change'],
  setup(props, { emit }) {
    type Option = { value: string | number | boolean, label: string }
    return () =>
      h('select', {
        value: props.modelValue ?? '',
        onChange: (event: Event) => {
          const value = (event.target as HTMLSelectElement).value
          emit('update:modelValue', value)
          emit('change', value, null)
        },
      }, (props.options as Option[]).map((option) => h('option', { value: option.value }, option.label)))
  },
})

describe('ChannelsView 弹框布局', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    channelsList.mockResolvedValue({ items: [], total: 0 })
    getModelDefaultPricing.mockResolvedValue({ found: false })
    groupsGetAll.mockResolvedValue([])
    settingsGetWebSearchEmulationConfig.mockResolvedValue({ enabled: false, providers: [] })
  })

  it('创建渠道表单为焦点 ring 保留横向安全区', async () => {
    const wrapper = mount(ChannelsView, {
      global: {
        stubs: {
          AppLayout: SlotStub,
          BaseDialog: BaseDialogStub,
          ConfirmDialog: true,
          DataTable: DataTableStub,
          EmptyState: SlotStub,
          Icon: true,
          Pagination: true,
          PlatformIcon: true,
          PricingEntryCard: true,
          Select: SelectStub,
          TablePageLayout: TablePageLayoutStub,
          Toggle: true,
        },
      },
    })

    await flushPromises()

    const createButton = wrapper
      .findAll('button')
      .find((button) => button.text().includes('Create Channel'))
    expect(createButton).toBeTruthy()

    await createButton!.trigger('click')
    await flushPromises()

    const form = wrapper.get('form#channel-form')
    expect(form.classes()).toEqual(expect.arrayContaining(['flex-1', 'overflow-y-auto', 'pt-4']))
  })

})
