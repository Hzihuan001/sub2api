# API Key 主备分组选组实现与维护

## 当前状态

2026-10-15，本功能与分组显式默认值已随 `.7` 部署主站/L1/COS，三站最终 healthy/restart 0。运行提交 `f36de3d4a00400c66994a02c13c5d23f6a7feac8` / `custom-0.2.16.7`，发布及集成分支已推 `3ce7de3ebe68263a3f0e8bf6a83393486747306f`，后续文档单独同步；GHCR digest `sha256:ec8860583ac3603fadc108e90b2036abf352d395b5035ee8f4cb51d1de66ac42` 本地/OVH 回拉一致。活动工作树、分支和开发基线仍分别为 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom`、`codex/group-failover-rules-v0.2.16`、`3b0145c8abbf57d581f99084e88118ea7e759e3f`。精确部署和证据见 [发布报告](CUSTOM_RELEASE_0_2_16_CUSTOM7_20261015.md)。

本轮没有合并新官方版本或运行付费模型请求；下方本地开发测试保留原始范围，不因发布自动扩大为生产测试。

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

旧程序忽略扩展表，回退后只能使用主组；扩展表保留，不删除数据或还原旧库覆盖新业务。独立 b 环境已验证 `.6` 主组读取/账目和扩展行保留、恢复 `.7`。**旧镜像期间不得编辑多组 Key 的主组或解绑主组**，旧逻辑不能维护备用组组合，恢复新版后应复核；只读回退不代表全部旧版操作/模型请求组合通过。本次生产未触发回滚。

## 本地验证

使用 portable Go 1.27.2、现有 Node 22.13.0 / pnpm 10.33.0，未更新依赖或锁文件。成功结果按未变源码复用。

- 后端定向回归：7 个包，441 个顶层 / 776 个含子测试，0 失败、0 skip。包括旧 Key 认证、纯策略、存储、组权限、模型映射、缓存和真实已注册 HTTP/SSE 路由。原始输出 `D:/projects/go-tmp/key-routing-final-20261015.jsonl`；随后新增的 Claude 专用组保护单独复验。
- 真实 PostgreSQL/Redis Testcontainers：APIKeyRepoSuite 33 子例、既有利润控制列兼容、扩展触发器持久化失效通过。只使用合成数据库，临时容器由测试回收。
- 已挂载网关链的 5 个场景：主组模型不匹配、主组冷却后的 SSE、备用组倍率与用户专属倍率、全部冷却零上游零账单、部分 SSE 中断不跨组重播。实际进入 RegisterGatewayRoutes、生产认证 provider、handler、RecordUsage 和 UsageBilling.Apply；计费存储使用捕获命令的替身，不能等同完整生产数据库结算验收。
- 前端原相关 5 文件 57 项、typecheck、受影响 ESLint 通过；新增 Claude 专用组校验后，编辑器和真实消息宿主 17 项、typecheck、scoped lint 再次通过。最终生产构建成功（1148 模块，43.98 秒），保留 Browserslist 数据较旧、动静态导入混用与大 chunk 提醒，未为此升级依赖。
- Wire 实际生成成功；架构检查 7 模块 / 10 测试组通过，架构单元最终 18 项与未变升级脚本 16 项通过；`git diff --check` 通过。最初测试筛选未包含纯 Planner 名称，已补 `^TestPlanner` 并在最终回归实际执行。
- 审查过程中修复了 Messages/渠道模型映射预检、歧义 JSON、资源引用、大小写意图和 Claude 专用组边界；本轮未进行 SSE 性能压测，也没有把既有性能风险改写为通过。

## 正式镜像与发布补充

- 独立 b 环境 `D:/projects/go-tmp/key-routing-docker-custom7-20261015b/evidence.json` 为 `passed`：7 请求、5 成功上游/5 usage/5 dedup，余额 `10→9.99998919`、同 Key quota `0.00001081`。实际组倍率 0.5/0.75/0.25/2、SSE、全部无资源 503 和图片主组 403 零调用零扣费、迁移重启、`.6` 只读回退及恢复 `.7` 通过。首轮 a 夹具透传配置与白名单预期冲突的失败保留，修夹具后 b 通过，运行源码未改。
- 精确候选的角色/JWT/API Key/WebSocket/重启/日志验收 exit 0，日志 `D:/projects/go-tmp/custom7-role-acceptance-20261015.log`。本地浏览器同镜像 a 环境检查 Keys 新增/编辑入口、重排草稿、计费/范围提示，Groups 四类默认值及清空首个次数恢复 `3`，console error `[]`；未保存草稿，未访问生产，持久化证据来自 Docker/API。
- 三站部署通过且旧迁移保持、仅新增上述迁移；生产没有现成 admin API Key，未执行生产角色登录、付费模型测试或长期监控。Actions 对运行/分支提交均无运行，原 SSE mixed、15 lint 基线、P4 双跑矩阵、真实升级 PR 链路继续保留。本轮 a/b 容器已停止并保留卷/证据，角色项目自动回收，临时浏览器代理及空网络已删除；本地浏览器入口不再保持可访问。

## 生产管理员实测

2026-10-15 北京时间 22:35–22:40，用户自行创建管理员 Key `codex-routing-admin-20261015`（ID 167），配置 GPT-稳定（7）→Kimi（30）→DeepSeek（33）。在 US$1 授权范围内，通过主站公网 HTTPS 完成一次目录查询和四次短文本请求；客户端不自动重试，每次最多 64 输出 token。以下结果补充上节发布时尚无生产测试的历史状态。

| 请求 | 结果 | 实际组 | 实际费用 | 使用记录 |
| --- | --- | --- | --- | --- |
| GET `/v1/models` | 200，11 个主组模型，无 Kimi/DeepSeek | 主组目录 | 0 | 无 |
| `gpt-5.6-luna` 普通文本 | 502，主组上游连续失败 | 7 | 0 | 无 |
| `kimi-k3` 普通文本 | 200，4.699 秒 | 30 | $0.000664 | 201277 |
| `deepseek-v4.1-flash` 普通文本 | 200，1.733 秒 | 33 | $0.000019 | 201278 |
| `deepseek-v4.1-flash` SSE | 200，2.070 秒，完整 `[DONE]`，无错误事件 | 33 | $0.0000318 | 201282 |

三条成功请求均按实际分组倍率 0.2 计费，合计 **$0.0007148**；三条唯一 usage 与三条唯一 dedup 对应，Key quota_used 与费用相等。主备顺序和主组字段保持。目录只返回主组是当前实现范围，不代表备用模型不能使用；自动拉取模型的客户端暂时看不到备用模型，需要手动填写 ID。

GPT 失败的应用请求 ID 为 `1c2ed278-5bc5-4ee3-a08d-940d414d9a0c`。22:35:59.582 开始选择主组；账号 31 的映射模型 `gpt-5.6-luna-openai-compact` 返回 404，账号 57 返回 502，账号 32 返回 403。三次组内切换后，剩余一个账号不支持模型，22:36:02.099 返回 502。应用与 OpenResty 访问日志一致，窗口内没有反代错误或应用 panic，应用/反代重启数均为 0。客户端纯文本 502 不能据此认定为入站 Cloudflare 故障；已确认的失败链在主组上游。

本轮没有手动停用或恢复账号，也没有更改模型映射、组配置或 Key 配额；失败请求触发原有自动模型冷却/账号临时停止规则。Kimi、DeepSeek 备用组并不支持所测 GPT 模型，且当前设计不跨组重播已发出的请求。本轮未生产演练所有账号暂停、长时间 SSE 或负载，不以三条成功请求宣称全场景通过。

后续事项：检查 GPT 主组该模型映射和上游可用性；如需客户端自动发现所有备用模型，单独扩展授权过滤和去重后的模型目录。透传/空映射账号可能宣称支持全部模型，需结合分组白名单保持真实路由边界；本次三个组使用明确账号映射，Kimi/DeepSeek 路由已实证。用户保留测试 Key 自行删除，助手不代删。

脱敏证据：`D:/projects/go-tmp/key-routing-admin-test-20261015/{models,gpt,kimi,deepseek,deepseek-sse}.json`，服务器同名目录位于 `/root/` 下；证据不包含明文 Key。前端范围提示删除及 21 项回归结果保持本地，未推送/部署；运行镜像仍为 `.7`。
