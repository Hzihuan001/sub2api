# 定制功能日志

> 本文件是 append-only 变更账本。每次定制、官方升级、验收和发布都追加记录，
> 不覆盖旧记录。详细操作流程见 [`CUSTOM_PROJECT_STATUS.md`](CUSTOM_PROJECT_STATUS.md)。
>
> 记录从 `custom-0.1.179.1` 开始。早期代理商/分站功能属于历史阶段；自
> v0.2.8 后代理商运行时已移除，不能在后续升级中误恢复。

## 当前状态（2026-10-04）

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
