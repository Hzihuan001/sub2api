# 定制项目状态与官方升级说明

### 2026-11-01 — 0.2.19-custom.8 — 渠道测智分组顺序三站发布

- **镜像**：`ghcr.io/hzihuan001/sub2api@sha256:7bdb76eb4524776a8f38ab61db60d47f7ba9e997965ad88127ec2b2d32e25c9b`，构建版本 `0.2.19-custom.8`，运行提交 `c2b58647b`。
- **功能**：测智管理新增独立的分组展示顺序设置，可上移、下移并保存；用户端按保存顺序展示，新分组自动追加到末尾，不影响普通分组排序。
- **接口**：新增管理员顺序读取/保存接口，顺序以独立设置持久化；无效、重复和已删除分组 ID 会被安全忽略。
- **部署验收**：主站 `ai.moshu.cloud`、L1 `api.moshu.cloud`、COS `cos.moshu.cloud` 按顺序更新；三端 healthy、restart 0，根页面、渠道测智页面、测智管理页面和 `/health` 均返回 200。
- **清理/回滚**：清理 custom.6 旧镜像和无用缓存；保留当前镜像与上一版 `sha256:856e30df75551c3e903ec672fe492f8faab266c91b6eefa5771af757b90e5c3c`。Compose 备份位于 `/opt/sub2api-deploy-backups/20261031184758/`。

### 2026-10-31 — 0.2.19-custom.7 — WireGPT 风格渠道测智三站发布

- **镜像**：`ghcr.io/hzihuan001/sub2api@sha256:856e30df75551c3e903ec672fe492f8faab266c91b6eefa5771af757b90e5c3c`，构建版本 `0.2.19-custom.7`，运行提交 `5b49d2132`。
- **页面改造**：用户端按 WireGPT 结构展示顶部说明、分组筛选、分组横向作品栏和动态 HTML/SVG 卡片；用户只看到分组、模型、时间和结果，不显示账号；每组公共结果上限提高到 50 条。
- **管理改造**：渠道管理 → 测智管理可直接选择上游账号/API Key、分组并打开完整定时测试配置，不再跳转账号管理。
- **部署验收**：主站 `ai.moshu.cloud`、L1 `api.moshu.cloud`、COS `cos.moshu.cloud` 按顺序更新；容器 healthy、restart 0，三站根页面、`/pelican-showcase`、`/admin/channels/showcase` 和 `/health` 均返回 200。
- **清理/回滚**：清理更早旧镜像和构建缓存；当前服务器保留运行镜像与上一版回滚镜像 `sha256:23ef5cabe31db07f555bc9211850a93e6425712b1897c60fa9256ed0c1fba821`。Compose 备份位于 `/opt/sub2api-deploy-backups/20261031092218/`。

### 2026-10-31 — 0.2.19-custom.6 — 渠道测智修复与三站发布

- **镜像**：`ghcr.io/hzihuan001/sub2api@sha256:23ef5cabe31db07f555bc9211850a93e6425712b1897c60fa9256ed0c1fba821`，构建版本 `0.2.19-custom.6`，运行提交 `46e75135b`。
- **修复**：定时测试兼容 NULL 租约字段；渠道管理新增“测智管理”；用户端渠道测智只返回并显示分组、模型和结果，不暴露账号名称。
- **部署**：主站 `ai.moshu.cloud`、L1 `api.moshu.cloud`、COS `cos.moshu.cloud` 按顺序更新，三端容器 healthy、restart 0；根页面和 `/health` 均正常返回 200。
- **清理**：服务器仅保留当前镜像和上一版回滚镜像 `sha256:8377cdfdd22f47c6b4fb6bd3ba6a8db0217dc3b74af109729532c10fde30cbc5`；删除更早旧镜像并清理无用构建缓存，不触碰数据库、Redis 和业务卷。
- **备份/回滚**：Compose 备份位于 `/opt/sub2api-deploy-backups/20261030190735/`；回滚只替换应用镜像，不恢复旧数据库。

### 2026-10-31 — 0.2.19-custom.5 — 渠道测智三站发布

- **镜像**：`ghcr.io/hzihuan001/sub2api@sha256:8377cdfdd22f47c6b4fb6bd3ba6a8db0217dc3b74af109729532c10fde30cbc5`，构建版本 `0.2.19-custom.5`，运行提交 `5b647e25d`。
- **部署**：主站 `ai.moshu.cloud`、L1 `api.moshu.cloud`、COS `cos.moshu.cloud` 按主站→L1→COS 顺序更新，三端应用容器均使用同一 digest、healthy、restart 0。
- **公网验收**：三站 `/health` 均返回 `{"status":"ok"}`。
- **本次功能**：用户可见名称统一为“渠道测智”；系统设置可编辑页面顶部说明，空值恢复默认说明；内部 Pelican 标识保持兼容。
- **备份/回滚**：Compose 备份位于服务器 `/opt/sub2api-deploy-backups/20261030183023/`；回滚只替换应用镜像为上一 digest `sha256:a70951a263a195e4184d8fa0f354f47ebc75de660f0ce2cf345c69b1a643f3f6`，不恢复数据库、Redis 或业务卷。

### 2026-10-30 — 未发布 — 渠道测智命名与顶部说明配置

- **改动**：用户端、管理员端和账号测试面板的可见名称统一改为“渠道测智”；内部 Pelican 类型和默认自行车测试提示词保持不变。
- **说明配置**：系统设置新增“页面顶部说明”文本框，默认保留原说明；管理员修改后，渠道测智接口和用户页面顶部动态显示新文本，清空后恢复默认。
- **验证**：前端 `vue-tsc`、渠道测智 API/功能开关 7 项测试、后端 service/handler/server 定向测试、架构合同检查通过；旧“鹈鹕测智”用户可见文案已清理。
- **发布状态**：本地修改尚未提交、推送或部署；生产仍为 `0.2.19-custom.4`。


### 2026-10-30 — 未发布 — 过滤未配置鹈鹕计划的分组

- **问题**：鹈鹕展示页按所有可见分组渲染，导致没有测试计划的分组也显示空等待卡片。
- **修复**：只返回存在启用中的、绑定目标分组的 Pelican 计划；有计划但尚无结果的分组保留等待状态，停用/删除计划的分组不再展示。兼容旧仓储时至少要求有可见结果。
- **验证**：service/repository 定向 Go 测试和编译通过。
- **发布状态**：本地提交 `ba6e0dc08`，尚未推送或部署；生产仍为 `0.2.19-custom.4`。


### 2026-10-30 — 0.2.19-custom.4 — 鹈鹕测智开关三站发布

- **镜像**：`ghcr.io/hzihuan001/sub2api@sha256:a70951a263a195e4184d8fa0f354f47ebc75de660f0ce2cf345c69b1a643f3f6`，构建版本 `0.2.19-custom.4`，运行提交 `ee748e5e9`。
- **部署**：主站 `ai.moshu.cloud`、L1 `api.moshu.cloud`、COS `cos.moshu.cloud` 已按顺序更新，三端容器均使用同一 digest、healthy、restart 0。
- **公网验收**：三站 `/health` 均返回 HTTP 200。
- **变更**：仅重建应用容器；数据库、Redis、业务卷、反代和站点配置保留。
- **回滚**：应用回退到上一 digest `sha256:5dfe6d4208edb477ea1f6e5980e0f4dd08c317b22c4fa89ad344ffc6112fecdd`，不恢复旧数据库。


### 2026-10-30 — 鹈鹕测智可见性开关（本地，未发布）

- **功能**：系统设置新增“显示鹈鹕测智”开关，默认开启以保持现有行为；关闭后用户侧边栏隐藏，前端直接访问重定向，后端 `/api/v1/pelican-showcase` 返回 404。
- **兼容**：仅控制用户展示入口和读取接口，不删除定时测试计划、历史结果或管理员配置。
- **实现**：DB-backed `pelican_showcase_enabled` 贯穿设置默认值、公共设置注入、管理员更新/审计、前端 feature flag、路由守卫和后端 handler。
- **验证**：Docker backend-builder 编译通过；前端 `vue-tsc --noEmit`、功能开关 6 项测试和生产前端构建通过；Pelican/设置相关 Go 定向测试通过。带 `unit` 标签的全包 service 测试仍受仓库中既有 Grok 审计测试编译错误阻塞，与本改动无关。
- **发布状态**：仅本地修改，未推送、未构建 GHCR、未部署生产；生产仍为 `0.2.19-custom.2`。


### 2026-10-30 — P0 网关故障转移验收修正（本地，未发布）

- **问题定位**：无分组故障规则时，数据库 `overload_until` 与 Redis 冷却均为空；同一 API Key 后续请求留在备用账号是粘性会话设计。另发现进程内 `openaiModelTransient` 会在新会话短暂过滤故障账号，这与“无规则不跨请求暂停”不一致。
- **修复**：保留模型瞬态记录用于诊断和请求内重试，但从 `isOpenAIAccountRequestRuntimeBlocked` 调度门槛移除；只有持久化账号状态、供应商明确限额/凭证状态或显式分组规则才能跨请求暂停。
- **验收**：本地候选镜像 `sub2api-custom-p0-app` 通过网关故障切换 smoke：overload 3 次请求内重试后切备用；同会话后续请求继续走健康备用；新 `X-Session-ID` 仍会尝试主账号并再次切备用；quota/model/server/timeout/auth/rate/stream/safety/parameter/partial 共 11 个场景全部通过。
- **代码/测试**：修改 `openai_account_runtime_block_fastpath.go`、相关单测和 `deploy/tests/gateway-failover-smoke.ps1`；`git diff --check` 通过，Docker 完整镜像构建通过。
- **发布状态**：仅本地未提交候选，未推送、未构建 GHCR、未部署生产；三站生产仍为 `0.2.19-custom.2`。
- **下一步**：补跑完整后端定向测试并提交本地修复；待用户明确批准后再进入推送/生产发布流程。
- **回归边界**：受影响的 OpenAI service 定向测试通过；完整 `internal/handler/admin` 与 `internal/repository` 回归仍有既有测试夹具/Redis 临时端口失败（模型能力元数据断言、Redis connection refused），未发现本次调度门槛修改导致的失败；因此不把全包回归标为通过。



### 2026-10-30 — P0 鹈鹕执行边界与调度安全改造（本地候选，未发布）

- **已实现候选**：新增 `PelicanTestExecutor` 和分组绑定校验适配器；Pelican 计划执行不再直接调用普通账号测试入口。普通连接测试保持原路径。
- **调度安全**：新增 `9006_custom_scheduled_test_leases.sql`，为计划增加租约、活动 run、失败计数和最后错误，为结果增加 `run_id` 及部分唯一索引；持久化仓库增加 `FOR UPDATE SKIP LOCKED` 领取和 lease/run CAS 完成。失败结果会落库，并按有限指数退避推进 `next_run_at`，避免失败计划每分钟重复请求。
- **已检查**：合同清单 8 modules/15 groups、Python 合同 19/19、Pelican API 测试 1/1、Docker backend-builder Go 编译通过、Pelican/调度相关 Go 单测通过、临时 PostgreSQL 上 9005/9006 重复迁移与字段/唯一索引检查通过、`git diff --check` 通过。
- **尚未通过**：双 worker 租约竞争、真实 runner/结果幂等集成、旧镜像读取和隔离 Compose 尚未验证；因此本候选仍不能推送、构建生产镜像或部署。
- **候选提交**：P0 代码 `9e3fdb748`，测试边界修复 `6c34b98de`；仍未推送。
- **当前生产**：三站继续运行 `0.2.19-custom.2`。本地候选改造与迁移仍需完成双 worker、真实 runner 和 Compose 验证，再决定是否进入发布分支。

### 2026-10-30 — P0 展示读取优化（本地候选，未发布）

- **已实现**：新增 `PelicanShowcaseReader` 批量读取端口；PostgreSQL 使用窗口函数按分组限制结果数量，展示页优先走单次批量查询，旧适配器保留受控回退路径。
- **验证**：Docker backend-builder 编译、Pelican/调度定向 Go 单测、前端类型检查和合同门禁通过；批量 SQL 尚未在完整应用 Compose 中运行。
- **候选提交**：`f760c1b7b`；未推送、未部署。

### 2026-10-30 — P0 隔离三站 Docker 验收

- **镜像**：本地候选完整镜像构建成功；backend-builder Go 编译、前端生产构建和 i18n 4 项通过。
- **三站**：隔离 Compose 主站/L1/COS 均 healthy；`127.0.0.1:18280/18281/18282/health` 全部 HTTP 200；Compose config 通过。
- **数据库**：三套 PostgreSQL 均发现 9005/9006 预期字段（计划 5 个、结果 run_id 1 个）。启动日志未发现 panic/启动错误，调度 runner 正常启动。
- **清理**：本轮三站容器和卷已删除；由于旧会话孤儿容器仍占用共享 Compose 网络，网络未强制删除，避免影响其他验收环境。临时应用镜像已删除。
- **结论**：本地容器验收通过；双 worker 竞争和真实 runner 计费/结果幂等仍需专门集成测试，未授权推送或生产部署。
- **新增证据**：已添加 `TestScheduledTestPlanClaimDueLeaseAndFinalize` 集成测试，覆盖双 worker 领取、过期租约保护、错误 token CAS 和重复完成；Docker 编译环境已验证该测试可编译，但 Testcontainers 真实执行需在能访问 Docker socket 的宿主测试环境运行。
- **持久化保护**：结果保存失败时保留租约，不提前 finalize；租约过期后允许完整任务重试。修复提交 `3c90e3310`，未推送。
- **真实集成**：通过临时 Go + Docker CLI 容器挂载 Docker Desktop socket，真实 Testcontainers PostgreSQL/Redis 集成测试通过，`TestScheduledTestPlanClaimDueLeaseAndFinalize` 用时约 189 秒；测试容器已清理。
- **Runner 行为**：新增单元测试覆盖 Pelican 失败结果落库和 `next_run_at` 推进；Docker 定向 Go 测试通过。
- **旧镜像兼容**：旧镜像 `0.2.19-custom.2` 在独立数据库追加 9005/9006 后重启，health 200 且无 panic/启动错误；临时容器/卷已清理。
- **结果幂等**：真实 Testcontainers lease 集成已扩展为重复 `run_id` 写入验证，同一计划/运行只返回同一结果 ID；测试用时约 27 秒，资源已清理。


### 2026-10-30 — 阶段 A 执行：ownership 登记与孤儿路径门禁（未发布）

- **已完成**：将 `pelican-showcase` 加入 `.github/custom-features.json`、`custom/manifest.yml`、`custom/contracts.json` 和后端稳定 FeatureID；登记 `9005_custom_pelican_scheduled_test_kind.sql`、Pelican API/页面/宿主接线，以及批量生图实际 service、queue、worker、provider、settlement、repository 和前端入口。
- **新增门禁**：`scripts/custom_contracts.py` 增加 orphan-custom-sources 扫描，识别 Pelican、保留范围迁移、Key/韧性/审计/生图定制路径；新增 19 项 Python 合同测试，验证漏登记路径会失败。当前合同检查通过：8 modules、15 executable groups；Python 合同测试 19/19；Pelican 前端 API 测试 1/1；前端类型检查通过。
- **执行调整**：扫描范围排除了测试文件和生成的静态资源，避免把测试/构建产物误当作生产所有权；真实业务文件仍必须登记。
- **阻断项**：只读审计确认 Pelican 计划仍未把 `group_id` 传入真实测试执行；失败计划可能不推进 `next_run_at`；`ListDue` 没有 lease/CAS/幂等 `run_id`；展示存在 N+1 查询；当前没有 Go 工具链且 Docker 引擎未启动，因此这些 P0 修复和后端/Docker 验收尚未执行。
- **候选保存**：候选代码已单独保存为本地提交 `1e2f67ed4`，仍未推送、未创建 PR，不能作为生产基线。这样清单和源文件在干净检出中保持一致。
- **生产**：三站仍为 `0.2.19-custom.2`，本轮没有推送、构建镜像或部署。


### 2026-10-30 — 深度解耦方案追加三轮审核（第 5–7 轮）

- **第 5 轮：边界登记**：发现未提交的 `pelican-showcase` 与 `9005` 迁移尚未进入 manifest/contracts，image-studio 的后端队列/计费接线登记不完整，operator 与 direct-access owner 混用，key-routing ownership 覆盖过多官方核心文件。新增 orphan-custom-sources 和三份清单一致性作为阶段 A 门禁；候选登记完成前不提交、不构建镜像。
- **第 6 轮：运行与数据**：要求鹈鹕执行使用显式 `GroupScopedTestAdapter`，失败计划推进 `next_run_at` 并退避，调度领取使用 lease/CAS 和幂等 `run_id`，公开展示改为批量读取/分页或受控缓存；`9005` 必须做旧镜像读取、重复迁移和删除分组语义验收。
- **第 7 轮：交付门禁**：要求升级 PR 执行 Compose 配置、隔离 health、HTTP/SSE、迁移重复、旧镜像、调度幂等和敏感信息检查；诊断缺失即失败，生成不可变 evidence manifest。任一步骤 skipped/unknown、P0 证据缺失或 digest 不一致，只生成 blocked draft，不得推送/部署。
- **方案文件**：[`CUSTOM_MODULARIZATION_REVIEWED_PLAN_20261030.md`](CUSTOM_MODULARIZATION_REVIEWED_PLAN_20261030.md) 已更新为七轮审核定稿；本轮只改方案和文档，没有迁移业务代码、推送镜像或修改生产。


### 2026-10-30 — 深度解耦方案四轮审核定稿

- 已完成四轮审核：架构边界、主线合并、数据/运行稳定性、实施/交付门禁。
- 定稿方案见 [`CUSTOM_MODULARIZATION_REVIEWED_PLAN_20261030.md`](CUSTOM_MODULARIZATION_REVIEWED_PLAN_20261030.md)，包括 P0/P1/P2 顺序、Contract/Core/Adapter/Surface/Verification 分层、停止条件和量化验收标准。
- 本轮仍是规划和文档工作，没有迁移业务代码、推送镜像或部署生产；现有未提交鹈鹕候选继续按上一条状态记录处理。


### 2026-10-30 — 鹈鹕测智分组归属与下一阶段解耦计划（仅本地，未发布）

- **当前生产**：主站 `ai.moshu.cloud`、L1 `api.moshu.cloud`、COS `cos.moshu.cloud` 仍运行 `0.2.19-custom.2`；本轮没有推送、构建镜像或修改生产环境。
- **当前工作树**：分支 `codex/v0.2.19-merge-20261029`，基线 `987912947`。工作区存在未提交的鹈鹕测智改造：分组作品墙、定时测试类型、目标分组、测试提示词和结果状态；这些改动只能作为本地候选，不能当作已发布版本。
- **本轮语义修正**：鹈鹕测试计划现在按“账号 + 分组”归属，展示接口只读取计划指定分组的结果；同一账号属于多个分组时，不再把同一结果复制到所有分组。普通连接测试仍保持账号级行为。
- **本地检查**：前端 `vue-tsc --noEmit`、生产构建、i18n 4 项和 `git diff --check` 已通过；当前环境没有 Go 工具链，后端编译、迁移和 Docker 联合验收尚未完成。
- **模块化结论**：展示页、API、handler、路由和鹈鹕聚合服务已经独立；计划字段和账号测试适配仍接入官方定时测试、账号测试服务和管理员面板，属于“低侵入模块化”，尚未达到完全解耦。
- **下一阶段方案**：见 [`CUSTOM_MODULARIZATION_NEXT_PHASE_20261030.md`](CUSTOM_MODULARIZATION_NEXT_PHASE_20261030.md)。重点是建立 feature contract、测试适配器和稳定 hook，减少对官方执行器、账号测试服务、网关和页面主文件的直接修改；本阶段先做边界盘点和接口冻结，再逐项迁移。


### 2026-10-29 — 0.2.19-custom.2 — 修复官方角色路由冲突并重新发布

- **问题**：官方 `v0.2.19` 已包含 `/admin/roles`，定制角色功能仍重复注册同一路由；前端启动阶段完整性校验抛出 `Duplicate custom route path: /admin/roles`，导致页面只返回 HTML 空壳而不挂载 Vue。
- **修复**：`frontend/src/custom/hostAdapters.ts` 在合并路由时识别“同路径、同名称”的官方路由并复用官方定义；不同名称的真实冲突仍继续报错，避免静默覆盖。保留定制菜单、权限和页面组件。
- **提交/版本**：修复提交 `8020d226a`，版本 `0.2.19-custom.2`；镜像 `ghcr.io/hzihuan001/sub2api:0.2.19-custom.2`，摘要 `sha256:5dfe6d4208edb477ea1f6e5980e0f4dd08c317b22c4fa89ad344ffc6112fecdd`。
- **验证**：前端类型检查通过；角色路由/菜单集成测试 31/31 通过；三站动态前端资源 183/183 返回 200；三站 `/health` 200，容器 healthy、restart 0，启动错误扫描无匹配。
- **生产**：主站 `ai.moshu.cloud`、L1 `api.moshu.cloud`、COS `cos.moshu.cloud` 已统一切换到该镜像，仅重建应用容器，未改数据库、Redis、卷或业务配置。部署前 compose 备份位于服务器 `/opt/sub2api-backups/v0.2.19-custom.2-route-fix-20261029T0145Z`。


### 2026-10-29 — 0.2.19-custom.1 — 官方 v0.2.19 合并并三站生产发布

- **官方基线**：`v0.2.19`，提交 `dece26153621959ccd7635814f4b2d08304e681a`；基于远端 `custom/integration` `86ae08dbb450eb1090c2a076c3d8da7865ac7cf7`。
- **合并提交**：`99d8b8f63`（官方合并及定制冲突处理），`a02e2f1b7`（前端上游模型同步类型补齐）；标签 `custom-0.2.19.1`。
- **保留的定制**：角色/权限、Moshu 生图工作台与批量队列、提示词审计、分组/Key 路由、直连入口及对客品牌；官方新增管理员 Key、图片按张计费、Grok/Gemini 模型目录和迁移均已合入。
- **本地检查**：后端 `go test -run '^$' ./...` 全包编译通过；前端 `vue-tsc --noEmit` 通过；模型同步与 Key 编辑定向测试 63/63；Docker 生产构建（含 i18n 4/4）通过；隔离 Compose 主站/L1/COS 三套应用、PostgreSQL、Redis healthy，三个本地 `/health` 返回 200。
- **生产镜像**：`ghcr.io/hzihuan001/sub2api:0.2.19-custom.1`，不可变摘要 `sha256:6f2e1c26aa491b67a038dd8ae98f9cc092c3aed5c38df8eed571ba7f388b5621`。
- **生产部署**：按主站 `ai.moshu.cloud` → L1 `api.moshu.cloud` → COS `cos.moshu.cloud` 顺序，仅重建应用容器，保留数据库、Redis、卷、网络和反代。三站容器均 healthy、restart 0、摘要一致；公网 `/health` 与首页均返回 200，发布后关键启动错误扫描无匹配。
- **备份/回滚**：部署前备份已在服务器生成并完成 SHA256 与 PostgreSQL dump 列表校验，标识为 `v0.2.19-custom.1-20261028T191356Z`；应用回退使用旧摘要 `sha256:80adc97b50213ba840e68d6c13b932701cff294f04fb5dcd291ffe12b63f49e6`，不恢复旧数据库覆盖新业务数据。
- **共享文档**：本条同步至 `CUSTOM_PROJECT_STATUS.md`、`CUSTOM_FEATURE_LOG.md`、`UPSTREAM_SYNC.md`；后续版本从 `custom/integration` 和本标签继续。

### 2026-10-27 — 共享指针：受控直连入口已上线

最新增量请读取 `D:/projects/sub2api-direct-access-20261027/docs/CUSTOM_PROJECT_STATUS.md`、`CUSTOM_DIRECT_ACCESS_20261027.md` 与 `deploy/direct-access/README.md`。入口代码 `4e746e82d` / `direct-access-20261027.1`，文档闭环 `c59f4c533` 已推送 `custom/integration` 和功能分支。`nocf.moshu.cloud` DNS only + HTTPS + 用户授权侧车已部署，用户 ID 4 已开通，本地48项/真实PG权限集成与生产免费验收通过；主站/分站应用仍 `.18.4`，没有重启或重建。旧树原文与其他修改保留，不从本树整树覆盖最新集成。


<!-- moshu-image-studio-20261027-diagnostics:start -->
> **2026-10-27 生图工作台错误排查与共享文档同步**：本轮为 `.18.4` 部署后的只读错误排查和文档更新，运行版本、源码标签及正式镜像不变，没有重新部署。用户最终说明 `Failed to fetch` 出现在生成过程中；浏览器进度请求失败不等于后台任务已取消，先检查任务列表，避免盲目重复提交。尚未确定该错误的单一根因。
>
> 权威工作树 `D:/projects/sub2api-image-batch-16refs-20261026`，分支 `codex/image-batch-16refs-20261026`；运行源 `1524aa0985f432df19c9144c726224d2852308d6`，不可变标签 `custom-0.2.18.4`，正式镜像 `sha256:80adc97b50213ba840e68d6c13b932701cff294f04fb5dcd291ffe12b63f49e6`。本次诊断文档提交 `28159e883499df18d6f0ef7819e500eab95a5896`，不代表新增运行版本。
>
> [本轮诊断](D:/projects/sub2api-image-batch-16refs-20261026/docs/CUSTOM_IMAGE_STUDIO_DIAGNOSTICS_20261027.md)、[项目状态](D:/projects/sub2api-image-batch-16refs-20261026/docs/CUSTOM_PROJECT_STATUS.md)、[共享功能日志](D:/projects/sub2api-image-batch-16refs-20261026/docs/CUSTOM_FEATURE_LOG.md)、[主线同步说明](D:/projects/sub2api-image-batch-16refs-20261026/docs/UPSTREAM_SYNC.md)。此旧树仅追加诊断入口，原文与其他会话未提交修改全部保留；后续以权威文档的检查范围和证据边界为准，勿用旧树整树覆盖。
<!-- moshu-image-studio-20261027-diagnostics:end -->


<!-- moshu-image-studio-custom-0.2.18.4-shared-pointer:start -->
> **2026-10-27 批量生图最新共享入口（已部署三站）**：当前正式版本为 `0.2.18-custom.4`。共享参考图只上传一次，后台再分配到各个普通单图并发任务；GPT-image 单图、批量共享和逐条参考图仍最多16张，其他模型及整组总量限制按各自能力执行。请求绑定错误已细分，原始参考图字节、质量及图片尺寸行为保留。旧日志仅确认两次400，不能据此锁定用户那次调用的具体原因。
>
> 权威工作树 `D:/projects/sub2api-image-batch-16refs-20261026`，分支 `codex/image-batch-16refs-20261026`；运行源 `1524aa0985f432df19c9144c726224d2852308d6`，不可变标签 `custom-0.2.18.4`，正式镜像 `sha256:80adc97b50213ba840e68d6c13b932701cff294f04fb5dcd291ffe12b63f49e6`。文档收尾提交 `a56bf0c0208f885e4fe4c4e3c2c5eebcfbca81d4`。后续先核对该工作树和最新集成引用，不从旧树整树覆盖发布。
>
> [项目状态](D:/projects/sub2api-image-batch-16refs-20261026/docs/CUSTOM_PROJECT_STATUS.md)、[共享功能日志](D:/projects/sub2api-image-batch-16refs-20261026/docs/CUSTOM_FEATURE_LOG.md)、[本轮发布验收](D:/projects/sub2api-image-batch-16refs-20261026/docs/CUSTOM_RELEASE_0_2_18_CUSTOM4_20261027.md)。此旧树仅追加共享指针，原文、旧维护记录以及其他会话未提交修改全部保留；旧发布段落作为历史阅读，勿覆盖其他会话工作。应用回退目标为 `.18.3`，保留现有业务数据及迁移记录。
<!-- moshu-image-studio-custom-0.2.18.4-shared-pointer:end -->


> **共享文档已推送（2026-10-26）**：提交 `b2ec356187dc74214c38a1a47e47b011228df074` 已同步至 `codex/image-batch-16refs-20261026` 和远端 `custom/integration`。后续会话从[最新项目状态](D:/projects/sub2api-image-batch-16refs-20261026/docs/CUSTOM_PROJECT_STATUS.md)、[定制日志](D:/projects/sub2api-image-batch-16refs-20261026/docs/CUSTOM_FEATURE_LOG.md)及[维护记录](D:/projects/sub2api-image-batch-16refs-20261026/docs/CUSTOM_MAINTENANCE_20261026.md)继续；运行版本仍为 `0.2.18-custom.3`，本次只同步文档。旧树源码及其他会话修改保留。


> **2026-10-26 批量参考图最新共享入口（已部署）**：三站已运行 `0.2.18-custom.3`，GPT-image单生图、批量整组统一参考图、批量逐条参考图均最多16张；其他模型仍按各自上限。运行源 `c2dbbe786e844ee1df77bfd710ebaf438f625126`，标签 `custom-0.2.18.3`，正式镜像 `sha256:cb1c937d398989ccff2256b0bf7a7a51c7b63405f8dd5a267a1cd50f47e7b7ac`。发布分支 `codex/image-batch-16refs-20261026` 与远端 `custom/integration` 已普通快进至文档收尾 `c8d14a1183f73cdefa28e698f45bbaf8f8c730b5`。
>
> 本轮记录位于 `D:/projects/sub2api-image-batch-16refs-20261026`：[项目状态](D:/projects/sub2api-image-batch-16refs-20261026/docs/CUSTOM_PROJECT_STATUS.md)、[功能日志](D:/projects/sub2api-image-batch-16refs-20261026/docs/CUSTOM_FEATURE_LOG.md)、[发布验收](D:/projects/sub2api-image-batch-16refs-20261026/docs/CUSTOM_RELEASE_0_2_18_CUSTOM3_20261026.md)。前端92项、后端unit及正式镜像16图mock验收通过；三站healthy/restart0、批量入口3/3与应用资源12/12匹配。无后端运行时/迁移/配置变化，应用回退目标`.18.2`。旧树仅追加此共享指针，原维护记录、其他会话未提交代码及文档完整保留；后续先核对最新远端，不从旧树整树覆盖发布。

> **2026-10-26 运维收尾**：生图版本仍为下方 `0.2.18-custom.2`。已补齐权威树当前功能摘要；本地删除52个停止的旧测试容器，保留运行容器和全部卷。服务器仅清理约0.92GB编译/包缓存，14个运行容器无重启，三站健康200，日志、备份和回滚镜像保留。详见[安全清理记录](D:/projects/sub2api-image-studio-16refs-20261026/docs/CUSTOM_MAINTENANCE_20261026.md)。本目录其他会话内容不变。


> **2026-10-26 生图工作台最新共享入口（已部署）**：三站已运行 `0.2.18-custom.2`，GPT-image参考图上限16；Nano/Gemini14、Seedream10和未知模型4保持。运行源 `879a99a22e47a303604927104a6f67329dcd3b38`，标签 `custom-0.2.18.2`，正式镜像 `sha256:c90609bc4546ec309a980929ec8f28eef3cef236022dd698700920439ce78412`。发布分支 `codex/image-studio-16refs-20261026` 与远端 `custom/integration` 已普通快进至文档收尾 `10b452931592de432db312ffc0a94972c1b89666`。
>
> 本轮权威记录位于 `D:/projects/sub2api-image-studio-16refs-20261026`：[项目状态](D:/projects/sub2api-image-studio-16refs-20261026/docs/CUSTOM_PROJECT_STATUS.md)、[功能日志](D:/projects/sub2api-image-studio-16refs-20261026/docs/CUSTOM_FEATURE_LOG.md)、[发布验收](D:/projects/sub2api-image-studio-16refs-20261026/docs/CUSTOM_RELEASE_0_2_18_CUSTOM2_20261026.md)。三站healthy/restart0，工作台入口3/3、相关JS资源9/9与验收镜像一致；无后端运行时/迁移/配置变化，兼容应用回退目标`.18.1`。本旧工作树仅追加此共享指针，其他会话未提交修改完整保留；下方旧发布坐标为历史，后续先核对最新远端，不从旧树整树覆盖发布。

> 本文是给后续维护会话、代码审查者和自动化工具使用的项目交接说明。
> 先读本文页首共享指针，再进入活动工作树阅读项目状态、发布记录、
> `UPSTREAM_SYNC.md` 和 `custom/manifest.yml`；不要以本目录旧源码作为发布基线。
>
> 本文不保存密码、Token、私钥、`.env` 内容或数据库备份路径中的敏感信息。

## 1. 当前快照

### 2026-10-26 — 已发布至 `0.2.18-custom.1`（最新共享入口）

- 权威活动树为 `C:/Users/a/.codex/worktrees/group-routing-current`，分支 `codex/v0.2.18-merge-20261023`。请先读该树的[项目状态](C:/Users/a/.codex/worktrees/group-routing-current/docs/CUSTOM_PROJECT_STATUS.md)、[定制功能日志](C:/Users/a/.codex/worktrees/group-routing-current/docs/CUSTOM_FEATURE_LOG.md)、[`.18 发布记录`](C:/Users/a/.codex/worktrees/group-routing-current/docs/CUSTOM_RELEASE_0_2_18_CUSTOM1_20261024.md)及 `UPSTREAM_SYNC.md`；此目录源码仍是旧预览，不从此构建发布，不覆盖其他会话修改。
- `.13` 的账号级路由、授权模型目录并集、所有分组统一优先级编辑及管理员完整 Key 编辑已发布；精确错误切组标签已随 `.14` 起发布；`.16` 补充 Responses 明确引用拒绝后的窄恢复，`.18` 合并官方 `v0.2.18` 并保留既有角色、生图、审计、品牌和使用记录增强。下方“未发布/仍 `.13`”均为历史阶段，不代表当前状态。半开探测未实现，未跟踪 `replay/` 不属于发布功能。
- 最近生产记录为 2026-10-24：主站 `ai.moshu.cloud`、L1 `api.moshu.cloud`、COS `cos.moshu.cloud` 已部署 `0.2.18-custom.1`，运行源 `fc0d798bf2d6a08a074970d0485c567291e02660`，标签 `custom-0.2.18.1`，三站同一镜像 `sha256:d7f365d1ba40b6c159d4155f7fbc2773d01f0fe6015af7906ebbece500ba1837`。验收范围和 `.16` 应用回退目标见权威报告，不恢复旧库覆盖新业务数据。
- 本次核对前工作分支与本地 `origin/custom/integration` 文档基线为 `e5d4e8c16388a5f061a261997f5840f53ed65f38`，与运行源分开；后续会话须重新核对远端。本次仅补齐共享文档及过期指针，没有代码、配置或生产变更，也未重新做生产健康检查。

### 2026-10-22 — 错误日志切组因果标记补丁（最新共享指针，未发布）

- 权威树 `C:/Users/a/.codex/worktrees/group-routing-current` 已补：仅标记实际触发切换的已有错误事件，详情显示目标组、账号、状态与原消息；后续备用组/本地失败不继承旧标签。沿用现有 JSON，不改变使用记录、计费、TTFT 过滤或 SLA/健康统计。
- 后端定向两包、前端39项及类型/lint、模块契约及18项测试通过；详见权威树同名状态和功能日志。补丁尚未提交/推送/部署，也未重建本地容器；生产仍为下述`.13`。本目录其他会话源码保持原样，不从本目录构建发布。

### 2026-10-22 — `.13` 已推送、三站生产已更新（当前共享指针）

- 文档收尾 `daf9153f438918acc8d1d46359ddfe2d0a1a88b1` 已推送工作分支及远端 `custom/integration`，活动树干净；运行标签仍指向下述90804511提交。下一会话先同步远端集成分支，勿使用尚未同步的旧本地同名分支。
- 当前主站 `ai.moshu.cloud`、L1 `api.moshu.cloud`、COS `cos.moshu.cloud` 均运行 `0.2.16-custom.13`，提交 `90804511dcfd57d9e99d7df470cfa05282182664`，标签 `custom-0.2.16.13`，镜像摘要 `sha256:9d79c4378f3e4dc6dd4d1648cff7420947ccb7984a33d7bb2cbd11030c32c14f`。运行源码、集成分支和标签已推送，文档收尾独立提交，不移动标签。
- 权威活动树仍是 `C:/Users/a/.codex/worktrees/group-routing-current`；先读取其 `docs/CUSTOM_PROJECT_STATUS.md`、`CUSTOM_FEATURE_LOG.md`、`CUSTOM_RELEASE_0_2_16_CUSTOM13_20261022.md` 及 `UPSTREAM_SYNC.md`，下次升级以最新 `custom/integration` 为底。本目录是旧预览源码，仅同步指针，其他会话改动未覆盖，不能从这里直接发布。
- 本轮账号级Key路由、模型目录、完整优先级编辑及管理员完整Key编辑已一起上线。正式镜像本地网关33/33、管理员HTTP5/5、48项临时资源清理通过；生产三站healthy/restart0、配置/DB/Redis不变，旧迁移全行保持，仅增9003/9004，18份源站/公网前端资源SHA一致。备份已验证、没有生产付费调用或实际回退；Actions422走已授权本地GHCR，不声称CI通过。回退`.12`的account_v2限制见权威报告；下方“未发布/待确认”均为历史。

### 2026-10-22 — 管理员完整编辑用户 Key（最新开发共享指针，未发布）

- 最新源码仍为 `C:/Users/a/.codex/worktrees/group-routing-current`。用户管理中“API密钥→编辑”已复用用户端完整表单，支持分组排序/替换/增删、故障规则、模型、状态及高级限制；后端使用原生更新服务，标准组授权与保存同事务，不重置用量。
- 前端相关回归、类型/lint、后端定向及模块契约/构建通过；本地真实HTTP 5/5和浏览器保存重开通过。三站候选已换为 `sha256:3be7acfaafd35211db0d2a0c927246bb3956ea73a7c8035110635e35a078b6f5`，入口仍 `18280–18282` healthy、重启0。下节原路由/长连接/回退证据属于旧候选；本轮未重复整套压测。
- 权威详细报告在该树 `docs/CUSTOM_KEY_ROUTING_STABILITY_ACCEPTANCE_20261022.md`、`CUSTOM_PROJECT_STATUS.md`、`CUSTOM_FEATURE_LOG.md`。生产未改，待用户本地验收，不推送或部署；本目录其他会话源码未覆盖。

### 2026-10-22 — 账号级 Key 路由本地验收（最新开发共享指针，未发布）

- 原升级工作树已归档；最新源码从归档快照 `4ca21d066` 恢复至 `C:/Users/a/.codex/worktrees/group-routing-current`，分支 `codex/key-account-routing-stability-20261022`。本目录其他会话改动保持原样。
- 后端完整四包、1025 候选批读边界、模块契约及前端类型/构建通过；三站路由账务 33/33、共享 Redis/双应用 11/11、三站 660 秒 SSE 3/3、custom.12 往返 6/6 通过。候选镜像 ID `sha256:e6af1a2dd6b7986d0aefc3a85f771873f29ff31315ee9ccb2f8fcfaef7c4fb3c`，本地入口 `18280–18282`。生产仍为下节 `.12`，待用户本地验收，不推送或部署；不将合成验收视为生产容量或全 provider 证明。
- 最新状态请读 `C:/Users/a/.codex/worktrees/group-routing-current/docs/CUSTOM_KEY_ROUTING_STABILITY_ACCEPTANCE_20261022.md` 及同目录 `CUSTOM_PROJECT_STATUS.md`、`CUSTOM_FEATURE_LOG.md`；下方旧活动树指针仅保留历史。

### 2026-10-20 — `.12` 三站发布完成（最新共享指针）

- 三站已更新为 `0.2.16-custom.12`，运行提交 `f42b02f9ea0f43c3e34d9bbf778cd9e450cb05b4`，标签 `custom-0.2.16.12`，同一 digest `sha256:e6a53ec608abd463bfa41ccc6e939dca0175756adeb40e957ebe31a6b68c3610`。独立 Key 编辑器、TTFT/SLA/故障规则、模型限制已上线，自定义规则不再全局停账号。
- 最终完整 service、147 项前端及正式镜像本地主站 13/13 路由/计费验收通过。北京时间 06:23–06:25 顺序 app-only 更新，06:26:50 三站健康/重启 0、配置与数据服务保持、旧迁移全行不变，仅增 9001/9002，公共 JS 与源站一致。Actions 实际 dispatch 422，不称 CI 成功；无生产付费调用，L1/COS 完整登录矩阵仍未执行。
- 完整共享源：`C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom/docs/CUSTOM_PROJECT_STATUS.md`、`CUSTOM_FEATURE_LOG.md`、`CUSTOM_RELEASE_0_2_16_CUSTOM12_20261020.md`。发布及 `custom/integration` 同步运行代码与文档，实际 ref 用 Git 核验；下次开发先读这些文件，不以本旧树源码发布。原未提交文档/源码与其他会话文件保留。

### 2026-10-16 — `.8` 三站发布完成 最新共享指针

- 本目录最新公共使用文档已独立移植到活动发布树，连同 Key 常驻范围提示删除，发布 `0.2.16-custom.8` / `custom-0.2.16.8` / 运行 `46083426af6cf993b0dfd86767fc35df9977dd22`。源码、标签、GHCR 及三站部署完成，digest `sha256:7eb13edccde298ea2b57721fd2581c8552291b81c65268c1b3f812a073d7647b` 本地/OVH 一致。
- 本树原教程内容与 14 图保留并已上线；活动树只移植文档路由一行，保留模块化角色入口。106 项前端检查、类型/lint、构建、角色容器复验及本地/三站真实文档资源哈希核对通过。首轮 PG 初始化就绪竞争的失败记录保留；本次临时角色容器和合成卷已回收，证据保留。
- 北京时间主站 01:11:28–01:11:42、L1 01:12:09–01:12:28、COS 01:12:52–01:13:17 部署通过；三站 healthy/restart 0，数据库/Redis、配置与迁移全行保持，备份 verified，回退 `.7`，未回滚。没有付费模型调用。
- 完整共享源：`C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom/docs/CUSTOM_PROJECT_STATUS.md`、`CUSTOM_FEATURE_LOG.md` 和 `CUSTOM_RELEASE_0_2_16_CUSTOM8_20261016.md`。本目录仍是旧预览源码，禁止直接作为发布基线；下方“仅本地/未合入/待发布”文档条目均为发布前的历史记录。原未提交源码与其他会话文件未重置。

### 2026-10-16 — 文档菜单二次调整与 CC Switch 下载页（verified，仅本地）

- 按用户后续要求移除“接入其他工具”菜单项；工具顺序以本条为准：Codex → Claude CLI → Claude Desktop → WorkBuddy → Trae → Cherry Studio → Zcode → OpenCode。原 `/docs/tools` 及各篇深链接继续可用。
- 手机端改为单一文档选择器，直接选择创建令牌、CCSwitch 或八种工具，不再有多余的工具分类选项。CC Switch 安装部分移除固定 v3.18.0 的 Windows/macOS 下载直链，统一链接至 `https://ccswitch.io/zh/download`；其余教程正文与图片不变。
- 本轮文档交互测试 **20/20**、页面/测试定向 ESLint、类型检查与 diff check 通过。真实浏览器确认桌面新顺序、下载链接、390px 单下拉直接进入 Claude Desktop；已恢复默认窗口尺寸。仅修改本地文档与共享记录，未提交、推送或部署，生产仍按 `.7` 记录，下方旧源码禁止直接发布的边界不变。

### 2026-10-16 — 公共接入文档工具菜单（verified，仅本地）

- **范围 / 负责人**：公共文档会话，feature `branding`；按用户提供的 `G:/接入工具/接入工具.md` 更新本工作区原有文档改动。源码仍在 `D:/projects/sub2api-custom`、`codex/isolation-refactor-v0.2.13`，HEAD `c9dd2286df2d9db79ec184f57092a8f966840139`，版本文件仍为 `0.2.13-custom.6`；这是文档预览工作区，不是生产/升级基线。
- **行为**：`/docs/tools/:tool` 独立文章与文档图标菜单，按 Markdown 顺序为 Codex、Claude CLI、WorkBuddy、Trae、Cherry Studio，再追加保留 Zcode、OpenCode、Claude Desktop。`/docs/tools` 默认 Codex，未知工具回退 Codex；桌面选中高亮，手机下拉导航。创建令牌和 CCSwitch 正文与截图保持不变。
- **内容 / 资源**：替换五份指南，导入 WorkBuddy 3 / Trae 5 / Cherry Studio 6 张截图；无明文密钥，14 张源图与构建产物哈希一致。修正原稿 Windows Codex 的 `mmoshu` 为 `moshu`，Claude Windows 的 `api.ai.moshu.cloud` 为根地址 `ai.moshu.cloud`；保留前轮 Codex 的非 OAuth、禁用 WS 兼容配置。截图历史供应商地址附文字配置优先提示，保留可用模型查询折叠说明。
- **验证**：真实 Vue mount + memory router 19 项、文档入口安全 7 项、路由回归 61 项共 87 项测试通过；定向 ESLint、`vue-tsc --noEmit`、`pnpm --dir frontend run build`（含 3 项 i18n 检查）、`git diff --check` 通过。12 个本地文档入口、14 张新图均 HTTP 200。真实浏览器验证桌面菜单跳转、390px 手机下拉切换及无横向溢出；未验证用户真实 API Key/模型调用。
- **状态 / 下一步**：未提交、推送、建镜像或部署，无 API/数据库/迁移变化；三站生产记录仍为 `.7`，本轮未重新检查生产。后续发布必须仅将文档页面、路由、测试、图片的独立补丁移植到含 `.7` 的最新活动分支再验收，禁止从本旧源码直接发布。其他会话修改和 `%SystemDrive%/` 未跟踪目录原样保留。完整本轮记录见本目录 `CUSTOM_FEATURE_LOG.md` 的 2026-10-16 条目。

> **2026-10-15 主备 Key 限定生产实测完成**：用户创建 Key 167，GPT-稳定→Kimi→DeepSeek；目录仅返回主组 11 模型。Kimi/DeepSeek 普通请求及 DeepSeek SSE 成功，实际组账务正确，共扣 $0.0007148；GPT 首测因主组上游依次 404/502/403 而失败，未扣费，需另查模型映射及上游可用性。共四次生成请求，无追加重试；Key 留给用户删除。常驻范围提示已本地删除，21 项前端测试及类型/定向 lint 通过；未推送部署，生产仍 `.7`。完整状态及证据只维护于活动工作树 `docs/CUSTOM_PROJECT_STATUS.md` 与 `CUSTOM_KEY_ROUTING_20261015.md`；不从本目录旧源码发布。

> **2026-10-15 `.7` 已部署三站（当前指针）**：主备 Key 模块和分组显式默认值已上线。运行 `f36de3d4a00400c66994a02c13c5d23f6a7feac8` / `custom-0.2.16.7`，digest `sha256:ec8860583ac3603fadc108e90b2036abf352d395b5035ee8f4cb51d1de66ac42` 本地/OVH 回拉一致；发布和集成分支已推 `3ce7de3ebe68263a3f0e8bf6a83393486747306f`，文档随后单独提交同步，精确 ref 由 Git 核验。
>
> 北京时间主站 21:42:45–21:42:58、L1 21:44:21–21:44:28、COS 21:45:01–21:45:12 顺序更新均通过，主站/L1 等在途归零才操作。三站最终 healthy/restart 0、版本/提交/digest 一致，配置指纹及 PG/Redis 身份、旧迁移保持，仅新增预期迁移；健康/工作台 200、匿名管理接口 401、关键日志 0。独立 Docker 真实账务/迁移重启/`.6` 只读回退与恢复 `.7`、角色和限定本地浏览器检查通过，首次夹具配置失败保留。
>
> 新增主备扩展表；回退 `.6` 时禁止编辑多组 Key 主组或解绑，保留扩展表，不还原旧库覆盖新业务。本次无生产回滚、生产角色登录、付费模型或长期监控。Actions 无新运行，SSE mixed、15 lint 基线、P4 双跑矩阵和真实升级 PR 链路限制继续披露。a/b 测试容器已停并保留卷/证据，角色项目回收、临时浏览器代理及空网络删除，其他项目未动。
>
> 唯一完整交接源：`C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom/docs/CUSTOM_PROJECT_STATUS.md`、`CUSTOM_RELEASE_0_2_16_CUSTOM7_20261015.md` 和 `CUSTOM_KEY_ROUTING_20261015.md`。本目录仅共享指针，不从旧源码发布，不动其他会话文件。

以下指针均为历史阶段，“未提交/当前 `.6`/尚待 Docker”等不覆盖上方 `.7` 已部署状态；原始证据与决定保留。

> **2026-10-15 API Key 主备分组本地开发**：活动工作树已实现独立 `key-routing` 模块：1 主＋最多 4 备，按模型/账号可调度性顺序选组，按实际组计费，共用 Key 额度；不新增 Key 级规则、不跨组重播已发送请求。标准无资源绑定文本 HTTP/SSE 为首期范围，图片/WS/目录仍走主组，Claude 客户端专用组明确排除多组组合。后端定向 441 顶层/776 含子测试、真实 PG、挂载 HTTP/SSE 计费链及前端构建等验证通过，补充边界定向复验。未提交/推送/部署，独立 Docker 应用与浏览器/回退验收仍是发布前下一步。完整证据见活动工作树 `docs/CUSTOM_KEY_ROUTING_20261015.md`，不要从本目录旧源码发布。下方默认值编辑器也仍未发布，生产继续 `.6`。

> **2026-10-15 最新本地改动（尚未发布）**：分组切换规则已移除“沿用系统 / 自定义”选择，四类参数直接预填可编辑默认值；保留已有配置，空白补默认，保存才写入全四类规则，不自动修改其他分组。完整实现与验证见活动工作树 `docs/CUSTOM_PROJECT_STATUS.md` / `CUSTOM_FEATURE_LOG.md` 页首。没有推送或部署，三站生产仍为下方 `.6`。

> **文档收尾已推送并核验**：`3b0145c8abbf57d581f99084e88118ea7e759e3f`，远端 `codex/group-failover-rules-v0.2.16` 与 `custom/integration` 均指向此提交。15 份仓库文档统一更新；运行标签仍指向 `655a16868`，文档提交未改变 backend/frontend 源码，不另建镜像。以下活动工作树是后续会话唯一完整交接源。

> **2026-10-15 `.6` 已部署三站（当前唯一入口）**：用户批准本地 GHCR 发布后，已于 17:49–17:51 完成主站 `ai.moshu.cloud`、L1 `api.moshu.cloud`、COS `cos.moshu.cloud` app-only 更新；17:54 复核均 healthy/restart 0、关键日志 0。版本 `0.2.16-custom.6`，标签 `custom-0.2.16.6`，运行提交 `655a16868fc310d6dcc742c4bb60a2f740456a58`，统一 digest `sha256:cb074f11766d9190f4e5f3479a0750c7a759a562bfc142aa3067a0d4bfbfc42e`。配置、数据库/Redis、卷、网络、迁移和反代保持，备份及旧 `.5` 镜像保留。
>
> 本轮发布模块化、取消结算恢复、分组切换规则/手动恢复；不包含 API Key 主备分组。SSE 风险已接受，不再追查；Actions disabled、15 项 lint 基线、P4 完整旧新矩阵及真实升级 PR 链路限制继续披露。本地最终 GHCR 角色/API Key/WebSocket/重启验收通过，不冒充生产逐角色登录或 CI 成功。
>
> 活动源码/完整文档：`C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom`，分支 `codex/group-failover-rules-v0.2.16`。先读该目录 `docs/CUSTOM_PROJECT_STATUS.md` 和 `docs/CUSTOM_RELEASE_0_2_16_CUSTOM6_20261015.md`，再读本轮已同步的方案/执行/复核/维护说明。长期集成分支本轮普通快进到包含 `.6` 的文档收尾点；实际 ref 由 Git 核验。本根目录旧源码和其他会话改动不动；下方旧“等待授权/未部署/SSE阻塞”指针只作历史。

以下共享指针均为历史过程；“最新”“未部署”“等待授权”仅描述当时，不覆盖页首 `.6` 已部署状态。

> **2026-10-15 发布通道阻塞（历史）**：用户批准满足条件后上线；本地模块化/分组规则与账务恢复复核未确认新增 P1/P2 阻断。准备提交 `d2476c39f`（`.6`）已推送 `codex/group-failover-rules-v0.2.16`，但正式 Actions dispatch 返回 HTTP 422 `Actions has been disabled for this repository.`。未建标签/镜像、未部署，三站现场仍 healthy 的 `.5`；按方案等待本地直发 GHCR 替代方式确认。细节只维护在活动工作树项目状态页首。SSE 疑点不再阻塞，其他证据限制保留。

> **2026-10-15 SSE 最新决定**：用户回复“那就不管”，停止追查 SSE 延迟疑点；接受该未决风险，此项不再作为当前阻塞项，不追加测试。保留原始 mixed 结论，不声称性能已通过。其他限制与暂缓发布不变，未推送部署。详见活动工作树 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom/docs/CUSTOM_PROJECT_STATUS.md` 页首；下方有关 SSE 阻塞的旧指针已被此决定取代。

> **2026-10-15 分组规则 Docker 已通过（最新功能指针）**：精确 `0068815bd` 本地镜像完成 8 次合成网关请求 / 23 次模拟上游尝试，阈值、全分组暂停、手动恢复、两次重启及权限通过；8 条唯一 usage 与余额实扣一致。实际路由契约补齐 18 条并通过。新验收容器已停止、数据和证据保留；原性能/恢复项目未动。详情只维护在活动工作树 `docs/CUSTOM_PROJECT_STATUS.md` / `CUSTOM_MODULARIZATION_FINAL_GATES_20261015.md`。未推送部署，原 SSE mixed 与发布暂缓不变。

> **2026-10-15 升级 workflow 离线加固（最新维护指针）**：活动工作树及分组规则分支不变；本轮只修自动升级报告/提交/补丁顺序和失败状态处理，固定架构与脚本门禁共 33 项通过。原 SSE mixed 与真实 Actions 链路仍未闭环，未 push/dispatch/deploy，不追加负载。唯一细节仍在活动工作树 `docs/CUSTOM_PROJECT_STATUS.md` 和 `CUSTOM_MODULARIZATION_FINAL_GATES_20261015.md`；本目录旧源码不动。

> **2026-10-15 分组切换规则本地完成（最新共享入口）**：用户批准先做分组规则并确认“触发后全分组暂停账号”。活动工作树不变，分支 `codex/group-failover-rules-v0.2.16`，基线 `260f3c1b6`，实现提交 `caa717338`；配置、页面、失败累计及恢复已实现，后端 422 含子例、前端相关测试/类型/构建、架构门禁和差异 lint 通过。只以该工作树 `docs/CUSTOM_PROJECT_STATUS.md` 页首为准。未推送、未部署；旧模块化性能 mixed 和暂缓发布仍有效，不改本根目录旧源码。

> **2026-10-15 最新本地进度入口**：模块化主体已落地，不推送/部署。活动工作树 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom` 的 `docs/CUSTOM_PROJECT_STATUS.md` 页首是唯一当前快照；精确证据见同目录 `CUSTOM_MODULARIZATION_FINAL_GATES_20261015.md`。运行提交 `53fd17b5b` 的微基准和 11 故障通过；4,000 次 HTTP/SSE、固定 1,200 次 SSE 及用户批准的各版本 100 次 DEBUG 均已结束，日志配置已恢复。本次 DEBUG 未复现候选更慢，但不推翻原 mixed 门禁，不再追加负载。未知图片结果保留待核对，暂缓发布有效。本根目录源码及其他会话修改不动，下方旧判断只作历史。

> **批量取消修复中（2026-10-15，本地）**：用户确认“已发出继续正常结算，取消不退款；上游明确生成失败仍不收费，未发出不收费”。活动工作树正在修复取消导致整批释放冻结的问题；部分成功 + 在途 + 未发出的精确计费回归已通过，恢复/未知结果等验收继续。尚未推送、未部署，整体模块化不能标记完成；详情见活动工作树最新状态与复核报告。下条为修复前停止记录。

> **本地坐标**：取消修复 `55765c473`；WS 既有问题复现 `3856b469a`（保留两项失败，未改 WS 协议）。取消相关单测、Go vet、真实 PostgreSQL CAS/取消三组测试及前端类型/定向 lint 通过；结果未知恢复仍待完善，不作为生产发布许可。

> **模块化停止收尾（2026-10-15，本地、不发布）**：活动分支 `codex/custom-modularization-v0.2.16`。接口/前端/装配已实现，局部验收通过，但完整复核发现既有普通批量取消漏结算缺陷，候选和 989594099 均复现，触发方案停止条件；WS 跨轮身份、恢复/性能等还未全部闭环。此前活动工作树“P0–P6 全部完成”已撤回。先读活动工作树 `docs/CUSTOM_MODULARIZATION_REVIEW_20261015.md` 和执行记录，待用户确认独立修复范围。未推送、未部署，生产仍 `.5`；本根目录旧源码和其他会话修改保留。

> **2026-10-14 最新状态（deployed）**：网关故障切换已部署三站 `0.2.16-custom.5`。同账号 5 分钟累计 3 次明确 overload 后默认冷却 10 分钟；额度/凭据/模型不可用和可恢复上游故障按类别限次切换；安全/参数拒绝和已输出的流不重放。相关 Go 测试、vet、隔离检查、Docker 11 场景通过，GHCR 回拉与验收镜像一致。无新迁移和计费改动，不要把本根目录旧源码作为发布起点。

> **最新发布（2026-10-14，deployed）**：`0.2.16-custom.5` 已于 20:32–20:33（Asia/Shanghai）完成三站 app-only 更新，20:34 复核 healthy、重启 0、健康和静态入口 200、关键启动错误 0。代码、标签和镜像已推送；保留 `.4` 批量不额外打折、`.3` Nano Banana 修复及现有定制。

> **账务边界**：只对新任务取消批量折扣，不重算历史任务、不补扣历史费用、不对上游超额返回的图片加收。新任务的 usage 基础费用与实扣金额分开记录；成功请求项数继续作为计费数量。

> **2026-10-14 共享交接**：下表记录最新生产进度，不代表本目录的源码已经更新。最新已发布代码在 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom`；请继续读取该工作树的 `docs/CUSTOM_PROJECT_STATUS.md`、`docs/CUSTOM_FEATURE_LOG.md`、`docs/CUSTOM_UPGRADE_ISSUES.md` 和 `docs/UPGRADE_V0_2_16_ACCEPTANCE.md`。本目录旧分支上的公共文档页和 DNS 等未提交改动仍保留，不得重置或当作已发布代码。

| 项目 | 当前值 |
| --- | --- |
| 记录日期 | 2026-10-14 |
| 官方基线 | `v0.2.16` / `01842576b3d5c1e2545b5a99ad555d36e28ed306` |
| 三站最近发布版本 | `0.2.16-custom.5`；`.2` 仅源码标签，未部署 |
| 发布标签 / 运行代码提交 | `custom-0.2.16.5` / `00e8f24a40eba481d1e61cfebb5f4ab3f9401a2b` |
| 最新发布分支 | `codex/v0.2.16-image-studio` |
| 已推送发布文档 | `989594099`；活动工作树 `docs/GATEWAY_RESILIENCE_V0_2_16_5_ACCEPTANCE.md`，验收脚本 `3fed247fa`；文档不改变运行镜像 |
| 本目录源码状态 | `D:/projects/sub2api-custom` 仍是 `codex/isolation-refactor-v0.2.13` / `c9dd2286d`，不能直接用作下次升级起点 |
| 长期集成基线 | 本地远程跟踪引用 `origin/custom/integration` / `fd7c1b91446a38666b7f5c32ab9d493f6431d071`，尚不包含 `.5` |
| 当前隔离改造 PR | 草稿 PR #1（如仍未合并）：`codex/isolation-refactor-v0.2.13` → `custom/integration` |
| 官方远程 | `upstream = Wei-Shaw/sub2api` |
| Fork 远程 | `origin = Hzihuan001/sub2api` |
| 三站生产镜像 | `ghcr.io/hzihuan001/sub2api@sha256:3a2f925574266819adc3cddad9595170cd71404519f315326f83d61098b6a80b` |
| 当前发布状态 | `deployed`：主站、L1、COS 已部署 `.5`；healthy、重启 0，内外健康与静态入口均为 200，无关键启动错误 |

### 本次交接结论与待办（`.5`）

- **实现 / 验证**：`gateway-resilience` 已登记升级保护。相关 Go 回归及 Docker 11 场景通过；正式镜像回拉一致。原调度权重、分组权限和计费保持，无新迁移。无健康备用时仍会失败，已输出流不保证无缝续写。
- **备份 / 回滚**：`/opt/sub2api-backups/v0.2.16-custom.5-20261014T120809Z` 的三站 dump、数据 tar、部署配置和 SHA 校验通过；回退 `.4` digest `sha256:a4b7d421e233a1484c92b02780fd2a2421d4ea450fc2a184c82ac253289c045e`，只退应用不恢复旧库。
- **新会话起点**：从包含 `custom-0.2.16.5` 的活动发布分支继续，先读其 `GATEWAY_RESILIENCE_V0_2_16_5_ACCEPTANCE.md`。根目录旧源码不能发布；Actions 恢复和落后的集成分支待后续处理。本次使用本地门禁直发，不声称 CI 通过。

### `.4` 发布交接（历史，已被 `.5` 包含；不是当前回滚目标）

- **已完成**：在 v0.2.16 和 Nano Banana 热修复基础上，取消所有新批量任务的额外折扣；管理员创建、更新、复制分组均将批量折扣归一为 1，冻结比例至少为 1。v2 usage 记录倍率前基础费用，实扣仍使用提交快照中的既有分组、用户和账号倍率。
- **数据库验收**：三站均存在 1 条 `9000_custom_batch_image_no_discount.sql` 迁移记录。主站/L1/COS 的 36/12/12 个分组折扣全部为 1、冻结比例全部至少为 1；主站 17 个旧任务的价格快照及已结算 actual 金额未变，L1/COS 无旧任务。
- **测试 / 发布依据**：本地最终镜像的三图 `$0.03`、并发、参考图、ZIP 与单张下载验收全部通过；代码、`custom-0.2.16.4` 标签和同一镜像 digest 已推送。三站生产验收在 15:52–15:54（Asia/Shanghai）完成；详细发布文档已提交并推送为 `dde88cd3dd05907d0c13a37258863e15b5c2d1d6`，活动工作树 clean。
- **备份 / 回滚**：`/opt/sub2api-backups/v0.2.16-custom.4-20261014T074350Z` 的 dump 目录、tar 与 SHA 校验已验证。回滚使用 `.3` digest `sha256:a96e2a5a52e0d7a83a9829371715359b2afcc0a02168fe8bded77b55dcfdeec3`，只回退应用、不恢复数据库；保留迁移后分组折扣 1 和旧任务快照，不撤销本次数据规范化。
- **后续维护，不影响本次部署完成**：GitHub Actions 仍 disabled，本次通过本地门禁直接发布，不能写成 CI 已通过。集成分支尚未纳入最新发布；后续另行恢复 Actions，并在检查其他功能会话提交后推进集成基线，本次未修改这些远程设置。
- **新会话起点**：先读最新发布工作树共享文档，核对提交与未提交改动；从包含 `custom-0.2.16.4` 的基线继续。保留新旧计价快照、历史不补扣、超额返回不加收，以及目录兼容和白名单边界的回归测试。根工作区 v0.2.13 源码不作为发布起点。

生产健康检查最近确认通过的入口：

- `https://ai.moshu.cloud/health`
- `https://api.moshu.cloud/health`
- `https://cos.moshu.cloud/health`

旧下游入口 `api.tokenpulse.top`、`cos.tokenpulse.top` 暂保留作为对照。2026-10-12 域名迁移时，新下游域名在 OVH OpenResty 上使用已签发的 SAN 证书，Cloudflare DNS 状态为 `Proxied`；该次域名操作没有变更应用镜像或数据服务，后续应用发布以本页最新快照为准。

生产发布必须继续使用通过验收的同一镜像 digest，不在不同服务器重新构建。

## 2. 当前定制功能边界

定制功能使用稳定的 feature ID 和独立设置命名空间，定义在
[`custom/manifest.yml`](../custom/manifest.yml) 和
[`backend/internal/custom/features.go`](../backend/internal/custom/features.go)。

| feature ID | 功能边界 | 主要代码边界 | 当前状态 |
| --- | --- | --- | --- |
| `operator` | operator 角色、管理端固定权限、菜单/路由过滤、权限管理 | `backend/internal/authz`、`backend/internal/server/middleware`、`frontend/src/authz`、管理员视图 | 保留并受测试保护 |
| `prompt-audit` | Prompt Audit、`capture_only`、事件列表/详情/筛选/导出/清理 | `backend/internal/securityaudit`、`frontend/src/features/prompt-audit` | 保留并受测试保护 |
| `image-studio` | 生图工作台、批量任务、应用层拆分、参考图、单张/ZIP 下载 | `backend/internal/service/batch_image_*`、`frontend/src/features/image-studio`、用户生图视图 | 保留并受测试保护 |
| `branding` | 对客品牌和公共文案统一为 Moshu，隐藏不应出现在对客界面的上游项目名称 | `frontend/src/i18n`、公共页面和品牌处理逻辑 | 保留；每次升级需做文案扫描 |
| `usage-extras` | 管理员使用记录增强，例如缓存命中率、分页和 API Key 管理辅助能力 | 管理员使用记录 handler/view 及对应测试 | 保留；管理员侧可见，用户侧按权限隐藏 |

历史代理商/分站运行时功能已从当前定制层移除，不应在后续官方升级时被旧分支或旧 PR 自动带回。历史代理商阶段只在
[`CUSTOM_FEATURE_LOG.md`](CUSTOM_FEATURE_LOG.md) 中留作变更记录。

## 3. 为什么以后合并会明显容易

> **流程生效前提（2026-10-14）**：下面描述的是已实现的升级机制，不代表自动任务当前正在成功运行。Actions 恢复且 `custom/integration` 确认包含最新已验收功能之前，不得直接依赖定时升级或从旧集成基线开始；第 4、5 节同样受此条件约束。

是的，和过去直接在生产分支上叠加定制代码相比，下一次官方主线更新的合并难度会大幅降低，原因是：

1. 官方代码、长期集成基线和临时升级分支已经分离。升级前先确认 `custom/integration` 包含最近已验收定制；若落后，明确选择最新已提交基线并记录待回补内容，不能从过期分支开始或整合历史旧分支。
2. `.github/workflows/upstream-sync.yml` 会定期或手动选择稳定 `vX.Y.Z` 标签，自动创建 `upgrade/upstream-vX.Y.Z` 分支和草稿 PR。
3. 上游 merge 会先单独提交，再处理定制补丁；每个边界可用 `range-diff` 检查，冲突不会被悄悄吞掉。
4. `scripts/check-custom-isolation.py` 会检查生成代码、迁移是否 append-only、feature manifest 是否同步，以及受保护路径是否仍存在。
5. 工作流默认只读权限，明确固定 Go、Node、pnpm 版本，并在 PR 中留下冲突、生成、测试和 lint 结果。
6. `custom/manifest.yml` 和 `.github/custom-features.json` 为定制功能提供稳定 ID，不再依赖显示名称、路由名称或数据库表名作为升级识别依据。
7. 自动同步流程没有部署权限。没有通过本地 Docker 验收、镜像质量门禁和人工发布确认，就不会改变任何生产环境。

这意味着“机械合并和基础回归”已经自动化，但不是零风险。以下情况仍需要人工判断：

- 官方改动了同一权限、菜单、Prompt Audit 或生图宿主边界；
- 官方新增或重排数据库迁移；
- 官方改变 API 请求/响应契约、生成代码或配置默认值；
- 官方删除了受保护功能需要的接口或资源；
- 定制功能的业务语义需要重新确认，而不是仅解决文本冲突。

## 4. 官方发布后的标准流程

### 4.1 自动准备升级 PR

自动工作流每周检查稳定 `v*` 标签，也可以在 GitHub Actions 手动输入目标标签。等草稿 PR 生成后，先看：

- PR 的上游标签和 commit；
- `sync-report.md`、`sync-merge.log`、`range-diff.txt`；
- 生成代码、迁移、后端、前端和 Compose 检查结果；
- 是否出现 `blocked`、缺少 patch branch 或受保护路径缺失。

本地等价操作（只生成报告，不修改工作树）：

```powershell
pwsh -NoProfile -File scripts/sync-upstream.ps1
```

需要真正生成本地升级分支时，必须明确指定官方标签并执行检查：

```powershell
pwsh -NoProfile -File scripts/sync-upstream.ps1 `
  -UpstreamTag vX.Y.Z -Apply -RunChecks
```

### 4.2 冲突处理原则

1. 只在 `upgrade/upstream-vX.Y.Z` 或其本地副本处理冲突；不要直接改 `custom/integration`，不要在生产服务器修代码。
2. 优先保留官方新行为，再通过定制宿主边界恢复需要保留的 Moshu 功能。
3. 生成文件按官方重新生成，不手工把旧生成结果覆盖回去。
4. 迁移只允许追加；不改已发布迁移校验值，不删除生产数据，不用旧备份覆盖新数据。
5. 每个功能冲突修复后补对应单元/集成/前端测试，并在日志中记录文件、行为和回滚影响。
6. 只有 PR、质量门禁和本地 Docker 联合验收全部通过，才允许进入镜像构建。

### 4.3 发布顺序

1. 合并升级 PR 到长期集成基线。
2. 创建自定义标签，例如 `custom-0.2.14.1`，版本文件对应 `0.2.14-custom.1`。
3. 等 CI 构建 GHCR 镜像并记录不可变 digest。
4. 拉取同一 digest 回本地做启动、健康和关键接口检查。
5. 先在测试站验证，再按批准顺序更新生产站；只重建应用容器，保留数据库、Redis、卷、网络和代理配置。
6. 生产异常立即切回发布前记录的旧 digest；若发生不可逆官方迁移，先停止并按迁移回滚预案处理，不能只切镜像。

## 5. 当前自动化的限制与后续优化

当前 `.github/upstream-sync-manifest.yml` 的 `patch_branches` 仍为空。这是有意的安全状态：旧历史分支混有部署和合并记录，不能直接当作可重放补丁。当前自动化可以合并上游、做保护检查和生成报告，但不会假装已经具备完整的自动补丁栈。

目前受保护路径重点覆盖 `authz`、`securityaudit`、前端 `authz` 和生图工作台；`branding`、`usage-extras` 和功能注册表虽然有 manifest/测试约束，但还没有全部纳入 protected path 列表。升级 PR 必须对这几个目录做人工 diff 检查，直到它们被重建为独立补丁并加入保护清单。

下一步若要继续降低冲突，应按下面顺序逐个重建补丁分支：

1. 从 `custom/integration` 创建只包含一个功能域的干净分支；
2. 将该功能拆成“实现、测试、必要迁移”三个可审查边界；
3. 通过本地 Docker 验收和 CI 后，才把分支加入 `patch_branches`；
4. 每加入一个补丁分支，都更新 `CUSTOM_FEATURE_LOG.md` 和本文件的功能边界。

在补丁栈重建完成前，官方升级 PR 仍需人工检查，但已经不需要从几十个历史提交中重新猜测定制内容。

## 6. 给下一个维护会话的开场检查清单

新会话开始修改前，按以下顺序读取和确认：

1. 本文件和 [`CUSTOM_FEATURE_LOG.md`](CUSTOM_FEATURE_LOG.md)；
2. [`CODEX_HANDOFF.md`](../CODEX_HANDOFF.md)；
3. [`docs/UPSTREAM_SYNC.md`](UPSTREAM_SYNC.md)；
4. [`custom/manifest.yml`](../custom/manifest.yml)；
5. [`.github/upstream-sync-manifest.yml`](../.github/upstream-sync-manifest.yml)；
6. [`backend/internal/custom/features.go`](../backend/internal/custom/features.go)；
7. 当前分支、版本文件、最近标签、工作树状态和远程分支；
8. 相关 feature 的测试，而不是只看页面是否能打开。

会话必须先报告：当前官方基线、定制版本、待合并分支、是否有未提交修改、是否需要数据库迁移，以及是否允许部署。任何秘密只从本地秘密配置或服务器安全环境读取，不写入文档、日志或 Git。

## 7. 多会话协作协议

本项目允许同时开多个会话处理不同定制，但所有会话共享同一套文档上下文。文档分工如下：

| 文档 | 用途 | 是否追加历史 |
| --- | --- | --- |
| `CUSTOM_PROJECT_STATUS.md` | 当前分支、版本、正在进行的任务、阻塞点和下一步 | 否；始终维护为最新快照 |
| `CUSTOM_FEATURE_LOG.md` | 已完成定制、官方升级、测试、发布和回滚证据 | 是；只追加，不改写旧条目 |
| `custom/manifest.yml` | 当前允许存在的定制 feature ID、测试边界和迁移策略 | 只在功能边界真实变化时修改 |
| `.github/upstream-sync-manifest.yml` | 官方升级基线、补丁顺序和受保护路径 | 只在补丁栈或保护策略经过验收后修改 |

### 每个新会话开始前

1. 先读取本文件、`CUSTOM_FEATURE_LOG.md`、`CODEX_HANDOFF.md`、`UPSTREAM_SYNC.md`、`custom/manifest.yml` 和 `.github/upstream-sync-manifest.yml`。
2. 检查当前分支、工作树、最近标签和远程状态；不要假设上一个会话已经推送或部署。
3. 明确本会话的 `feature ID`、目标分支、是否允许生产操作，以及当前是否有其他会话正在修改同一功能。
4. 如果发现状态文档、代码和 Git 历史不一致，以代码和 Git 为准，先修正状态快照，再开始修改。
5. 同一分支只允许一个写入会话；其他定制使用独立的 `codex/<feature>` 分支。发现工作树有其他会话未提交修改时先停，不覆盖、不重置、不清理。

### 定制功能会话结束前

1. 在 `CUSTOM_PROJECT_STATUS.md` 更新当前分支、commit、PR、测试、未完成事项和阻塞原因。
2. 在 `CUSTOM_FEATURE_LOG.md` 追加一条完成记录；如果只完成部分工作，明确写“未发布/未部署”和剩余事项。
3. 把新增 API、页面、配置、迁移和测试路径写清楚，避免版本更新会话重新扫描整个仓库猜测影响范围。
4. 不把凭据、API Key、服务器秘密、真实用户数据或完整 `.env` 写入文档。
5. 状态统一使用 `planned`、`in-progress`、`verified`、`deployed`、`blocked`、`rolled-back` 之一，避免“做过但未验收”被误认为完成。

### 版本更新会话的职责

版本更新会话只消费已登记且可验证的定制边界：以 `custom/integration` 为基线，通过自动升级 PR 合并官方版本，并按日志中的测试和发布证据复核功能。它不能把其他会话聊天内容当作已完成代码，也不能把旧历史分支或已移除代理商功能重新套回当前版本。

如果某个定制会话尚未完成，版本更新会话必须将其标为阻塞或待复核，不得静默覆盖；如果官方升级和定制会话同时修改同一文件，应先暂停其中一个会话并记录冲突归属。

### 会话之间的最小同步信息

每次交接至少要能从文档中回答：

- 正在处理哪个 feature ID；
- 基于哪个官方/custom 版本和分支；
- 最近一个 commit/PR；
- 已通过哪些测试；
- 是否产生迁移或配置变化；
- 是否构建、发布或部署过；
- 下一步和回滚方式是什么。

每次交接在状态文档中保留一段简短摘要：最后更新时间、变更会话/作者、当前状态、下一步、阻塞原因和验证证据链接。这样新会话无需重新阅读完整聊天记录即可继续工作。

