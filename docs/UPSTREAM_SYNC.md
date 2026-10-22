# 官方主线同步说明

本仓库把官方主线、长期定制集成分支和临时升级分支分开。预期流程是每次官方发布后，先由同步工作流生成升级分支、冲突报告和测试结果，再由人工确认定制功能边界；同步工作流不负责构建、部署或修改生产环境。

## 最新状态（2026-10-22）：custom.13 已完成三站生产部署

当前权威工作树为 `C:/Users/a/.codex/worktrees/group-routing-current`，分支 `codex/key-account-routing-stability-20261022`。本轮仍以官方 `v0.2.16` 为底，不额外合并新版；已发布版本 `0.2.16-custom.13`，运行提交 `90804511dcfd57d9e99d7df470cfa05282182664`，运行标签 `custom-0.2.16.13`，镜像 `ghcr.io/hzihuan001/sub2api:0.2.16-custom.13`，不可变 digest `sha256:9d79c4378f3e4dc6dd4d1648cff7420947ccb7984a33d7bb2cbd11030c32c14f`。

**正式 digest 本地验收、备份和主站 → L1 → COS 顺序部署均已完成，三站现运行上述 `.13`。** 正式镜像的合成网关检查 33/33（cleanup 42）、管理员编辑器检查 5/5（cleanup 6）全部通过，无 findings；证据分别为 `D:/projects/custom13-published-gateway-20261022/evidence.json`、`D:/projects/custom13-published-admin-20261022/evidence.json`。这些是本次正式产物的检查，不是沿用候选结果。步骤与回退边界见 [发布运行手册](../deploy/CUSTOM_RELEASE_RUNBOOK.md)。

三站部署时间为北京时间 2026-10-22：主站 13:35:48–13:36:01.557991，L1 13:37:19–13:37:27.344938，COS 13:39:21–13:39:29.286409（UTC 分别为 05:35:48–05:36:01.557991、05:37:19–05:37:27.344938、05:39:21–05:39:29.286409）。各站只读排空为零后仅重建应用，healthy、restart 0；环境/挂载/端口/网络指纹、PostgreSQL/Redis 容器 ID 和启动时间均保持。旧迁移全行不变，仅增 9003/9004，迁移总数为主站 328、L1/COS 各 337。

源站和公网的健康、密钥、生图工作台与文档工具检查均为 200，三个受保护匿名接口检查均为 401，关键启动日志为零。后续只读复核确认三站 namespace 独立，三站入口 index、KeysView、UsersView 的源站/公网共 18 个资源 SHA 均与正式本地产物一致。未进行生产付费模型调用或回退；不据此宣称全部模型商、所有端点、并发容量或长期稳定性已验收。

本轮复用仓库根 `Dockerfile`，保留 PostgreSQL 工具阶段及运行时 `pg_dump`、`psql` 和依赖，以免定制镜像破坏备份能力。实际 Actions dispatch 返回 HTTP 422，使用已获授权的本地构建/认证 GHCR 推送路径；不据此声称 CI 或自动升级 PR 链路已成功。文档收尾不能移动运行标签。

相对此前生产 `.12` 仅追加 `9003_custom_api_key_account_routes.sql`、`9004_custom_key_route_site.sql`，旧迁移与校验值保持不变。9003 提供选择启用的 `account_v2` 路由作用域、修订和授权隔离，旧 `group_v1` 保持兼容；9004 持久化站点 UUID 命名空间，共享 Redis 的不同站点不能共用命名空间。本次管理员管理用户 Key 复用完整编辑器本身不增加迁移，不要将整个 `.13` 包的 9003/9004 误归为该界面改动。

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
