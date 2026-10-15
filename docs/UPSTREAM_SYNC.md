# 官方主线同步说明

本仓库把官方主线、长期定制集成分支和临时升级分支分开。预期流程是每次官方发布后，先由同步工作流生成升级分支、冲突报告和测试结果，再由人工确认定制功能边界；同步工作流不负责构建、部署或修改生产环境。

2026-10-15 当前边界：上一轮 workflow dispatch 为 HTTP 422 `Actions has been disabled for this repository`；本轮按 `.7` 运行 f36/分支 3ce 查询均无 Actions 运行。离线升级脚本/YAML Shell 检查不代表真实 Actions、草稿 PR 和报告链路成功。用户批准的 `.7` 本地 GHCR 发布已完成，运行 `f36de3d4a` / `custom-0.2.16.7` 已部署三站，精确镜像本地验收、GHCR 回拉、备份和逐站检查通过，见 [发布报告](CUSTOM_RELEASE_0_2_16_CUSTOM7_20261015.md)。

发布分支与 `origin/custom/integration` 已推送 `3ce7de3ebe68263a3f0e8bf6a83393486747306f`，包含正式运行 `f36de3d4a00400c66994a02c13c5d23f6a7feac8`；本轮文档随后提交同步，精确 ref 由 Git 核验。这仍不是远端自动升级 PR 链路成功。下次升级从 [项目状态](CUSTOM_PROJECT_STATUS.md) 和实际远端重新核对完整功能集合，不从历史 `.6` 收尾点遗漏主备 Key 模块。

标准顺序：

1. 读取 [`CUSTOM_PROJECT_STATUS.md`](CUSTOM_PROJECT_STATUS.md)、[`CUSTOM_FEATURE_LOG.md`](CUSTOM_FEATURE_LOG.md) 和 `custom/manifest.yml`。
2. 先确认 `custom/integration` 包含最新已验收定制提交和共享日志登记的功能提交；落后时明确选择最新已提交定制基线并记录差异，不从旧功能分支整分支合并。然后合并指定官方标签，复核 `operator`、`prompt-audit`、`image-studio`、`branding`、`usage-extras`、`gateway-resilience` 和 `key-routing`；后者的备用组存储、缓存失效、有效组计费及禁止跨组重播不能因宿主升级丢失。
3. 只允许追加数据库迁移；生成代码按当前官方工具重新生成，不用旧分支覆盖。
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
