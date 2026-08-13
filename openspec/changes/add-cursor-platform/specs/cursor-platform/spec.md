## ADDED Requirements

### Requirement: 系统必须新增 Cursor 平台并同步所有平台枚举触点
系统 SHALL 新增平台标识 `PlatformCursor = "cursor"`，并在所有依赖平台枚举的分派点保持一致。平台常量、re-export、quota 白名单、composite concrete 集合、利润控制、调度快照、令牌刷新注册、令牌缓存失效、Wire、Admin/Gateway 路由、Handler 分派、error passthrough 平台列表和数据库 `platform IN (...)` CHECK 约束 MUST 全部识别 `cursor`；未配置 Cursor 账号时既有平台行为 MUST NOT 改变。

#### Scenario: 新增平台标识
- **WHEN** 后端编译并加载平台常量
- **THEN** `domain.PlatformCursor` 与 `service.PlatformCursor` MUST 存在且等于 `"cursor"`
- **THEN** `error_passthrough_rule.AllPlatforms()` MUST 包含 `cursor`

#### Scenario: 调度与缓存分派识别 Cursor
- **WHEN** 一个 `platform=cursor` 的账号进入调度快照或令牌缓存失效流程
- **THEN** 系统 MUST 为其执行对应平台分支，而不是落入默认忽略分支
- **THEN** `schedulerSnapshotPlatforms` MUST 包含 `cursor`

#### Scenario: 未配置 Cursor 时零回归
- **WHEN** 系统升级到包含本能力的版本但未创建任何 Cursor 账号
- **THEN** 既有平台的 `/v1/models`、账号调度、计费和 `/admin/*` MUST 与升级前完全一致

### Requirement: Cursor 上游协议必须以独立协议包实现 Connect/protobuf
系统 SHALL 在 `backend/internal/pkg/cursor/` 内实现 Cursor 上游协议，包括 protobuf 消息编解码和 Connect 帧编解码。帧格式 MUST 为 `[flag:1][payload_len:4 大端 uint32][payload]`，flag MUST 按 `0x00` 数据、`0x01` gzip 数据、`0x02` 流结束/trailer（payload 为 JSON，`{}`=正常、含 `error`=错误）、`0x03` gzip 结束帧解释。上游对话 MUST 通过 `POST {agentBase}/agent.v1.AgentService/Run`（HTTP/2 双向流，`application/connect+proto`，封帧，默认 agent 主域 `https://agentn.global.api5.cursor.sh`），请求头 MUST 只含 CLI 那 10 个头且 MUST NOT 携带 `x-cursor-checksum` 等 identity 头；模型列表 MUST 通过 `POST {base}/aiserver.v1.AiService/AvailableModels`（unary，`application/proto`，不封帧，默认主域 `https://api2.cursor.sh`）。api2 的 `ChatService/StreamUnifiedChatWithTools` 已被上游下线，MUST NOT 再用于对话（其编解码保留仅供回滚）。协议包 MUST NOT 依赖 `internal/service` 或 `internal/handler`。

#### Scenario: Connect 帧编解码往返
- **WHEN** 一段 protobuf payload 经 `WriteEnvelope` 编码后再由 `EnvelopeReader` 读取
- **THEN** 读取得到的 flag 与 payload MUST 与写入一致
- **THEN** 读取到 `0x02` 结束帧时 MUST 能识别流结束，并在 JSON 含 `error` 字段时判为上游错误

#### Scenario: 帧长度使用大端
- **WHEN** 解析 Connect envelope 的 4 字节长度前缀
- **THEN** 系统 MUST 按大端解释长度，并在字节不足时返回明确错误而不是静默截断

#### Scenario: 按已冻结字段号编解码模型与对话
- **WHEN** 编解码 `AvailableModelsResponse` 与 `StreamUnifiedChatResponse`
- **THEN** 系统 MUST 使用已冻结字段号：模型 `Model.name=1` / `server_model_name=18` / `context_token_limit=15`，对话 `response.text=1` / `thinking(text)=25` / `tool_call{id=3,name=9,raw_args=10,is_last=11}`
- **THEN** 系统 MUST NOT 依赖不存在的 `is_premium` 字段判定模型（premium 结合定价表判断）

### Requirement: 系统必须支持三种 Cursor 凭据来源与两条 JWT 刷新路径
系统 SHALL 支持从浏览器 Cookie `WorkosCursorSessionToken`（格式 `userId::JWT`）、`crsr_` 前缀 User API Key 和 loginDeepControl PKCE 深链登录 + `/auth/poll` 轮询三种来源导入 Cursor 凭据。Cursor access token 是 ~1 小时过期的 JWT；系统 MUST 支持两条刷新路径：API Key 路线通过 `POST /auth/exchange_user_api_key`，client 路线通过 `POST /oauth/token` 使用 refresh_token。凭据 MUST 使用现有加密 credentials 存储。

#### Scenario: 从 Cookie 导入
- **WHEN** 管理员提交 `WorkosCursorSessionToken`
- **THEN** 系统 MUST 解析出 `userId` 与 JWT 并加密落库
- **THEN** 系统 MUST 记录凭据来源为 cookie

#### Scenario: API Key 过期后重新兑换
- **WHEN** 账号使用 `crsr_` User API Key 且 access token 已过期
- **THEN** 系统 MUST 通过 `exchange_user_api_key` 重新兑换 access token
- **THEN** 请求路径 MUST NOT 因过期而静默失败

#### Scenario: client 路线刷新
- **WHEN** 账号持有 refresh_token 且 access token 接近过期
- **THEN** 系统 MUST 通过 `POST /oauth/token` 刷新并更新缓存
- **THEN** 刷新 MUST 接入既有 `TokenRefreshService` 注册与令牌缓存失效

### Requirement: 每次上游调用必须携带 Cursor 客户端标识头
系统 SHALL 在每次上游调用前生成 Cursor 客户端标识头。`x-cursor-checksum` MUST 按已冻结算法派生：`machineId = sha256hex(token+"machineId")`、`macMachineId = sha256hex(token+"macMachineId")`；`ts = unixMillis/1_000_000` 取大端 uint64 的后 6 字节，经 Jyh 滚动异或（`t=165; b[i]=((b[i]^t)+(i%256))&0xFF; t=b[i]`）后 base64（URL-safe 无 pad）编码为 `encoded`；`checksum = encoded + machineId + "/" + macMachineId`。还 MUST 包含 `x-client-key = sha256hex(token)`、`x-session-id = uuid5(DNS, token)`、`x-request-id`（uuid4）、`x-amzn-trace-id: Root=<x-request-id>`、`x-cursor-config-version`（uuid4）、`connect-protocol-version: 1`、`x-cursor-client-version`（需贴近真实客户端版本，否则上游 `permission_denied`）、`x-ghost-mode`、`authorization: Bearer <JWT>`。这些派生值和原始 token MUST NOT 出现在日志、错误响应或前端持久化状态。

#### Scenario: 生成请求头
- **WHEN** 网关为一次 Cursor 上游调用构造请求头
- **THEN** 请求头 MUST 包含按上述算法计算的 `x-cursor-checksum`、`x-client-key`、`x-session-id`
- **THEN** 对同一 token，`x-session-id` MUST 稳定

#### Scenario: 客户端版本过旧
- **WHEN** `x-cursor-client-version` 明显落后于真实客户端且上游返回 `permission_denied`
- **THEN** 系统 MUST 把该情况映射为可诊断的稳定错误码，而不是当作账号封禁

### Requirement: 网关必须在 OpenAI Chat Completions 与 Cursor protobuf 间双向翻译
系统 SHALL 把入站 OpenAI Chat Completions 请求翻译为 Cursor `agent.v1.AgentService/Run` protobuf（无状态桥接：历史展平进单条 prompt，`conversation_state` 留空），并把上游 `AgentServerMessage` 帧流翻译回 OpenAI 兼容响应。翻译 MUST 同时支持非流式聚合与 SSE 流式；接入位置 MUST 遵循既有平台不变量——在鉴权、body limit、模型解析之后，账号选择、并发 slot、计费预扣和上游拨号之前。

#### Scenario: 非流式对话
- **WHEN** 客户端以 `stream=false` 调用 `/v1/chat/completions` 且分组命中 Cursor 账号
- **THEN** 系统 MUST 把请求翻译为 Cursor protobuf 调用上游
- **THEN** 系统 MUST 把上游帧聚合为 OpenAI 兼容 `chat.completion` 响应

#### Scenario: 流式对话
- **WHEN** 客户端以 `stream=true` 调用
- **THEN** 系统 MUST 在收到上游首帧前不写响应首字节
- **THEN** 系统 MUST 把每个上游 delta 转为 OpenAI `chat.completion.chunk` SSE，并以 `data: [DONE]` 结束

#### Scenario: 上游错误映射
- **WHEN** 上游返回 Connect 错误帧或非 2xx 状态
- **THEN** 系统 MUST 映射为 OpenAI 兼容 error envelope 和稳定 HTTP 状态
- **THEN** 系统 MUST 记录凭据 / 上游失败以驱动既有停调策略

### Requirement: 网关必须原样透传工具调用且不得降智
系统 SHALL 把入站 `tools` 与 `tool_choice` 原样透传到 Cursor 上游（编码进 `supported_tools`），并支持单次响应内的并行工具调用。系统 MUST 把上游 `tool_call{id,name,raw_args,is_last}` 映射为 OpenAI `delta.tool_calls[]`，把 `thinking.text` 映射为 `delta.reasoning_content`。系统 MUST NOT 为适配而 strip `tools`、system prompt 或 `cache_control`。

#### Scenario: 透传工具定义
- **WHEN** 客户端在请求中提供 `tools` 与 `tool_choice`
- **THEN** 系统 MUST 将其编码后传给上游，不得丢弃或改写工具定义
- **THEN** system prompt MUST 映射为 `Instruction.text` 而不是被删除

#### Scenario: 单次响应内并行工具调用
- **WHEN** 上游在一次响应内产生多个 `tool_call`
- **THEN** 系统 MUST 将它们并列映射为 OpenAI `delta.tool_calls[]`（含各自 index）
- **THEN** 流式模式下 MUST 按到达顺序增量输出，直至 `is_last` 收口

### Requirement: 系统必须按已冻结规则分类处置上游错误
系统 SHALL 依据上游错误类型采取确定处置：`401` / `unauthenticated` MUST 先刷新 token、失败再切号；`resource_exhausted`（错误码 57，结束帧 JSON `isRetryable/isExpected=true`）MUST 视为瞬时容量问题——可重试或换模型、**不计费**、**不得据此封号**；明确“额度耗尽”文案 MUST 使账号进入冷却并切号；错误带模型名 MUST 从该账号可用集合剔除该模型。

#### Scenario: 遇到 resource_exhausted
- **WHEN** 上游返回 `resource_exhausted`（错误码 57）
- **THEN** 系统 MUST NOT 计费，且 MUST NOT 据此封禁账号
- **THEN** 系统 MUST 允许重试 / 换模型，并对该账号施加指数退避

#### Scenario: 认证失效
- **WHEN** 上游返回 `401` / `unauthenticated`
- **THEN** 系统 MUST 先尝试刷新 token
- **THEN** 刷新失败后 MUST 切换到其它可用账号

#### Scenario: 模型级错误
- **WHEN** 上游错误信息中带有具体模型名
- **THEN** 系统 MUST 从该账号的可用模型集合剔除该模型

### Requirement: 首期不实现 Cursor 有状态多 agent 能力
系统 SHALL 在首期只提供无状态 OpenAI 兼容 chat 网关（`/v1/chat/completions`、`/v1/models`、`tools` 透传）。系统 MUST NOT 在本能力内实现 Cursor 产品侧的有状态多 agent 能力（Cloud Agents、Agents Window 并行 agents、SDK agent runs、cloud subagents），因为它们有状态、长时、绑定 GitHub repo、产出 PR/diff、按官方 API pricing 计费，且只能用官方 `crsr_` Key 调 `api.cursor.com/v1/agents`（逆向订阅 JWT 无法访问）。

#### Scenario: 请求落在无状态 chat 网关
- **WHEN** 客户端通过本能力发起对话
- **THEN** 系统 MUST 以无状态方式经 `agent.v1.AgentService/Run` 完成单次请求-响应（`conversation_state` 留空，历史随请求自带）
- **THEN** 系统 MUST NOT 创建有状态 agent run、PR 或 artifact

#### Scenario: 后续增强候选保持解耦
- **WHEN** 后续确需支持 Cloud Agent
- **THEN** 实现 MUST 另建独立的 `/v1/agents` 风格直通通道，并要求用户提供官方 API Key
- **THEN** 该通道 MUST 与无状态 chat 路径解耦，且不在本变更范围内

### Requirement: 系统必须动态统计 Cursor 模型并合并进对外模型列表
系统 SHALL 在鉴权成功后调用上游 `AvailableModels` 统计模型列表，并写入 `account.Extra.cursor_observed_models`。系统 MUST 把 observed 模型合并进对外 `/v1/models`（`GetAvailableModels` 或 `Models` handler）以及 Admin 可用模型接口；对外列表 MUST NOT 只依赖硬编码默认模型清单。

#### Scenario: 鉴权后同步模型
- **WHEN** 一个 Cursor 账号完成导入 / 探测 / 刷新且鉴权成功
- **THEN** 系统 MUST best-effort（fire-and-forget、带 TTL 与 in-flight 去重）调用 `AvailableModels`
- **THEN** 系统 MUST 把模型 ID 列表写入 `account.Extra.cursor_observed_models`

#### Scenario: 对外模型列表包含 observed 模型
- **WHEN** 客户端请求 `GET /v1/models` 且分组命中 Cursor 账号
- **THEN** 返回列表 MUST 包含该账号 observed 模型（与 model_mapping 合并、去重、排序）
- **THEN** Admin 可用模型接口 MUST 返回与之一致的结果

#### Scenario: observed 为空的兜底
- **WHEN** 某 Cursor 账号尚未成功同步 observed 模型
- **THEN** 系统 MUST 不因缺失 observed 而使对外列表报错
- **THEN** 系统 MAY 返回既有 model_mapping 或极小兜底集合

### Requirement: Cursor 账号必须复用既有账号基础设施
系统 SHALL 复用既有账号存储、分组、调度、计费、粘性会话、限流封禁基础设施承载 Cursor 账号，而不是为 Cursor 另建一套。Cursor MUST 可加入 `AllowedQuotaPlatforms`；Cursor 缺乏干净的原生用量窗口，因此 MUST NOT 加入 `AllowedSchedulingThresholdPlatforms`。对话流**不保证**随流返回 usage（`TurnEnded` 用量帧偶发缺失），因此计费 MUST 在拿到非全零上游用量时按其口径计费、拿不到时回退本地 token 估算，且 MUST NOT 因缺失用量而按 0 计费；横向扩展 MUST 依赖多账号轮询与既有并发 / RPM 阈值，每账号建议并发 1–3。

#### Scenario: Cursor 账号参与调度
- **WHEN** 一个可调度的 Cursor 账号存在于某分组
- **THEN** 系统 MUST 像其它平台一样对其应用分组调度、粘性会话、并发 slot 和限流封禁

#### Scenario: 设置 Cursor 平台配额
- **WHEN** 管理员为某用户设置 `platform=cursor` 的配额
- **THEN** 系统 MUST 接受该配额（`AllowedQuotaPlatforms` 含 cursor，`user_platform_quota` schema 与迁移 CHECK 允许 cursor）

#### Scenario: 调度阈值白名单排除 Cursor
- **WHEN** 系统构建 `AllowedSchedulingThresholdPlatforms`
- **THEN** 该列表 MUST NOT 包含 `cursor`
- **THEN** Cursor 的用量对账 MAY 通过 dashboard 用量端点异步回填，而不是作为实时停调阈值

#### Scenario: 上游未返回用量帧时回退本地估算
- **WHEN** 一次 Cursor 对话完成但上游未随流返回 usage（无 `TurnEnded` 帧或其用量全为 0）
- **THEN** 系统 MUST 使用本地 token 估算计费
- **THEN** 系统 MAY 通过周期性读取 dashboard 用量端点回填对账

#### Scenario: 上游返回用量帧时按其口径计费
- **WHEN** 一次 Cursor 对话以带非零用量的 `TurnEnded` 帧结束
- **THEN** 系统 MUST 采用上游报告的 input / output / cache token 数，而不是本地估算

### Requirement: 数据库迁移必须把 cursor 加入所有平台 CHECK 约束
系统 SHALL 新增数据库迁移（编号取实施时最大序号递增，不修改已应用迁移），把 `cursor` 加入所有 `platform IN (...)` 与 `target_platform IN (...)` CHECK 约束，至少覆盖 `user_platform_quotas` 与 `composite_model_routes`。迁移 MUST 可从空库与当前生产前一版本应用，且 MUST NOT 改变既有表数据。

#### Scenario: 应用迁移后接受 cursor
- **WHEN** 迁移应用完成
- **THEN** 向 `user_platform_quotas` 写入 `platform='cursor'` MUST 成功
- **THEN** 向 `composite_model_routes` 写入 `target_platform='cursor'` MUST 成功

#### Scenario: 仍拒绝非法平台
- **WHEN** 写入一个不在允许集合中的平台值
- **THEN** CHECK 约束 MUST 拒绝该写入

### Requirement: 管理台必须支持创建与展示 Cursor 账号
控制台 SHALL 在账号与分组界面支持 Cursor 平台。前端类型联合 MUST 包含 `'cursor'`；账号创建 / 编辑表单 MUST 支持 Cursor 三来源凭据录入；平台图标、徽章、颜色 MUST 覆盖 Cursor；新增中英文 i18n 文案键 MUST 成对提供并通过现有 lint、typecheck 和 vitest。

#### Scenario: 创建 Cursor 账号
- **WHEN** 管理员在账号创建界面选择 Cursor 平台
- **THEN** 界面 MUST 提供 Cookie、User API Key 和深链登录三种录入入口
- **THEN** 提交成功后明文凭据 MUST NOT 保留在浏览器持久化状态

#### Scenario: 展示 Cursor 平台
- **WHEN** 管理员查看账号或分组列表中的 Cursor 项
- **THEN** 界面 MUST 显示 Cursor 平台图标、徽章和颜色
- **THEN** 中英文文案 MUST 成对存在
