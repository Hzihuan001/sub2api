import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const dir = dirname(fileURLToPath(import.meta.url))
const source = readFileSync(resolve(dir, '../DocumentationView.vue'), 'utf8')

describe('DocumentationView', () => {
  it('exposes exactly the three final documentation topics', () => {
    expect(source).toContain("type TopicId = 'token' | 'ccswitch' | 'tools'")
    expect(source).toContain("{ id: 'token', label: '创建令牌' }")
    expect(source).toContain("{ id: 'ccswitch', label: 'CCSwitch 的使用' }")
    expect(source).toContain("{ id: 'tools', label: '接入其他工具' }")
    expect(source).not.toContain("id: 'overview'")
    expect(source).not.toContain("id: 'codex-claude'")
  })

  it('includes the final token and CC Switch screenshots', () => {
    expect(source).toContain("@/assets/docs/token/create-key.png")
    expect(source).toContain("@/assets/docs/token/copy-key.png")
    expect(source).toContain("@/assets/docs/ccswitch/import-from-key-page.png")
    expect(source).toContain("@/assets/docs/ccswitch/conversation-history-result.png")
  })

  it('keeps Codex and Claude configuration inside the tools chapter', () => {
    expect(source).toContain('Codex 配置方法')
    expect(source).toContain('Claude Desktop 配置方法')
    expect(source).toContain('[model_providers.moshu]')
    expect(source).toContain('inferenceGatewayBaseUrl')
  })
})
