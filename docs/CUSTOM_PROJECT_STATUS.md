# 定制项目状态与官方升级说明

> 本文是给后续维护会话、代码审查者和自动化工具使用的项目交接说明。
> 先读本文，再读 [`CODEX_HANDOFF.md`](../CODEX_HANDOFF.md)、
> [`UPSTREAM_SYNC.md`](UPSTREAM_SYNC.md) 和
> [`custom/manifest.yml`](../custom/manifest.yml)。
>
> 本文不保存密码、Token、私钥、`.env` 内容或数据库备份路径中的敏感信息。

## 1. 当前快照

| 项目 | 当前值 |
| --- | --- |
| 记录日期 | 2026-10-04 |
| 官方基线 | `v0.2.13` / `3040209f205472038c1ba745a1bedd2edd9053b1` |
| 最近生产版本 | `0.2.13-custom.6` |
| 最近生产标签 | `custom-0.2.13.6` / `1770b4c1b983a062e62dfd78dd5c34ae79071e24` |
| 已发布候选版本 | `0.2.13-custom.7` / `custom-0.2.13.7` / `045d95cbb289a92b9247309b222f04ec1e1f54e6` |
| 候选镜像 digest | `ghcr.io/hzihuan001/sub2api@sha256:74a504284b0368f7c8187b98335cbca69cf4fe56b079c814986b31a724e8895a` |
| 当前工作分支 | `codex/image-studio-session-20261004`（提交 `f4c902529`；父工作树未修改） |
| 协作父分支/工作树 | `codex/isolation-refactor-v0.2.13` / `07bf51ba0`；父工作树保持 clean，未被本会话修改 |
| 同步框架 HEAD | `1fa5f4478`（本文档记录前；文档提交后的当前 HEAD 以 `git log` 为准） |
| 长期集成基线 | `origin/custom/integration` / `fd7c1b91446a38666b7f5c32ab9d493f6431d071` |
| 当前隔离改造 PR | 草稿 PR #1（如仍未合并）：`codex/isolation-refactor-v0.2.13` → `custom/integration` |
| 官方远程 | `upstream = Wei-Shaw/sub2api` |
| Fork 远程 | `origin = Hzihuan001/sub2api` |
| 生产镜像 | `ghcr.io/hzihuan001/sub2api@sha256:1dc23cebaa1b8fb3d7629c0a84581c5f8f6b006268d19dff6cd5f832eef59c48` |
| 当前发布状态 | `.7` 已推送并发布到 GHCR；生产仍运行 `.6`，部署等待 staging SSH 访问和验收门禁 |

生产健康检查最近确认通过的入口：

- `https://ai.moshu.cloud/health`
- `https://api.tokenpulse.top/health`
- `https://cos.tokenpulse.top/health`

生产发布必须继续使用通过验收的同一镜像 digest，不在不同服务器重新构建。

### 当前会话摘要（2026-10-04）

- **Feature ID**：`image-studio`；状态：`blocked`（代码、CI、GHCR 发布已完成；部署被 staging SSH 公钥认证失败阻塞）。
- **实现边界**：将浏览器端生图画廊改为按认证用户隔离的 IndexedDB v2 命名空间；账号切换时清理旧用户的内存结果、预览和进行中的生成，并对异步读取/保存做作用域校验；工作台透传自定义尺寸与扩展质量档位，内部测量并保存最终文件实际像素，但界面不展示实际尺寸、不弹出尺寸不匹配数字提示，也不做本地放大；Gemini 原生批量请求转发 `generationConfig.imageConfig.aspectRatio` 与 `imageSize`；API-key Images 非流式响应从 Base64/内联 data URL 回填实际尺寸元数据。
- **数据与兼容**：没有后端数据库迁移；旧的 v1 全局 IndexedDB 保留但不自动迁移/读取，以避免跨账号显示历史内容。
- **验证证据**：image-studio/API/size/视图前端 4 个测试文件 `18/18`；完整前端 lint/typecheck/test/build、完整后端 unit、隔离检查和发布 CI 均通过；GHCR 已发布候选 digest。修正 operator 验收脚本后，发布镜像隔离容器完整验收通过（包含角色登录、权限矩阵、API Key、WebSocket、重启持久化和日志检查）。
- **已知限制/下一步**：工作台只在内部验证最终文件像素，不能证明模型内部原生生成；CPA/ChatGPT OAuth 上游仍可能忽略或改写 4K 请求，界面不显示实际尺寸，也不会伪造或自动放大。OVH 生产仅完成只读预检，仍运行 `.6`；Tencent staging 地址 `101.34.249.20` 当前无法使用现有 SSH 公钥认证，因此未执行 staging/生产写操作。下一步需恢复 staging SSH 访问并完成同一 digest 的 staging 验收，再按发布运行手册备份并部署生产；在独立补丁栈验收前继续保持 `.github/upstream-sync-manifest.yml` 的 `patch_branches: []`。

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

是的，和过去直接在生产分支上叠加定制代码相比，下一次官方主线更新的合并难度会大幅降低，原因是：

1. 官方代码、长期集成基线和临时升级分支已经分离。升级以 `custom/integration` 为唯一基线，不再继续使用历史的 `codex/v0.2.13-merge-gpt-reference` 作为新升级起点。
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
