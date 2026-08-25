<template>
  <div class="min-h-screen bg-slate-50 text-slate-800 dark:bg-dark-950 dark:text-slate-100">
    <header class="sticky top-0 z-40 border-b border-slate-200/80 bg-white/90 backdrop-blur dark:border-dark-700 dark:bg-dark-900/90">
      <div class="mx-auto flex h-16 max-w-[1440px] items-center justify-between px-4 sm:px-6 lg:px-8">
        <div class="flex min-w-0 items-center gap-3">
          <router-link to="/home" class="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-primary-600 font-bold text-white shadow-sm">M</router-link>
          <div class="min-w-0">
            <p class="truncate text-sm font-semibold text-slate-900 dark:text-white">{{ siteName }}</p>
            <p class="text-xs text-slate-500 dark:text-dark-400">接口文档</p>
          </div>
        </div>
        <div class="flex items-center gap-2">
          <router-link to="/home" class="rounded-lg px-3 py-2 text-sm text-slate-600 hover:bg-slate-100 dark:text-dark-300 dark:hover:bg-dark-800">返回首页</router-link>
          <router-link :to="authStore.isAuthenticated ? '/keys' : '/login'" class="rounded-lg bg-primary-600 px-3.5 py-2 text-sm font-medium text-white hover:bg-primary-700">{{ authStore.isAuthenticated ? '令牌管理' : '登录控制台' }}</router-link>
        </div>
      </div>
    </header>
    <div class="mx-auto grid max-w-[1440px] grid-cols-1 lg:grid-cols-[250px_minmax(0,1fr)]">
      <aside class="border-b border-slate-200 bg-white px-4 py-4 dark:border-dark-700 dark:bg-dark-900 lg:sticky lg:top-16 lg:h-[calc(100vh-4rem)] lg:border-b-0 lg:border-r lg:px-5 lg:py-8">
        <label class="mb-2 block text-xs font-semibold uppercase tracking-wider text-slate-400 lg:hidden">选择文档</label>
        <Select
          :model-value="activeTopic"
          :options="topicOptions"
          :searchable="false"
          class="lg:hidden"
          aria-label="选择文档"
          @update:model-value="selectTopic"
        />
        <nav class="hidden space-y-1 lg:block" aria-label="文档目录">
          <p class="mb-3 px-3 text-xs font-semibold uppercase tracking-wider text-slate-400">使用指南</p>
          <router-link v-for="item in topics" :key="item.id" :to="topicPath(item.id)" class="block rounded-lg px-3 py-2.5 text-sm transition-colors" :class="activeTopic === item.id ? 'bg-primary-50 font-medium text-primary-700 dark:bg-primary-900/20 dark:text-primary-300' : 'text-slate-600 hover:bg-slate-100 dark:text-dark-300 dark:hover:bg-dark-800'">
            {{ item.label }}
          </router-link>
        </nav>
        <div class="mt-8 hidden rounded-xl border border-primary-100 bg-primary-50 p-4 text-xs leading-5 text-primary-800 dark:border-primary-900/40 dark:bg-primary-900/10 dark:text-primary-200 lg:block">
          <p class="font-semibold">安全提醒</p>
          <p class="mt-1">API Key 等同于密码。不要发送到聊天、截图或公开仓库。</p>
        </div>
      </aside>
      <main class="min-w-0 px-4 py-8 sm:px-8 lg:px-12 lg:py-12 xl:px-16">
        <article class="mx-auto max-w-4xl">
          <template v-if="activeTopic === 'token'">
            <DocHeading title="创建 API Key" description="API Key 是调用 MoShu API 的访问凭证。不同设备或项目建议使用独立 Key，方便统计消耗、限制额度和单独停用。" />
            <Callout title="请妥善保管" tone="warning">请勿将 API Key 发布到公开仓库、截图、聊天记录或前端网页代码中。</Callout>
            <StepBlock number="1" title="进入 API 密钥页面">
              <p>点击右侧菜单中的“API 密钥”，然后点击“创建密钥”。</p>
              <DocImage :src="createKeyImage" alt="在 API 密钥页面点击创建密钥" />
            </StepBlock>
            <StepBlock number="2" title="填写 API Key 信息">
              <ul class="doc-list list-disc">
                <li><strong>名称：</strong>建议按照用途命名，例如“个人电脑-Codex”或“实验室-Claude”。</li>
                <li><strong>分组：</strong>选择要使用的模型和倍率渠道。</li>
                <li><strong>其他参数：</strong>设置完成后点击“创建”。</li>
              </ul>
              <DocImage :src="keyFormImage" alt="创建 API Key 的名称、分组和参数设置" />
            </StepBlock>
            <StepBlock number="3" title="管理 API Key">
              <p>创建后的 Key 可以在“API 密钥”页面管理。点击“选择分组”可变更分组；点击“编辑”可修改名称、分组等信息。</p>
              <DocImage :src="keyManagementImage" alt="API 密钥管理页面" />
            </StepBlock>
            <StepBlock number="4" title="复制并保存 API Key">
              <p>创建后复制生成的 Key，例如 <code class="inline-code">sk-xxxxxxxxxxxxxxxx</code>，并妥善保存。</p>
              <DocImage :src="copyKeyImage" alt="复制并保存创建完成的 API Key" />
            </StepBlock>
          </template>
          <template v-else-if="activeTopic === 'ccswitch'">
            <DocHeading title="CC Switch 的使用" description="CC Switch 可以统一管理 Codex、Claude Code 等工具的 API 服务商配置。" />
            <DocSection title="安装 CC Switch">
              <p>从 CC Switch 官方发布页下载当前系统对应的版本：</p>
              <ul class="doc-list list-disc">
                <li><a class="doc-link" href="https://github.com/farion1231/cc-switch/releases/download/v3.18.0/CC-Switch-v3.18.0-Windows.msi" target="_blank" rel="noopener noreferrer">Windows 安装包</a></li>
                <li><a class="doc-link" href="https://github.com/farion1231/cc-switch/releases/download/v3.18.0/CC-Switch-v3.18.0-macOS.dmg" target="_blank" rel="noopener noreferrer">macOS 安装包</a></li>
              </ul>
            </DocSection>
            <DocSection title="一、使用平台一键导入配置">
              <ol class="doc-list list-decimal">
                <li>打开 CC Switch。</li>
                <li>进入 MoShu 的“API 密钥”页面，点击目标密钥对应的“导入到 CCS”按钮。</li>
              </ol>
              <DocImage :src="ccImportFromKeyImage" alt="API 密钥页面的导入到 CCS 按钮" />
              <p class="mt-5">若浏览器弹出提示框，点击“打开”。</p>
              <DocImage :src="ccBrowserPromptImage" alt="浏览器打开 CC Switch 的确认提示" />
              <p class="mt-5">在弹出的 CC Switch 应用中选择“导入”。</p>
              <DocImage :src="ccImportDialogImage" alt="CC Switch 导入配置对话框" />
            </DocSection>
            <DocSection title="二、手动配置 Codex 或 Claude 供应商">
              <ol class="doc-list list-decimal">
                <li>打开 CC Switch，切换到“Codex”页面；配置 Claude 时切换到“Claude”页面。</li>
                <li>点击右上角“+”按钮。</li>
              </ol>
              <DocImage :src="ccAddProviderImage" alt="CC Switch 添加供应商按钮" />
              <p class="mt-5">选择“Codex 供应商”页签中的“自定义配置”，按下表填写：</p>
              <DefinitionGrid :items="ccSwitchFields" />
              <Callout title="上游格式" tone="success">MoShu 已原生支持 Responses API，通常不需要开启 Chat Completions 转换。</Callout>
              <DocImage :src="ccProviderFormImage" alt="CC Switch 自定义供应商配置表单" />
              <p class="mt-5">信息填写完成后，点击右下角“保存”。</p>
            </DocSection>
            <DocSection title="三、启用配置和本地路由">
              <ol class="doc-list list-decimal">
                <li>点击“启用”，使用刚刚设置的配置，然后点击右上角齿轮进入设置。</li>
              </ol>
              <DocImage :src="ccEnableSettingsImage" alt="启用 CC Switch 配置并打开设置" />
              <ol class="doc-list list-decimal" start="2">
                <li>切换到“路由”页签，打开“在主页面显示本地路由开关”。</li>
              </ol>
              <DocImage :src="ccRouteSettingImage" alt="CC Switch 路由设置" />
              <ol class="doc-list list-decimal" start="3">
                <li>回到主页，开启“路由开关”，重启 Codex 后即可使用。</li>
              </ol>
              <DocImage :src="ccRouteSwitchImage" alt="CC Switch 主页面本地路由开关" />
              <Callout title="保留原有 Codex 对话历史" tone="info">若之前使用账号进行对话，可开启“设置 → 通用 → Codex 应用增强 → 统一 Codex 会话历史”。</Callout>
              <DocImage :src="ccHistorySettingImage" alt="统一 Codex 会话历史设置" />
              <DocImage :src="ccHistoryResultImage" alt="统一 Codex 会话历史后的效果" />
            </DocSection>
          </template>

          <template v-else>
            <DocHeading title="接入其他工具" description="本章包含 Codex、Claude Desktop、WorkBuddy、Zcode 和 OpenCode 的接入方法。" />
            <DocSection title="配置前准备">
              <p>开始前需要准备三个信息：</p>
              <ol class="doc-list list-decimal">
                <li><strong>中转站 API 地址：</strong><code class="inline-code">https://ai.moshu.cloud</code></li>
                <li><strong>API Key：</strong><code class="inline-code">sk-xxxxxxxxxxxxxxxx</code></li>
                <li><strong>可用模型 ID：</strong>必须从模型列表接口获取。</li>
              </ol>
              <p class="mt-5">打开 PowerShell，执行以下命令。把示例 Key 替换成你创建的 API Key：</p>
              <CodeBlock :code="modelsCommand" />
              <DocImage :src="modelListImage" alt="PowerShell 查询 MoShu 模型列表返回结果" caption="返回结果中每个 id 的值就是模型 ID。" />
              <Callout title="模型名称必须完全一致" tone="info">工具中填写的模型名称必须与接口返回的 <code class="inline-code">id</code> 完全一致。</Callout>
            </DocSection>
            <DocSection title="接口地址填写规则">
              <div class="grid gap-4 sm:grid-cols-2">
                <div class="rounded-xl border border-emerald-200 bg-emerald-50 p-4 dark:border-emerald-900/50 dark:bg-emerald-950/20">
                  <p class="text-sm font-semibold text-emerald-800 dark:text-emerald-300">正确：Base URL</p>
                  <code class="mt-2 block break-all text-sm">https://ai.moshu.cloud/v1</code>
                </div>
                <div class="rounded-xl border border-rose-200 bg-rose-50 p-4 dark:border-rose-900/50 dark:bg-rose-950/20">
                  <p class="text-sm font-semibold text-rose-800 dark:text-rose-300">不要填写完整接口</p>
                  <code class="mt-2 block break-all text-sm">https://ai.moshu.cloud/v1/chat/completions</code>
                </div>
              </div>
              <p class="mt-4">AI Agent 会自动拼接 <code class="inline-code">/chat/completions</code>。只填写 <code class="inline-code">https://ai.moshu.cloud</code> 在部分软件中可用，但兼容性较差。</p>
            </DocSection>
            <DocSection title="Codex 配置方法">
              <p>在 <code class="inline-code">%USERPROFILE%\.codex\config.toml</code> 中添加：</p>
              <CodeBlock :code="codexConfig" />
              <p class="mt-5">然后在 <code class="inline-code">auth.json</code> 中设置 API Key：</p>
              <CodeBlock :code="codexAuth" />
              <p class="mt-4">也可以打开 Codex，选择“使用其他方式登录”，输入生成的 API Key 完成登录。</p>
            </DocSection>
            <DocSection title="Claude Desktop 配置方法">
              <p>先在 PowerShell 中生成 UUID：</p>
              <CodeBlock code="[guid]::NewGuid().ToString()" />
              <p class="mt-5">修改 <code class="inline-code">%LOCALAPPDATA%\Claude-3p\configLibrary\_meta.json</code>：</p>
              <CodeBlock :code="claudeMeta" />
              <p class="mt-5">在同一目录创建以该 UUID 命名的 JSON 文件，并填入：</p>
              <CodeBlock :code="claudeConfig" />
            </DocSection>
            <DocSection title="WorkBuddy 配置示例">
              <p>依次进入“模型设置 → 添加模型 → 自定义”。</p>
              <DocImage :src="workbuddyAddModelImage" alt="WorkBuddy 添加自定义模型入口" />
              <DefinitionGrid :items="workbuddyFields" />
              <DocImage :src="workbuddyConfigImage" alt="WorkBuddy 中填写 MoShu 配置" caption="确认模型 ID 来自 /v1/models 后保存。" />
            </DocSection>
            <DocSection title="Zcode 配置示例">
              <p>在自定义提供商中填写相同的 API 地址、API Key 和模型 ID。</p>
              <DocImage :src="zcodeConfigImage" alt="Zcode 接入 MoShu 配置示例" />
            </DocSection>
            <DocSection title="OpenCode 配置示例">
              <p>先新增自定义提供商，再添加从模型接口获取的模型 ID。</p>
              <DocImage :src="opencodeProviderImage" alt="OpenCode 自定义提供商配置" />
              <DocImage :src="opencodeModelImage" alt="OpenCode 模型配置" />
            </DocSection>
          </template>
          <footer class="mt-16 border-t border-slate-200 pt-6 text-sm text-slate-500 dark:border-dark-700 dark:text-dark-400">
            <div class="flex flex-col justify-between gap-3 sm:flex-row">
              <p>文档中的 API Key 均为示例，请替换为你自己的令牌。</p>
              <router-link to="/home" class="text-primary-600 hover:text-primary-700 dark:text-primary-400">返回</router-link>
            </div>
          </footer>
        </article>
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, type PropType } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import createKeyImage from '@/assets/docs/token/create-key.png'
import keyFormImage from '@/assets/docs/token/key-form.png'
import keyManagementImage from '@/assets/docs/token/key-management.png'
import copyKeyImage from '@/assets/docs/token/copy-key.png'
import ccImportFromKeyImage from '@/assets/docs/ccswitch/import-from-key-page.png'
import ccBrowserPromptImage from '@/assets/docs/ccswitch/browser-open-prompt.png'
import ccImportDialogImage from '@/assets/docs/ccswitch/ccswitch-import-dialog.png'
import ccAddProviderImage from '@/assets/docs/ccswitch/add-provider.png'
import ccProviderFormImage from '@/assets/docs/ccswitch/provider-form.jpeg'
import ccEnableSettingsImage from '@/assets/docs/ccswitch/enable-and-settings.png'
import ccRouteSettingImage from '@/assets/docs/ccswitch/route-setting.png'
import ccRouteSwitchImage from '@/assets/docs/ccswitch/route-switch.png'
import ccHistorySettingImage from '@/assets/docs/ccswitch/conversation-history-setting.png'
import ccHistoryResultImage from '@/assets/docs/ccswitch/conversation-history-result.png'
import modelListImage from '@/assets/docs/tools/model-list.jpeg'
import workbuddyAddModelImage from '@/assets/docs/tools/workbuddy-add-model.png'
import workbuddyConfigImage from '@/assets/docs/tools/workbuddy-config.jpeg'
import zcodeConfigImage from '@/assets/docs/tools/zcode-config.jpeg'
import opencodeProviderImage from '@/assets/docs/tools/opencode-provider.png'
import opencodeModelImage from '@/assets/docs/tools/opencode-model.jpeg'

type TopicId = 'token' | 'ccswitch' | 'tools'

const topics: Array<{ id: TopicId; label: string }> = [
  { id: 'token', label: '创建令牌' },
  { id: 'ccswitch', label: 'CCSwitch 的使用' },
  { id: 'tools', label: '接入其他工具' }
]
const topicOptions: SelectOption[] = topics.map((item) => ({
  value: item.id,
  label: item.label
}))

const route = useRoute()
const router = useRouter()
const appStore = useAppStore()
const authStore = useAuthStore()
const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'MoShu')
const activeTopic = computed<TopicId>(() => {
  const candidate = String(route.params.topic || 'token')
  return topics.some((item) => item.id === candidate) ? candidate as TopicId : 'token'
})

function topicPath(id: TopicId) {
  return '/docs/' + id
}

function selectTopic(value: string | number | boolean | null) {
  const topic = String(value) as TopicId
  if (topics.some((item) => item.id === topic)) {
    void router.push(topicPath(topic))
  }
}

const modelsCommand = 'curl.exe https://ai.moshu.cloud/v1/models -H "Authorization: Bearer sk-xxxxxxxx"'
const codexConfig = [
  'model_provider = "moshu"',
  'model = "gpt-5.6-sol"',
  'model_reasoning_effort = "high"',
  '',
  '[model_providers.moshu]',
  'name = "MoShu 模枢"',
  'wire_api = "responses"',
  'base_url = "https://ai.moshu.cloud/v1"'
].join('\n')
const codexAuth = ['{', '  "OPENAI_API_KEY": "生成的apikey"', '}'].join('\n')
const claudeMeta = [
  '{',
  '  "appliedid": "生成的UUID",',
  '  "entries": [',
  '    {',
  '      "id": "生成的UUID",',
  '      "name": "MoShu"',
  '    }',
  '  ]',
  '}'
].join('\n')
const claudeConfig = [
  '{',
  '  "coworkEgressAllowedHosts": ["*"],',
  '  "disableDeploymentModeChooser": true,',
  '  "inferenceGatewayApiKey": "生成的apikey",',
  '  "inferenceGatewayAuthScheme": "bearer",',
  '  "inferenceGatewayBaseUrl": "https://ai.moshu.cloud",',
  '  "inferenceProvider": "gateway"',
  '}'
].join('\n')
const ccSwitchFields = [
  ['供应商名称', '自行填写，仅作为配置标志'],
  ['API Key', '控制台复制的 API Key'],
  ['API 请求地址', 'https://ai.moshu.cloud（注意末尾不加斜杠）'],
  ['上游格式', 'Responses（原生）'],
  ['默认模型', '点击“获取模型列表”测试连通性即可']
]
const workbuddyFields = [
  ['提供商', '自定义 / MoShu'],
  ['接口地址', 'https://ai.moshu.cloud/v1'],
  ['API Key', 'sk-xxxxxxxx'],
  ['模型名称', '例如 gpt-5.6-sol（以 /v1/models 返回为准）']
]
const DocHeading = defineComponent({
  props: {
    title: { type: String, required: true },
    description: { type: String, required: true }
  },
  setup(props) {
    return () => h('header', { class: 'mb-10' }, [
      h('p', { class: 'mb-3 text-sm font-semibold text-primary-600 dark:text-primary-400' }, 'MoShu 使用指南'),
      h('h1', { class: 'text-3xl font-bold tracking-tight text-slate-950 dark:text-white sm:text-4xl' }, props.title),
      h('p', { class: 'mt-4 max-w-3xl text-base leading-7 text-slate-600 dark:text-dark-300' }, props.description)
    ])
  }
})

const DocSection = defineComponent({
  props: { title: { type: String, required: true } },
  setup(props, { slots }) {
    return () => h('section', { class: 'mt-12 scroll-mt-24' }, [
      h('h2', { class: 'mb-5 text-2xl font-semibold tracking-tight text-slate-950 dark:text-white' }, props.title),
      h('div', { class: 'text-[15px] leading-7 text-slate-700 dark:text-dark-200' }, slots.default?.())
    ])
  }
})

const StepBlock = defineComponent({
  props: {
    number: { type: String, required: true },
    title: { type: String, required: true }
  },
  setup(props, { slots }) {
    return () => h('section', { class: 'relative mt-10 border-l-2 border-primary-200 pl-7 dark:border-primary-900/70' }, [
      h('span', { class: 'absolute -left-4 top-0 flex h-8 w-8 items-center justify-center rounded-full bg-primary-600 text-sm font-bold text-white' }, props.number),
      h('h2', { class: 'mb-4 text-xl font-semibold text-slate-950 dark:text-white' }, props.title),
      h('div', { class: 'text-[15px] leading-7 text-slate-700 dark:text-dark-200' }, slots.default?.())
    ])
  }
})

const Callout = defineComponent({
  props: {
    title: { type: String, required: true },
    tone: { type: String, default: 'info' }
  },
  setup(props, { slots }) {
    const tones: Record<string, string> = {
      info: 'border-blue-200 bg-blue-50 text-blue-900 dark:border-blue-900/50 dark:bg-blue-950/20 dark:text-blue-200',
      success: 'border-emerald-200 bg-emerald-50 text-emerald-900 dark:border-emerald-900/50 dark:bg-emerald-950/20 dark:text-emerald-200',
      warning: 'border-amber-200 bg-amber-50 text-amber-950 dark:border-amber-900/50 dark:bg-amber-950/20 dark:text-amber-200'
    }
    return () => h('div', { class: 'rounded-xl border p-4 ' + (tones[props.tone] || tones.info) }, [
      h('p', { class: 'text-sm font-semibold' }, props.title),
      h('div', { class: 'mt-1 text-sm leading-6' }, slots.default?.())
    ])
  }
})

const CodeBlock = defineComponent({
  props: { code: { type: String, required: true } },
  setup(props) {
    return () => h('pre', { class: 'mt-4 overflow-x-auto rounded-xl border border-slate-800 bg-slate-950 p-4 text-sm leading-6 text-slate-100 shadow-sm' }, [
      h('code', props.code)
    ])
  }
})

const DocImage = defineComponent({
  props: {
    src: { type: String, required: true },
    alt: { type: String, required: true },
    caption: { type: String, default: '' }
  },
  setup(props) {
    return () => h('figure', { class: 'mt-5' }, [
      h('div', { class: 'overflow-hidden rounded-xl border border-slate-200 bg-white p-2 shadow-sm dark:border-dark-700 dark:bg-dark-900' }, [
        h('img', {
          src: props.src,
          alt: props.alt,
          class: 'mx-auto h-auto max-h-[720px] max-w-full rounded-lg object-contain',
          loading: 'lazy'
        })
      ]),
      props.caption ? h('figcaption', { class: 'mt-2 text-center text-xs text-slate-500 dark:text-dark-400' }, props.caption) : null
    ])
  }
})

const DefinitionGrid = defineComponent({
  props: { items: { type: Array as PropType<string[][]>, required: true } },
  setup(props) {
    return () => h('dl', { class: 'mt-5 divide-y divide-slate-200 overflow-hidden rounded-xl border border-slate-200 dark:divide-dark-700 dark:border-dark-700' }, props.items.map(([term, value]) =>
      h('div', { class: 'grid gap-1 bg-white px-4 py-3 sm:grid-cols-[140px_1fr] dark:bg-dark-900' }, [
        h('dt', { class: 'text-sm font-medium text-slate-500 dark:text-dark-400' }, term),
        h('dd', { class: 'break-all text-sm text-slate-900 dark:text-white' }, value)
      ])
    ))
  }
})

</script>

<style scoped>
.doc-list {
  margin-top: 0.75rem;
  padding-left: 1.5rem;
}

.doc-list li + li {
  margin-top: 0.45rem;
}

.inline-code {
  border-radius: 0.35rem;
  background: rgb(241 245 249);
  padding: 0.1rem 0.35rem;
  color: rgb(15 118 110);
  font-size: 0.875em;
}

:global(.dark) .inline-code {
  background: rgb(30 41 59);
  color: rgb(94 234 212);
}

.doc-link {
  color: rgb(13 148 136);
  text-decoration: underline;
  text-underline-offset: 3px;
}
</style>
