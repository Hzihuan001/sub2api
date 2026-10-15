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
      <aside class="border-b border-slate-200 bg-white px-4 py-4 dark:border-dark-700 dark:bg-dark-900 lg:sticky lg:top-16 lg:h-[calc(100vh-4rem)] lg:overflow-y-auto lg:border-b-0 lg:border-r lg:px-5 lg:py-8">
        <label class="mb-2 block text-xs font-semibold uppercase tracking-wider text-slate-400 lg:hidden">选择文档</label>
        <Select
          :model-value="activeTopic === 'tools' ? activeTool.id : activeTopic"
          :options="documentOptions"
          :searchable="false"
          class="lg:hidden"
          aria-label="选择文档"
          @update:model-value="selectDocument"
        />
        <nav class="hidden space-y-1 lg:block" aria-label="文档目录">
          <p class="mb-3 px-3 text-xs font-semibold uppercase tracking-wider text-slate-400">使用指南</p>
          <router-link v-for="item in topics" :key="item.id" :to="topicPath(item.id)" class="block rounded-lg px-3 py-2.5 text-sm transition-colors" :class="activeTopic === item.id ? 'bg-primary-50 font-medium text-primary-700 dark:bg-primary-900/20 dark:text-primary-300' : 'text-slate-600 hover:bg-slate-100 dark:text-dark-300 dark:hover:bg-dark-800'">
            {{ item.label }}
          </router-link>
        </nav>
        <nav class="mt-5 hidden space-y-1 lg:block" aria-label="常用工具接入指南">
          <p class="mb-2 px-3 text-xs font-semibold tracking-wider text-slate-400">常用工具接入指南</p>
          <router-link
            v-for="item in toolGuides"
            :key="item.id"
            :to="toolPath(item.id)"
            :aria-current="activeTopic === 'tools' && activeTool.id === item.id ? 'page' : undefined"
            class="flex items-center gap-2.5 rounded-lg px-3 py-2.5 font-mono text-[13px] transition-colors"
            :class="activeTopic === 'tools' && activeTool.id === item.id ? 'bg-slate-100 font-medium text-slate-950 dark:bg-dark-800 dark:text-white' : 'text-slate-500 hover:bg-slate-50 hover:text-slate-900 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white'"
          >
            <svg class="h-4 w-4 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round" d="M14 3H6a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V8l-5-5Zm0 0v5h5M8 12h8M8 16h8" />
            </svg>
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
              <p class="mt-3"><a class="doc-link" href="https://ccswitch.io/zh/download" target="_blank" rel="noopener noreferrer">CC Switch 官方下载页</a></p>
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
            <DocHeading :title="activeTool.title" :description="activeTool.description" />
            <div v-if="activeTool.id === 'codex'" class="text-[15px] leading-7 text-slate-700 dark:text-dark-200">
              <h2 class="mt-7 text-xl font-semibold text-slate-950 dark:text-white">1. 写入配置</h2>
              <p class="mt-3">命令会先将已有配置备份为 <code class="inline-code">config.toml.bak</code>，再写入 MoShu 配置。</p>
              <p class="mt-5 font-medium">macOS / Linux</p>
              <p class="mt-3">打开终端，粘贴并执行：</p>
              <CodeBlock :code="codexConfigUnix" />
              <p class="mt-5 font-medium">Windows</p>
              <p class="mt-3">打开 PowerShell，粘贴并执行：</p>
              <CodeBlock :code="codexConfigWindows" />
              <h2 class="mt-7 text-xl font-semibold text-slate-950 dark:text-white">2. 设置 API Key</h2>
              <p class="mt-3">将命令中的“你的 MoShu API Key”替换为自己的 API Key。</p>
              <p class="mt-5 font-medium">macOS</p>
              <CodeBlock :code="codexApiKeyMac" />
              <p class="mt-5 font-medium">Linux</p>
              <CodeBlock :code="codexApiKeyLinux" />
              <p class="mt-5 font-medium">Windows</p>
              <CodeBlock :code="codexApiKeyWindows" />
              <h2 class="mt-7 text-xl font-semibold text-slate-950 dark:text-white">3. 开始使用</h2>
              <ul class="doc-list mt-3">
                <li>Codex 桌面版：完全退出应用后重新打开。</li>
                <li>Codex CLI：打开新的终端窗口，在项目目录运行 <code class="inline-code">codex</code>。</li>
              </ul>
            </div>
            <div v-if="activeTool.id === 'claude-cli'" class="text-[15px] leading-7 text-slate-700 dark:text-dark-200">
              <h2 class="mt-7 text-xl font-semibold text-slate-950 dark:text-white">1. 安装 Claude Code</h2>
              <p class="mt-3">确认已安装 Node.js，然后打开终端执行：</p>
              <CodeBlock :code="claudeInstall" />
              <p class="mt-3">显示版本号即安装成功。</p>
              <h2 class="mt-7 text-xl font-semibold text-slate-950 dark:text-white">2. 配置 MoShu</h2>
              <p class="mt-3">将代码中的“你的 MoShu API Key”替换为自己的 API Key。</p>
              <p class="mt-5 font-medium">macOS / Linux</p>
              <CodeBlock :code="claudeCliUnix" />
              <p class="mt-5 font-medium">Windows</p>
              <p class="mt-3">打开命令提示符执行：</p>
              <CodeBlock :code="claudeCliWindows" />
              <Callout title="模型 ID 以接口返回为准" tone="warning">如果某个模型 ID 在你的账号中不可用，请先查询 <code class="inline-code">/v1/models</code>，再替换对应变量。</Callout>
              <h2 class="mt-7 text-xl font-semibold text-slate-950 dark:text-white">3. 开始使用</h2>
              <p class="mt-3">重新打开终端，在项目目录执行：</p>
              <CodeBlock code="claude" />
            </div>
            <template v-if="activeTool.id === 'workbuddy'">
              <DocSection title="1. 添加模型">
                <p>打开 WorkBuddy「系统设置」，选择「模型」，点击「添加模型」。</p>
                <DocImage :src="workbuddyAddModelImage" alt="WorkBuddy 系统设置中的添加模型入口" />
              </DocSection>
              <DocSection title="2. 填写配置">
                <p>按下表填写，然后点击「保存」。</p>
                <DefinitionGrid :items="workbuddyFields" />
                <p class="mt-5">高级配置参考模型能力按需填写，这里参考 <code class="inline-code">gpt-6.1-sol</code>：开启「工具调用」和「图片输入」，输入选择 <code class="inline-code">256K</code>，输出选择 <code class="inline-code">64K</code>。</p>
                <DocImage :src="workbuddyConfigImage" alt="WorkBuddy 模型接口与高级能力配置" />
              </DocSection>
              <DocSection title="3. 开始使用">
                <p>返回对话页面，在底部选择 <code class="inline-code">gpt-6.1-sol</code>，发送消息即可。</p>
                <DocImage :src="workbuddyChatImage" alt="WorkBuddy 选择模型并发送消息" />
                <Callout title="连接失败时检查" tone="info">如果提示 API Key 或模型不可用，请检查 API Key 是否完整、模型名称是否填写正确。</Callout>
              </DocSection>
            </template>
            <template v-if="activeTool.id === 'trae'">
              <DocSection title="1. 添加模型">
                <p>在 Trae 对话框底部打开模型列表，点击「添加模型」。</p>
                <DocImage :src="traeAddModelImage" alt="Trae 对话框底部添加模型入口" />
              </DocSection>
              <DocSection title="2. 进入自定义模型">
                <p>在模型设置中点击「添加模型」或「在这里添加自定义模型」。</p>
                <DocImage :src="traeModelSettingsImage" alt="Trae 模型设置中的添加模型按钮" />
                <p class="mt-5">选择「自定义模型」。</p>
                <DocImage :src="traeCustomModelImage" alt="Trae 选择自定义模型" />
              </DocSection>
              <DocSection title="3. 填写配置">
                <p>按下表填写，然后点击「添加模型」。如果要使用 Claude 模型，需要选择 Anthropic Messages 的 API 格式。</p>
                <DefinitionGrid :items="traeFields" />
                <DocImage :src="traeConfigImage" alt="Trae 自定义请求地址、模型 ID 和 API 密钥配置" />
              </DocSection>
              <DocSection title="4. 开始使用">
                <p>确认模型已启用，返回对话页面选择 <code class="inline-code">gpt-6.1-sol</code>，发送消息即可。</p>
                <DocImage :src="traeChatImage" alt="Trae 启用模型并开始对话" />
              </DocSection>
            </template>
            <template v-if="activeTool.id === 'cherry-studio'">
              <DocSection title="1. 打开模型服务设置">
                <p>打开 CherryStudio 设置，在侧边栏进入「模型服务」，点击底部的「添加」。</p>
                <DocImage :src="cherrySettingsImage" alt="Cherry Studio 打开模型服务设置" caption="截图中的历史供应商地址仅用于展示界面，实际配置以本文文字为准。" />
              </DocSection>
              <DocSection title="2. 添加 Anthropic 供应商">
                <p>在添加供应商窗口中填写供应商名称，例如 <code class="inline-code">MoShu</code>，供应商类型选择「Anthropic」。</p>
                <DocImage :src="cherryProviderImage" alt="Cherry Studio 添加 Anthropic 供应商" />
              </DocSection>
              <DocSection title="3. 填写 API Key 和接口地址">
                <p>按下表填写配置。API Key 请替换为自己的 MoShu API Key，不要把真实 Key 写入文档、截图或提交到代码仓库。</p>
                <DefinitionGrid :items="cherryFields" />
                <p class="mt-5">CherryStudio 会自动在该地址后拼接 Anthropic 请求路径（例如 <code class="inline-code">/v1/messages</code>）。如果你的版本要求填写完整接口前缀，也可以填写 <code class="inline-code">https://ai.moshu.cloud/v1</code>，以界面下方显示的请求预览为准。</p>
                <DocImage :src="cherryApiImage" alt="Cherry Studio 填写 API 密钥与接口地址" />
                <p class="mt-5">填写完成后，向下滚动到模型列表，点击「添加」。</p>
                <DocImage :src="cherryAddModelImage" alt="Cherry Studio 模型列表中的添加按钮" />
              </DocSection>
              <DocSection title="4. 添加模型">
                <p>在「添加模型」窗口填写模型信息：</p>
                <DefinitionGrid :items="cherryModelFields" />
                <p class="mt-5">模型 ID 区分大小写，请以 MoShu 模型广场中显示的名称为准。填写后点击「添加模型」。</p>
                <DocImage :src="cherryModelImage" alt="Cherry Studio 添加模型 ID、名称与分组" />
              </DocSection>
              <DocSection title="5. 开始使用">
                <p>返回 CherryStudio 会话页面，在顶部模型选择器中选择刚添加的模型，然后发送消息测试连接。</p>
                <DocImage :src="cherryChatImage" alt="Cherry Studio 选择模型并测试连接" />
                <Callout title="请求失败时检查" tone="info">如果请求失败，请依次检查 API Key 是否完整、供应商类型是否为「Anthropic」、API 地址是否正确，以及模型 ID 是否与模型广场中的名称完全一致。</Callout>
              </DocSection>
            </template>
            <DocSection v-if="activeTool.id === 'claude-desktop'" title="Claude Desktop 配置方法">
              <p>先在 PowerShell 中生成 UUID：</p>
              <CodeBlock code="[guid]::NewGuid().ToString()" />
              <p class="mt-5">修改 <code class="inline-code">%LOCALAPPDATA%\Claude-3p\configLibrary\_meta.json</code>：</p>
              <CodeBlock :code="claudeMeta" />
              <p class="mt-5">在同一目录创建以该 UUID 命名的 JSON 文件，并填入：</p>
              <CodeBlock :code="claudeConfig" />
            </DocSection>
            <DocSection v-if="activeTool.id === 'zcode'" title="Zcode 配置示例">
              <p>在自定义提供商中填写相同的 API 地址、API Key 和模型 ID。</p>
              <DocImage :src="zcodeConfigImage" alt="Zcode 接入 MoShu 配置示例" />
            </DocSection>
            <DocSection v-if="activeTool.id === 'opencode'" title="OpenCode 配置示例">
              <p>先新增自定义提供商，再添加从模型接口获取的模型 ID。</p>
              <DocImage :src="opencodeProviderImage" alt="OpenCode 自定义提供商配置" />
              <DocImage :src="opencodeModelImage" alt="OpenCode 模型配置" />
            </DocSection>
            <details class="mt-12 rounded-xl border border-slate-200 bg-white p-5 text-sm leading-7 dark:border-dark-700 dark:bg-dark-900">
              <summary class="cursor-pointer font-medium text-slate-700 dark:text-dark-200">查询可用模型与接口地址说明</summary>
              <p class="mt-4">模型 ID 以 MoShu 模型广场或接口返回为准。打开 PowerShell，将示例 Key 替换为自己的 API Key 后执行：</p>
              <CodeBlock :code="modelsCommand" />
              <DocImage :src="modelListImage" alt="PowerShell 查询 MoShu 模型列表返回结果" caption="返回结果中每个 id 的值就是模型 ID。" />
              <p class="mt-4">OpenAI / Codex 通常填写 <code class="inline-code">https://ai.moshu.cloud/v1</code>；Claude Code / Anthropic 类型按本页对应步骤填写根地址 <code class="inline-code">https://ai.moshu.cloud</code>。不要填写完整的 <code class="inline-code">/chat/completions</code> 或 <code class="inline-code">/responses</code> 接口路径。使用 CCSwitch 时，请求地址栏填写根地址即可。</p>
            </details>
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
import workbuddyAddModelImage from '@/assets/docs/tools/integrations/WB1.png'
import workbuddyConfigImage from '@/assets/docs/tools/integrations/WB2.png'
import workbuddyChatImage from '@/assets/docs/tools/integrations/WB3.png'
import traeAddModelImage from '@/assets/docs/tools/integrations/T1.png'
import traeModelSettingsImage from '@/assets/docs/tools/integrations/T2.png'
import traeCustomModelImage from '@/assets/docs/tools/integrations/T3.png'
import traeConfigImage from '@/assets/docs/tools/integrations/T4.png'
import traeChatImage from '@/assets/docs/tools/integrations/T5.png'
import cherrySettingsImage from '@/assets/docs/tools/integrations/CS1.png'
import cherryProviderImage from '@/assets/docs/tools/integrations/CS2.png'
import cherryApiImage from '@/assets/docs/tools/integrations/CS3.jpg'
import cherryAddModelImage from '@/assets/docs/tools/integrations/CS4.jpg'
import cherryModelImage from '@/assets/docs/tools/integrations/CS5.jpg'
import cherryChatImage from '@/assets/docs/tools/integrations/CS6.jpg'
import zcodeConfigImage from '@/assets/docs/tools/zcode-config.jpeg'
import opencodeProviderImage from '@/assets/docs/tools/opencode-provider.png'
import opencodeModelImage from '@/assets/docs/tools/opencode-model.jpeg'

type TopicId = 'token' | 'ccswitch' | 'tools'

const topics: Array<{ id: TopicId; label: string }> = [
  { id: 'token', label: '创建令牌' },
  { id: 'ccswitch', label: 'CCSwitch 的使用' }
]

// Menu order follows the user's latest requested reading order.
const toolGuides = [
  {
    id: 'codex', label: 'Codex 接入指南', title: 'Codex 接入指南',
    description: '本指南同时适用于 Codex 桌面版和 Codex CLI。准备好 MoShu API Key 后，按以下步骤操作。'
  },
  {
    id: 'claude-cli', label: 'Claude CLI 接入指南', title: 'Claude Code 接入指南',
    description: '准备好 MoShu API Key 后，按以下步骤操作。'
  },
  {
    id: 'claude-desktop', label: 'Claude Desktop', title: 'Claude Desktop 配置方法',
    description: '使用现有 Claude Desktop 网关配置接入 MoShu。'
  },
  {
    id: 'workbuddy', label: 'WorkBuddy 接入指南', title: 'WorkBuddy 接入指南',
    description: '准备好 MoShu API Key 后，按以下步骤操作。'
  },
  {
    id: 'trae', label: 'Trae 接入指南', title: 'Trae 接入指南',
    description: '准备好 MoShu API Key 后，按以下步骤操作。'
  },
  {
    id: 'cherry-studio', label: 'Cherry Studio', title: 'CherryStudio 接入指南',
    description: '准备好 MoShu API Key 后，可以在 CherryStudio 中通过 Anthropic 兼容接口接入 Claude 模型。以下步骤以 CherryStudio 的桌面版界面为例。'
  },
  {
    id: 'zcode', label: 'Zcode', title: 'Zcode 配置示例',
    description: '在 Zcode 中添加 MoShu 自定义提供商，使用你的 API Key 和可用模型。'
  },
  {
    id: 'opencode', label: 'OpenCode', title: 'OpenCode 配置示例',
    description: '在 OpenCode 中添加自定义提供商与模型。'
  }
] as const
type ToolId = typeof toolGuides[number]['id']
const documentOptions: SelectOption[] = [...topics, ...toolGuides].map((item) => ({ value: item.id, label: item.label }))

const route = useRoute()
const router = useRouter()
const appStore = useAppStore()
const authStore = useAuthStore()
const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'MoShu')
const activeTopic = computed<TopicId>(() => {
  const candidate = String(route.params.topic || 'token')
  return candidate === 'tools' || topics.some((item) => item.id === candidate) ? candidate as TopicId : 'token'
})
const activeTool = computed(() => toolGuides.find((item) => item.id === route.params.tool) || toolGuides[0])

function topicPath(id: TopicId) {
  return '/docs/' + id
}

function toolPath(id: ToolId) {
  return '/docs/tools/' + id
}

function selectDocument(value: string | number | boolean | null) {
  const topic = topics.find((item) => item.id === value)
  if (topic) {
    void router.push(topicPath(topic.id))
    return
  }
  const tool = toolGuides.find((item) => item.id === value)
  if (tool) {
    void router.push(toolPath(tool.id))
  }
}

const modelsCommand = 'curl.exe https://ai.moshu.cloud/v1/models -H "Authorization: Bearer sk-xxxxxxxx"'
const codexConfigUnix = String.raw`mkdir -p ~/.codex
[ -f ~/.codex/config.toml ] && cp ~/.codex/config.toml ~/.codex/config.toml.bak
cat > ~/.codex/config.toml <<'EOF'
model_provider = "moshu"
model = "gpt-6.1-sol"
model_reasoning_effort = "high"

[model_providers.moshu]
name = "OpenAI"
base_url = "https://ai.moshu.cloud/v1"
env_key = "OPENAI_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
EOF`
const codexConfigWindows = String.raw`$codexDir = Join-Path $HOME ".codex"
$configPath = Join-Path $codexDir "config.toml"
New-Item -ItemType Directory -Force -Path $codexDir | Out-Null
if (Test-Path $configPath) {
  Copy-Item $configPath "$configPath.bak" -Force
}
@'
model_provider = "moshu"
model = "gpt-6.1-sol"
model_reasoning_effort = "high"

[model_providers.moshu]
name = "OpenAI"
base_url = "https://ai.moshu.cloud/v1"
env_key = "OPENAI_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
'@ | Set-Content -Path $configPath -Encoding UTF8`
const codexApiKeyMac = 'echo \'export OPENAI_API_KEY="你的 MoShu API Key"\' >> ~/.zshrc\nsource ~/.zshrc'
const codexApiKeyLinux = 'echo \'export OPENAI_API_KEY="你的 MoShu API Key"\' >> ~/.bashrc\nsource ~/.bashrc'
const codexApiKeyWindows = '[Environment]::SetEnvironmentVariable("OPENAI_API_KEY", "你的 MoShu API Key", "User")'
const claudeInstall = 'npm install -g @anthropic-ai/claude-code\nclaude --version'
const claudeCliWindows = [
  'setx ANTHROPIC_BASE_URL "https://ai.moshu.cloud"',
  'setx ANTHROPIC_AUTH_TOKEN "你的 MoShu API Key"',
  'setx ANTHROPIC_MODEL "claude-opus-5-5"',
  'setx ANTHROPIC_DEFAULT_OPUS_MODEL "claude-opus-4-8"',
  'setx ANTHROPIC_DEFAULT_SONNET_MODEL "claude-sonnet-5-5"',
  'setx ANTHROPIC_DEFAULT_HAIKU_MODEL "claude-haiku-5-5"',
  'setx CLAUDE_CODE_SUBAGENT_MODEL "claude-fable-5-1"'
].join('\n')
const claudeCliUnix = String.raw`SHELL_RC="$HOME/.zshrc"; case "$SHELL" in *bash*) SHELL_RC="$HOME/.bashrc";; esac
cat >> "$SHELL_RC" <<'EOF'
export ANTHROPIC_BASE_URL="https://ai.moshu.cloud"
export ANTHROPIC_AUTH_TOKEN="你的 MoShu API Key"
export ANTHROPIC_MODEL="claude-opus-5-5"
export ANTHROPIC_DEFAULT_OPUS_MODEL="claude-opus-4-8"
export ANTHROPIC_DEFAULT_SONNET_MODEL="claude-sonnet-5-5"
export ANTHROPIC_DEFAULT_HAIKU_MODEL="claude-haiku-5-5"
export CLAUDE_CODE_SUBAGENT_MODEL="claude-fable-5-1"
EOF
source "$SHELL_RC"`
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
  ['提供商', '自定义 / Custom'],
  ['接口地址', 'https://ai.moshu.cloud/v1'],
  ['API Key', '你的 MoShu API Key'],
  ['模型名称', 'gpt-6.1-sol']
]
const traeFields = [
  ['API 格式', 'OpenAI Chat Completions 格式'],
  ['自定义请求地址', 'https://ai.moshu.cloud/v1'],
  ['模型 ID', 'gpt-6.1-sol'],
  ['模型展示名称', 'gpt-6.1-sol'],
  ['API 密钥', '你的 MoShu API Key']
]
const cherryFields = [
  ['API 密钥', '你的 MoShu API Key'],
  ['API 地址', 'https://ai.moshu.cloud']
]
const cherryModelFields = [
  ['模型 ID', 'MoShu 模型广场中的完整模型 ID，例如 claude-sonnet-4-6'],
  ['模型名称', '与模型 ID 保持一致，或填写便于识别的名称'],
  ['分组名称', '例如 claude-sonnet']
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
