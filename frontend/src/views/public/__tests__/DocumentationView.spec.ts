import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h, type PropType } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, describe, expect, it, vi } from 'vitest'

import DocumentationView from '../DocumentationView.vue'

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ cachedPublicSettings: null, siteName: 'MoShu' })
}))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isAuthenticated: false })
}))

const dir = dirname(fileURLToPath(import.meta.url))
const source = readFileSync(resolve(dir, '../DocumentationView.vue'), 'utf8')

const guides = [
  { id: 'codex', label: 'Codex 接入指南', title: 'Codex 接入指南' },
  { id: 'claude-cli', label: 'Claude CLI 接入指南', title: 'Claude Code 接入指南' },
  { id: 'claude-desktop', label: 'Claude Desktop', title: 'Claude Desktop 配置方法' },
  { id: 'workbuddy', label: 'WorkBuddy 接入指南', title: 'WorkBuddy 接入指南' },
  { id: 'trae', label: 'Trae 接入指南', title: 'Trae 接入指南' },
  { id: 'cherry-studio', label: 'Cherry Studio', title: 'CherryStudio 接入指南' },
  { id: 'zcode', label: 'Zcode', title: 'Zcode 配置示例' },
  { id: 'opencode', label: 'OpenCode', title: 'OpenCode 配置示例' }
] as const

// Keep the real view and RouterLinks while avoiding Select's popup mechanics.
const SelectStub = defineComponent({
  props: {
    modelValue: { type: [String, Number, Boolean], default: null },
    options: {
      type: Array as PropType<Array<{ value: string; label: string }>>,
      default: () => []
    }
  },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    return () => h('select', {
      value: props.modelValue,
      onChange: (event: Event) => emit('update:modelValue', (event.target as HTMLSelectElement).value)
    }, props.options.map((option) => h('option', { value: option.value }, option.label)))
  }
})

const wrappers: VueWrapper[] = []

afterEach(() => {
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount())
})

async function renderPage(path: string) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/docs/:topic?/:tool?', component: DocumentationView },
      { path: '/:pathMatch(.*)*', component: { render: () => null } }
    ]
  })
  await router.push(path)
  await router.isReady()
  const wrapper = mount(DocumentationView, {
    global: { plugins: [router], stubs: { Select: SelectStub } }
  })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, router }
}

describe('DocumentationView', () => {
  it('keeps only token creation and CC Switch as top-level topics', async () => {
    const { wrapper } = await renderPage('/docs/token')
    const links = wrapper.get('nav[aria-label="文档目录"]').findAll('a')
    expect(links.map((link) => link.text())).toEqual(['创建令牌', 'CCSwitch 的使用'])
    expect(links.map((link) => link.attributes('href'))).toEqual(['/docs/token', '/docs/ccswitch'])
    expect(source).not.toContain("id: 'overview'")
    expect(source).not.toContain("id: 'codex-claude'")
  })

  it('continues to render the token and CC Switch guides with their screenshots', async () => {
    const { wrapper, router } = await renderPage('/docs/token')
    expect(wrapper.get('main h1').text()).toBe('创建 API Key')
    expect(wrapper.findAll('main img')).toHaveLength(4)

    await router.push('/docs/ccswitch')
    await flushPromises()
    expect(wrapper.get('main h1').text()).toBe('CC Switch 的使用')
    expect(wrapper.findAll('main img')).toHaveLength(10)
    expect(wrapper.get('nav[aria-label="常用工具接入指南"]').findAll('a[aria-current="page"]')).toHaveLength(0)
  })

  it('lists tools in the requested order, with Claude Desktop immediately after Claude CLI', async () => {
    const { wrapper } = await renderPage('/docs/tools')
    const links = wrapper.get('nav[aria-label="常用工具接入指南"]').findAll('a')
    expect(links.map((link) => link.text())).toEqual(guides.map((guide) => guide.label))
    expect(links.map((link) => link.attributes('href'))).toEqual(
      guides.map((guide) => `/docs/tools/${guide.id}`)
    )
    expect(wrapper.findAll('select')).toHaveLength(1)
    const mobileOptions = wrapper.get('select[aria-label="选择文档"]').findAll('option')
    expect(mobileOptions.map((option) => option.text()))
      .toEqual(['创建令牌', 'CCSwitch 的使用', ...guides.map((guide) => guide.label)])
    expect(mobileOptions.map((option) => option.attributes('value')))
      .toEqual(['token', 'ccswitch', ...guides.map((guide) => guide.id)])
    expect(wrapper.find('select[aria-label="选择接入工具"]').exists()).toBe(false)
  })

  it.each(['/docs/tools', '/docs/tools/unknown-tool'])(
    'defaults to Codex for %s', async (path) => {
      const { wrapper } = await renderPage(path)
      expect(wrapper.get('main h1').text()).toBe('Codex 接入指南')
      expect((wrapper.get('select[aria-label="选择文档"]').element as HTMLSelectElement).value).toBe('codex')
      const activeLinks = wrapper.get('nav[aria-label="常用工具接入指南"]').findAll('a[aria-current="page"]')
      expect(activeLinks.map((link) => link.text())).toEqual(['Codex 接入指南'])
    }
  )

  it.each(guides)('renders only the selected $label article and marks its menu entry', async (guide) => {
    const { wrapper } = await renderPage(`/docs/tools/${guide.id}`)
    expect(wrapper.get('main h1').text()).toBe(guide.title)
    const toolMenu = wrapper.get('nav[aria-label="常用工具接入指南"]')
    expect(toolMenu.findAll('a[aria-current="page"]').map((link) => link.text())).toEqual([guide.label])
    expect((wrapper.get('select[aria-label="选择文档"]').element as HTMLSelectElement).value).toBe(guide.id)

    const article = wrapper.get('main article')
    expect(article.text().includes('model_provider = "moshu"')).toBe(guide.id === 'codex')
    expect(article.text().includes('npm install -g @anthropic-ai/claude-code')).toBe(guide.id === 'claude-cli')
    expect(article.text().includes('inferenceGatewayBaseUrl')).toBe(guide.id === 'claude-desktop')
    expect(article.find('img[src*="zcode-config"]').exists()).toBe(guide.id === 'zcode')
    expect(article.find('img[src*="opencode-provider"]').exists()).toBe(guide.id === 'opencode')
  })

  it('supports direct links and desktop menu navigation without mixing articles', async () => {
    const { wrapper, router } = await renderPage('/docs/tools/claude-cli')
    await wrapper.get('nav[aria-label="常用工具接入指南"] a[href="/docs/tools/workbuddy"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/docs/tools/workbuddy')
    expect(wrapper.get('main h1').text()).toBe('WorkBuddy 接入指南')
    expect(wrapper.get('main').text()).not.toContain('npm install -g @anthropic-ai/claude-code')
  })

  it('navigates between tools and top-level topics using a single mobile selector', async () => {
    const { wrapper, router } = await renderPage('/docs/tools')
    await wrapper.get('select[aria-label="选择文档"]').setValue('cherry-studio')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/docs/tools/cherry-studio')
    expect(wrapper.get('main h1').text()).toBe('CherryStudio 接入指南')

    await wrapper.get('select[aria-label="选择文档"]').setValue('token')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/docs/token')
    expect(wrapper.findAll('select')).toHaveLength(1)
    expect((wrapper.get('select[aria-label="选择文档"]').element as HTMLSelectElement).value).toBe('token')

    await wrapper.get('select[aria-label="选择文档"]').setValue('codex')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/docs/tools/codex')
    expect(wrapper.get('main h1').text()).toBe('Codex 接入指南')

    await wrapper.get('select[aria-label="选择文档"]').setValue('ccswitch')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/docs/ccswitch')
    expect(wrapper.get('main h1').text()).toBe('CC Switch 的使用')
  })

  it('uses only the current official CC Switch download page with safe external-link attributes', async () => {
    const { wrapper } = await renderPage('/docs/ccswitch')
    const downloads = wrapper.findAll('main a[href="https://ccswitch.io/zh/download"]')
    expect(downloads).toHaveLength(1)
    expect(downloads[0].attributes('target')).toBe('_blank')
    expect(downloads[0].attributes('rel').split(/\s+/)).toEqual(expect.arrayContaining(['noopener', 'noreferrer']))
    expect(source).not.toContain('releases/download/v3.18.0')
    expect(source).not.toContain('CC-Switch-v3.18.0-Windows.msi')
    expect(source).not.toContain('CC-Switch-v3.18.0-macOS.dmg')
  })

  it.each([
    { id: 'workbuddy', count: 3 },
    { id: 'trae', count: 5 },
    { id: 'cherry-studio', count: 6 }
  ])('renders every supplied $id screenshot', async ({ id, count }) => {
    const { wrapper } = await renderPage(`/docs/tools/${id}`)
    const screenshots = wrapper.findAll('main img').filter((image) => !image.element.closest('details'))
    expect(screenshots).toHaveLength(count)
    screenshots.forEach((image) => {
      expect(image.attributes('src')).toBeTruthy()
      expect(image.attributes('alt')).toBeTruthy()
      expect(image.attributes('src')).not.toMatch(/^[A-Z]:\\/i)
    })
  })

  it('uses consistent provider names and MoShu addresses rather than the Markdown typos', () => {
    expect(source).toContain('model_provider = "moshu"')
    expect(source).toContain('[model_providers.moshu]')
    expect(source).toContain('base_url = "https://ai.moshu.cloud/v1"')
    expect(source).toContain('https://ai.moshu.cloud')
    expect(source).not.toContain('model_provider = "mmoshu"')
    expect(source).not.toContain('api.ai.moshu.cloud')
    expect(source).not.toContain('api.bananarouter.com')
    expect(source).not.toContain('api.moshu.com')
    expect(source).not.toContain('ai.moshu.com')
  })
})
