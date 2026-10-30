# 定制功能日志


### 2026-10-30 — 未发布 — 深度解耦方案追加三轮审核

- **审核范围**：第 5 轮边界/主线冲突、第 6 轮运行时/数据/调度、第 7 轮测试/发布/证据链。
- **关键结论**：鹈鹕候选及 `9005` 迁移尚未登记，不能进入升级分支；真实执行必须携带目标分组；失败计划必须退避且使用 lease/CAS 与幂等 `run_id`；展示接口需要批量读取或受控缓存；Compose、旧镜像、迁移、SSE 和敏感信息检查必须成为发布门禁。
- **文档**：详见 [`CUSTOM_MODULARIZATION_REVIEWED_PLAN_20261030.md`](CUSTOM_MODULARIZATION_REVIEWED_PLAN_20261030.md) 第 5–8 节。
- **状态**：本轮只更新方案和共享文档，无代码发布、镜像构建或生产部署；当前三站仍为 `0.2.19-custom.2`。


### 2026-10-30 — 未发布 — 鹈鹕测智改为按分组归属并启动深度模块化规划

- **需求**：用户端展示的测试结果必须属于具体分组，不能因为一个账号属于多个分组而重复展示；同时继续降低定制功能与官方主线的合并冲突。
- **Feature ID**：`pelican-showcase`；关联 `image-studio`、`key-routing`、`operator` 的解耦工作只做现状记录，本轮未迁移这些功能。
- **官方基线**：当前工作树基于 `v0.2.19` 定制代码；生产仍为 `0.2.19-custom.2`。
- **实现边界**：定时测试计划增加分组归属、测试类型和提示词；鹈鹕展示服务按 `plan.group_id` 聚合；管理员面板提供目标分组、Cron 间隔和提示词设置；普通连接测试保持账号级行为。
- **数据库/迁移**：新增候选迁移 `9005_custom_pelican_scheduled_test_kind.sql`，追加 `test_kind`、`test_prompt`、`group_id` 及索引；尚未在生产执行。
- **测试**：前端类型检查、生产构建、i18n 4 项和 `git diff --check` 通过；Go 后端编译、真实 PostgreSQL 迁移、Docker 验收待具备 Go/Docker 环境后执行。
- **提交/PR**：仅本地未提交，无 PR。
- **发布镜像**：无。
- **部署站点**：未部署；三站继续运行 `0.2.19-custom.2`。
- **回滚**：候选未进入生产，不涉及生产回滚；正式发布仍以 `0.2.19-custom.2` 为回退基线。
- **备注**：深度解耦计划见 [`CUSTOM_MODULARIZATION_NEXT_PHASE_20261030.md`](CUSTOM_MODULARIZATION_NEXT_PHASE_20261030.md)。当前鹈鹕模块仍依赖官方定时测试、账号测试和管理员面板，后续需抽取 `PelicanTestAdapter`、feature contract 和独立测试计划端口。


### 2026-10-29 — 0.2.19-custom.2 — 官方角色路由兼容修复

- **现象**：0.2.19 发布后页面空白，服务和静态资源均正常。
- **根因**：主线新增 `/admin/roles`，与 `operator` 定制功能重复注册；路由资源校验在 `app.mount('#app')` 前抛错。
- **改动**：路由适配层复用同路径、同名称的官方路由；不同名称冲突仍拒绝启动。这样后续主线吸收定制页面时无需删除定制 manifest，保持模块边界。
- **验证/发布**：类型检查通过，角色路由集成测试 31/31，通过镜像 `0.2.19-custom.2` 发布到主站、L1、COS；三站健康 200、容器 healthy/restart 0。


### 2026-10-29 — 0.2.19-custom.1 — 主线合并与生产发布

- **需求**：合并官方 v0.2.19，保留定制化功能，并完成三站发布。
- **Feature ID**：`operator` / `prompt-audit` / `image-studio` / `branding` / `usage-extras` / `key-routing`。
- **官方基线**：`v0.2.19` / `dece26153621959ccd7635814f4b2d08304e681a`；基线 `custom/integration` `86ae08dbb450eb1090c2a076c3d8da7865ac7cf7`。
- **实现边界**：官方 API Key 管理、图片按张计费、Grok/Gemini 目录、迁移 247/248；冲突集中在 Ent Group 字段、账号处理器兼容构造、模型同步组件和 OpenAI 网关，采用最小手工兼容补丁，保留角色、审计、生图、路由、直连和品牌层。
- **数据库/迁移**：保留官方新增迁移，使用现有数据库追加迁移；三站已核对 `247_scoped_admin_api_keys.sql`、`248_group_image_count_billing.sql`；未执行破坏性降级。
- **测试**：后端全包编译、前端类型检查、模型同步/Key 编辑 63 项、Docker 前端 i18n 4 项、三站隔离 Compose 健康检查通过。
- **提交/PR**：`99d8b8f63`、`a02e2f1b7`；分支 `codex/v0.2.19-merge-20261029` 已推送，`custom/integration` 已快进，标签 `custom-0.2.19.1`。
- **发布镜像**：`ghcr.io/hzihuan001/sub2api:0.2.19-custom.1` / `sha256:6f2e1c26aa491b67a038dd8ae98f9cc092c3aed5c38df8eed571ba7f388b5621`。
- **部署站点**：`ai.moshu.cloud`、`api.moshu.cloud`、`cos.moshu.cloud`；容器 healthy/restart 0，公网首页和 `/health` 均 HTTP 200。
- **回滚**：应用旧摘要 `sha256:80adc97b50213ba840e68d6c13b932701cff294f04fb5dcd291ffe12b63f49e6`；保留本轮新增迁移和业务数据。
- **备注**：Docker 构建曾发现并修复 `discovery_source` 前端 API 类型遗漏；当前工作树已清洁，后续合并优先复用此分支边界。

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


> **共享文档已推送（2026-10-26）**：文档提交 `b2ec356187dc74214c38a1a47e47b011228df074` 已同步工作分支与远端 `custom/integration`；[最新日志](D:/projects/sub2api-image-batch-16refs-20261026/docs/CUSTOM_FEATURE_LOG.md)已收录 `.18.3` 当前功能及[此前安全清理记录](D:/projects/sub2api-image-batch-16refs-20261026/docs/CUSTOM_MAINTENANCE_20261026.md)。本次没有代码或生产变更，运行标签不动。


> **2026-10-26 批量参考图最新共享入口（已部署）**：三站已运行 `0.2.18-custom.3`，GPT-image单生图、批量整组统一参考图、批量逐条参考图均最多16张；其他模型仍按各自上限。运行源 `c2dbbe786e844ee1df77bfd710ebaf438f625126`，标签 `custom-0.2.18.3`，正式镜像 `sha256:cb1c937d398989ccff2256b0bf7a7a51c7b63405f8dd5a267a1cd50f47e7b7ac`。发布分支 `codex/image-batch-16refs-20261026` 与远端 `custom/integration` 已普通快进至文档收尾 `c8d14a1183f73cdefa28e698f45bbaf8f8c730b5`。
>
> 本轮记录位于 `D:/projects/sub2api-image-batch-16refs-20261026`：[项目状态](D:/projects/sub2api-image-batch-16refs-20261026/docs/CUSTOM_PROJECT_STATUS.md)、[功能日志](D:/projects/sub2api-image-batch-16refs-20261026/docs/CUSTOM_FEATURE_LOG.md)、[发布验收](D:/projects/sub2api-image-batch-16refs-20261026/docs/CUSTOM_RELEASE_0_2_18_CUSTOM3_20261026.md)。前端92项、后端unit及正式镜像16图mock验收通过；三站healthy/restart0、批量入口3/3与应用资源12/12匹配。无后端运行时/迁移/配置变化，应用回退目标`.18.2`。旧树仅追加此共享指针，原维护记录、其他会话未提交代码及文档完整保留；后续先核对最新远端，不从旧树整树覆盖发布。

> **2026-10-26 运维收尾共享入口**：生图 `0.2.18-custom.2` 的当前摘要已补齐；本地52个停止容器已清理，运行容器和全部卷保留。服务器编译/包缓存减少约0.92GB，未重启生产，三站健康200。[完整记录](D:/projects/sub2api-image-studio-16refs-20261026/docs/CUSTOM_MAINTENANCE_20261026.md)维护于最新权威树，旧阶段历史保留。


> **2026-10-26 生图工作台最新共享入口（已部署）**：三站已运行 `0.2.18-custom.2`，GPT-image参考图上限16；Nano/Gemini14、Seedream10和未知模型4保持。运行源 `879a99a22e47a303604927104a6f67329dcd3b38`，标签 `custom-0.2.18.2`，正式镜像 `sha256:c90609bc4546ec309a980929ec8f28eef3cef236022dd698700920439ce78412`。发布分支 `codex/image-studio-16refs-20261026` 与远端 `custom/integration` 已普通快进至文档收尾 `10b452931592de432db312ffc0a94972c1b89666`。
>
> 本轮权威记录位于 `D:/projects/sub2api-image-studio-16refs-20261026`：[项目状态](D:/projects/sub2api-image-studio-16refs-20261026/docs/CUSTOM_PROJECT_STATUS.md)、[功能日志](D:/projects/sub2api-image-studio-16refs-20261026/docs/CUSTOM_FEATURE_LOG.md)、[发布验收](D:/projects/sub2api-image-studio-16refs-20261026/docs/CUSTOM_RELEASE_0_2_18_CUSTOM2_20261026.md)。三站healthy/restart0，工作台入口3/3、相关JS资源9/9与验收镜像一致；无后端运行时/迁移/配置变化，兼容应用回退目标`.18.1`。本旧工作树仅追加此共享指针，其他会话未提交修改完整保留；下方旧发布坐标为历史，后续先核对最新远端，不从旧树整树覆盖发布。

### 2026-10-26 — 既有代码改造与发布进度同步（最新共享入口）

- 完整记录以权威树 `C:/Users/a/.codex/worktrees/group-routing-current/docs/` 的[状态](C:/Users/a/.codex/worktrees/group-routing-current/docs/CUSTOM_PROJECT_STATUS.md)、[功能日志](C:/Users/a/.codex/worktrees/group-routing-current/docs/CUSTOM_FEATURE_LOG.md)及[`.18 发布报告`](C:/Users/a/.codex/worktrees/group-routing-current/docs/CUSTOM_RELEASE_0_2_18_CUSTOM1_20261024.md)为准，活动分支 `codex/v0.2.18-merge-20261023`。本目录保留其他会话内容，只同步共享入口。
- 已发布链路：`.13` 账号级避让、授权目录并集、完整优先级/管理员 Key 编辑 → `.14` 精确切组错误标签和受限 Responses 恢复 → `.16` 明确引用拒绝后的窄恢复 → `.18` 官方升级且保留七个定制模块。当前功能摘要已补齐 `account_v2`、迁移 `9000–9004` 与实际组原生计费；下方各阶段“未发布”保留为历史。没有把半开探测或实验性 `replay/` 写成已实现。
- 最近生产记录为 2026-10-24 三站统一部署 `0.2.18-custom.1`，运行源 `fc0d798bf2d6a08a074970d0485c567291e02660`、标签 `custom-0.2.18.1`，digest `sha256:d7f365d1ba40b6c159d4155f7fbc2773d01f0fe6015af7906ebbece500ba1837`；核对前文档基线为 `e5d4e8c16`。本次仅文档对齐，没有新增代码、配置、部署或测试结论，不移动运行标签。

### 2026-10-22 — 切组错误精确标记（开发共享指针，未发布）

- 最新补丁只在权威树 `C:/Users/a/.codex/worktrees/group-routing-current`：实际触发切组的已有事件写入目标组，错误详情独立显示；不把历史切组标签贴到后续备用组或本地错误。复用JSON，无迁移、无新增使用字段/错误行，计费和原统计不变。
- 后端定向、前端39项/类型/lint、模块契约及18项测试已通过。源码和详细记录见权威树同名文档；未提交、推送、构建或部署，生产和本地验收容器仍`.13`。本目录仅同步指针，其他会话源码未改。

### 2026-10-22 — `.13` 账号级Key路由与管理员完整编辑已发布（共享指针）

- 正式运行发布后，8份权威共享文档的收尾提交 `daf9153f438918acc8d1d46359ddfe2d0a1a88b1` 已推送工作分支及远端 `custom/integration`；运行标签未移动。后续会话同步远端后再开发，不以旧本地集成ref判断当前进度。
- 主站/L1/COS于北京时间13:35–13:39完成同一正式镜像部署，版本 `0.2.16-custom.13`，运行 `90804511dcfd57d9e99d7df470cfa05282182664`，标签 `custom-0.2.16.13`，digest `sha256:9d79c4378f3e4dc6dd4d1648cff7420947ccb7984a33d7bb2cbd11030c32c14f`。运行源码、集成分支/标签和GHCR已推送，非仅文档更新；文档收尾不移动运行标签。
- 本轮账号级避让、模型目录、优先级编辑及管理员复用完整Key编辑器一起上线；正式镜像本地33/33+5/5通过、48项资源清理成功。生产healthy/restart0，备份已验证，配置/数据服务保持、旧迁移全行不变，仅新增9003/9004，18份关键JS源站/公网SHA匹配。没有生产付费调用或实际回退；Actions仍422，走批准本地GHCR流程，不宣称CI、全provider或容量验收。
- 权威记录只在 `C:/Users/a/.codex/worktrees/group-routing-current/docs/` 的状态、日志、`.13`发布报告和同步/验收文档维护；下一会话从最新`custom/integration`继续。回退`.12`的account_v2语义限制已记录，不恢复旧库。本根目录仅更新共享指针，不覆盖其他会话源码；下方“未发布”是历史阶段。

### 2026-10-22 — 用户管理复用完整 Key 编辑器（开发共享指针，未发布）

- 权威活动树 `C:/Users/a/.codex/worktrees/group-routing-current` 已补完整管理员编辑入口，复用用户端共享表单与原生更新服务，保留所有者/订阅校验、用量及旧scope；修正切用户和迟到响应隔离。
- 前后端定向、类型/lint、模块契约/构建、本地真实HTTP5/5和浏览器保存重开通过。三站当前候选 `sha256:3be7acfaafd35211db0d2a0c927246bb3956ea73a7c8035110635e35a078b6f5`，入口18280–18282；原路由长连接证据属于此前候选。验收详情和源码边界见权威树同名文档及 `CUSTOM_KEY_ROUTING_STABILITY_ACCEPTANCE_20261022.md`；待用户人工验收，未推送/部署，无真实模型调用。

### 2026-10-22 — Key 账号路由稳定性收尾（开发共享指针，未发布）

- 最新活动树为 `C:/Users/a/.codex/worktrees/group-routing-current`，由归档快照 `4ca21d066` 恢复。修复无头断线与插件未发送证明的区分、SSE 回归断言及 1025+ 候选健康批读；测试/候选/本地验收证据维护在该树 `docs/CUSTOM_KEY_ROUTING_STABILITY_ACCEPTANCE_20261022.md`。
- 后端四包完整测试、runtime 边界、模块契约及前端类型/构建通过；Docker 路由账务 33/33、共享状态 11/11、三站 660 秒 SSE 3/3、稳定版往返 6/6 通过。`18280–18282` 已保留供人工验收。生产不变，用户本地验收前不推送或部署；不冒称全部 provider 或生产容量验证。本根目录的文档页面等其他会话改动未覆盖。

### 2026-10-20 — 已发布 `.12` Key 编辑器与独立故障切组（共享指针）

- 版本 `0.2.16-custom.12`、运行 `f42b02f9ea0f43c3e34d9bbf778cd9e450cb05b4`、标签 `custom-0.2.16.12`、digest `sha256:e6a53ec608abd463bfa41ccc6e939dca0175756adeb40e957ebe31a6b68c3610` 已完成主站/L1/COS 同镜像发布；原公共使用文档和生图等定制保留。
- 独立模块实现主备选择、TTFT/SLA/故障规则、模型限制与安全重放，按实际组原生计费；不再因自定义规则全局停账号。正式镜像主站 13/13、完整 service、前端 147 项通过；生产有限复核 healthy/restart 0、公共 JS 匹配源站，配置/数据服务不变，仅增迁移 9001/9002。
- 本次 Actions 实际 dispatch 422，未运行 CI；无生产付费模型，L1/COS 本地完整登录矩阵未执行。目录仍主组、计数重启清零和旧镜像忽略模型限制等边界均在报告中保留。
- 完整日志/发布/回滚只维护在活动树 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom/docs/` 下 `CUSTOM_FEATURE_LOG.md`、`CUSTOM_PROJECT_STATUS.md`、`CUSTOM_RELEASE_0_2_16_CUSTOM12_20261020.md`；此目录仅更新共享指针，不重置原预览源码，也不从旧树发布。

### 2026-10-16 — 已发布 `.8` 公共使用文档与 Key 提示删除

- 本目录最新版八工具教程、菜单/手机单选择器、CC Switch 下载页和 14 张图片已作为独立补丁移植到最新活动分支，连同 Key 范围提示删除发布；未整体复制旧 router，模块化角色入口保留。
- 版本 `0.2.16-custom.8`，标签 `custom-0.2.16.8`，运行 `46083426af6cf993b0dfd86767fc35df9977dd22`，digest `sha256:7eb13edccde298ea2b57721fd2581c8552291b81c65268c1b3f812a073d7647b`。源码、标签和镜像已推；主站/L1/COS 于北京时间 01:11–01:13 顺序部署完成，最终 healthy/restart 0，配置/数据服务/迁移保持，三站新文档及 14 图真实资源校验通过。
- 前端 106 项、类型/lint/构建、角色容器复验通过；首轮 PG 就绪竞争导致日志检查失败的记录保留，本次合成项目已回收。三站备份 verified、旧 `.7` 镜像保留，未回滚、未付费请求。
- 完整证据只维护在 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom/docs/CUSTOM_RELEASE_0_2_16_CUSTOM8_20261016.md` 及同目录状态/日志。本目录旧源码未重置，不从这里发布；下方“仅本地”是原预览阶段历史状态。

### 2026-10-16 — 本地已验证（verified）— 菜单顺序调整及 CC Switch 下载入口

- **范围**：公共文档 `branding` 后续调整；仍在本根目录旧文档预览分支，版本/发布基线不变。只改 `DocumentationView.vue`、对应测试和共享文档。
- **菜单**：删除“接入其他工具”顶层入口，八工具顺序改为 Codex、Claude CLI、Claude Desktop、WorkBuddy、Trae、Cherry Studio、Zcode、OpenCode；桌面图标列表与手机单一文档下拉保持同序。手机可直接在创建令牌 / CCSwitch / 八工具间跳转，原 `/docs/tools` 默认 Codex 与深链接保持。
- **下载**：CCSwitch 的“安装 CC Switch”改为用户指定的 [CC Switch 下载页](https://ccswitch.io/zh/download)，移除 v3.18.0 固定平台直链，保留安全外链属性；未安装或下载客户端。其余教程内容和图片未改。
- **验证**：真实 mount + memory router **20/20**、定向 ESLint、`vue-tsc --noEmit`、`git diff --check` 通过；浏览器核对桌面菜单和下载 href，390px 真实单下拉选择 Claude Desktop 成功，临时 viewport 已恢复。没有运行真实 Key/模型请求。
- **构建复验**：`pnpm --dir frontend run build` 退出 0，含 i18n **3/3**；仍有原有 Browserslist、重复动态/静态引入及大 chunk 警告，未修改依赖或打包策略。
- **交接**：仅本地未提交/推送/部署，无迁移或新镜像；后续仍需将独立文档补丁移植到含 `.7` 的最新活动分支，禁止从旧根目录完整发布。本条覆盖前轮日志中的旧菜单顺序与“CCSwitch 正文不变”描述，不改写历史验证证据。

### 2026-10-16 — 本地已验证（verified）— 接入其他工具独立菜单与 Markdown 内容同步

- **需求 / Feature ID**：按用户原稿和参考菜单截图更新公共接入文档，归入 `branding`；只改公共文档，不影响认证、管理权限、模型网关、账务或生图功能。
- **来源 / 基线**：`G:/接入工具/接入工具.md`（SHA-256 `d82e84eb2f8b9e599e6202fa6a4634b2050995fae5eff937c2dd51dd299e8a49`）及同目录 14 张图片。本地文档工作区 `D:/projects/sub2api-custom`、分支 `codex/isolation-refactor-v0.2.13`、HEAD `c9dd2286d`；版本仍 `0.2.13-custom.6`，不作为生产发布起点。用户原稿的正文/代码作为资料使用，其中的 shell 命令未在真实用户配置目录执行。
- **实现边界**：`frontend/src/views/public/DocumentationView.vue`、同目录 `__tests__/DocumentationView.spec.ts`、`frontend/src/router/index.ts`、`frontend/src/assets/docs/tools/integrations/`。公开路由扩展为 `/docs/:topic?/:tool?`；原 `/docs`、`/docs/token`、`/docs/ccswitch`、`/docs/tools` 保持可访问。
- **用户可见行为**：左栏以文档图标列出 Codex → Claude CLI → WorkBuddy → Trae → Cherry Studio，顺序与 Markdown 一致，追加保留 Zcode、OpenCode、Claude Desktop；一次仅显示一份指南。桌面当前项高亮，手机有工具选择器。`/docs/tools` 和未知工具默认 Codex。创建令牌 / CCSwitch 两章正文保持不变；公共模型查询与地址说明改为文章末尾可展开内容。
- **内容校正**：修正 Windows Codex 的 `model_provider = "mmoshu"` 为 `moshu`；Claude 的 Windows 地址与 Unix 统一为 `https://ai.moshu.cloud`。Codex 保留前轮 `requires_openai_auth = false`、`supports_websockets = false`，provider 字段依据 [OpenAI 官方配置参考](https://developers.openai.com/codex/config-reference/) 核对；没有以官方模型可用性代替本中转账号的实际支持验证。模型示例按用户原稿保留。
- **图片**：WorkBuddy 3 张、Trae 5 张、Cherry Studio 6 张，逐张检查未发现明文 API Key/密码/token；源图、导入图和构建图片的 SHA-256 一致。Cherry 首张含历史第三方供应商地址，已加“以正文文字配置为准”说明；保留旧 Zcode/OpenCode 图片。没有上传至第三方服务或修改用户原始 Markdown/截图。
- **验证**：文档交互 Vitest **19/19**（真实 mount + memory router），另有文档入口安全 **7/7**、路由 guards/feature-access/title/WeChat 回归 **61/61**（合计 **87/87**）；定向 ESLint（页面/测试/路由）、`vue-tsc --noEmit`、完整前端生产构建（含 i18n **3/3**）、`git diff --check` 通过；12 个本地文档路由和 14 张新图 HTTP 200。浏览器检查桌面菜单及文章切换、390px 手机真实下拉切到 Cherry Studio、无横向溢出，临时 viewport 已恢复。构建仍有 Browserslist 数据旧、动态/静态重复引入及大 chunk 警告，未冒称全量 lint、全部前端测试或后端/Docker 门禁已执行。
- **提交 / 发布 / 数据**：仅本地，未提交/推送/打标签，无新镜像或部署；无数据库迁移、服务器配置修改和真实付费模型请求。前端预览地址 `http://127.0.0.1:4173/docs/tools`，无本地后端时公共设置/初始化 API 的连接失败属于预览限制，不是生产异常。
- **后续 / 回滚**：发布前将本次文档专属补丁移植至含生产 `.7` 的最新活动分支，再完成该基线验收；不能把本根目录旧源码覆盖到生产。未改变生产，无需生产回滚；本地变更可按独立文档补丁撤回，保留其他会话已有改动。

### 2026-10-15 — 主备 Key 提示删除及生产实测指针

- 活动工作树已移除常驻范围说明，实际限制和计费逻辑不变；21 项前端测试及类型/定向 lint 通过。未推送部署，生产 `.7` 不变。
- 用户创建管理员 Key 167，GPT-稳定→Kimi→DeepSeek。四次生成请求中 Kimi、DeepSeek 普通及 SSE 通过，实际组/倍率/账务正确，总扣费 $0.0007148；GPT 因主组上游 404/502/403 失败且零扣费，未自动修改配置。目录只返回主组，备用模型可直接调用；Key 保留待用户删除。完整记录见 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom/docs/CUSTOM_PROJECT_STATUS.md` 与 `CUSTOM_KEY_ROUTING_20261015.md`，本目录仅共享指针。

### 2026-10-15 — `.7` 已部署三站（当前共享指针）

- 主备 Key 与分组显式默认值已上线，运行 `f36de3d4a` / `custom-0.2.16.7`，发布/集成分支已推 `3ce7de3eb`，文档随后提交同步。GHCR digest `sha256:ec8860583ac3603fadc108e90b2036abf352d395b5035ee8f4cb51d1de66ac42` 本地/OVH 回拉一致；主站/L1/COS 于北京时间 21:42–21:45 顺序完成，最终 healthy/restart 0、版本/提交/digest 一致，旧迁移保持且仅新增主备扩展迁移。
- 独立 Docker 真实账务/额度、重启、`.6` 只读回退与恢复 `.7`、角色及限定本地浏览器检查通过；首次夹具配置失败、草稿未保存及生产无角色登录的边界保留。三站备份核验通过，未触发回滚；旧镜像禁止多组主组编辑/解绑，扩展表保留，不恢复旧库覆盖新业务。
- 完整证据只维护于 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom/docs/CUSTOM_PROJECT_STATUS.md` 和 `CUSTOM_RELEASE_0_2_16_CUSTOM7_20261015.md`。Actions 无新 CI，SSE mixed、15 lint、P4 双跑矩阵及真实升级 PR 限制继续披露，无生产付费模型/长期监控。本轮 a/b 容器停止保留卷/证据，角色项目及临时浏览器代理/空网络回收，其他项目未动。

下方开发和旧版本条目都是历史阶段，不覆盖本条；本根目录源码不参与发布。

### 2026-10-15 — API Key 主备分组模块本地完成

- 在活动工作树新增 `key-routing`，主组模型缺失或账号全暂停时顺序选备用，按实际组计费；没有 Key 级 SLA 规则或跨组重播。独立策略/适配/存储/前端组件及升级接线门禁，扩展表不替换原 `group_id`。
- 后端定向回归、真实 PostgreSQL/Redis、挂载 HTTP/SSE 与实际组计费命令链及前端测试/构建通过；具体范围、后补边界测试与尚待 Docker 应用/浏览器/回退验收见活动工作树 `docs/CUSTOM_KEY_ROUTING_20261015.md`。
- 当前仍未提交、推送、部署，三站生产 `.6` 不变；此前分组默认值编辑器改动保留。唯一源码位于 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom`，本根目录仅更新共享指针。

### 2026-10-15 — 分组规则直接编辑默认值（本地未发布）

- 用户要求去掉“沿用系统 / 自定义”，四类规则直接填写，缺失项预填默认；已有配置不变，清空字段补默认，保存后才生效。打开页面不自动改配置，后台协议与调度不变。
- 编辑器与真实分组页 59 项复验、其他相关 27 项、类型检查、受影响 ESLint、生产构建与 diff check 通过。具体工具链与构建提醒见活动工作树日志；本轮未提交、推送或部署，三站仍 `.6`。
- 维护源码仍为 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom`。本根目录仅交接指针，不从旧源码发布。

### 2026-10-15 — `.6` 三站部署完成（最新共享指针）

- 文档收尾 `3b0145c8abbf57d581f99084e88118ea7e759e3f` 已原子推送至发布分支与 `custom/integration`，两远端 ref 核验一致；15 份仓库文档及本目录两个共享入口同步。仅文档提交，运行标签及镜像不变。

- 运行 `655a16868` / `custom-0.2.16.6` / digest `sha256:cb074f11766d9190f4e5f3479a0750c7a759a562bfc142aa3067a0d4bfbfc42e` 已发布；17:49–17:51 顺序更新主站、L1、COS，17:54 复核健康、重启 0、关键日志 0。主站等待请求归零后重建，未强制中断。
- 本地最终镜像权限/WebSocket/重启验收通过，三站 PostgreSQL、配置/数据/OpenResty 备份校验通过。只重建应用，不改数据库、Redis、卷、网络、反代或历史账目；备份与 `.5` 回滚镜像保留。
- 模块化、取消结算恢复与分组切换规则已上线；新 API Key 主备分组尚未开发。共享状态、发布报告、方案/执行/复核、性能/账务/WS、升级问题/维护说明均以活动工作树最新文件为准，不掩盖 Actions 与原验收限制。
- 完整发布/回滚记录：`C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom/docs/CUSTOM_RELEASE_0_2_16_CUSTOM6_20261015.md`。本根目录源码不参与发布，不批量提交其旧改动。

### 2026-10-15 — `.6` 源码已推送，发布通道阻塞（共享指针）

- 准备提交 `d2476c39f` 已推分组规则分支；本轮模块化、取消结算和分组规则均有本地验收，尚未部署。
- 正式工作流返回 Actions disabled / HTTP 422；未重复测试、未改生产。等待确认本地 GHCR 发布方式；实际步骤和限制见活动工作树共享文档。

### 2026-10-15 — 停止 SSE 性能追查（共享指针）

- 用户接受 SSE 未决性能风险，不再以此阻塞当前工作，不追加诊断或压测。原始结果保留，不改写为性能通过；其他限制及暂缓发布不变。
- 仅同步文档，未改代码、推送或部署；详细决定见活动工作树的项目状态、功能日志和性能报告。

### 2026-10-15 — 分组规则独立容器验收通过（共享指针）

- 固定 8 请求 / 23 尝试、两次重启、跨分组暂停及恢复、配置权限和唯一账单核对均通过；只用合成上游，无真实费用。补齐真实应用 18 路由契约；应用源码未再改。
- 本次新建容器停止但数据/日志保留。精确镜像及证据见活动工作树最终门禁报告新增节；旧模块化性能门禁仍未关闭，未推送部署。

### 2026-10-15 — 自动升级脚本加固（共享指针，本地未发布）

- 活动工作树修复 workflow 的 Bash 报告、bot 身份、连续 patch merge 和异常结果标识，33 项离线契约通过。没有应用源码或生产变化，不等于真实 Actions 已恢复。
- 详细变更和证据只维护在活动工作树 `docs/CUSTOM_FEATURE_LOG.md` / `CUSTOM_MODULARIZATION_FINAL_GATES_20261015.md`；原 SSE 性能门禁及用户暂缓发布保留。

### 2026-10-15 — 分组切换规则（共享指针，本地未发布）

- 单独分支 `codex/group-failover-rules-v0.2.16`，基线 `260f3c1b6`。分组可按错误类别设置次数/窗口/暂停，用户确认触发后全分组停调；保留手动恢复、原有安全重试和计费。
- 本地开发完成，提交 `caa717338`：后端 157 顶层 / 422 含子例、前端相关 52 项、类型/构建、22 项架构负例和差异 lint 通过。未做本功能 Docker/生产验收，不关闭旧模块化性能门禁。
- 完整实现与测试只记录于活动工作树 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom/docs/CUSTOM_FEATURE_LOG.md`、`CUSTOM_PROJECT_STATUS.md`。旧模块化限制不关闭，未推送部署。

### 2026-10-15 — 有限 DEBUG 已结束（共享指针）

- 两版各 100 次本地合成 SSE 全部成功，日志恢复；DEBUG 首字节 p95 48.869→41.849ms，未复现候选更慢但不关闭原 mixed 门禁。本轮不再加测，运行代码仍 `53fd17b5b`，未推送/部署。
- 详情只维护在活动工作树的 `docs/CUSTOM_PROJECT_STATUS.md` 和 `deploy/acceptance/MODULAR_PERFORMANCE.md`，不从本根目录旧源码发布。

### 2026-10-15 — 模块化性能及交付收尾（共享入口，本地未发布）

- 活动运行提交 `53fd17b5b`：恢复旧故障短路求值，同源微基准、受影响单测/差异 lint 与精确新镜像 11 故障通过。正常 HTTP/SSE 对照的尾延迟仍在有限诊断，不额外开发业务或重复全量测试。
- 唯一状态在 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom/docs/CUSTOM_PROJECT_STATUS.md`；最终证据见同目录 `CUSTOM_MODULARIZATION_FINAL_GATES_20261015.md`。根目录不作为发布源码，本轮未推送、未部署，未知图片结果继续待核对。

### 2026-10-15 — 最终容器与恢复验收（共享入口，本地未发布）

- 运行候选 `57721bba9`，验收脚本与设置演练 `a44618924`；角色、生图、11 类故障以及真实中断/重复投递通过，未知任务按用户决定继续待核对。性能收尾中，不宣称已完成或已部署。
- 唯一当前快照与逐项证据仍位于活动工作树 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom/docs/CUSTOM_PROJECT_STATUS.md`、`CUSTOM_MODULARIZATION_FINAL_GATES_20261015.md`。根目录仅同步入口，不合并旧源码或触碰其他会话改动。

### 2026-10-15 — 模块化最终验收推进（共享指针，本地未发布）

- 本地结构主体已落地；独立修复提交 `8d5dba9a4`、`3e78ebe2b`，旧测试对齐 `9831e38c6`。完整前端 3176 项及真实 PG 取消/确认丢失/冻结隔离通过，最终 Go WS、构建/恢复/性能/镜像尚未全部完成。
- 用户再次确认主任务是降低主线合并耦合；不扩展新业务。未知上游结果保留待核对、不新增管理员核对入口，暂缓发布。没有 push、标签或生产变更。
- 唯一当前状态和精确证据在活动工作树 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom/docs/CUSTOM_PROJECT_STATUS.md`，根目录只同步入口；不将下方历史失败/完成判断当作最新结论。

### 2026-10-15 — 批量取消规则确认及本地修复（仅同步）

- 用户确认：已发出继续正常结算，取消不退款；上游明确生成失败仍不收费，未发出不收费。保留已生成图片及下载，不修改历史费用。
- 活动工作树已修复成功及迟到成功因取消被整笔退款的路径，合成用例精确验证 3 项仅 2 项已发送，实扣 0.50 / 原预留 0.75，重复取消不重复扣费；其它恢复/未知结果边界仍在验证，不能作为发布通过记录。
- 入口 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom/docs/CUSTOM_PROJECT_STATUS.md`；未推送部署，三站生产仍 `.5`，本目录其他会话修改保留。
- 本地取消修复 `55765c473`，相关定向/Go vet/真实 PostgreSQL CAS 及前端类型/lint 通过；WS 独立复现 `3856b469a` 仍失败，未知结果恢复等剩余门禁没有隐瞒为已完成。

### 2026-10-15 — 模块化复核停止项（仅同步，本地未发布）

- 活动分支 `codex/custom-modularization-v0.2.16` 已完成多项接口解耦与局部测试；P4/P6 账务验收发现普通兼容批量部分成功后取消漏结算，原基线和候选均复现。保留失败用例，未修改运行账务或生产费用。
- 此前“P0–P6 全部完成”已撤回；WS 跨轮身份、全量门禁、恢复及性能仍需闭环，按方案停止收尾，待确认独立修复范围。详细证据在活动工作树 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom/docs/CUSTOM_MODULARIZATION_REVIEW_20261015.md`。
- 没有推送、标签或部署，生产仍 `0.2.16-custom.5`；根目录其他会话改动保留。
- 活动工作树新增本地提交 `31545ff8a`、`d596bae70`、`fa677d301`；包含刻意保留的账务失败回归用例，不是可发布候选。

### 2026-10-14 — 本地实施（未发布）— 定制模块化 P0/P1

- 用户已批准执行，不再是下方历史记录的“待审批”。活动分支 `codex/custom-modularization-v0.2.16`；P0 提交 `b3dbe92d6`。P1 增加真实路由/菜单/设置权限接入及负向架构检查，已本地通过；后续阶段仍在执行。
- 详细变更及测试以活动工作树 `docs/CUSTOM_MODULARIZATION_EXECUTION.md` 为准，根目录只同步入口。未推送、未部署、生产仍 `.5`；不覆盖其他会话源码。

### 2026-10-14 — 仅方案（planned）— 定制模块化执行方案

- 用户要求将解耦建议写成执行方案。入口：[CUSTOM_MODULARIZATION_PLAN.md](CUSTOM_MODULARIZATION_PLAN.md)；正文在活动工作树同名文件维护，不复制两份正文。
- 方案包含 P0 基线、P1 门禁、P2 前端/品牌、P3 后端装配、P4 生图/审计、P5 故障策略和 P6 联合验收；每阶段有操作、验收与停止条件，保持 API/数据库/权限/计费行为。
- 本轮只更新文档，阶段均待审批；没有修改运行代码、提交推送或部署，生产仍为 `.5`。其他会话未提交改动保留。推送与生产发布需后续明确授权。

### 2026-10-14 — 已部署（deployed）— 网关故障切换 `0.2.16-custom.5`

- **坐标**：活动工作树 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom`，分支 `codex/v0.2.16-image-studio`；运行提交 `00e8f24a40eba481d1e61cfebb5f4ab3f9401a2b`，标签 `custom-0.2.16.5` 已推送。验收脚本 `3fed247fa`、本地报告 `42e456012`，最终生产发布文档 `989594099` 已推送。
- **功能**：同账号 5 分钟累计 3 次 overload 默认冷却 10 分钟；额度/凭据/模型不可用及暂时性上游错误按类别有界重试与冷却，按原调度换备用；参数/安全拒绝和已输出流不重放。不改计费、原调度评分、分组权限，无新迁移，不声称所有供应商原生协议已重写。
- **验证 / 镜像**：相关 Go 单测、vet、隔离检查、Docker 构建及 11 类合成故障验收通过，无真实模型费用。镜像 `ghcr.io/hzihuan001/sub2api:0.2.16-custom.5` / `sha256:3a2f925574266819adc3cddad9595170cd71404519f315326f83d61098b6a80b`，GHCR 回拉与验收镜像一致。Actions 仍 disabled，本地门禁后直发，不算 CI 通过。
- **生产**：20:32–20:33（Asia/Shanghai）主站 ai.moshu.cloud、L1 api.moshu.cloud、COS cos.moshu.cloud app-only 顺序更新；20:34 复核 healthy、重启 0、版本/commit/digest/Compose 一致，内外健康及静态入口 200，关键启动错误 0。数据库、Redis、网络、卷和反代保持。
- **备份 / 回滚**：`/opt/sub2api-backups/v0.2.16-custom.5-20261014T120809Z` 已校验；`.4` digest `sha256:a4b7d421e233a1484c92b02780fd2a2421d4ea450fc2a184c82ac253289c045e` 保留，只退应用不还原旧库。
- **交接**：完整验收及回滚命令见活动工作树 `docs/GATEWAY_RESILIENCE_V0_2_16_5_ACCEPTANCE.md`。根目录仅同步共享文档，其他会话源码保留；下方“未发布”为之前阶段，不是现状。以后从包含 `.5` 的分支继续。

### 2026-10-14 — 本地未发布 — 跨请求 overload 临时冷却

- 活动代码位于 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom`，基于 `custom-0.2.16.4`；本根目录旧分支源码未改。
- 新增 `gateway-resilience`：OpenAI 账号滚动 5 分钟累计 3 次明确 overload，复用现有冷却设置，暂避故障账号并使用既有调度选择备用；不重放已经输出的流，不改变计费。修复纯排队心跳误算输出及 WS 配对错误重复计数，支持本实例管理员清除恢复。
- 相关服务、handler、Redis 单测、WS 定向及最终组合回归全部通过；四包 Go vet 通过，迁移/manifest检查通过。未运行发布镜像/Docker验收；完整实现、测试证据和限制见活动工作树的同名日志与状态文档。
- 尚未推送、打标签、构建发布或部署；三站生产仍为 `0.2.16-custom.4`。无数据库迁移。后续发布必须从活动工作树包含这些本地修改的基线继续，不能将旧根目录代码覆盖过去。

> 本文件是 append-only 变更账本。每次定制、官方升级、验收和发布都追加记录，
> 不覆盖旧记录。详细操作流程见 [`CUSTOM_PROJECT_STATUS.md`](CUSTOM_PROJECT_STATUS.md)。
>
> 记录从 `custom-0.1.179.1` 开始。早期代理商/分站功能属于历史阶段；自
> v0.2.8 后代理商运行时已移除，不能在后续升级中误恢复。

### 2026-10-14 — 已部署（deployed）— 取消批量额外折扣 `0.2.16-custom.4`

- **完成坐标**：工作树 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom`，分支 `codex/v0.2.16-image-studio`；标签 `custom-0.2.16.4`，运行提交 `c9f6829730c8f74bbf171e03b7e384e93e2f9ea2`，业务修复 `29945161c`。代码、标签和镜像均已推送；发布文档已提交并推送为 `dde88cd3dd05907d0c13a37258863e15b5c2d1d6`，活动工作树 clean。
- **Feature ID / 实现边界**：`image-studio`、`usage-extras`；批量提交与结算、管理员分组创建/更新/复制、生图定价界面和文案。新任务使用 `pricing_snapshot_version=2`，批量折扣固定为 1，冻结比例至少为 1；保留既有分组、用户和账号倍率，usage 基础费用与实扣金额分开记录。
- **收费规则已落实**：取消新批量任务的额外五折，继续按成功请求项结算；历史任务保持提交快照，不重算、不补扣，上游额外返回图片不加收。下方“只读诊断”条目是修复前历史，不能作为当前待确认事项。
- **数据库 / 迁移**：新增 `9000_custom_batch_image_no_discount.sql`，只规范分组的批量配置和未来默认值，不修改旧任务、历史使用记录或已扣金额。主站/L1/COS 各有 1 条迁移记录；36/12/12 个分组的折扣全部为 1、冻结比例全部至少为 1。主站 17 个旧任务的价格快照及已结算 actual 金额保持不变，L1/COS 无旧任务。
- **本地验收**：最终发布镜像的三图 `$0.03`、并发、参考图、ZIP 和单张下载检查全部通过；隔离检查只对 `.sql` 应用迁移命名与不可变规则，新增 Go 迁移测试不再误判，7 项脚本回归通过。
- **发布镜像**：`ghcr.io/hzihuan001/sub2api:0.2.16-custom.4` / `sha256:a4b7d421e233a1484c92b02780fd2a2421d4ea450fc2a184c82ac253289c045e`。Actions 仍 disabled，使用通过本地门禁的同一镜像直接发布，未将其记作 CI 成功。
- **部署站点 / 时间**：2026-10-14 15:52–15:54（Asia/Shanghai），主站 `ai.moshu.cloud`、L1 `api.moshu.cloud`、COS `cos.moshu.cloud` 完成 app-only 升级；均 healthy、重启 0，内外 `/health` 与生图工作台入口 HTTP 200，无关键错误日志。
- **备份 / 回滚**：`/opt/sub2api-backups/v0.2.16-custom.4-20261014T074350Z` 已验证 dump 目录、tar 和 SHA。保留 `.3` 镜像 `sha256:a96e2a5a52e0d7a83a9829371715359b2afcc0a02168fe8bded77b55dcfdeec3` 用于回滚；只回退应用、不恢复数据库，保留迁移后分组折扣 1 和旧任务快照。
- **共享交接**：此根工作区仅更新两份共享文档，`codex/isolation-refactor-v0.2.13` 源码和其他会话未提交改动保持原样。下次工作从含 `custom-0.2.16.4` 的活动发布分支继续；旧日志逐条保留。

### 2026-10-14 — 只读诊断 — 批量生图计价与图片数

- **状态**：发现问题，未修复/未发布/未调整生产费率或历史扣费；用户尚未确认具体批次，以下为最近生产记录与代码交叉核对。
- **已验证样本**：主站 14:57（Asia/Shanghai）`gpt-image-2`，使用记录 #190308：2 项成功，每项 1 张，基础单价 $0.03，批量折扣 0.5，结算单价 $0.015，总金额 $0.03，使用记录图片数为 2。因此该笔不是漏乘 2，而是应用了五折；一批仅写一条汇总记录。
- **原因**：`batch_image_public.go` 的价格快照对 app-managed 单图请求队列也沿用批量折扣，`batch_image_settlement.go` 按成功 item 数乘快照单价。历史默认批量折扣为 0.5；不能据此推断上游普通请求也提供折扣。
- **额外发现**：主站另一批次明细的 image_count 合计 7，而汇总/用量记为 6；代码按成功子任务计数而不是图片数。批量 usage 的 total_cost 已含折扣但 UI 又标为原始金额，展示口径也需要校正。
- **建议待确认**：应用层拆单批量默认与普通单图同价，折扣必须显式配置；统一请求张数、返回张数、收费张数与日志金额语义。上游超额返回图片不能未经确认自动追加收费；历史记录不自动补扣。定位证据来自只读事务和源码，未发起付费测试。

### 2026-10-14 — 运维清理 — 无引用旧镜像

- **范围 / 结果**：按精确镜像 ID 删除本地 Docker 15 个无容器引用的旧应用/验收镜像，以及 OVH 的 2 个旧发布镜像（`0.2.15-custom.2`、`0.2.15-custom.1`）。不使用全局 prune 或强制删除；每次删除前重新检查所有运行和停止容器的引用。
- **保留**：三站当前 `a96e2a5…`、直接回滚 `edf55576…`，以及历史显式 rollback `0390d87…`；所有容器引用镜像、宣传站、CPA、数据库/Redis、基础工具镜像均保留。没有删除容器、卷、网络、备份或构建缓存。
- **验证**：本地 16 个容器、OVH 14 个容器的镜像与运行/停止状态前后未变。三站仍 healthy、重启 0；OVH 根盘可用空间从 48,357,138,432 增至 48,699,949,056 字节，约增加 327 MiB。本地未把镜像标称大小相加当作实际回收量，也未压缩 Docker 虚拟磁盘。
- **恢复 / 记录**：已发布旧镜像可从 GHCR 按原 tag/digest 重新拉取；本地临时验收镜像需由对应源码重建，原构建产物已删除。本次仅清理及同步本地文档，没有新版本或部署；本日志尚未另行推送。

### 2026-10-14 — 共享进度同步 — 发布完成后的多会话交接

- **阅读优先级**：历史条目中的“当前”“未发布”“发布中”均指记录当时；最新生产状态以 `CUSTOM_PROJECT_STATUS.md` 和最近完成的发布条目为准，历史记录不改写。
- **完成坐标**：三站 `0.2.16-custom.3`，运行提交 `a1eb0aaac`；完整发布文档已推送为 `da654fb2c`。最新代码在 `codex/v0.2.16-image-studio` 工作树，不在根目录旧 v0.2.13 分支。
- **本次文档变化**：根目录状态表更新为真实生产快照，单列旧工作树状态、备份/回滚、验证范围和下一会话起点；明确 Actions disabled 与集成分支落后的限制。本次仅更新共享文档，没有改业务代码、生产配置、发布标签或运行镜像。
- **待后续处理**：恢复 Actions；核查其他功能会话后推进集成分支，确保包含 `.3`。不能把上述未完成项写成自动升级已恢复；本次新增交接说明为本地文档更新，未另行提交推送。

### 2026-10-14 — 已部署 — Nano Banana 模型发现热修复 `0.2.16-custom.3`

- **来源**：工作树 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom`，分支 `codex/v0.2.16-image-studio`；运行代码提交 `a1eb0aaac70cda552eb92882305e6f5241c95418`，标签 `custom-0.2.16.3`。`.2` 仅保留源码标签，未部署。
- **修复**：上游 Gemini 模型目录实际返回 OpenAI `data[].id`。后端归一化后继续应用分组白名单，前端兼容两种目录；异常条目不绕过过滤，原生空目录不回填。生成、计费、参考图和下载行为不变。
- **部署**：主站 `ai.moshu.cloud`、L1 `api.moshu.cloud`、COS `cos.moshu.cloud` 使用同一镜像 `ghcr.io/hzihuan001/sub2api:0.2.16-custom.3` / `sha256:a96e2a5a52e0d7a83a9829371715359b2afcc0a02168fe8bded77b55dcfdeec3`，均 healthy、重启 0、入口与健康检查 200。
- **验证**：受影响后端/前端测试、静态检查、生产构建和隔离 Docker 白名单允许/拒绝验证通过。主站真实分组密钥经本机、公网均返回 `gemini-nano-banana-2.1`；未提交付费生图。Actions 禁用限制仍在，发布本地已验收镜像，未声称 CI 通过。
- **备份 / 同步**：完整备份 `/opt/sub2api-backups/v0.2.16-custom.2-20261014T031705Z` 可读且校验通过；回滚 `.1` 只需重建应用，无迁移。完整发布、验证、回滚记录以最新工作树的共享文档为准；此旧树其他会话的未提交改动保持原样。

### 2026-10-13 — 升级会话交接 — v0.2.16 与生图移植

- 新候选在 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom`，分支 `codex/v0.2.16-image-studio`；从 `custom-0.2.15.3` 合并官方 v0.2.16，选择移植生图会话 14 个功能提交。
- 官方 merge `d3b78cf8c`，生图移植止于 `2c007ec6a`，运行修复 `e42db1f11`，验收脚本 `f17cae568`。前端、后端受影响回归、真实 PostgreSQL、Docker 权限与 mock 生图、同数据库旧镜像回滚验收通过；本地地址 `http://127.0.0.1:18086`。
- 详细功能、验证和限制以候选工作树的 `docs/CUSTOM_FEATURE_LOG.md`、`docs/UPGRADE_V0_2_16_ACCEPTANCE.md` 为准；发布标签 `custom-0.2.16.1` 已推送，GHCR digest `sha256:edf55576e8ea73d2a552d7489cabdd2a6f282290e2ac713cda0d5504a55f5f17` 已部署。
- 当前生产主站、L1、COS 均为 `0.2.16-custom.1`。本树旧快照和未提交公共文档/DNS 记录原样保留，不能将旧 v0.2.13 基线误当作最新版本。

### 2026-10-14 — 生产发布记录 — v0.2.16-custom.1

- 发布标签：`custom-0.2.16.1`；候选分支：`codex/v0.2.16-image-studio`。
- GHCR 镜像：`ghcr.io/hzihuan001/sub2api:0.2.16-custom.1`，digest `sha256:edf55576e8ea73d2a552d7489cabdd2a6f282290e2ac713cda0d5504a55f5f17`。
- 主站、L1、COS 均已只替换应用镜像并健康运行；三个域名 `/health` 均 200。备份标识：`/opt/sub2api-backups/v0.2.16-custom.1-20261014T0010Z`。
- 清理未使用旧 Sub2API/CPA 镜像和可回收 Buildx 缓存，保留当前及必要回滚镜像；数据库、Redis、卷、网络未删除。
- 详细验收和 Actions 暂停导致的本地 Buildx 发布说明见候选工作树的 `docs/UPGRADE_V0_2_16_ACCEPTANCE.md`。

## 当前状态（2026-10-12）

- 最近发布：`custom-0.2.13.6`，官方基线 `v0.2.13`。
- 当前生产镜像：`ghcr.io/hzihuan001/sub2api@sha256:1dc23cebaa1b8fb3d7629c0a84581c5f8f6b006268d19dff6cd5f832eef59c48`。
- 当前工作分支：`codex/isolation-refactor-v0.2.13`；同步框架 HEAD 为 `1fa5f4478`，本文档提交后的 HEAD 以 `git log` 为准。
- 长期集成基线：`origin/custom/integration`，当前为 `fd7c1b914`。
- 本文件和项目状态文档不包含服务器凭据、API Key、私钥、`.env` 或生产数据库内容。

## 当前功能清单

| Feature ID | 当前行为 | 代码/测试边界 | 迁移要求 |
| --- | --- | --- | --- |
| `operator` | operator 认证、默认拒绝的管理路由、角色权限配置、菜单和页面守卫；admin 仍保留严格超级管理员语义 | `backend/internal/authz`、`backend/internal/server/middleware`、`frontend/src/authz`、角色/权限测试 | 已有迁移保持不变；新增迁移只能使用 `9000_custom_` 前缀 |
| `prompt-audit` | `off`、`capture_only`、`async_audit`、`blocking`；捕获事件、类型筛选、详情、导出、删除和保留策略 | `backend/internal/securityaudit`、`frontend/src/features/prompt-audit`、对应后端/前端测试 | 追加式；不得修改已发布迁移 |
| `image-studio` | Moshu 生图工作台、批量任务、应用层单图拆分、队列、参考图、单张下载和 ZIP 下载；默认生图尺寸为 2K | `backend/internal/service/batch_image_*`、`frontend/src/features/image-studio`、用户工作台视图、批量测试 | 追加式；对象/任务数据需兼容旧镜像 |
| `branding` | 对客页面、公共文案和图片工作台统一使用 Moshu，避免把上游项目名暴露给用户 | `frontend/src/i18n`、品牌工具、公共页面测试 | 无独立数据迁移 |
| `usage-extras` | 管理员使用记录显示缓存命中率、分页跳转等增强；用户侧不显示管理员专属字段 | 管理员使用记录 handler/view 与前端测试 | 无破坏性迁移 |

### 2026-10-12 — 运维变更 — 下游域名迁移与 Cloudflare 代理

- **需求**：改善两个下游站点从国内网络访问时偶发无法进入和首页响应较慢的问题。
- **Feature ID**：`infrastructure`
- **官方基线**：不涉及代码版本；基于当前生产部署。
- **实现边界**：
  - `api.moshu.cloud` 指向 OVH 上的 L1 站点，`cos.moshu.cloud` 指向 OVH 上的 COS 站点；
  - OVH OpenResty 两个 vhost 增加新域名，继续复用原应用容器、端口、数据库、Redis、卷和 10 小时读写超时；
  - 为两个新域名签发 Let’s Encrypt SAN 证书，并配置自动续期部署钩子；
  - Cloudflare DNS 的 `api`、`cos` 记录均切换为 `Proxied`，旧 `tokenpulse.top` 记录保持原状作为对照；
  - 变更前备份保存在服务器的运维备份目录，未将凭据、私钥或完整生产配置写入仓库。
- **用户可见行为**：可通过 `https://api.moshu.cloud` 和 `https://cos.moshu.cloud` 访问两个下游站点，站点内容和账号数据不变。
- **数据库/迁移**：无。
- **测试**：两个新域名 HTTPS 证书校验通过；`/health` 均返回 HTTP 200；容器均为 healthy；服务器本机应用健康检查约 1 ms。开启 Cloudflare 代理后，测试网络下载首页约 3.5–4.7 秒；切换前直连 OVH 的同类测试曾超过 25 秒未完成，说明主要瓶颈在外部传输链路而非应用容器。
- **提交/PR**：仅文档追加，未产生代码提交。
- **发布镜像**：无。
- **部署站点**：OVH；两个站点的 DNS、证书和 OpenResty 配置已生效，健康检查通过。
- **回滚**：恢复 DNS 为 DNS-only、恢复变更前 vhost 与证书备份即可；应用数据库和容器无需回滚。
- **备注**：继续观察不同地区访问速度、SSE/API 长连接和 Cloudflare 5xx。若出现长请求中断，优先检查 Cloudflare 代理对长连接的限制与 SSE 心跳，再调整边缘规则；不要仅通过延长源站超时处理。

## 版本与功能时间线

### 2026-10-04 — 同步隔离和升级自动化加固

- **需求/目的**：让官方下一次发布可以自动生成升级分支、冲突报告和测试结果，避免再次从历史大合并提交中人工猜测定制内容。
- **实现**：
  - 以 `custom/integration` 作为长期集成基线；
  - 上游 merge 单独提交，定制补丁逐个提交；
  - 自动生成 `upgrade/upstream-vX.Y.Z` 草稿 PR；
  - 固定 Go、Node、pnpm；默认只读权限；显式处理 fetch/merge/检查失败；
  - 增加 range-diff、生成代码、迁移、feature manifest 和受保护路径检查；
  - 通过 `custom/manifest.yml` 与 `.github/custom-features.json` 维护稳定 feature ID。
- **关键提交**：`c72ec17e2`、`6f04e714e`、`7eac4e915`、`0771ce2eb`、`dcce7b44c`、`8f8e58825`、`3360bd4d9`、`1fa5f4478`。
- **验证**：主线同步工作流成功运行；最新 CI 和 Security Scan 通过；升级工作流不负责构建、发布或部署。
- **发布**：未产生新的生产镜像；生产继续使用 `custom-0.2.13.6` 对应 digest。
- **后续**：`patch_branches` 暂为空，不能把旧历史分支直接加入自动补丁栈。

### 2026-10-04 — `custom-0.2.13.6`

- **官方基线**：`v0.2.13` / `3040209f2`。
- **功能**：完成 v0.2.13 定制发布后的隔离验收资料整理、合规文案品牌修复和发布坐标固定。
- **保留**：operator、Prompt Audit、Moshu 生图工作台、批量生图、使用记录增强和对客品牌。
- **验证**：本地三站 Docker 验收、批量生图 fan-out、参考图、单张下载、ZIP 下载和关键质量门禁通过。
- **回滚**：保留上一版镜像和数据库/配置备份；生产回滚只恢复应用镜像，除非官方迁移不可逆。

### `custom-0.2.13.1` 至 `custom-0.2.13.5`

- 合并官方 v0.2.13 并保留 GPT 图片引用、工作台批量任务和既有定制功能。
- 完成应用管理的批量单图请求、参考图处理、下载和本地 Docker 验收记录。
- 这些版本的验收报告是历史证据；不能把旧报告当作当前 `.6` 的新生产验收结果。

### `custom-0.2.11.10`

- 完成批量生图工作台下载体验和 ZIP 归档改造。
- 保留单张下载、批量下载、任务状态和失败项信息。

### `custom-0.2.8.x`

- 扩展角色权限和对客品牌；移除代理商运行时功能，保留 operator、审计、生图等当前定制层。
- 之后的升级不得因为旧代理商分支或旧生产镜像而恢复已移除的代理协议、产品同步或结算代码。

### `custom-0.2.5.x`（历史代理阶段）

- 曾实现代理商协议、产品/账号同步、DeepSeek/Kimi 兼容、定价快照与结算对账等能力。
- 这些改动已标记为历史，不属于当前运行时；只用于解释旧标签和旧数据库/日志，不作为新升级的合并目标。

### `custom-0.2.0.2`

- 角色权限管理升级为可配置策略，扩展 operator 菜单与内容控制。

### `custom-0.1.183.x`

- 增加管理员使用记录的缓存命中率和分页增强。
- 增加 Prompt Audit 的 `capture_only`、筛选、导出、保留和排除用户能力。
- 增加 Moshu 生图工作台、参考图和尺寸/下载相关能力。

### `custom-0.1.179.1`

- 初始 Operator 权限定制：固定允许/拒绝管理接口，普通用户与管理角色认证边界分离。

## 每次新定制必须追加的记录模板

复制下面模板追加到本文件顶部（不要修改历史条目）：

```markdown
### YYYY-MM-DD — <版本或“未发布”> — <简短标题>

- **需求**：
- **Feature ID**：`operator` / `prompt-audit` / `image-studio` / `branding` / `usage-extras` / 新 ID
- **官方基线**：官方 tag、commit；若是定制迭代，注明基于哪个 custom tag
- **实现边界**：列出新增/修改文件夹、API、页面和配置；不要把整仓库写成影响范围
- **用户可见行为**：
- **数据库/迁移**：无；或列出新增迁移文件（必须是 `9000_custom_<name>.sql`）
- **测试**：单元、集成、前端、Docker 验收的明确结果
- **提交/PR**：commit、PR URL；若未推送写“仅本地”
- **发布镜像**：tag、immutable digest；未发布写“无”
- **部署站点**：主站/测试站/未部署；记录健康检查结果
- **回滚**：旧镜像、是否需要数据库恢复、是否有不可逆迁移
- **备注**：兼容性、已知限制、后续工作
```

## 每次官方升级必须追加的记录模板

```markdown
### YYYY-MM-DD — upstream <vX.Y.Z>

- **上游**：tag、commit、自动升级 PR URL
- **基线**：`custom/integration` commit
- **冲突**：无；或列出文件、原因和处理方式
- **检查**：range-diff、生成代码、迁移、后端、前端、Compose、Docker 验收
- **定制保留**：列出本次确认过的 feature ID
- **发布决定**：阻塞 / 仅合并未发布 / 已创建 custom tag
- **镜像**：tag 与 digest；未构建写“无”
- **部署**：站点、时间、健康结果；未部署写“无”
- **回滚证据**：旧 digest、备份位置（只写非敏感标识）
```

## 多会话记录规则

- 功能会话负责记录自己实际完成的代码、测试和边界；不要让版本更新会话替代填写功能日志。
- 版本更新会话负责记录官方 tag、冲突、合并、镜像 digest、部署和回滚；不要把未验收的功能写成“已发布”。
- 同一 feature 同时被多个会话修改时，先在 `CUSTOM_PROJECT_STATUS.md` 标明负责人、分支和阻塞关系；合并后再追加一条完整日志。
- 失败的升级、回滚或验收也要记录，但要明确标记为“失败/阻塞/未部署”，不能删除失败历史。
- 日志引用 commit、PR、镜像 digest 和测试结果即可，不引用密码、Token、私钥、真实提示词或完整生产配置。
- 日志和 PR 不得出现 JWT/TOTP、完整 Authorization/Header、可直接登录的 SSH 命令或数据库内容；测试凭据只放本地秘密配置。

