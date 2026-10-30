<template>
  <AppLayout>
    <div class="mx-auto max-w-[1680px] space-y-6">
      <header class="flex flex-col justify-between gap-4 sm:flex-row sm:items-end">
        <div>
          <h1 class="text-2xl font-bold tracking-tight text-gray-900 dark:text-white sm:text-3xl">
            渠道测智
          </h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
            各分组使用同一道题完成测试，直接查看生成作品，判断模型是否出现降智。
          </p>
        </div>
        <button class="btn btn-secondary" :disabled="loading" @click="load">
          <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
          刷新
        </button>
      </header>

      <div class="rounded-2xl border border-sky-200 bg-sky-50 px-5 py-4 text-sm text-sky-800 dark:border-sky-500/30 dark:bg-sky-500/10 dark:text-sky-200">
        <div class="flex items-start gap-3">
          <Icon name="infoCircle" size="sm" class="mt-0.5 shrink-0" />
          <p>{{ showcaseDescription }}</p>
        </div>
      </div>

      <div v-if="groups.length > 0" class="flex gap-2 overflow-x-auto pb-1">
        <button
          class="shrink-0 rounded-full px-4 py-2 text-sm transition"
          :class="selectedGroup === null ? 'bg-gray-900 text-white dark:bg-white dark:text-dark-900' : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-800 dark:text-dark-300 dark:hover:bg-dark-700'"
          @click="selectedGroup = null"
        >全部分组</button>
        <button
          v-for="group in groups"
          :key="`filter-${group.id}`"
          class="max-w-[220px] shrink-0 truncate rounded-full px-4 py-2 text-sm transition"
          :class="selectedGroup === group.id ? 'bg-gray-900 text-white dark:bg-white dark:text-dark-900' : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-800 dark:text-dark-300 dark:hover:bg-dark-700'"
          @click="selectedGroup = group.id"
        >{{ group.name }}</button>
      </div>

      <div v-if="loading" class="grid gap-5 md:grid-cols-2 xl:grid-cols-3">
        <div v-for="n in 3" :key="n" class="h-96 animate-pulse rounded-2xl bg-gray-200 dark:bg-dark-800" />
      </div>

      <div v-else-if="error" class="rounded-2xl border border-red-200 bg-red-50 px-5 py-10 text-center text-sm text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-300">
        测试结果暂时无法加载，请稍后重试。
      </div>

      <div v-else-if="groups.length === 0" class="rounded-2xl border border-dashed border-gray-300 px-5 py-16 text-center dark:border-dark-600">
        <p class="text-sm text-gray-500 dark:text-dark-400">暂时没有可展示的分组测试结果。</p>
        <p class="mt-2 text-xs text-gray-400 dark:text-dark-500">管理员在账号的定时测试中选择“渠道测智”后，最新作品会显示在这里。</p>
      </div>

      <div v-else class="space-y-8">
        <article
          v-for="group in visibleGroups"
          :key="group.id"
          class="space-y-3"
        >
          <div class="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
            <div class="flex min-w-0 items-center gap-3">
              <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-emerald-100 text-lg dark:bg-emerald-500/15">🪿</div>
              <div class="min-w-0">
                <h2 class="truncate text-lg font-semibold text-gray-900 dark:text-white">{{ group.name }}</h2>
                <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
                  {{ group.platform || '—' }} · {{ group.success_count }} 次成功<span v-if="group.failure_count"> · {{ group.failure_count }} 次失败</span>
                </p>
              </div>
            </div>
            <div class="flex items-center gap-2 text-xs">
              <span :class="statusClass(group)">{{ statusLabel(group) }}</span>
              <span v-if="group.last_test_at" class="text-gray-400 dark:text-dark-500">最近 {{ formatTime(group.last_test_at) }}</span>
            </div>
          </div>

          <div v-if="groupResults(group).length" class="flex gap-4 overflow-x-auto pb-3">
            <article
              v-for="result in groupResults(group)"
              :key="result.id"
              class="w-[270px] shrink-0 overflow-hidden rounded-2xl border border-gray-200 bg-white shadow-card dark:border-dark-700 dark:bg-dark-800 sm:w-[300px]"
            >
              <div class="relative aspect-[4/3] bg-gray-50 dark:bg-dark-900/60">
                <iframe
                  v-if="result.status === 'success' && isMarkup(result.response_text)"
                  class="h-full w-full border-0"
                  :srcdoc="sandboxDocument(result.response_text || '')"
                  sandbox="allow-scripts"
                  referrerpolicy="no-referrer"
                  title="渠道测智结果"
                />
                <pre
                  v-else-if="result.status === 'success'"
                  class="h-full overflow-auto whitespace-pre-wrap p-4 text-xs leading-5 text-gray-700 dark:text-dark-200"
                >{{ result.response_text || '上游返回了空内容' }}</pre>
                <div v-else class="flex h-full flex-col items-center justify-center gap-2 px-5 text-center text-xs text-red-600 dark:text-red-300">
                  <span class="rounded-full bg-red-100 px-2.5 py-1 dark:bg-red-500/15">测试失败</span>
                  <span>{{ result.error_message || '上游未返回有效结果' }}</span>
                </div>
                <span class="absolute left-3 top-3 rounded-full bg-black/60 px-2 py-1 text-[11px] text-white">
                  {{ result.status === 'success' ? '已完成' : '失败' }}
                </span>
              </div>
              <div class="space-y-1 border-t border-gray-100 px-4 py-3 dark:border-dark-700">
                <div class="flex items-center justify-between gap-2">
                  <span class="truncate font-medium text-gray-800 dark:text-dark-100">{{ result.model_id || '未知模型' }}</span>
                  <button v-if="result.status === 'success' && result.response_text" class="shrink-0 text-xs text-sky-600 hover:underline dark:text-sky-300" @click="downloadResult(result)">下载</button>
                </div>
                <div class="flex items-center justify-between text-xs text-gray-400 dark:text-dark-500">
                  <span>{{ formatTime(result.finished_at) }}</span>
                  <span>{{ formatLatency(result.latency_ms) }}</span>
                </div>
              </div>
            </article>
          </div>
          <div v-else class="rounded-2xl border border-dashed border-gray-300 px-5 py-10 text-center text-sm text-gray-400 dark:border-dark-600 dark:text-dark-500">等待管理员为该分组账号启用“渠道测智”</div>
        </article>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { getPelicanShowcase, type PelicanShowcaseGroup, type PelicanShowcaseResult } from '@/api/pelicanShowcase'

const groups = ref<PelicanShowcaseGroup[]>([])
const defaultShowcaseDescription = '作品由系统定时使用各分组账号生成。相同题目下，可以通过画面完整度、指令遵循和响应稳定性直观看出差异；页面不会改变正常请求、计费或账号调度。'
const showcaseDescription = ref(defaultShowcaseDescription)
const loading = ref(false)
const error = ref(false)
const route = useRoute()
const selectedGroup = ref<number | null>(null)

const visibleGroups = computed(() => selectedGroup.value === null
  ? groups.value
  : groups.value.filter(group => group.id === selectedGroup.value))

const previewGroups: PelicanShowcaseGroup[] = [
  {
    id: 1,
    name: 'GPT-稳定',
    platform: 'openai',
    results: [],
    status: 'healthy',
    success_count: 1,
    failure_count: 0,
    latest: {
      id: 1,
      model_id: 'gpt-6-astra',
      thinking_effort: 'medium',
      status: 'success',
      response_text: '<!doctype html><html><body style="font:20px system-ui;padding:32px;background:#f5f7fb"><h2>今天的测试结果</h2><p>这是占位结果，用于检查首页布局。</p></body></html>',
      latency_ms: 1480,
      started_at: '2026-10-29T10:00:00Z',
      finished_at: '2026-10-29T10:00:01Z',
    },
  },
  {
    id: 2,
    name: 'Kimi 稳定',
    platform: 'kimi',
    results: [],
    status: 'healthy',
    success_count: 1,
    failure_count: 1,
    latest: {
      id: 2,
      model_id: 'kimi-k2',
      thinking_effort: 'default',
      status: 'success',
      response_text: '<!doctype html><html><body style="font:20px system-ui;padding:32px;background:#fffaf0"><h2>分组能力正常</h2><p>不同分组会在这里并排展示。</p></body></html>',
      latency_ms: 2230,
      started_at: '2026-10-29T10:00:00Z',
      finished_at: '2026-10-29T10:00:02Z',
    },
    latest_failure: {
      id: 3,
      model_id: 'kimi-k2',
      thinking_effort: 'default',
      status: 'failed',
      error_message: '最近一次测试超时（占位）',
      latency_ms: 10000,
      started_at: '2026-10-29T09:50:00Z',
      finished_at: '2026-10-29T09:50:10Z',
    },
  },
]

async function load() {
  if (route.query.preview === '1') {
    groups.value = previewGroups
    return
  }
  loading.value = true
  error.value = false
  try {
    const response = await getPelicanShowcase()
    groups.value = response.groups || []
    showcaseDescription.value = response.description?.trim() || defaultShowcaseDescription
  } catch {
    error.value = true
  } finally {
    loading.value = false
  }
}

function isMarkup(value?: string) {
  const text = (value || '').trim().toLowerCase()
  return text.startsWith('<') && (text.includes('<html') || text.includes('<svg') || text.includes('<!doctype'))
}

function sandboxDocument(value: string) {
  if (/<meta[^>]+content-security-policy/i.test(value)) return value
  return `<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; connect-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none';"></head><body>${value}</body></html>`
}

function groupResults(group: PelicanShowcaseGroup) {
  if (group.results?.length) return group.results
  return [group.latest, group.latest_failure].filter(Boolean) as PelicanShowcaseResult[]
}

function formatLatency(value?: number) {
  return value == null || value <= 0 ? '—' : `${(value / 1000).toFixed(1)} 秒`
}

function formatTime(value?: string) {
  return value ? new Date(value).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }) : '—'
}

function statusLabel(group: PelicanShowcaseGroup) {
  if (group.status === 'healthy') return '最近测试正常'
  if (group.status === 'degraded') return '最近测试失败'
  if (group.status === 'stale') return '测试已过期'
  return '等待测试'
}

function statusClass(group: PelicanShowcaseGroup) {
  if (group.status === 'healthy') return 'rounded-full bg-emerald-100 px-2.5 py-1 text-xs text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300'
  if (group.status === 'degraded') return 'rounded-full bg-red-100 px-2.5 py-1 text-xs text-red-700 dark:bg-red-500/15 dark:text-red-300'
  if (group.status === 'stale') return 'rounded-full bg-amber-100 px-2.5 py-1 text-xs text-amber-700 dark:bg-amber-500/15 dark:text-amber-300'
  return 'rounded-full bg-gray-100 px-2.5 py-1 text-xs text-gray-500 dark:bg-dark-700 dark:text-dark-400'
}

function downloadResult(result: PelicanShowcaseResult) {
  const blob = new Blob([result.response_text || ''], { type: 'text/html;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = `channel-test-${result.model_id || 'result'}-${result.id}.html`
  anchor.click()
  URL.revokeObjectURL(url)
}

onMounted(load)
</script>
