# add-cursor-platform

新增 `PlatformCursor = "cursor"`，把用户的 Cursor 订阅账号作为上游接入 sub2api 平台体系，对外提供 OpenAI 兼容 API。与既有 JSON 平台不同，Cursor 上游走 Connect RPC over HTTP/2 + Protobuf，需新建独立协议包 `backend/internal/pkg/cursor/` 并在网关层做 OpenAI⇄Cursor 双向翻译；同时动态统计 Cursor 上游模型并合并进对外 `/v1/models`。基础设施（账号存储、分组、调度、计费、粘性会话、限流封禁）复用现有实现，参照最近接入的 Grok 平台模板。

阅读顺序：`proposal.md` → `design.md` → `specs/cursor-platform/spec.md` → `implementation-guide.md` → `tasks.md` → `verification.md`。

上游协议细节与多智能体范围结论均已冻结并回填：Connect 封帧、`AvailableModels` / `StreamUnifiedChat*` protobuf 字段号、`x-cursor-checksum` 与全部客户端标识头算法、深链 `/auth/poll`、错误分类、计费（首期本地 token 估算）与调度取舍（`cursor` 不入调度阈值白名单），`pkg/cursor` 按此实现。首期做无状态 OpenAI 兼容 chat 网关（chat + 动态 models + `tools` 透传）；Cursor 有状态多 agent 能力（Cloud Agents / Agents Window / SDK runs / cloud subagents）明确 out-of-scope。

对话上游后续从 api2 `ChatService/StreamUnifiedChatWithTools`（已被 Cursor 下线，回 "Update Required"）切到 api5 `agent.v1.AgentService/Run`（HTTP/2 双向流、限速分帧、10 个 CLI 头、无 checksum）；`/v1/models` 仍走 api2 `AvailableModels`。对话 host 已核定为 `agentn.global.api5.cursor.sh`，原 ghost 模式 host 注记关闭。

仅保留两项二期 TODO：多模态图片输入字段号、`ClientSideToolV2` 内置工具枚举完整对照表。
