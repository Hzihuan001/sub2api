# 官方主线同步说明

本仓库把官方主线、长期定制集成分支和临时升级分支分开。预期流程是每次官方发布后，先由同步工作流生成升级分支、冲突报告和测试结果，再由人工确认定制功能边界；同步工作流不负责构建、部署或修改生产环境。

## 最新状态（2026-10-24）：v0.2.18 已合并，本地三站 Docker/数据库验收通过，尚未发布

- 官方 `v0.2.18` 为 `ac1b8f1271be0d674fa6f074403c1410f30a3a81`；`upstream/main` 仅把版本文件推进到 `5c185c5c5`，候选以正式 tag 内容为准并明确设置版本 `0.2.18-custom.1`。
- 合并基线为 `custom.16` 生产同步提交 `4356fafb7707ab0334338a56ef4ff02e004164db`。候选合并提交 `885edb605702a4af52b0bb8ac0060b58698c872d`，路由选择语义补丁 `a688883f1bc09fbce2c38a7253be7e1577793678`。实际三方 merge 探针发现 Chat/Responses、网关转发、定价、管理页和迁移等 16 组语义冲突，均人工处理；不能把本次合并视为自动无冲突升级。
- 合并修复提交 `305afd7f9` 修复了 Chat/Responses 流桥接、Kiro 错误传递和 Gemini Chat 推理档位计费记录；`8b5726b0c` 补齐 `key-routing` 内建功能清单测试。上述修复已纳入候选，实验性 `backend/internal/keyrouting/replay/` 仍排除。
- 已复核定制功能：角色与菜单、审计、生图工作台、错误/响应标记、Key 多分组优先级与账号级故障切换、请求级计费/价格快照、管理员完整 Key 编辑。特别恢复了 Chat/Responses 的 `keyAccount*`、prompt injection、分组上下文、`tryKeyRouteSelectionUnavailable` 和 `keyAccountForwardContext` 接线，避免官方文件覆盖定制路由。
- 已通过：前端 key-routing 94 项、typecheck、build；Docker Go 1.27.3 中 `go test ./internal/... -count=1` 全部通过；custom isolation、architecture-negative 34/34、custom contracts 18/18、upstream sync 5/5、workflow 11/11。候选镜像 `sha256:7a3eaee680b7708628cd1f8565818d22e830b836676b35d099f640e3397b0dbc` 在隔离 Compose 项目 `moshu-reseller-acceptance-v018c1` 的主站/L1/COS（18280/18281/18282）均 healthy，/health、管理员登录、Redis、重启持久化通过；每站 329 条迁移，最新 9004。迁移锁、幂等及定制 Key 路由测试通过。迁移回退演练尚未执行，因此仍不能直接发布。
- 官方新迁移 `246_enable_gemini_image_generation_groups.sql` 已保留；`9000–9004` 定制迁移不改不删。重复数字前缀按完整文件名排序，必须通过 PostgreSQL 升级/回退演练后才能发布。实验性 `backend/internal/keyrouting/replay/` 明确排除。
- 生产不变：三站继续运行 `0.2.16-custom.16`。候选源码已推送到 `codex/v0.2.18-merge-20261023` 与 `custom/integration`，共同指向 `77a0e620e7798d743f08557ab167f59d1f3236e9`；本轮仅构建并验收本地候选镜像，尚未创建发布 tag 或部署。下一步是用户批准后构建并拉回不可变发布镜像，随后按发布手册执行生产流程。

## 最新状态（2026-10-23）：custom.16 已完成三站生产部署

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
