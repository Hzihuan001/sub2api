# 验证手册

## 1. 验证原则

本文件是实现期验收矩阵与上线前证据索引模板。所有“待实现”项必须替换为可重复执行的测试名、命令输出或端到端结果；仅写“人工验证通过”不算证据。

验证顺序：

1. OpenSpec 结构与需求完整性。
2. `pkg/cursor` 纯协议单测（envelope / proto / oauth / header）。
3. service 单测（token、observed、翻译层、错误映射）。
4. `go build ./...` + wire 生成 + 平台枚举回归。
5. 前端 lint / typecheck / vitest / build。
6. 端到端 `/v1/models` 动态列表与 `/v1/chat/completions` 流式；既有平台零回归。

关键不变量：

- 未配置 Cursor 账号时，既有平台的 `/v1/models`、调度、计费、`/admin/*` 与升级前完全一致。
- Cursor 凭据 / checksum 派生 token 不出现在日志、错误响应、前端持久化状态。
- observed 模型对外可见，且 `/v1/models` 与 Admin 可用模型接口结果一致。
- protobuf 字段号 / checksum 未冻结时，网关翻译层不进入真实上游联调。

## 2. Requirement → Evidence 追踪矩阵

状态词：`待实现`、`通过`、`失败`、`二期`。上游协议细节已冻结，原“阻塞（待 wire 细节）”项已全部改为可执行的 `待实现`；仅多模态图片与工具枚举对照保留 `二期`。

| ID | Requirement（见 spec） | 必备自动化证据 | 状态 |
| --- | --- | --- | --- |
| P01 | 新增 cursor 平台且全枚举同步 | `go build ./...`；平台枚举表驱动测试；grep 触点核对 | 待实现 |
| P02 | Connect/protobuf 上游协议隔离在 pkg/cursor | `pkg/cursor` envelope round-trip / 结束帧 JSON error / gzip；按已冻结字段号的 proto round-trip 单测 | 待实现 |
| P03 | 三来源认证与两条 JWT 刷新 | oauth 解析 / `UserIDFromJWT` / 深链 poll 退避单测；token provider/refresher 单测 | 待实现 |
| P04 | checksum 与客户端标识头 | `x-cursor-checksum` 已知向量单测；`BuildUpstreamHeaders` 稳定性单测 | 待实现 |
| P05 | OpenAI⇄Cursor 翻译（非流式 + SSE） | 请求 golden、SSE 增量、非流式聚合、首字节时序单测 | 待实现 |
| P06 | `tools` 透传与并行工具调用 | tools/tool_choice 编码不丢失；多 tool_call → 并列 `delta.tool_calls[]`；thinking→reasoning_content 单测 | 待实现 |
| P07 | 上游错误分类处置 | resource_exhausted 不计费/不封号 + 退避；401 先刷新再切号；带模型名剔除模型；额度耗尽冷却单测 | 待实现 |
| P08 | 动态模型统计写入 Extra | observed 写入 / 解析（含 name/server_model_name）单测 | 待实现 |
| P09 | observed 合并进对外 /v1/models 与 Admin | `GetAvailableModels` cursor 合并单测；端到端 /v1/models | 待实现 |
| P10 | 复用调度 / 计费 / 限流基础设施 | 调度快照含 cursor；cursor **不**在调度阈值白名单；本地估算计费单测；既有平台回归 | 待实现 |
| P11 | 凭据不泄露 | 日志 / 错误 / 前端状态 canary 扫描 | 待实现 |
| P12 | 未配置 Cursor 零回归 | 既有平台 /v1/models、/admin、调度回归 | 待实现 |
| P13 | 迁移把 cursor 加入所有 platform CHECK | 空库 + 增量迁移应用；CHECK 允许 cursor 拒绝非法值 | 待实现 |
| P14 | 前端可创建 / 展示 Cursor | vitest（modal / badge / composable）；typecheck | 待实现 |
| P15 | 首期不实现有状态多 agent | 结构断言：无 `/v1/agents` run 存储 / PR / artifact 路径；范围守卫测试 | 待实现 |
| P16 | 多模态图片输入 | — | 二期 |
| P17 | 内置工具枚举完整对照 | — | 二期 |

## 3. 标准验证命令

### 3.1 OpenSpec

```bash
cd d:/sjwen/sub2api
openspec validate add-cursor-platform --type change --strict --no-interactive
openspec show add-cursor-platform
```

### 3.2 后端

```bash
cd d:/sjwen/sub2api/backend

# 协议包
go test ./internal/pkg/cursor/... -count=1

# 平台枚举 / 网关 / 模型合并 / token
go test ./internal/service -run 'Cursor|Platform|Models' -count=1
go test ./internal/handler/... -run 'Cursor|Models' -count=1

# 构建与 wire
go generate ./...            # 重生成 wire_gen.go
go build ./...
go vet ./internal/service/... ./internal/pkg/cursor/...
```

上游协议细节已冻结，`pkg/cursor` 单测应使用已冻结字段号 / 封帧 flag / checksum 已知向量做真实断言；无需再用占位。仅多模态图片与内置工具枚举对照属于二期，不在本轮 `pkg/cursor` 单测覆盖。

### 3.3 前端

```bash
cd d:/sjwen/sub2api
pnpm --dir frontend run lint:check
pnpm --dir frontend run typecheck
pnpm --dir frontend exec vitest run \
  src/components/account/__tests__/CreateAccountModal.cursor.spec.ts \
  src/components/common/__tests__/PlatformTypeBadge.cursor.spec.ts \
  src/composables/__tests__/useCursorOAuth.spec.ts
pnpm --dir frontend run build
```

### 3.4 全量

```bash
cd d:/sjwen/sub2api
make build          # 后端 binary + 前端 production build
make test-backend   # 若可用
```

## 4. 端到端矩阵

前置：至少导入一个健康 Cursor 账号（Cookie 或 API Key），归入一个测试分组；access token 已刷新有效；observed 模型已同步。

### 4.1 动态模型列表

| 步骤 | 期望 |
| --- | --- |
| 鉴权成功后触发 observed 同步 | `account.Extra.cursor_observed_models.models` 非空，`source=upstream_available_models` |
| `GET /v1/models`（cursor 分组 key） | 返回合并后的 Cursor 动态模型（observed ∪ model_mapping），去重排序 |
| Admin 可用模型接口 | 与 `/v1/models` 结果一致，前端白名单可见 Cursor 模型 |
| 删除 / 停用 Cursor 账号后 | 该账号 observed 模型不再出现在对外列表（缓存过期后） |

### 4.2 对话（非流式 + 流式）

| 入口 | 场景 | 期望 |
| --- | --- | --- |
| `POST /v1/chat/completions`（stream=false） | 正常对话 | 返回 OpenAI 兼容 `chat.completion`，内容来自上游 protobuf 聚合 |
| `POST /v1/chat/completions`（stream=true） | 正常对话 | 首帧前 0 字节；逐 delta 输出 `chat.completion.chunk`；`thinking`→`reasoning_content`；末尾 `data: [DONE]` |
| 带 `tools`/`tool_choice` | 工具调用（含并行） | tools 原样透传不丢失；上游多 `tool_call` → 并列 `delta.tool_calls[]`；不 strip system prompt / cache_control |
| `resource_exhausted`（错误码 57） | 上游瞬时容量 | 不计费、不封号；触发指数退避、可重试 / 换模型 |
| `401` / `unauthenticated` | 认证失效 | 先刷新 token；失败再切号 |
| 错误带模型名 | 模型级错误 | 从该账号可用集合剔除该模型 |
| 明确“额度耗尽”文案 | 额度耗尽 | 账号进入冷却并切号 |
| access token 过期 | 请求路径命中过期 | 走刷新路径（API Key exchange 或深链 refresh_token）后成功，或明确失败不静默挂起 |

### 4.3 副作用与不变量

- 拒绝 / 错误时账号选择、计费预扣、上游写入符合既有平台不变量（在账号选择/计费之后才真正拨号）。
- Cursor 账号进入既有调度：粘性会话、并发 slot、限流封禁与其它平台一致。

## 5. 凭据泄露门禁

- 日志：只出现 account ID、稳定错误码、模型名；不出现 access token / refresh token / user API key / checksum 明文 / `WorkosCursorSessionToken`。
- 错误响应：只有通用消息 + code + request ID。
- 前端：`useCursorOAuth` 提交后清空明文；不写 localStorage / sessionStorage / console。
- 用 canary（如 `CURSOR_TOKEN_CANARY_<random>`）在导入 → 刷新 → 探测 → 对话链路后扫描日志与前端状态，命中即为 P0。

## 6. 既有平台回归

必须保存迁移前后同一套结果一致：

- 未配置 Cursor 时 `GET /v1/models`（各既有平台）列表不变。
- Anthropic / OpenAI / Gemini / Antigravity / Grok 的调度、计费、令牌刷新、`/admin/*` 行为不变。
- 迁移不改既有表数据；`user_platform_quotas` / `composite_model_routes` 对既有平台值仍接受。

## 7. 已冻结上游细节的核对与二期项

上游协议细节已冻结并回填 `design.md`；实现时按以下方式核对，仅两项保留为二期：

1. `pkg/cursor` 按已冻结字段号 / 封帧 flag / checksum 算法实现，无 `// TODO(wire)`；`pkg/cursor` 与翻译层 golden 直接可执行。
2. 对真实 Cursor 上游跑一次 api2 `AvailableModels`（unary）与 api5 `agent.v1.AgentService/Run`（h2 双向流）冒烟，核对 `/v1/models` 与流式输出。对话 host 已核定为 `agentn.global.api5.cursor.sh` + `x-ghost-mode: true`（真机验证）；api2 的 `StreamUnifiedChatWithTools` 已被上游下线，不再冒烟。注意 `TurnEnded` 用量帧可能缺失，此时计费应落到本地估算而非 0。
3. `cursor` 确认**不**加入 `AllowedSchedulingThresholdPlatforms`；断言其不在该白名单，且前端调度阈值数组不含 cursor。
4. 计费按本地 token 估算验证；如启用 dashboard 用量端点回填，核对金额单位为**美分**、`membership_type` 会员判定。
5. **二期项**（本变更不验收，另起）：多模态图片输入字段号（P16）、`ClientSideToolV2` 内置工具枚举完整对照（P17）。

## 8. 发布检查清单

| 项目 | 结果/链接 | 责任人 | 时间 |
| --- | --- | --- | --- |
| OpenSpec strict validate | TODO | TODO | TODO |
| pkg/cursor 单测 | TODO | TODO | TODO |
| service / handler 单测 | TODO | TODO | TODO |
| go build + wire | TODO | TODO | TODO |
| 前端 lint/typecheck/vitest/build | TODO | TODO | TODO |
| 端到端 /v1/models 动态列表 | TODO | TODO | TODO |
| 端到端 /v1/chat/completions 流式 + tools | TODO | TODO | TODO |
| 上游错误分类（resource_exhausted 不封号等） | TODO | TODO | TODO |
| 凭据泄露 canary 扫描 | TODO | TODO | TODO |
| 既有平台零回归 | TODO | TODO | TODO |

上游协议细节已冻结，P02–P05 为可执行的 `待实现`，实现完成即可标 `通过`。仅 P16（多模态图片）/ P17（内置工具枚举对照）为二期，不在本变更验收范围。
