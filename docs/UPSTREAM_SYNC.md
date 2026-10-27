# 官方主线同步说明

## 最新增量（2026-10-27）：独立受控直连入口

本轮不合并官方或重建三站应用，主应用仍 `.18.4`。从 `28159e883499df18d6f0ef7819e500eab95a5896` 建立 `D:/projects/sub2api-direct-access-20261027` / `codex/direct-access-20261027`，新增 `backend/cmd/direct-access` 与 `deploy/direct-access` 独立入口，在 manifest 登记部署扩展、契约测试及升级保护路径；没有主程序接线、前端功能标志、新依赖或业务迁移。后续合并须保留该独立基础设施，并核对现有用户属性表列、原生 API 鉴权及可信 IP 头语义；升级主应用不必重建此侧车。

`nocf.moshu.cloud` 已 DNS only + HTTPS 上线，管理员按用户授权，原 Key/计费保持；本地48项/真实PG集成及生产免费验收、模拟续期通过。发布坐标、证据边界和回退见[直连记录](CUSTOM_DIRECT_ACCESS_20261027.md)。最新源码仍以实际远端 `custom/integration` 为准，下文历史权威树描述保留为各轮事实，不作为回退当前代码的依据。

## 文档补充（2026-10-27）：发布后生图错误诊断

上轮 `.18.4` 发布闭环 `a56bf0c0208f885e4fe4c4e3c2c5eebcfbca81d4` 已同步功能分支与 `custom/integration`，三个旧树保留原文及共享指针。此次追加[错误排查记录](CUSTOM_IMAGE_STUDIO_DIAGNOSTICS_20261027.md)和共享日志，运行源 `1524aa098`、标签 `custom-0.2.18.4`、正式digest保持，不合并上游、不修改运行代码或重部署。

用户最终指出 `Failed to fetch` 发生在生成过程中；浏览器进度请求失败与后台单张生成失败分开。发布后主站3次批量POST均200，但3任务6条目仅1成功、5条上游502；无新批量绑定错误。17:43三站健康复核通过。单图超时/策略错误、跨域/大体积边缘限制及未知提交响应的幂等恢复列为有边界的后续项，不宣称已修复。新发现部署窗口3条Responses连接错误补充原发布记录，原零在途采样/关键启动0不能解释为绝对零中断。所有具体时间、窗口和计数以诊断记录为准，不从旧树整树覆盖当前运行代码。

## 最新状态（2026-10-27）：批量生图请求修复已部署三站

本轮没有合并官方上游，仍以 `v0.2.18` 为底；从 `b2ec356187dc74214c38a1a47e47b011228df074` 仅修改image-studio，三站现运行 `0.2.18-custom.4`。运行源 `1524aa0985f432df19c9144c726224d2852308d6`、不可变标签 `custom-0.2.18.4`、正式digest `sha256:80adc97b50213ba840e68d6c13b932701cff294f04fb5dcd291ffe12b63f49e6`。权威树/分支仍为 `D:/projects/sub2api-image-batch-16refs-20261026` / `codex/image-batch-16refs-20261026`，文档收尾与运行提交分开；后续先核对实际远端集成，禁止旧树整树覆盖。

共享参考图按能力协商只上传一次，服务端规范为原item/幂等哈希再展开；旧服务降级、展开总量检查和具体413/JSON/Base64/读体错误一并保留。GPT-image/Seedream/Grok按app-managed普通单图并发；原生Gemini/Vertex、Nano单图async、参考图原字节/图数/总量/尺寸/质量/计费及所有迁移保持。初步provider根因判断证据不足已勘误；真实handler确认泛化错误缺陷，旧线上日志不能精确确定用户那次原因。

前端99项/typecheck/lint/build、后端显式unit与handler/准入/审计、隔离/契约/依赖审计通过；正式digest权限/重启及共享16图4K参数转发mock通过。验证备份后按主站→L1→COS自然排空、仅更新应用，三站healthy/restart0，配置/数据服务/namespace/完整迁移保持（329/338/338，无新增）。兼容应用回退 `.18.3`；不恢复旧业务库，不声称生产付费模型、原生4K输出或Actions成功。完整资源核对和观察范围见[`.18.4发布记录`](CUSTOM_RELEASE_0_2_18_CUSTOM4_20261027.md)。

本仓库把官方主线、长期定制集成分支和临时升级分支分开。预期流程是每次官方发布后，先由同步工作流生成升级分支、冲突报告和测试结果，再由人工确认定制功能边界；同步工作流不负责构建、部署或修改生产环境。

## 历史状态（2026-10-26）：批量共享16张参考图已部署

官方仍为 `v0.2.18`，新增定制版本 `0.2.18-custom.3` 已部署主站/L1/COS。运行源 `c2dbbe786e844ee1df77bfd710ebaf438f625126`，标签 `custom-0.2.18.3`，正式镜像 `sha256:cb1c937d398989ccff2256b0bf7a7a51c7b63405f8dd5a267a1cd50f47e7b7ac`；只补齐GPT-image批量共享16图、多选和提交边界，其他模型/总量/计费保持，后端运行时和迁移不变。

最新隔离树 `D:/projects/sub2api-image-batch-16refs-20261026`，分支 `codex/image-batch-16refs-20261026`，基于已提交集成 `10b452931`。此前单图树的共享维护记录等未提交文档继续保留，不混入运行提交；后续升级先核对远端 `custom/integration` 的本轮普通快进结果，禁止旧树整树覆盖。

本轮共享文档同步基线为 `c8d14a1183f73cdefa28e698f45bbaf8f8c730b5`，补齐当前路由功能表与验收台账入口，并归档[安全清理记录](CUSTOM_MAINTENANCE_20261026.md)。清理发生在约15:57–16:04的 `.18.2` 运行时点，先于16:36–16:40的 `.18.3` 发布；当时容器未重启的结果不代表后续应用更新未重建容器。该同步仅文档，不新增运行实现、清理或测试结论；远端引用仍须按实际Git重新核对。

本轮前端92项、类型/lint/build、后端显式unit标签回归、正式digest权限/重启与16图mock验收通过；三站备份/排空/应用更新及健康/权限通过，配置、数据服务与迁移保持。`.18.2`的批量测试标签遗漏已勘误，不沿用其未执行目标作为证据。回退目标`.18.2`，详细坐标/范围见 [`.18.3发布记录`](CUSTOM_RELEASE_0_2_18_CUSTOM3_20261026.md)。运行标签不随后续文档提交移动；没有Actions成功或新增付费模型调用结论。

## 历史状态（2026-10-26）：单生图16张参考图已部署

本轮官方基线仍为 `v0.2.18`，没有额外合并上游。最新运行源 `879a99a22e47a303604927104a6f67329dcd3b38` / 标签 `custom-0.2.18.2` / 版本 `0.2.18-custom.2`；三站已运行同一不可变镜像 `sha256:c90609bc4546ec309a980929ec8f28eef3cef236022dd698700920439ce78412`。仅提高GPT-image单生图参考图至16并增加回归测试，无后端运行时、迁移、计费或配置变化；其他模型上限保留。

工作树 `D:/projects/sub2api-image-studio-16refs-20261026`，分支 `codex/image-studio-16refs-20261026`，从已提交集成 `e5d4e8c16` 隔离创建。`group-routing-current` 和旧根目录的未提交修改保留；后续升级先核对远端 `custom/integration` 与本轮运行源/文档收尾，不从旧工作树整树覆盖。源码标签保持不可变，文档提交与运行提交分开。

前端76项定向、类型/lint/build、后端图片定向、隔离/契约和安全审计检查通过；全量Vitest有1项既有密钥路由select失败（3414通过），不声明全量通过。正式digest拉回后容器权限/重启和mock生图验收通过；三站备份、只读排空、应用顺序更新及健康/匿名权限通过，数据库/Redis/迁移/namespace保持。无新付费模型请求，Actions仍不作为本轮成功证据。当前应用回退目标 `.18.1`，完整限制与证据见 [`.18.2发布记录`](CUSTOM_RELEASE_0_2_18_CUSTOM2_20261026.md)。

## 历史状态（2026-10-24）：v0.2.18 已合并、发布镜像并部署三站

- 官方 `v0.2.18` 为 `ac1b8f1271be0d674fa6f074403c1410f30a3a81`；`upstream/main` 仅把版本文件推进到 `5c185c5c5`，候选以正式 tag 内容为准并明确设置版本 `0.2.18-custom.1`。
- 合并基线为 `custom.16` 生产同步提交 `4356fafb7707ab0334338a56ef4ff02e004164db`。候选合并提交 `885edb605702a4af52b0bb8ac0060b58698c872d`，路由选择语义补丁 `a688883f1bc09fbce2c38a7253be7e1577793678`。实际三方 merge 探针发现 Chat/Responses、网关转发、定价、管理页和迁移等 16 组语义冲突，均人工处理；不能把本次合并视为自动无冲突升级。
- 合并修复提交 `305afd7f9` 修复了 Chat/Responses 流桥接、Kiro 错误传递和 Gemini Chat 推理档位计费记录；`8b5726b0c` 补齐 `key-routing` 内建功能清单测试。上述修复已纳入候选，实验性 `backend/internal/keyrouting/replay/` 仍排除。
- 已复核定制功能：角色与菜单、审计、生图工作台、错误/响应标记、Key 多分组优先级与账号级故障切换、请求级计费/价格快照、管理员完整 Key 编辑。特别恢复了 Chat/Responses 的 `keyAccount*`、prompt injection、分组上下文、`tryKeyRouteSelectionUnavailable` 和 `keyAccountForwardContext` 接线，避免官方文件覆盖定制路由。
- 已通过：前端 key-routing 94 项、typecheck、build；Docker Go 1.27.3 中 `go test ./internal/... -count=1` 全部通过；custom isolation、architecture-negative 34/34、custom contracts 18/18、upstream sync 5/5、workflow 11/11。候选镜像 `sha256:7a3eaee680b7708628cd1f8565818d22e830b836676b35d099f640e3397b0dbc` 在隔离 Compose 项目 `moshu-reseller-acceptance-v018c1` 的主站/L1/COS（18280/18281/18282）均 healthy，/health、管理员登录、Redis、重启持久化通过；每站 329 条迁移，最新 9004。迁移锁、幂等及定制 Key 路由测试通过。迁移回退演练尚未执行。
- 官方新迁移 `246_enable_gemini_image_generation_groups.sql` 已保留；`9000–9004` 定制迁移不改不删。重复数字前缀按完整文件名排序，必须通过 PostgreSQL 升级/回退演练后才能发布。实验性 `backend/internal/keyrouting/replay/` 明确排除。
- 生产已按主站→L1→COS 顺序运行 `0.2.18-custom.1`。发布源提交为 `fc0d798bf2d6a08a074970d0485c567291e02660`；发布标签为 `custom-0.2.18.1`；GHCR 不可变镜像为 `ghcr.io/hzihuan001/sub2api@sha256:d7f365d1ba40b6c159d4155f7fbc2773d01f0fe6015af7906ebbece500ba1837`。三站应用 healthy、restart 0，公网 `/health` 均 HTTP 200，关键启动错误匹配为 0。发布前备份 `/opt/sub2api-backups/v0.2.18-custom.1-20261024T000000Z` 已验证；生产迁移主站 329 条、L1/COS 各 338 条，定制 `9000–9004` 与官方 `246` 均在。未调用付费模型、未做长期容量或故障注入；`.16` digest `sha256:037d353afff1a4e18c88ff9ad59fad6735195465a1dbb60ab5ace7c5d33223e7` 为应用回退目标，回退不得覆盖新业务数据库。
- 上线后短时复核继续通过：三站 `/image-studio` 与 `/docs/tools/codex` 均 200，未授权 `/v1/models` 均 401，最近 15 分钟关键启动/数据库/Redis 错误匹配为 0。

## 历史发布状态（2026-10-23）：custom.16 已完成三站生产部署

当前权威工作树为 `C:/Users/a/.codex/worktrees/group-routing-current`，分支 `codex/key-account-routing-stability-20261022`。本轮仍以官方 `v0.2.16` 为底，不额外合并新版；三站当前运行 `0.2.16-custom.16`，提交 `fb02af3ece9ebf2cd54633bbfb6b839c6ba0051b`，标签 `custom-0.2.16.16`，镜像不可变 digest `sha256:037d353afff1a4e18c88ff9ad59fad6735195465a1dbb60ab5ace7c5d33223e7`。`.14` 是本轮前生产基线，`.15` 标签不可变但未出镜像/部署，由 `.16` 修正替代。

**正式 digest 本地验收、三站备份和主站 → L1 → COS 顺序部署均已完成，三站现运行上述 `.16`。** `.16` 发布前本地镜像 smoke 与 Responses 明确引用拒绝后的真实 handler 回归通过；生产健康/版本/迁移和匿名权限检查通过。完整步骤、备份位置及没有执行的生产项目见 [`.16` 发布记录](CUSTOM_RELEASE_0_2_16_CUSTOM16_20261023.md) 和 [发布运行手册](../deploy/CUSTOM_RELEASE_RUNBOOK.md)。`.14` 的 `12/12` 隔离请求留作此前版本证据，不冒称本轮在生产重复执行。

三站于北京时间 2026-10-23 按主站→L1→COS 顺序部署，healthy、restart 0；数据库迁移总数与指纹保持主站 328、L1/COS 各 337，无新增迁移。因需轮换此前暴露的 DB 凭据，PostgreSQL 容器按新配置重建并保持原数据卷；Redis、应用持久化目录、端口、网络和反代配置不变。上线后数据库活动查询为零；预部署排空脚本没有产生有效读数，因此文档不声称更新前在途数为零。

公网 `/health`、`/keys`、`/image-studio`、`/docs/tools/codex` 均为 200，匿名管理 API 为 401，关键启动错误匹配为零。没有再次做三站前端 chunk SHA 比较，沿用此前 `.14` 的历史证据但不将其表述为 `.16` 验收。未进行生产付费模型调用、故障注入、实际回退或长期观测；不据此宣称所有模型商、所有端点、并发容量或长期稳定性已验收。

本轮复用仓库根 `Dockerfile`，保留 PostgreSQL 工具阶段及运行时 `pg_dump`、`psql` 和依赖，以免定制镜像破坏备份能力。实际 Actions dispatch 返回 HTTP 422，使用已获授权的本地构建/认证 GHCR 推送路径；不据此声称 CI 或自动升级 PR 链路已成功。文档收尾不能移动运行标签。

相对此前生产 `.13` 无数据库迁移；旧迁移与校验值保持不变。Responses 恢复适配器只新增代码层的严格 provider 适配，不改变路由、结算和站点命名空间结构。

精确回退镜像仍为 `.12`：`ghcr.io/hzihuan001/sub2api@sha256:e6a53ec608abd463bfa41ccc6e939dca0175756adeb40e957ebe31a6b68c3610`。回退只换应用，保留新增兼容结构和业务数据；旧程序不理解 `account_v2` 语义，必须先检查仍启用该作用域的 Key，有风险时停止并明确处理，不能静默降级、删 Key 或恢复旧数据库覆盖新账目。下一次主线升级应保留上述独立模块和薄接线，不从历史 `.12` 或旧根工作树重建当前功能集合。

下次发布继续使用“正式 digest 本地验收 → 私有可读备份 → 只读排空 → 主站/L1/COS 逐站应用更新 → 版本/迁移/命名空间/资源与日志核对”的清单；失败先停后续站点，核查 scope 兼容后才回退应用，不修改业务数据。文档闭环提交与运行提交分开登记，运行标签保持不动。

## 历史状态（2026-10-20；以下不是本轮部署结论）

2026-10-20 新增**未发布本地 c 候选**：普通主备模型目录并集已在独立 `key_route_catalogue` service/middleware/handler 模块实现，宿主仅薄接线，契约/测试已登记；无新增迁移，不变更调度/计费。b 的两个备用独有模型 HTTP/SSE 通过；c 再对齐原生中间组权限阻断，最终零模型调用目录/权限/缓存补验通过，未重复网关测试。尚未提交、推送或部署；详见 [目录验收记录](CUSTOM_KEY_CATALOGUE_ACCEPTANCE_20261020.md)。后续升级先核对该工作区成果，不能只从远端正式 `.12` 推断功能已齐；本地 18180/18181/18182 已非下文正式 digest。

2026-10-20 当前边界：`.12` Key 编辑器、独立 TTFT/SLA/故障规则和模型限制已部署三站，运行 `f42b02f9ea0f43c3e34d9bbf778cd9e450cb05b4` / `custom-0.2.16.12`；镜像 digest `sha256:e6a53ec608abd463bfa41ccc6e939dca0175756adeb40e957ebe31a6b68c3610`。完整 service、正式产物本地主站 13/13、三站启动健康及逐站生产检查见 [发布报告](CUSTOM_RELEASE_0_2_16_CUSTOM12_20261020.md)，不据此声称 L1/COS 完整登录矩阵或生产付费调用已做。Fork 权限 API 虽 enabled，本次真实 dispatch 仍返回 HTTP 422 `Actions has been disabled for this repository`，未产生运行；已批准本地 GHCR 路径不等于 CI 或真实自动升级 PR 链路成功。

发布分支与 `origin/custom/integration` 同步 `.12` 运行提交及文档收尾，精确 ref 由 Git 核验，运行标签不因文档更新而移动。下次升级从 [项目状态](CUSTOM_PROJECT_STATUS.md) 和实际远端核对完整功能集合，保留主备 Key、独立规则、模型限制及公共文档。不要从旧 `D:/projects/sub2api-custom` 根检出全量发布或合并历史功能分支。GitHub CLI 默认仓库曾指向上游；查询/触发工作流必须显式指定 `-R Hzihuan001/sub2api` 或完整 API 路径。

## 标准顺序

1. 读取 [`CUSTOM_PROJECT_STATUS.md`](CUSTOM_PROJECT_STATUS.md)、[`CUSTOM_FEATURE_LOG.md`](CUSTOM_FEATURE_LOG.md) 和 `custom/manifest.yml`。
2. 先确认 `custom/integration` 包含最新已验收定制提交和共享日志登记的功能提交；落后时明确选择最新已提交定制基线并记录差异，不从旧功能分支整分支合并。然后合并指定官方标签，复核 `operator`、`prompt-audit`、`image-studio`、`branding`、`usage-extras`、`gateway-resilience` 和 `key-routing`；后者的备用组/策略存储、缓存 v28、实际组计费及内容/已知用量/工具副作用后禁止重放不能丢失。`.12` 允许安全的前内容切组，不再全局暂停账号，不应恢复历史绝对禁止重放或全局停号语义。
3. 只允许追加数据库迁移；新增定制命名为 `^9[0-9]{3}_custom_[a-z0-9][a-z0-9_-]*\.sql$`，编号 9000–9999，已发布迁移不可改删。生成代码按当前官方工具重新生成，不用旧分支覆盖；9001/9002 的软删除清理触发器仍需在宿主升级时复核。
4. 运行受影响后端测试、前端 typecheck/lint/test/build、`scripts/check-custom-isolation.py` 和 Docker 验收。
5. 验收通过后再创建 `custom-X.Y.Z.N` 标签、构建不可变镜像并按发布运行手册部署。

`.github/workflows/upstream-sync.yml` 设计为以只读默认权限检查稳定 `v*` 标签，并在明确请求时创建 `upgrade/upstream-vX.Y.Z` 草稿 PR；真实运行状态按上方限制记录。冲突或测试失败必须保留在报告中；不能通过选择整文件 ours/theirs 来静默丢弃定制功能。`patch_branches` 只登记从 `custom/integration` 重新创建且已经独立验收的功能分支，历史发布分支不得直接复用。

历史 `v0.2.15` 生图回补使用独立工作分支 `codex/v0.2.15-image-studio-sync`，来源提交仅选择 image-studio 代码和测试，没有带入旧版本文件、代理商历史代码或生产凭据。

2026-10-13 的历史 `v0.2.16` 候选为 `codex/v0.2.16-image-studio`，起点 `custom-0.2.15.3` / `0e5158d24`；官方 merge `d3b78cf8c`，14 个生图功能提交止于 `2c007ec6a`。当时 `origin/custom/integration` 为旧提交 `fd7c1b914`，不能把该历史坐标当作最新生产/功能基线；本轮普通快进结果见上方，当前版本与验证记录以 `CUSTOM_PROJECT_STATUS.md` 为准。

## 操作命令

长期集成分支应与普通上游跟踪分支分离：

```powershell
git switch --create custom/integration <tested-custom-commit>
git push --set-upstream origin custom/integration
```

同步工作流默认只生成元数据报告，不修改工作树：

```powershell
pwsh -NoProfile -File scripts/sync-upstream.ps1
```

只有明确指定标签和检查参数时才创建本地升级分支：

```powershell
pwsh -NoProfile -File scripts/sync-upstream.ps1 `
  -UpstreamTag vX.Y.Z -Apply -RunChecks
```

同步脚本没有发布或部署路径；生产发布必须经过单独的人工确认、不可变镜像验收和发布运行手册。每次升级都应把冲突、测试和回滚信息追加到 `CUSTOM_UPGRADE_ISSUES.md` 与 `CUSTOM_FEATURE_LOG.md`，不得写入密钥、Token、私钥、`.env` 或生产数据。
