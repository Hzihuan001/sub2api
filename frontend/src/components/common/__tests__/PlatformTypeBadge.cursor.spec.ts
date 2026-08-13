import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { vi } from 'vitest'

import PlatformTypeBadge from '../PlatformTypeBadge.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

describe('PlatformTypeBadge Cursor platform', () => {
  it('labels Cursor OAuth accounts with the indigo platform chip', () => {
    const wrapper = mount(PlatformTypeBadge, {
      props: {
        platform: 'cursor',
        type: 'oauth',
      },
    })

    expect(wrapper.text()).toContain('Cursor')
    expect(wrapper.text()).toContain('OAuth')
    expect(wrapper.html()).toContain('bg-indigo-100')
  })

  it('labels Cursor apikey accounts as Key without Grok-specific plan handling', () => {
    const wrapper = mount(PlatformTypeBadge, {
      props: {
        platform: 'cursor',
        type: 'apikey',
        planType: 'free',
      },
    })

    expect(wrapper.text()).toContain('Cursor')
    expect(wrapper.text()).toContain('Key')
    // Cursor free plans render as plain Free, not Grok Free.
    expect(wrapper.text()).toContain('Free')
    expect(wrapper.text()).not.toContain('Grok Free')
    expect(wrapper.find('[data-testid="grok-free-plan-icon"]').exists()).toBe(false)
  })
})
