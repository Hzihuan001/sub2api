# Upstream upgrade PRs


### 2026-10-29 — upstream v0.2.19

- **上游**：`v0.2.19` → `dece26153621959ccd7635814f4b2d08304e681a`。
- **基线**：`custom/integration` `86ae08dbb450eb1090c2a076c3d8da7865ac7cf7`。
- **冲突**：Ent Group/Mutation/Runtime 的图片计费字段、账号处理器和测试服务构造签名、Gemini/Geminicli 模型目录、OpenAI 网关调用、模型白名单同步组件；均按最小范围保留定制并补入官方能力。
- **检查**：后端全包编译、前端 `vue-tsc`、定向 63 项前端测试、Docker i18n 4 项、Linux/amd64 镜像构建、三站隔离 Compose 和公网健康检查通过。
- **定制保留**：角色权限、提示词审计、Moshu 生图/批量任务、Key/分组路由、直连入口、品牌文案及使用记录增强。
- **发布决定**：已创建 `custom-0.2.19.1` 并发布。
- **镜像**：`ghcr.io/hzihuan001/sub2api:0.2.19-custom.1`，摘要 `sha256:6f2e1c26aa491b67a038dd8ae98f9cc092c3aed5c38df8eed571ba7f388b5621`。
- **部署**：主站 → L1 → COS 顺序完成；三站 healthy/restart 0，首页和 `/health` HTTP 200，关键启动错误扫描无匹配。
- **回滚证据**：旧应用摘要 `sha256:80adc97b50213ba840e68d6c13b932701cff294f04fb5dcd291ffe12b63f49e6`；部署前备份标识 `v0.2.19-custom.1-20261028T191356Z`，PostgreSQL dump 列表和 SHA256 已校验（主站按应用实际 `sub2api` 数据库重做）。

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



> **2026-10-26 批量参考图最新共享入口（已部署）**：三站已运行 `0.2.18-custom.3`，GPT-image单生图、批量整组统一参考图、批量逐条参考图均最多16张；其他模型仍按各自上限。运行源 `c2dbbe786e844ee1df77bfd710ebaf438f625126`，标签 `custom-0.2.18.3`，正式镜像 `sha256:cb1c937d398989ccff2256b0bf7a7a51c7b63405f8dd5a267a1cd50f47e7b7ac`。发布分支 `codex/image-batch-16refs-20261026` 与远端 `custom/integration` 已普通快进至文档收尾 `c8d14a1183f73cdefa28e698f45bbaf8f8c730b5`。
>
> 本轮记录位于 `D:/projects/sub2api-image-batch-16refs-20261026`：[项目状态](D:/projects/sub2api-image-batch-16refs-20261026/docs/CUSTOM_PROJECT_STATUS.md)、[功能日志](D:/projects/sub2api-image-batch-16refs-20261026/docs/CUSTOM_FEATURE_LOG.md)、[发布验收](D:/projects/sub2api-image-batch-16refs-20261026/docs/CUSTOM_RELEASE_0_2_18_CUSTOM3_20261026.md)。前端92项、后端unit及正式镜像16图mock验收通过；三站healthy/restart0、批量入口3/3与应用资源12/12匹配。无后端运行时/迁移/配置变化，应用回退目标`.18.2`。旧树仅追加此共享指针，原维护记录、其他会话未提交代码及文档完整保留；后续先核对最新远端，不从旧树整树覆盖发布。


> **2026-10-26 生图工作台最新共享入口（已部署）**：三站已运行 `0.2.18-custom.2`，GPT-image参考图上限16；Nano/Gemini14、Seedream10和未知模型4保持。运行源 `879a99a22e47a303604927104a6f67329dcd3b38`，标签 `custom-0.2.18.2`，正式镜像 `sha256:c90609bc4546ec309a980929ec8f28eef3cef236022dd698700920439ce78412`。发布分支 `codex/image-studio-16refs-20261026` 与远端 `custom/integration` 已普通快进至文档收尾 `10b452931592de432db312ffc0a94972c1b89666`。
>
> 本轮权威记录位于 `D:/projects/sub2api-image-studio-16refs-20261026`：[项目状态](D:/projects/sub2api-image-studio-16refs-20261026/docs/CUSTOM_PROJECT_STATUS.md)、[功能日志](D:/projects/sub2api-image-studio-16refs-20261026/docs/CUSTOM_FEATURE_LOG.md)、[发布验收](D:/projects/sub2api-image-studio-16refs-20261026/docs/CUSTOM_RELEASE_0_2_18_CUSTOM2_20261026.md)。三站healthy/restart0，工作台入口3/3、相关JS资源9/9与验收镜像一致；无后端运行时/迁移/配置变化，兼容应用回退目标`.18.1`。本旧工作树仅追加此共享指针，其他会话未提交修改完整保留；下方旧发布坐标为历史，后续先核对最新远端，不从旧树整树覆盖发布。

The custom integration branch is intentionally separate from the plain
upstream-tracking branch. Create it once after the isolation refactor:

```powershell
git switch --create custom/integration <tested-custom-commit>
git push --set-upstream origin custom/integration
```

The canonical upstream mirror ref recorded in `custom/manifest.yml` is
`upstream/main`. The official remote currently exposes `main` (not
`mainline`); keep this metadata aligned if the upstream repository changes its
default branch. Release synchronization still selects immutable `vX.Y.Z` tags.

`.github/workflows/upstream-sync.yml` checks the official `v*` tags weekly
and can also be started manually. It creates `upgrade/upstream-vX.Y.Z`, merges
the selected official tag, applies the ordered `patch_branches` list from
`.github/upstream-sync-manifest.yml`, runs generated-code, migration and test
checks, then opens a draft pull request. A conflict or failed check produces a
blocked draft PR with logs. The job never builds, publishes, or deploys an
application image.

The workflow uses read-only token permissions by default and grants write
access only to the sync job that publishes the upgrade branch and draft PR.
It pins Go from `backend/go.mod`, Node from `.nvmrc`, and pnpm 9.15.9 before
running generation, backend tests, frontend typecheck, and lint. Git merge and
fetch failures are handled explicitly so an unrelated command failure cannot
be silently treated as a successful upgrade.

The default tag lookup selects only stable `vX.Y.Z` tags. A pre-release may be
selected only by supplying `upstream_tag` (or `-UpstreamTag`) explicitly. The
upstream merge is committed before the first custom patch, and every patch is
merged and committed independently. This keeps `MERGE_HEAD` out of the next
merge and makes each patch boundary visible in the draft PR history. Keep
`patch_branches` empty until a feature branch is recreated from
`custom/integration`, contains only that feature's commits, and has passed its
own Docker/CI acceptance; old historical feature branches must not be reused.

For an offline or PowerShell-driven run, the default is a metadata-only dry
run:

```powershell
pwsh -NoProfile -File scripts/sync-upstream.ps1
```

Only an explicitly requested local upgrade creates a branch. Keep the working
tree clean and review the report before pushing:

```powershell
pwsh -NoProfile -File scripts/sync-upstream.ps1 `
  -UpstreamTag vX.Y.Z -Apply -RunChecks
pwsh -NoProfile -File scripts/sync-upstream.ps1 `
  -UpstreamTag vX.Y.Z -Apply -RunChecks -Push -CreatePr
```

The script has no deployment path. Production release remains a separate,
manually approved workflow.

## Project handoff and change history

Before starting an upgrade or a new customization, read
[`CUSTOM_PROJECT_STATUS.md`](CUSTOM_PROJECT_STATUS.md) for the current branch,
release, feature boundaries, production state and upgrade gates. Append every
custom change and every upstream upgrade to
[`CUSTOM_FEATURE_LOG.md`](CUSTOM_FEATURE_LOG.md); do not rewrite historical
entries. These documents contain no credentials or server secrets and are the
stable context for a new maintenance session.

The current `patch_branches` list is intentionally empty. Do not add old
historical branches: they mix deployment and merge history and are not safe to
replay. A feature branch may be added only after it is recreated from
`custom/integration`, contains one isolated feature domain, and passes its own
tests and Docker acceptance. Until then, the sync workflow still provides the
upstream merge, conflict report, range-diff and quality gates, but a human must
review the custom feature boundaries before merging the draft PR.

