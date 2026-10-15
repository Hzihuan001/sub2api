# API Key 主备分组选组实现与维护

## 当前状态

2026-10-15，本地实现 API Key 按模型和账号可调度状态选择主备分组。代码位于活动工作树 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom`，分支 `codex/group-failover-rules-v0.2.16`，开发基线 `3b0145c8abbf57d581f99084e88118ea7e759e3f`。本功能尚未提交、推送或部署，生产仍是 `0.2.16-custom.6`。

同时保留前一任务未发布的分组规则编辑器默认值改动；本轮没有合并新官方版本、修改生产配置或运行付费模型请求。

## 用户行为和计费

- 一个主分组加最多四个有序备用分组。用户在 API 密钥完整创建/编辑窗口增删和上下移动备用分组。
- 每个请求从主分组开始，按优先级选第一个支持请求模型、协议且存在可调度账号的分组。主组没有模型，或者分组内账号因已有规则全部暂停时，可以选备用组。
- 暂时没有并发空位不等于不可调度，仍交给原调度器排队。Key 不新增 TTFT、SLA、错误累计或冷却规则，账号暂停和手动恢复沿用现有功能。
- 以最终选中分组的价格、用户专属倍率及原有计费规则结算。Key ID、总额度和使用计数共享，不创建替身 Key，不复制计费系统。
- 只在发送上游前选组。选中后账号重试仍由原分组内逻辑处理；不把任意上游错误变成跨组重播，已经输出的 SSE 内容更不能重发。
- 主组恢复后，新请求重新优先使用主组，不持久化“当前备用组”。选组只读取当时的可调度快照，若之后账号状态变化，最终调度器仍可拒绝请求；本功能不保证消除这种竞争窗口。

## 首期兼容范围

多分组仅支持用户已获授权的 active、standard 按量分组。单分组 Key 的原有行为不变。权限失效、存储读取失败、配置损坏明确报错，不作为理由绕过权限或悄悄更换收费组。

主备选组入口为 POST `/v1/messages`、`/v1/chat/completions`、`/chat/completions`、`/v1/responses`、`/responses`、`/backend-api/codex/responses` 的无资源绑定文本 HTTP/SSE 请求。模型白名单按公共请求模型校验；复用原有 composite 解析、Messages dispatch 和 Responses/Chat 渠道映射来判断账号支持情况。

- 图片、批量图片、WebSocket、强制平台路径、元数据和资源接口仍只走主组；`/v1/models` 本轮不合并备用组目录。已知备用模型 ID 可以直接发起受支持的文本请求。
- 已存储 response/conversation、file ID、vector store、既有 container 等引用不能安全跨账号迁移，多分组 Key 明确拒绝，使用单组 Key 保持原行为。图片或压缩意图保持主组处理，包含大小写变体保护；不扫描用户工具 schema 中同名字段。
- `ClaudeCodeOnly` 分组暂不允许加入多分组组合。它的内部 fallback 依赖稍后在 handler 建立的客户端身份，认证阶段不能准确复用；前后端配置校验和运行时均保护。普通单分组 Claude 客户端用法不变。
- 管理员用户详情里的快捷改组不编辑多组顺序，已有多组 Key 提示使用完整 API 密钥编辑入口；后端管理接口已支持受所属用户权限约束的 `backup_group_ids`。

## 模块边界与升级接线

新增 feature ID 为 `key-routing`，已登记 manifest、features、contracts 与前端消息贡献。

| 层 | 主要位置 | 职责 |
| --- | --- | --- |
| 纯选组策略 | `backend/internal/keyrouting` | 顺序决策、结果原因及错误分类；仅标准库依赖 |
| 宿主适配 | `service/key_routing_service.go`、`key_routing_readiness.go` | 复用权限、模型映射、现有资格判定和快照，不抢占槽位、不发请求 |
| 请求入口 | `middleware/key_routing.go`、`key_routing_json.go` | 一次解析、边界拒绝、返回原 body，放置请求级有效分组 |
| Key 扩展存储 | `service/custom_api_key_routes.go`、`repository/custom_api_key_routes.go` | 配置校验、原事务内扩展读写及失效 |
| 前端组件 | `frontend/src/features/key-routing` | 有序备用组编辑器、校验、独立中英文消息 |

宿主改动限于 Key DTO/缓存字段、CRUD/批量换组的扩展调用、认证入口一次 `selectAPIKeyGroupRoute`、Wire provider 和 KeysView 组件入口。原生/兼容 handler 与收费逻辑没有新增跨组重试循环。

`custom/contracts.json` 包含纯策略禁反向依赖、前端功能禁依赖宿主页、认证入口调用次数检查及两个真实测试组。升级时重点复核：

1. API Key 所有读取路径都加载备用 ID；缓存版本与深拷贝不能丢失。
2. 认证先验证身份/IP/用户，再选组，之后以有效组执行原权限、额度、白名单和平台分发。
3. 原调度器新增模型归一化或资格判定时，同步调整只读 readiness 适配，不复制整个调度器。
4. 原子结算、日志和倍率读取仍接收有效 APIKey.Group，而非持久化主组。
5. Wire 必须用 `go generate ./cmd/server` 生成，不能手改 `wire_gen.go`。

## 数据与回退

新增迁移 `backend/migrations/9000_custom_api_key_routes.sql`，保留 `api_keys.group_id` 为主组，仅扩展表记录 Key、顺序及备用组。主组和备用组写入使用同一事务；没有改动已执行的历史迁移、用户余额或旧使用记录。

更新 API 的 `backup_group_ids`：省略/null 保留，空数组清空，非空数组替换。权限校验包含主备全部分组，配置不自动授予用户额外权限。数据库触发器处理 Key 软删除、备用组变更和持久化鉴权失效，事件不保存明文 Key。缓存版本提升到 25，跨实例失效沿用原 outbox。

旧程序忽略扩展表，回退后只能使用主组；新建表可以保留，无需为此删除数据。旧程序期间若编辑主组，不会维护备用组配置，重新升级时应复核组合。尚未执行本功能完整新旧应用容器回退演练，发布前不得把此兼容设计当成已验收结果。

## 本地验证

使用 portable Go 1.27.2、现有 Node 22.13.0 / pnpm 10.33.0，未更新依赖或锁文件。成功结果按未变源码复用。

- 后端定向回归：7 个包，441 个顶层 / 776 个含子测试，0 失败、0 skip。包括旧 Key 认证、纯策略、存储、组权限、模型映射、缓存和真实已注册 HTTP/SSE 路由。原始输出 `D:/projects/go-tmp/key-routing-final-20261015.jsonl`；随后新增的 Claude 专用组保护单独复验。
- 真实 PostgreSQL/Redis Testcontainers：APIKeyRepoSuite 33 子例、既有利润控制列兼容、扩展触发器持久化失效通过。只使用合成数据库，临时容器由测试回收。
- 已挂载网关链的 5 个场景：主组模型不匹配、主组冷却后的 SSE、备用组倍率与用户专属倍率、全部冷却零上游零账单、部分 SSE 中断不跨组重播。实际进入 RegisterGatewayRoutes、生产认证 provider、handler、RecordUsage 和 UsageBilling.Apply；计费存储使用捕获命令的替身，不能等同完整生产数据库结算验收。
- 前端原相关 5 文件 57 项、typecheck、受影响 ESLint 通过；新增 Claude 专用组校验后，编辑器和真实消息宿主 17 项、typecheck、scoped lint 再次通过。最终生产构建成功（1148 模块，43.98 秒），保留 Browserslist 数据较旧、动静态导入混用与大 chunk 提醒，未为此升级依赖。
- Wire 实际生成成功；架构检查 7 模块 / 10 测试组通过，架构单元最终 18 项与未变升级脚本 16 项通过；`git diff --check` 通过。最初测试筛选未包含纯 Planner 名称，已补 `^TestPlanner` 并在最终回归实际执行。
- 审查过程中修复了 Messages/渠道模型映射预检、歧义 JSON、资源引用、大小写意图和 Claude 专用组边界；本轮未进行 SSE 性能压测，也没有把既有性能风险改写为通过。

下一步是当前候选的独立 Docker 应用验收及浏览器人工检查，特别是迁移后重启、完整 Key 创建/编辑、跨平台文本路由和回退。通过并获发布授权后再提交推送、构建镜像和三站发布；当前本地实现结果不代表已经上线。
