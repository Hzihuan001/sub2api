import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'

const { getPelicanShowcase } = vi.hoisted(() => ({ getPelicanShowcase: vi.fn() }))

vi.mock('vue-router', () => ({ useRoute: () => ({ query: {} }) }))
vi.mock('@/api/pelicanShowcase', () => ({ getPelicanShowcase }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: defineComponent({ template: '<div><slot /></div>' }) }))
vi.mock('@/components/icons/Icon.vue', () => ({ default: defineComponent({ template: '<span />' }) }))

import PelicanShowcaseView from '../PelicanShowcaseView.vue'

enableAutoUnmount(afterEach)

describe('PelicanShowcaseView successful results', () => {
  it('hides failed result cards and error details while retaining group status and counts', async () => {
    getPelicanShowcase.mockResolvedValueOnce({
      groups: [{
        id: 1,
        name: 'GPT-企业',
        platform: 'openai',
        status: 'degraded',
        last_test_at: '2026-11-04T01:00:00Z',
        success_count: 2,
        failure_count: 3,
        results: [
          { id: 11, status: 'success', model_id: 'gpt-test', response_text: '<html><body>good</body></html>', latency_ms: 1000, started_at: '', finished_at: '2026-11-04T01:00:00Z' },
          { id: 12, status: 'failed', model_id: 'gpt-test', error_message: 'secret upstream failure', latency_ms: 0, started_at: '', finished_at: '2026-11-04T00:59:00Z' },
        ],
        latest_failure: { id: 12, status: 'failed', model_id: 'gpt-test', error_message: 'secret upstream failure', latency_ms: 0, started_at: '', finished_at: '2026-11-04T00:59:00Z' },
      }],
    })

    const wrapper = mount(PelicanShowcaseView)
    await flushPromises()

    expect(wrapper.text()).toContain('GPT-企业')
    expect(wrapper.text()).toContain('5 张')
    expect(wrapper.text()).toContain('最近测试失败')
    expect(wrapper.text()).not.toContain('secret upstream failure')
    expect(wrapper.findAll('article article')).toHaveLength(1)
    expect(wrapper.findAll('iframe')).toHaveLength(1)
  })

  it('does not fall back to a failed latest result when no success exists', async () => {
    getPelicanShowcase.mockResolvedValueOnce({
      groups: [{
        id: 2,
        name: 'Kiro',
        platform: 'anthropic',
        status: 'degraded',
        success_count: 0,
        failure_count: 1,
        results: [],
        latest: { id: 21, status: 'failed', model_id: 'claude-test', error_message: 'hidden reason', latency_ms: 10, started_at: '', finished_at: '' },
      }],
    })

    const wrapper = mount(PelicanShowcaseView)
    await flushPromises()

    expect(wrapper.text()).toContain('1 张')
    expect(wrapper.text()).not.toContain('hidden reason')
    expect(wrapper.findAll('iframe')).toHaveLength(0)
    expect(wrapper.findAll('article article')).toHaveLength(0)
  })
})
