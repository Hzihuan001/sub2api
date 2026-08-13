## 1. 固定研究结论与实施边界（已冻结）

- [x] 1.1 上游协议研究已冻结并回填 `design.md`：Connect 封帧 flag 语义、`AvailableModels` / `StreamUnifiedChat*` 字段号、对话响应 text/thinking/tool_call 映射、`x-cursor-checksum` 与全部客户端标识头算法、深链 `/auth/poll` 协议、错误分类、计费与调度取舍。`pkg/cursor` 按此实现，**不再保留 `// TODO(wire)` 占位**
- [x] 1.2 范围结论已冻结：首期做无状态 chat 网关（chat/models + `tools` 透传），Cursor 有状态多 agent 能力（Cloud Agents / Agents Window / SDK runs / cloud subagents）out-of-scope（见 12 组）
- [x] 1.3 开放问题已定：`cursor` **不**加入 `AllowedSchedulingThresholdPlatforms`（无干净原生用量窗口）；上游对话流不保证随流返回 usage，首期计费=本地 token 估算 + 可选 dashboard 端点回填
- [ ] 1.4 仅剩两项二期 TODO 保留未定：多模态图片字段号、`ClientSideToolV2` 工具枚举完整对照表（见 11 组）
- [x] 1.5 用 grep + 编译枚举当前所有 `PlatformGrok` 触点，确认与 `design.md` 决策 1 的触点表一致，作为逐点接线基线
- [x] 1.6 保存实施前基线：`cd backend && go build ./...`、`go test ./internal/service -run 'Platform|Models' -count=1`

## 2. 平台枚举接线（Phase B）

- [x] 2.1 `internal/domain/constants.go`：在 const 块新增 `PlatformCursor = "cursor"`
- [x] 2.2 `internal/service/domain_constants.go`：re-export `PlatformCursor`；将 `cursor` 加入 `AllowedQuotaPlatforms`；调度阈值白名单按 1.3 结论决定是否加入
- [x] 2.3 `internal/model/error_passthrough_rule.go`：新增 `PlatformCursor` 常量并加入 `AllPlatforms()`
- [x] 2.4 `internal/service/composite_platform.go`：把 `cursor` 加入 `isConcreteRequestPlatform` 等 concrete 平台集合
- [x] 2.5 `internal/service/group.go`：`profitControlPlatformSupported` 加入 `PlatformCursor`（若纳入利润控制）并更新其错误文案
- [x] 2.6 `internal/service/admin_group.go`：允许创建 / 更新 Cursor 平台分组
- [x] 2.7 `internal/service/channel_service.go`：Cursor 渠道分派（如适用）
- [x] 2.8 `internal/service/scheduler_snapshot_service.go`：在平台 switch 加 `PlatformCursor` 分支；`schedulerSnapshotPlatforms()` 返回类型 `[5]string` → `[6]string` 并加入 `PlatformCursor`
- [x] 2.9 `internal/service/token_cache_invalidator.go`：新增 `case PlatformCursor`，删除 `CursorTokenCacheKey(account)` 及 `cursor:<id>` 键
- [x] 2.10 `internal/handler/endpoint.go`：按 Cursor endpoint 归一策略接线（如需与 OpenAI/Grok 并列）
- [x] 2.11 `ent/schema/user_platform_quota.go`：在 `Validate` 允许 `cursor`（构建期约束）
- [x] 2.12 编译通过并跑平台枚举相关单测，确认无遗漏分支

## 3. 上游协议包 `pkg/cursor`（Phase C）

- [x] 3.1 `pkg/cursor/doc.go`：定义 `DefaultBaseURL = "https://api2.cursor.sh"`、端点常量、`ContentTypeConnectProto`
- [x] 3.2 `pkg/cursor/envelope.go`：实现 Connect 帧编解码 `WriteEnvelope` / `EnvelopeReader.Next`（`[flag:1][len:4 大端][payload]`）；flag 0x00 数据 / 0x01 gzip / 0x02 结束(JSON) / 0x03 gzip 结束
- [x] 3.3 `pkg/cursor/proto.go`：按已冻结字段号定义 protobuf 消息与 `Marshal*/Unmarshal*`（`AvailableModels*`、`StreamUnifiedChatRequestWithTools`/`StreamUnifiedChatRequest`、`ConversationMessage`、`Instruction`、`Model`、`StreamUnifiedChatResponse*`、`Thinking`、`ClientSideToolV2Call`）
- [x] 3.4 `pkg/cursor/models.go`：`AvailableModelsRequest{use_model_parameters=5,do_not_use_markdown=7}`、`AvailableModelsResponse{models=2}`、`Model{name=1,server_model_name=18,context_token_limit=15,...}` 与解析（无 is_premium）
- [x] 3.5 `pkg/cursor/chat.go`：`ChatRequest`（messages/role(1/2)/Instruction/model/unified_mode/thinking_level/supported_tools）、`ChatDelta`（text=1 / thinking=25 / tool_call{id=3,name=9,raw_args=10,is_last=11}）
- [x] 3.6 `pkg/cursor/oauth.go`：`Credential`、`ParseWorkosSessionToken`（`userId::JWT`）、`UserIDFromJWT`（sub 取最后一个 `|` 尾）、`ExchangeUserAPIKey`、`RefreshWithRefreshToken`（仅深链 refresh_token）、深链 `NewDeepLinkChallenge` + `PollDeepLink`（退避 base 1s×~1.2，约 60 次）
- [x] 3.7 `pkg/cursor/checksum.go`：`x-cursor-checksum`（machineId/macMachineId=sha256hex；ts=unixMillis/1e6 大端后 6 字节；Jyh 滚动异或 t=165；base64 URL-safe 无 pad；`encoded+machineId+"/"+macMachineId`）
- [x] 3.8 `pkg/cursor/identity.go`：`x-client-key=sha256hex(token)`、`x-session-id=uuid5(DNS,token)`、`x-request-id`/`x-amzn-trace-id`/`x-cursor-config-version`/`connect-protocol-version:1`/`x-cursor-client-version`/`x-ghost-mode`/`authorization`；`BuildUpstreamHeaders(token, now)`
- [x] 3.9 单测：envelope round-trip / 结束帧 JSON error / gzip / 截断错误、checksum 已知向量、header 生成稳定性、`ParseWorkosSessionToken`+`UserIDFromJWT` 边界、模型解析

## 4. 认证与令牌链路（Phase D）

- [x] 4.1 `internal/service/account.go`：新增 `IsCursor()` / `GetCursorAccessToken()` / `GetCursorBaseURL()` / `GetCursorRefreshToken()` / `GetCursorUserAPIKey()`
- [x] 4.2 `internal/service/cursor_oauth_service.go`：三来源导入（Cookie `WorkosCursorSessionToken`、`crsr_` API Key、深链 + poll），落库加密 credentials（参照 `grok_oauth_service.go`）
- [x] 4.3 `internal/service/cursor_token_provider.go`：`GetAccessToken(ctx, account)`，缓存命中 / skew 预热 / 触发刷新（参照 `grok_token_provider.go`）；定义 `CursorTokenCacheKey`
- [x] 4.4 `internal/service/cursor_token_refresher.go`：实现 `OAuthRefreshExecutor`（`CanRefresh`/`NeedsRefresh`/`Refresh`），两条路径（API Key `exchange_user_api_key`、client `/oauth/token` refresh_token）
- [x] 4.5 `internal/service/token_refresh_service.go`：在 `registrations` 注册 `{platform: PlatformCursor, refresher, executor}`
- [x] 4.6 `internal/service/cursor_credential_failure.go`：失败计数与临时停调策略（参照 `grok_credential_failure.go`）
- [x] 4.7 `internal/repository/cursor_oauth_client.go`：`CursorOAuthClient` 对认证端点的 HTTP 封装（参照 `grok_oauth_client.go`）
- [x] 4.8 单测：三来源导入、access token 过期后 API Key 重新 exchange、refresh_token 刷新、失败停调、cache key

## 5. 网关翻译层（Phase E）

- [x] 5.1 `internal/service/cursor_upstream_url.go`：Base URL 归一与端点拼接（默认 `api2.cursor.sh`，可账号覆盖）
- [x] 5.2 `internal/service/cursor_upstream_headers.go`：调用 `pkg/cursor.BuildUpstreamHeaders` 并叠加账号 header 覆盖 / 代理
- [x] 5.3 `internal/service/cursor_upstream_errors.go`：按 `design.md` 决策 9 分类——`401/unauthenticated` 先刷新再切号；`resource_exhausted`(57) 可重试/换模型/不计费/不封号 + 指数退避；额度耗尽文案冷却切号；带模型名错误剔除该模型；统一映射 OpenAI error envelope
- [x] 5.4 `internal/service/openai_gateway_cursor{,_translate}.go`：OpenAI Chat Completions JSON → `cursor.AgentRunParams`（system/developer→`custom_system_prompt`；多轮 messages 展平成单条 prompt（单条 user 原样、多轮加 `User:/Assistant:/Tool result (id):` 标签）；model→wire id（`auto`/空→`default`、`-max`→`max_mode`、`reasoning_effort` + observed 模型命中时用 `-thinking` 变体）；`mode=AGENT`；`cwd=/tmp`；`conversation_state` 留空）→ `cursor.OpenAgentStream` 走 api5 `agent.v1.AgentService/Run`
- [ ] 5.5 **`tools`/`tool_choice` 原样透传**编码进 api5 `McpTools`（`input_schema` 走 `google.protobuf.Value`）；禁止 strip tools/system prompt/`cache_control`；上游 `ToolCall` → `delta.tool_calls[]`（含单次响应内并行工具调用，按 index），`Thinking` → `delta.reasoning_content`
- [x] 5.6 `internal/service/openai_gateway_cursor.go`：`AgentEvent` 流 → OpenAI；非流式聚合为 `chat.completion`（含 tool_calls），流式转 OpenAI SSE chunk + `[DONE]`；首个增量前不写首字节；`TurnEnded` 用量帧缺失时回退本地估算
- [x] 5.7 `internal/handler/openai_gateway_handler.go`：为 `platform == cursor` 分派到 Cursor 翻译层，接入位置遵循既有不变量（账号选择/计费/上游之前）
- [x] 5.8 `internal/server/routes/gateway.go`：如 Cursor 需要独立 gateway 接线则补齐
- [x] 5.9 计费接线：优先用 `TurnEnded` 用量帧，缺失或全零时回退本地 token 估算，复用既有计费路径；可选周期性读 dashboard 用量端点（美分单位）回填对账
- [x] 5.10 单测：请求翻译 golden、`tools` 透传与并行 tool_calls、SSE 流式增量、非流式聚合、错误分类映射（含 resource_exhausted 不封号）、首字节时序
- [x] 5.11 **对话上游从 api2 切到 api5**（api2 `StreamUnifiedChatWithTools` 已被上游下线，回 "Update Required"）：`internal/service/openai_gateway_cursor_transport.go` 提供 api5 配置（`client-version` / agent base url 含区域覆盖 / `ghost` / 首字节 & 空闲超时，按 credentials → extra → env → `pkg/cursor` 常量逐级解析）与 h2 代理客户端缓存；`_bridges.go`（Responses / Anthropic）同步切换。`/v1/models` 仍走 api2 `AvailableModels`，`pkg/cursor/chat.go` 保留备回滚

## 6. 动态模型统计与对外合并（用户核心诉求）

- [x] 6.1 `internal/service/cursor_observed_models.go`：`scheduleCursorObservedModelsSync`（fire-and-forget + TTL + in-flight 去重）+ `syncCursorObservedModels`（调 `AvailableModels`，写 `Extra.cursor_observed_models`），参照 `grok_observed_models.go`
- [x] 6.2 鉴权成功后触发同步（导入 / 探测 / 刷新成功回调）
- [x] 6.3 `internal/service/gateway_service.go: GetAvailableModels`：`platform == cursor` 时把各账号 `Extra.cursor_observed_models.models` 并入结果，去重排序并复用 `modelsListCache`
- [x] 6.4 `internal/handler/gateway_handler.go`：`compositeAvailableModels`（1150 平台数组）加入 `PlatformCursor`；`writeModelsList` / `Models` 正确输出 Cursor 模型
- [x] 6.5 Admin“可用模型”接口读取同一合并结果，前端模型白名单可见 Cursor 模型
- [x] 6.6 单测：observed 写入 Extra、合并进 `GetAvailableModels`、空 observed 兜底、缓存命中/失效

## 7. Admin + Wire（Phase F）

- [x] 7.1 `internal/handler/admin/cursor_oauth_handler.go`：`GetCapabilities` / `GenerateAuthURL`（深链）/ `Poll` / 从 Cookie 或 API Key 创建账号 / `RefreshAccountToken` / `RuntimeSanity`（参照 `grok_oauth_handler.go`）
- [x] 7.2 `internal/server/routes/admin.go`：新增 `registerCursorOAuthRoutes(admin, h)`，挂 `/admin/cursor/*` 并在 `RegisterRoutes` 调用
- [x] 7.3 `internal/service/wire.go`：新增 `ProvideCursorOAuthService` / `ProvideCursorTokenProvider`（含 `SetRefreshAPI`/`SetRefreshPolicy`）及 `wire.Bind`
- [x] 7.4 `cmd/server/wire_gen.go`：重新生成或手工同步 Cursor provider 接线
- [x] 7.5 `internal/handler` 聚合结构（`Handlers.Admin`）加入 `CursorOAuth` 字段
- [x] 7.6 `go generate ./...`（wire）后 `go build ./...` 通过

## 8. 数据库迁移

- [x] 8.1 新增迁移（编号取实施时最大序号 +1，当前最大为 221）把 `cursor` 加入 `user_platform_quotas` 的 `platform IN (...)` CHECK（参照 157 号做法：DROP + ADD CONSTRAINT）
- [x] 8.2 新增 / 更新迁移把 `cursor` 加入 `composite_model_routes` 的 `target_platform IN (...)` CHECK（参照 172 号）
- [x] 8.3 复核其它含 `platform IN (...)` 的迁移 / 渠道监控 provider 约束（参照 176 号），需要时补齐 `cursor`
- [x] 8.4 迁移可从空库与当前生产前一版应用；不修改已应用迁移、不改既有表数据

## 9. 前端（Phase H）

- [ ] 9.1 `frontend/src/types/index.ts`：`AccountPlatform`、`GroupPlatform` 联合类型加 `'cursor'`
- [ ] 9.2 `frontend/src/api/admin/settings.ts`：`PLATFORMS` 加 `'cursor'`（调度阈值数组按 1.3 结论）
- [ ] 9.3 新建 `frontend/src/api/admin/cursor.ts`：Admin Cursor OAuth API 封装
- [ ] 9.4 新建 `frontend/src/composables/useCursorOAuth.ts`：授权流状态机（Cookie / API Key / 深链三入口 + 轮询），参照 `useGrokOAuth.ts`
- [ ] 9.5 `frontend/src/composables/useModelWhitelist.ts`：白名单可获取 Cursor observed 模型
- [ ] 9.6 `frontend/src/components/account/{CreateAccountModal,EditAccountModal,OAuthAuthorizationFlow}.vue` + `credentialsBuilder.ts`：Cursor 凭据录入
- [ ] 9.7 `frontend/src/components/common/{PlatformIcon,PlatformTypeBadge}.vue` + `utils/platformColors.ts`：Cursor 图标 / 徽章 / 颜色
- [ ] 9.8 `frontend/src/views/admin/{GroupsView,AccountsView}.vue`：允许选择 / 展示 Cursor
- [ ] 9.9 i18n `frontend/src/locales/{zh,en}/admin/{accounts,settings}.ts`：中英文成对文案
- [ ] 9.10 前端单测：`CreateAccountModal.cursor.spec.ts` / `PlatformTypeBadge.cursor.spec.ts` / `useCursorOAuth.spec.ts`（参照 grok spec）

## 10. 测试与验证

- [x] 10.1 `pkg/cursor` 单测：envelope / proto round-trip、header / checksum 形状、oauth 解析
- [x] 10.2 service 单测：token provider/refresher、observed models 合并、翻译层 golden、SSE 流式、错误映射
- [x] 10.3 `go build ./...` + wire 生成通过；`go vet ./...`
- [ ] 10.4 前端 `pnpm --dir frontend run lint:check` / `typecheck` / 相关 vitest / `build`
- [ ] 10.5 端到端：`/v1/models` 返回 Cursor observed 动态列表（api2）；`/v1/chat/completions` 非流式与 `stream=true` 流式经 api5 `AgentService/Run` 跑通（免费额度账号用 `default`/`auto`；注意 `TurnEnded` 用量帧可能缺失，此时应看到本地估算计费而非 0）
- [x] 10.6 回归：未配置 Cursor 账号时既有平台 `/v1/models`、调度、计费、`/admin/*` 行为不变
- [ ] 10.7 `openspec validate add-cursor-platform --type change --strict --no-interactive`
- [ ] 10.8 实现偏离设计时，先回写 `proposal/design/specs/tasks`，再继续编码

## 11. 二期 TODO（保留未定，不阻塞首期）

- [ ] 11.1 多模态图片输入：api2 `ConversationMessage` 内图片字段号未定；api5 `SelectedImage` 已实现内联字节，仅 `dimension` 子消息布局沿用 api2 猜测（宽高留零即与抓包一致），待真机核对
- [ ] 11.2 `ClientSideToolV2` 工具枚举完整对照表：`tool_call.name` 与内置工具语义的完整映射；`tools` 原样透传不依赖该表；Cursor 自带 agentic 工具（shell/read/edit）暂不桥接
- [x] 11.3 对话 host 精确值核对：**已核定**——默认 `agentn.global.api5.cursor.sh` + `x-ghost-mode: true` 真机跑通；`agent.api5.cursor.sh` / `agentn.us.api5.cursor.sh` 作为可配置区域覆盖保留

## 12. 明确 out-of-scope（不排期）

- [ ] 12.1 Cloud Agents / Agents Window 并行 agents / SDK agent runs / cloud subagents：有状态、长时、绑 GitHub repo、产出 PR/diff、按官方 API pricing 计费，且只能用官方 `crsr_` Key 调 `api.cursor.com/v1/agents`（逆向订阅 JWT 无法访问），与无状态 chat 网关模型不匹配——**首期不做、不排期**
- [ ] 12.2 后续增强候选（方案 C）：如确需 Cloud Agent，另建独立 `/v1/agents` 风格直通通道，要求用户提供官方 API Key，独立有状态 run 存储 / SSE / 按量计费，与主 chat 路径解耦；优先级低于首期，本变更不排期
