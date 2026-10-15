# Moshu 0.2.16-custom.7 发布记录

日期：2026-10-15，Asia/Shanghai。`0.2.16-custom.7` 已完成源码/标签推送、GHCR 发布、本地和 OVH 按不可变 digest 回拉核验，以及主站 → L1 → COS 顺序部署；三站逐站检查全部 passed、部署命令 exit 0。精确镜像的本地 Docker 账务/生命周期、角色和限定范围浏览器证据如下，生产验证范围单列。首轮夹具失败与本机 Docker 引擎意外退出均保留记录；引擎已恢复，未因此重建发布产物。

## 范围与授权

- 本轮包含 API Key 主备分组选组和分组规则默认值编辑器；不合并新的官方版本，不升级依赖或锁文件。
- 主备 Key 使用同一个 Key ID、总额度和使用计数，按最终选中分组及用户专属倍率计费；最多 1 主加 4 个有序备用组。每个新请求优先主组，只在发送上游前选组，已发送请求不跨组重播。临时并发占满继续沿用原排队，不新增 Key 级 TTFT、SLA、失败累计或冷却规则。
- 首期限已授权、active、standard 按量分组的无资源绑定文本 HTTP/SSE。图片、批量图片、WebSocket、强制平台路径、资源和元数据接口仍走主组；模型目录不合并备用组。多组组合排除 `ClaudeCodeOnly`，已有 response/conversation、file、vector store、container 等绑定状态引用明确拒绝；单组原行为保留。详细入口与边界见 [实现与维护记录](CUSTOM_KEY_ROUTING_20261015.md)。
- 分组规则移除“沿用系统 / 自定义”选择，四类参数直接预填并可编辑。已有值保留，空白补默认，非法非空内容阻止保存；保存才显式提交四类规则，只打开页面不修改其他分组或后端调度。
- 已按用户授权完成本地构建、候选验收、GHCR、按 digest 回拉核验及主站 → L1 → COS 顺序 app-only 部署。每站通过后再进入下一站；主站和 L1 等待在途槽位归零后才更新，没有强制中断请求。

## 发布坐标

| 项目 | 值 / 当前状态 |
| --- | --- |
| 官方基线 | `v0.2.16` / `01842576b3d5c1e2545b5a99ad555d36e28ed306` |
| 当前生产版本 | 三站统一 `0.2.16-custom.7` |
| 源码分支 | `codex/group-failover-rules-v0.2.16` |
| 运行代码提交 | `f36de3d4a00400c66994a02c13c5d23f6a7feac8` |
| 发布前已推送分支坐标 | `codex/group-failover-rules-v0.2.16` 与 `custom/integration` 已 atomic 推送至 `3ce7de3ebe68263a3f0e8bf6a83393486747306f`；后续文档提交不改变镜像 |
| 源码标签 | `custom-0.2.16.7` 已推送，远端标签指向运行提交 `f36de3d4a00400c66994a02c13c5d23f6a7feac8` |
| 已发布镜像 | `ghcr.io/hzihuan001/sub2api:0.2.16-custom.7` |
| 本地 image ID | `sha256:ec8860583ac3603fadc108e90b2036abf352d395b5035ee8f4cb51d1de66ac42` |
| GHCR / 三站不可变 digest | `sha256:ec8860583ac3603fadc108e90b2036abf352d395b5035ee8f4cb51d1de66ac42` |
| 架构 / 构建时间 | `linux/amd64` / `2026-10-15T13:15:08Z` |
| 回滚版本 / 标签 | `0.2.16-custom.6` / `custom-0.2.16.6` |
| 回滚提交 | `655a16868fc310d6dcc742c4bb60a2f740456a58` |
| 回滚 digest | `sha256:cb074f11766d9190f4e5f3479a0750c7a759a562bfc142aa3067a0d4bfbfc42e` |

标签固定在本次验收的运行提交；后续测试及文档提交不改变该镜像。分支与标签已远端核验，GHCR push 日志记录上述 digest，本地及 OVH 按 digest 拉取后核对 image ID、版本和完整 commit 均通过。远端文档收尾 ref 以最终 Git 核验为准，不移动运行标签。

## 数据迁移与兼容边界

本次新增 `backend/migrations/9000_custom_api_key_routes.sql`，其 trim 后 SHA-256 为 `468f453bd5381c9a6a336371d893b1fc3a3396035f1ced20702825020fc17d2f`。保留 `api_keys.group_id` 为主组，新增 `custom_api_key_routes` 表保存备用组顺序，并增加清理和鉴权缓存持久化失效触发器；不改已执行的历史迁移、旧使用记录或用户余额。

主组与备用组写入使用原事务，缓存版本为 25，跨实例失效沿用现有 outbox。API 的 `backup_group_ids` 省略或 null 保留，空数组清空，非空数组替换；配置不授予用户额外分组权限。

三站本次均仅新增 `9000_custom_api_key_routes.sql`，checksum 与上述值一致，旧迁移记录全行保持不变。新增迁移意味着总数和整体指纹发生预期变化，不能沿用 `.6` 的“迁移总数和全行指纹完全不变”判据。本地独立应用另已完成迁移 checksum 核对、重启、旧 `.6` 镜像读取与恢复 `.7`，具体范围见下方。

## 已有本地验证

以下为当前功能的源码测试证据，详细范围和限制见 [实现与维护记录](CUSTOM_KEY_ROUTING_20261015.md)。正式镜像检查另列于下方；源码测试不能自动外推为生产执行了相同测试。

- 后端定向回归 7 个包、441 个顶层 / 776 个含子测试，0 失败、0 skip；原始输出为 `D:/projects/go-tmp/key-routing-final-20261015.jsonl`。随后新增的 Claude 专用组运行时和 CRUD 保护分别定向复验通过。
- 真实 PostgreSQL/Redis Testcontainers 的 APIKeyRepoSuite 33 子例、既有利润控制列兼容和扩展触发器持久化失效通过；只使用合成数据库，临时容器由测试回收。
- 已注册网关链 5 个场景覆盖主组模型不匹配、冷却后的 SSE、实际组及用户专属倍率、全部冷却零上游零账单、部分 SSE 中断不跨组重播。请求实际进入生产路由、认证 provider、handler、RecordUsage 与 UsageBilling.Apply；计费存储为捕获命令的替身，不能等同完整应用容器中的真实数据库结算验收。
- 前端初版相关 5 文件 57 项、typecheck 和受影响 ESLint 通过；Claude 专用组限制加入后，编辑器及真实消息宿主 17 项、typecheck、scoped lint 再次通过。最终生产构建成功，1148 模块、43.98 秒；保留 Browserslist 数据较旧、混合动态/静态导入和大 chunk 提醒。
- 分组默认值编辑器 43 项、真实分组页 16 项及其他相关 5 文件 27 项通过；类型检查、受影响 ESLint、构建与 diff check 已通过。上述检查的范围和工具链按原记录复用，不累加为本次重跑全部套件。
- Wire 实际生成成功；架构登记为 7 模块 / 10 测试组，架构单元 18 项、未变升级脚本 16 项通过。测试筛选已包含 `^TestPlanner`，纯策略在最终回归中实际执行。
- 本轮本地工具为 portable Go 1.27.2、Node 22.13.0、pnpm 10.33.0；未改依赖或锁文件，不冒称 `.6` 使用的 Node 20 / pnpm 9 环境。

## 镜像构建与 Docker 验收阶段

- 精确源码 `f36de3d4a00400c66994a02c13c5d23f6a7feac8` 的本地镜像构建已退出 0，image ID、amd64 架构和构建时间如上。构建日志为 `D:/projects/go-tmp/custom7-release-build-20261015.log`；同一产物已推送 GHCR，本地与 OVH 按不可变 digest 拉取及版本/提交核验通过，没有在生产重建镜像。
- 第一轮独立环境 a 的首个请求命中了主组，夹具却期待因模型不支持而切换备用，断言因此失败。原因是夹具把主组设为 `openai_passthrough=true`，原生透传路径不按该 mapping 排除模型；这次主组选路符合配置，不能把夹具失败改写为应用选组失败，也不能隐去首次未通过结果。
- 夹具改为主组非透传并使用模型白名单后，新建独立环境 `sub2api-operator-test-key-routing-20261015b`，在 UTC 13:27:03–13:33:32 完成固定 7 次合成请求，`evidence.json` 的最终状态为 `passed`。没有修改 `.7` 运行源码；验收不访问外网或付费模型，测试资源和证据保留。
- b 环境实际使用上述候选 image ID 与运行提交，真实 PostgreSQL 账目最终为余额 `10 → 9.99998919`、同一 Key 的 `quota_used=0.00001081`，共 5 次成功上游、5 条 usage 和 5 条 dedup。请求范围与结果如下，不能外推为所有平台/协议组合都已运行。

| 合成场景 | 实际结果 |
| --- | --- |
| 主组不支持模型 | 选 backup，倍率 0.5，HTTP 200 |
| 编辑备用顺序 | 选 third，倍率 0.75，HTTP 200 |
| 主组账号全部冷却后的 SSE | 选 backup，倍率 0.5，HTTP 200 |
| 选中分组的用户专属倍率 | 选 backup，倍率 0.25，HTTP 200 |
| 主组恢复 | 重新选 primary，倍率 2，HTTP 200 |
| 全部分组不可用 | HTTP 503，零上游、零新增账单 |
| 图片保持主组路径 | HTTP 403，零上游、零新增账单；未借备用组绕过主组限制 |

- 同一隔离数据库完成应用重启、回退 `.6` 和恢复 `.7`。重启后扩展配置与倍率保留；旧镜像可读取 Key 的主组，账目与扩展行保持；恢复候选后运行版本/提交正确。迁移 checksum 为上述 `468f453b…fc17d2f`。旧镜像阶段验证的是读取和持久化兼容，没有在旧程序中编辑多组 Key 主组/解绑，也不据此声称所有回退后的模型请求组合均验证。
- 独立角色项目在端口 `18119` 对同一本地候选完成角色/JWT/Admin API Key/WebSocket、两次应用重启及日志检查，脚本 exit 0。`custom7-role-acceptance-20261015.log` 记录 acceptance passed，脚本随后回收该专属角色项目的容器、卷和网络；不删除保留的 Key 路由验收项目。

## 本地浏览器检查

浏览器使用 a 环境中的同一候选 image，经当时的专属 loopback 代理 `127.0.0.1:18109` 检查真实页面。该临时代理现已删除，不是当前可访问入口。检查范围：

- 用户 API Keys 页显示主组加两个备用组，检查了创建入口和首期限制说明；编辑窗口的上下重排草稿正确，本次浏览器重排未保存。
- 管理员 Groups 页四类默认值分别为 `3/300/600`、`3/60/60`、`2/60/60`、`3/60/60`；清空首个次数后失焦恢复为 `3`，关闭窗口且未保存。
- 浏览器 console error 为 `[]`。工具中保留了 AX 和截图观察，未另外保存截图文件，因此不列虚构的截图路径。

以上不是生产浏览器检查，不包含付费模型调用；浏览器未保存的草稿不作为数据库写入成功证据，持久化创建/编辑的证据来自独立 Docker/API 验收。

本轮 a/b 所有测试容器已按精确 Compose 标签停止，停机命令 exit 0；合成卷及 evidence 保留，不影响其他旧测试项目。需要本地复现时再启动并核对完整环境，不将验收时的 healthy 状态当作当前运行状态。浏览器临时代理容器及空独立网络已回收，`18109` 入口现在不可访问。

## 验收与发布状态

| 验收项 | 当前状态 |
| --- | --- |
| 精确运行提交的本地镜像构建及元数据 | 已完成，构建 exit 0；本地坐标见上表 |
| 独立 Docker 应用：Key 创建/编辑、文本路由、真实账目和额度 | b 环境固定 7 次验收通过；a 环境首次夹具失败保留 |
| 新迁移、应用重启、`.6` 回退及重新升级 | 本地通过；旧镜像验证主组读取及账目/扩展行保留，不含旧版多组改组操作 |
| 本地候选角色/API Key/WebSocket/重启/日志 | 同一发布产物的独立项目通过并自动回收；未冒称发布后又重跑一遍 |
| 本地浏览器 Keys/Groups 入口及草稿交互 | 已完成上述限定检查；重排和默认值草稿未保存 |
| 源码分支、集成分支、正式标签 | 已推送并核验，运行标签保持 f36，发布前分支坐标为 3ce7de3eb |
| GHCR 发布及不可变 digest | 已推送并核验，为上表 ec886… digest |
| GHCR 按 digest 回拉同一产物 | 本地与 OVH 拉取、image ID/版本/完整 commit 核验通过；角色矩阵复用同一产物的本地已完成结果 |
| 主站 / L1 / COS 顺序部署与逐站核验 | 全部 passed、exit 0，具体时间及边界见下节 |

Docker Desktop 本地引擎在上述验收后曾意外退出，现已恢复。恢复后使用原镜像完成 GHCR 推送和回拉核验；未因引擎恢复重新构建或更换已验收产物。保留原始验收结果和首次夹具失败，不将后续成功倒写为初次全绿。

## 生产备份

备份目录：`/opt/sub2api-backups/v0.2.16-custom.7-20261015T131751Z`。

`D:/projects/go-tmp/custom7-production-backup-20261015.log` 已记录三站 PostgreSQL dump、应用/配置归档、SHA-256、原应用容器和 Compose 路径，最终结果为 `verified=true`、`sites=3`。各站 `pg_restore --list` 为 passed，归档已核验可读。备份是在 `.6` 生产状态下创建，不代表执行过完整恢复演练；凭据及归档内容不写入仓库。

| 站点 | 数据库 dump 字节 | 应用/配置归档字节 | 归档条目数 | 备份验证 |
| --- | ---: | ---: | ---: | --- |
| 主站 | 96,117,665 | 170,911,566 | 32 | passed |
| L1 | 11,536,840 | 10,706,289 | 14 | passed |
| COS | 3,076,841 | 2,960,822 | 12 | passed |

三站记录的旧镜像均为上述 `.6` digest。备份后若现场业务或配置变化影响发布恢复条件，应在对应站点更新前复核备份时点及在途工作，不能把先前备份等同更新前无条件可用的业务快照。

## 逐站部署结果

三站严格按主站 → L1 → COS 顺序更新，每站检查通过后才进入下一站。主站和 L1 遇到在途请求时等待请求槽位归零才操作，未强制中断；COS 更新前批量在途、请求槽位和等待槽位均为 0。

| 站点 | 北京时间（2026-10-15） | 应用容器 / Compose 服务 | `.7` 结果 |
| --- | --- | --- | --- |
| `ai.moshu.cloud` | 21:42:45–21:42:58 | `1Panel-sub2api-bjGj` / `sub2api` | passed / exit 0 |
| `api.moshu.cloud` | 21:44:21–21:44:28 | `api-L1` / `api` | passed / exit 0 |
| `cos.moshu.cloud` | 21:45:01–21:45:12 | `api-COS` / `api` | passed / exit 0 |

三站均仅替换应用镜像并重建对应服务，使用同一 GHCR digest。逐站结果确认：

- 应用 healthy、重启计数 0，版本 `0.2.16-custom.7`、完整运行提交及 digest 与上表一致。
- 环境/端口/挂载/网络等配置指纹保持不变；PostgreSQL、Redis 容器 ID 和 StartedAt 不变，没有重建数据服务、卷或网络。
- 旧迁移全行记录不变，仅新增 `9000_custom_api_key_routes.sql`，checksum 为 `468f453bd5381c9a6a336371d893b1fc3a3396035f1ced20702825020fc17d2f`。
- 本地及公网 `/health`、`/image-studio` 均 HTTP 200；匿名访问 `/api/v1/admin/groups/1/failure-policy` 和 `/api/v1/admin/settings` 均 HTTP 401。
- 关键启动日志匹配 0。完整逐站结果另外保存在上述备份目录的 `main-release-result.json`、`l1-release-result.json`、`cos-release-result.json`。

收尾再次通过 Docker inspect 核对三站，仍为同一 `ec886…` digest、healthy、重启计数 0。

生产没有现成的 Admin API Key，因此未执行新一轮有鉴权角色登录，本轮也未新建生产管理密钥。完整角色/JWT/Admin API Key/WebSocket 验收来自同一发布产物的本地独立项目。生产没有付费模型或生图调用，没有生产浏览器登录检查；本次为逐站发布后的快速检查，不是 30 分钟、24 小时或长期稳定性监控结论。

## 回滚方案与限制

回滚目标为 `.6` digest `sha256:cb074f11766d9190f4e5f3479a0750c7a759a562bfc142aa3067a0d4bfbfc42e`。旧程序忽略备用组扩展表，回滚后请求只走主组；保留扩展表和数据，不删除新增表、不恢复旧数据库覆盖新业务。

**旧镜像运行期间，不得编辑多组 Key 的主组或解绑主组。** 旧程序不能维护备用组配置与新主组的组合一致性，旧版管理页面或 API 的这类改组操作应暂停。重新升级后必须复核主备组合；不能把旧程序“只用主组”误解为可以安全编辑多组配置。

以下是生产应用单服务回滚命令；本轮已完成的是隔离环境旧镜像读取/持久化回退及恢复候选，生产尚未执行回滚。只在需要回滚的站点执行对应一组：

```sh
# 先将对应 docker-compose.yml 中应用 image 恢复为上方 .6 digest。
# 主站
cd /opt/1panel/apps/sub2api/sub2api
docker compose config --quiet
docker compose up -d --no-deps sub2api

# L1
cd /opt/api-L1
docker compose config --quiet
docker compose up -d --no-deps api

# COS
cd /opt/api-COS
docker compose config --quiet
docker compose up -d --no-deps api
```

生产回滚后仍需核对应用健康、主组请求及配置/数据服务状态；不能把本地有限回退演练直接写成生产回滚已验证。旧镜像期间禁止多组 Key 改组/解绑的限制继续有效。

## 保留限制与证据索引

- 原 SSE INFO/CBBC mixed 仍为已接受的未决风险，本轮不追加性能压测，不改判为通过。15 项全量 lint 基线欠账、P4 完整旧新账务双跑矩阵和真实自动升级 PR 链路仍未闭环。
- Actions 上一轮实际 dispatch 为 HTTP 422 disabled；本轮按运行提交 `f36de3d4a` 和发布前收尾提交 `3ce7de3eb` 查询运行均返回 `[]`，没有对应 CI 运行。此次本地镜像发布和三站成功部署不作为远端 CI 成功证明。
- 无上游查询能力的未知图片结果继续待核对并保留预留金额，不自动重发、扣款或退款，不新增管理员确认入口。
- 本地测试事实与功能边界：[API Key 实现与维护记录](CUSTOM_KEY_ROUTING_20261015.md)、[项目状态](CUSTOM_PROJECT_STATUS.md)、[功能日志](CUSTOM_FEATURE_LOG.md)。
- 已有原始证据：`D:/projects/go-tmp/key-routing-final-20261015.jsonl`、`D:/projects/go-tmp/custom7-production-backup-20261015.log`。
- 本地镜像构建日志：`D:/projects/go-tmp/custom7-release-build-20261015.log`，构建 exit 0。
- 独立 Docker 账务与生命周期证据：`D:/projects/go-tmp/key-routing-docker-custom7-20261015b/evidence.json`，status passed；夹具 SHA-256 为 `f463c93ed29284882e42ff899c3b1d7676c5b1c818a4258461fceaa1bcad5c53`。
- 本地候选角色验收日志：`D:/projects/go-tmp/custom7-role-acceptance-20261015.log`，端口 18119，exit 0。
- 本地浏览器 AX/截图在本轮工具记录中，未保存独立截图文件。
- GHCR 推送日志：`D:/projects/go-tmp/custom7-ghcr-push-20261015.log`；OVH 不可变拉取日志：`D:/projects/go-tmp/custom7-ovh-pull-20261015.log`。本地拉取及版本/commit 核验结果在本轮工具记录中。
- 三站部署日志：`D:/projects/go-tmp/custom7-deploy-main-20261015.log`、`D:/projects/go-tmp/custom7-deploy-l1-20261015.log`、`D:/projects/go-tmp/custom7-deploy-cos-20261015.log`；服务器完整结果在各站备份目录内。
- 本轮共享文档与本报告作为独立收尾提交同步至发布分支和 `custom/integration`，精确远端 ref 由 Git 核验；该提交不改变运行标签或镜像。活动树的项目状态、功能日志、实现记录、升级问题、升级说明、模块维护入口及发布手册均已更新；旧 D 盘工作目录的两份共享指针同步到本轮结果。保留 [.6 历史发布记录](CUSTOM_RELEASE_0_2_16_CUSTOM6_20261015.md)，不将其改写为本次结果。
