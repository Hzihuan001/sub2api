## Why

sub2api 目前把用户的订阅账号（Anthropic、OpenAI、Gemini、Antigravity、Grok）当作上游，对外提供统一的 OpenAI / Claude / Gemini 兼容 API，并复用同一套账号存储、分组、调度、计费、粘性会话、限流封禁基础设施。Grok 是最近一次完整接入的平台，形成了清晰的“平台适配”模板。

Cursor 订阅账号目前无法作为上游被复用。用户希望把 Cursor 订阅账号当作上游接入，对外暴露 OpenAI 兼容 API，并且能够“根据上游动态统计 Cursor 的模型列表”——Cursor 模型很多且会变化，必须从上游 `AvailableModels` 动态获取并合并进对外 `/v1/models`，而不是硬编码一份默认模型表。

与既有平台不同，Cursor 上游对话协议不是 OpenAI 兼容 JSON，而是 **Connect RPC over HTTP/2 + Protobuf**：

- `POST https://api2.cursor.sh/aiserver.v1.AiService/AvailableModels`
- `POST https://api2.cursor.sh/aiserver.v1.ChatService/StreamUnifiedChatWithTools`
- 内容类型 `application/connect+proto`，响应是由 5 字节 envelope 帧（1 字节 flag + 4 字节大端长度）串起来的 protobuf 消息流。

因此 Cursor 不能像 Grok 那样直接挂在现有 OpenAI 网关上透传 JSON，需要新建独立的上游协议包并在网关层做“OpenAI Chat Completions ⇄ Cursor protobuf”双向翻译。仓库现有的 `cursor` 字样（`isResponsesShape` / `cursorResponsesUnsupportedFields`）只是把 Cursor IDE 当作【客户端】做兼容处理，方向相反，不能作为本次平台适配的实现基础。

本变更以 Grok 平台适配为权威模板，新增 `PlatformCursor = "cursor"`，在尽量少改动既有代码的前提下把 Cursor 订阅账号接入平台体系，并补齐 Grok 现状缺失的“observed 模型合并进对外列表”能力。

## 范围澄清：两种「多智能体」

“多智能体”在本项目语境有两种完全不同的含义，必须先区分：

1. **sub2api 自身的多 worker 并行**：即网关进程内并行处理多个请求 / 账号轮询。这是既有实现细节，本就支持，Cursor 平台自动复用，不需要新增设计。
2. **Cursor 产品侧的多 agent 能力**：包括 Cloud Agents、Agents Window 内并行 agents、SDK + `api.cursor.com/v1/agents` REST、cloud subagents，以及单次响应内的并行工具调用。这些是本变更需要明确取舍的对象。

本变更**首期只做无状态 OpenAI 兼容 chat 网关**：`/v1/chat/completions`（流式 + 非流式）+ 动态 `/v1/models` + `tools`/`tool_choice` 原样透传（含单次响应内的并行工具调用）。Cursor 的**有状态 agent run 能力（Cloud Agents / Agents Window / SDK runs / cloud subagents）首期明确不做**，理由与后续候选方案见下方 Non-Goals。

## What Changes

- 新增平台枚举 `PlatformCursor = "cursor"`，并同步所有平台枚举触点（domain / service 常量、quota / 调度阈值白名单、composite、group、channel、scheduler snapshot、token 刷新注册、缓存失效、Wire、Admin/Gateway 路由、Handler、error passthrough、SQL migration 的 `platform IN (...)` CHECK 约束）。
- 新增独立上游协议包 `backend/internal/pkg/cursor/`，封装 Cursor 的 protobuf 消息编解码与 Connect 帧（envelope）编解码，以及上游端点、请求头（`x-cursor-checksum` / `x-client-key` / `x-session-id` 等）构造。
- 新增网关翻译层 `service/openai_gateway_cursor*.go`，把入站 OpenAI Chat Completions 请求翻译成 `StreamUnifiedChatWithTools` 的 protobuf，并把上游 Connect proto 帧流翻译回 OpenAI 兼容（含 SSE 流式）响应；`tools`/`tool_choice` 原样透传（含单次响应内并行工具调用），禁止 strip tools / system prompt / `cache_control` 以避免降智。
- 新增 Cursor 认证与令牌链路：`cursor_oauth_service.go`、`cursor_token_provider.go`、`cursor_token_refresher.go`、`cursor_credential_failure.go`，支持三种凭据来源（浏览器 Cookie `WorkosCursorSessionToken`、`crsr_` User API Key、loginDeepControl PKCE 深链登录 + `/auth/poll` 轮询）与两条 JWT 刷新路径（API Key `exchange_user_api_key`、client `POST /oauth/token` refresh_token）。
- 新增**动态模型统计** `cursor_observed_models.go`：鉴权成功后调用上游 `AvailableModels`，把模型列表写入 `account.Extra.cursor_observed_models`；并**比 Grok 更进一步**——把 observed 模型合并进对外 `/v1/models`（`GetAvailableModels` 或 `handler/gateway_handler.go` 的 `Models`）以及 Admin“可用模型”接口。
- 新增 Admin OAuth 接线：`repository/cursor_oauth_client.go`、`handler/admin/cursor_oauth_handler.go`，并在 `server/routes/admin.go` 注册 `/admin/cursor/*` 路由组（参照 `registerGrokOAuthRoutes`）。
- 在 `service/account.go` 新增 `IsCursor()` / `GetCursorAccessToken()` / `GetCursorBaseURL()` 等访问器，凭据形态复用现有加密 credentials 存储。
- 新增前端支持：`types/index.ts` 联合类型加 `'cursor'`、`api/admin/settings.ts` 的 `PLATFORMS` 加 `cursor`、新建 `api/admin/cursor.ts` 与 `composables/useCursorOAuth.ts`，改造账号表单 / OAuth 授权流 / 平台图标 / 徽章 / 颜色 / 分组与账号视图 / i18n（zh + en）。
- 复用现有账号存储、分组、调度、计费、粘性会话、限流封禁基础设施，不为 Cursor 另起一套。

不做的事（Non-Goals）见下节 Impact 之后的说明。

## Capabilities

### New Capabilities

- `cursor-platform`：定义把 Cursor 订阅账号作为上游平台接入的完整能力——平台枚举接线、凭据形态与三来源认证、JWT 刷新、Connect/protobuf 上游协议、OpenAI⇄Cursor 网关翻译（含 SSE）、动态模型统计与对外 `/v1/models` 合并、以及对既有账号 / 分组 / 调度 / 计费 / 限流基础设施的复用不变量。

### Modified Capabilities

无。仓库当前没有已发布的 OpenSpec capability；既有平台（含 Grok）的行为在本变更中作为兼容基线，不修改其正式需求语义。本变更只在 `GetAvailableModels` / `Models` 合并 observed 模型这一处，顺带补齐 Grok 现状缺失但不改变其对外默认模型列表的行为。

## Impact

- **平台枚举**：`backend/internal/domain/constants.go`（+`PlatformCursor`）、`backend/internal/service/domain_constants.go`（re-export，并评估是否加入 `AllowedQuotaPlatforms` / `AllowedSchedulingThresholdPlatforms`）、`backend/internal/model/error_passthrough_rule.go`（常量 + `AllPlatforms()`）、`service/composite_platform.go`（`isConcreteRequestPlatform` 等）、`service/group.go`（`profitControlPlatformSupported`）、`service/admin_group.go`、`service/channel_service.go`、`service/scheduler_snapshot_service.go`（含 `schedulerSnapshotPlatforms` 数组长度）、`service/token_refresh_service.go`（`registrations`）、`service/token_cache_invalidator.go`、`service/wire.go` + `cmd/server/wire_gen.go`、`server/routes/admin.go`、`server/routes/gateway.go`、`handler/openai_gateway_handler.go`、`handler/gateway_handler.go`（Models / composite）、`handler/endpoint.go`。
- **上游协议**：新增 `backend/internal/pkg/cursor/`（models / oauth / protobuf 编解码 / Connect 帧编解码），是 Cursor 与既有 JSON 平台的关键差异面。
- **网关**：新增 `service/openai_gateway_cursor*.go` 翻译层与 `service/cursor_upstream_{url,headers,errors}.go`；接入位置遵循既有平台不变量（鉴权、body limit、模型解析之后，账号选择/计费/上游之前）。
- **认证与令牌**：新增 `service/cursor_oauth_service.go`、`cursor_token_provider.go`、`cursor_token_refresher.go`、`cursor_credential_failure.go`，注册进 `TokenRefreshService.registrations` 与 `token_cache_invalidator`。
- **动态模型**：新增 `service/cursor_observed_models.go`，写入 `account.Extra.cursor_observed_models`；`gateway_service.go` / `gateway_handler.go` 合并 observed 模型进对外列表。
- **Admin API**：新增 `/admin/cursor/*`，复用 AdminAuth 与现有管理操作审计；`repository/cursor_oauth_client.go`、`handler/admin/cursor_oauth_handler.go`。
- **数据库**：新增迁移（编号取实施时最大序号递增），把 `cursor` 加入所有 `platform IN (...)` / `target_platform IN (...)` CHECK 约束（参照 Grok 的 157 号 `user_platform_quotas` 与 172 号 `composite_model_routes`）；不改既有表数据。
- **前端**：`types/index.ts`、`api/admin/settings.ts`、`api/admin/cursor.ts`（新建）、`composables/useCursorOAuth.ts`（新建）、`composables/useModelWhitelist.ts`、`components/account/{CreateAccountModal,EditAccountModal,OAuthAuthorizationFlow}.vue`、`components/account/credentialsBuilder.ts`、`components/common/{PlatformIcon,PlatformTypeBadge}.vue`、`utils/platformColors.ts`、`views/admin/{GroupsView,AccountsView}.vue`、i18n `locales/{zh,en}/admin/{accounts,settings}.ts`。
- **兼容性**：不改变任何既有平台的对外 API；新增平台在未配置 Cursor 账号时对现有请求零影响。
- **实现风险（已收敛）**：Cursor 的 Connect 封帧、`AvailableModels` / `StreamUnifiedChat*` protobuf 字段号、`x-cursor-checksum` 算法、深链 `/auth/poll` 协议、错误分类、计费与调度取舍均已由上游协议研究确认并回填 `design.md`，`pkg/cursor` 按此实现。仅剩两项二期 TODO（多模态图片字段号、`ClientSideToolV2` 工具枚举完整对照表）与一处 wire 级注记（ghost 模式下对话 host 可能为 `agent(n).api5.cursor.sh`，以实现核对为准），均不阻塞首期。

### Non-Goals

- 不改变仓库现有把 Cursor IDE 当【客户端】兼容的逻辑（`isResponsesShape` / `cursorResponsesUnsupportedFields`），两者方向相反、互不影响。
- 不为 Cursor 引入新的第三方 protobuf / HTTP2 运行时以外的重型依赖；优先使用标准库与仓库既有依赖（proto 编解码可用最小手写或既有 `google.golang.org/protobuf`）。
- 不在本变更实现 Cursor 的图像 / 视频 / 音频等非文本能力；第一版只覆盖 Chat Completions（含流式、含 `tools` 透传）。多模态图片字段号列为二期 TODO。
- 不改造既有账号调度、计费或分组架构；只做平台枚举扩展与显式接线。
- 不硬编码 Cursor 模型清单作为对外默认；对外模型来自 observed 动态统计（可保留极小兜底）。
- **不实现 Cursor 产品侧的有状态多 agent 能力**：Cloud Agents、Agents Window 并行 agents、SDK agent runs、cloud subagents 首期一律不做。理由：这些能力有状态、长时（分钟~小时）、绑定 GitHub repo、异步 run 生命周期、产出为 PR / diff / artifacts、按官方 API pricing 计费，且**只能用官方 `crsr_` Key 调 `api.cursor.com/v1/agents`（逆向订阅 JWT 无法访问）**，与无状态 OpenAI 兼容 chat 网关模型根本不匹配；现有逆向项目也无一实现此映射。

### 后续增强候选（方案 C，优先级低于首期）

如后续确需支持 Cloud Agent，应另建独立的 `/v1/agents` 风格直通通道：要求用户提供**官方 API Key**（而非逆向订阅 JWT），与主 chat 路径完全解耦，配备独立的有状态 run 存储、SSE 推送与按量计费。此候选优先级明确低于首期无状态 chat 网关，不在本变更排期。

## Execution References

- `design.md`：平台枚举接线、`pkg/cursor` 协议包接口级设计、OpenAI⇄Cursor 翻译层、token provider/refresher、动态模型合并、计费/调度复用、凭据形态与 checksum。
- `tasks.md`：按后端基础设施 / 上游协议 / 网关翻译 / Admin+Wire / 前端 / 迁移 / 测试分组的可勾选任务清单。
- `implementation-guide.md`：按依赖顺序的分阶段实施指南（Phase B 平台枚举 → C `pkg/cursor` → D token → E 网关翻译 → F Admin+Wire → H 前端 → 迁移 → 测试）。
- `verification.md`：`go build` / wire、后端单测、前端 vitest、端到端 `/v1/models` 动态列表与 `/v1/chat/completions` 流式验证。
- `specs/cursor-platform/spec.md`：能力规格（Requirement / Scenario）。
