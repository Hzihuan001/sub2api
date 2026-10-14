# 定制项目状态与官方升级说明

> 本文是给后续维护会话、代码审查者和自动化工具使用的项目交接说明。
> 先读本文，再读 [`CODEX_HANDOFF.md`](../CODEX_HANDOFF.md)、
> [`UPSTREAM_SYNC.md`](UPSTREAM_SYNC.md) 和
> [`custom/manifest.yml`](../custom/manifest.yml)。
>
> 本文不保存密码、Token、私钥、`.env` 内容或数据库备份路径中的敏感信息。

## 1. 当前快照

> **最新进度（2026-10-14）**：用户确认所有批量生图取消额外五折，并批准三站推送部署。候选 `0.2.16-custom.4` 已通过相关单测、真实 PostgreSQL 迁移测试、前端测试/类型/lint、生产构建及隔离 Docker 三图/并发/下载/结算验收；正在发布，下面生产表仍记录切换前 `.3`。新任务固定批量倍率 1，普通倍率保留；旧任务快照及历史账目不改，超额返回图片不自动追加收费。

> **最近运维（2026-10-14）**：清理本地 15 个、OVH 2 个无引用旧镜像，当前版本和回滚镜像保留；没有删除容器、数据库、卷或缓存，三站仍 healthy、重启 0。详见功能日志的“运维清理”记录，生产版本不变。

### 2026-10-14 发布完成交接

- **已推送记录**：运行版本 `.3` 对应 `a1eb0aaac`；完整发布文档已推送为 `da654fb2c`。两者区分记录，文档提交不改变镜像 commit。
- **共享入口**：`D:/projects/sub2api-custom/docs/CUSTOM_PROJECT_STATUS.md` 和 `CUSTOM_FEATURE_LOG.md` 已同步生产快照及本工作树位置；根目录源码仍是旧分支，不得覆盖其中其他会话的未提交修改。
- **当前状态 / 下一步**：本次修复及三站部署完成。后续另行恢复 GitHub Actions，并核对其他功能会话后推进落后的集成分支；恢复前不能认为定时升级可用，不能从旧集成引用丢失 `.3` 修复。
- **此次同步范围**：只更新本地共享文档，未另行提交推送；没有修改代码、重跑全量测试、创建新版本或操作生产。下面的验证结果来自已完成发布验收。

| 项目 | 当前值 |
| --- | --- |
| 记录日期 | 2026-10-14 |
| 官方基线 | `v0.2.16` / `01842576b3d5c1e2545b5a99ad555d36e28ed306` |
| 当前定制版本 | `0.2.16-custom.3`（Nano Banana 模型目录兼容修复；`.2` 未部署） |
| 最近生产版本 | 主站、L1、COS 均为 `0.2.16-custom.3` |
| 最新已发布基线 | `custom-0.2.16.3` / `a1eb0aaac70cda552eb92882305e6f5241c95418` |
| 当前工作分支 | `codex/v0.2.16-image-studio` |
| 升级工作树 | `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom` |
| 官方合并 / 生图移植提交 | `d3b78cf8c` / `2c007ec6a`；运行代码修复 `e42db1f11`，验收脚本 `f17cae568` |
| 长期集成基线 | `origin/custom/integration` / `fd7c1b91446a38666b7f5c32ab9d493f6431d071` |
| 当前隔离改造 PR | 草稿 PR #1（如仍未合并）：`codex/isolation-refactor-v0.2.13` → `custom/integration` |
| 官方远程 | `upstream = Wei-Shaw/sub2api` |
| Fork 远程 | `origin = Hzihuan001/sub2api` |
| 主站生产镜像 digest | `sha256:a96e2a5a52e0d7a83a9829371715359b2afcc0a02168fe8bded77b55dcfdeec3` |
| L1/COS 生产镜像 digest | `sha256:a96e2a5a52e0d7a83a9829371715359b2afcc0a02168fe8bded77b55dcfdeec3` |
| 当前发布状态 | `.3` 已发布并部署三站，快速验收通过；`.2` 仅保留源码标签，未发布镜像或部署 |

生产健康检查最近确认通过的入口：

- `https://ai.moshu.cloud/health`
- `https://api.moshu.cloud/health`
- `https://cos.moshu.cloud/health`

2026-10-12 两个下游入口切换到 Moshu 子域名并开启 Cloudflare 代理；旧 `tokenpulse.top` 入口保留。OVH OpenResty 继续使用 HTTPS、10 小时读写超时和关闭响应缓冲。域名运维记录见功能日志。

本次生产三站统一使用同一镜像 digest：`sha256:a96e2a5a52e0d7a83a9829371715359b2afcc0a02168fe8bded77b55dcfdeec3`。发布前完整备份标识为 `/opt/sub2api-backups/v0.2.16-custom.2-20261014T031705Z`（名称沿用最初候选）；三份数据库目录、数据归档和校验和已验证。切换前另有 `.3-config-*` 各站配置快照。回滚恢复 `.1` 的 digest `sha256:edf55576e8ea73d2a552d7489cabdd2a6f282290e2ac713cda0d5504a55f5f17`，只重建应用；本热修复没有数据库迁移。

### Nano Banana 热修复（2026-10-14，deployed）

- **根因**：第三方 Gemini 上游在 `/v1beta/models` 返回 OpenAI `data[].id` 信封，原工作台只读取 `models[].name`；模型真实存在，不是账号停用或白名单拒绝。
- **边界**：`gemini_models_endpoints.go` 将成功的兼容目录转为 Gemini 格式后再经过原有分组白名单；前端兼容两种信封，原生空列表保持空，不能从备用字段重新填充被权限过滤的模型。
- **不变**：生成路径、调度、计费、尺寸、参考图和下载；不额外查询停用/限流账号，不注入静态假模型，无数据库迁移。
- **验证**：受影响 service/handler 单测、Go vet 通过；前端 API/视图 25 项、typecheck、定向 lint、Docker 生产构建通过。隔离容器用兼容格式与非法条目验证允许/拒绝模型目录通过。发布镜像拉回本地与验收镜像 digest 完全一致。
- **生产验收**：13:28–13:30（Asia/Shanghai）顺序完成主站/L1/COS；三站 healthy、重启 0、版本/commit/digest 一致，内外 `/health` 和公网工作台入口 200，启动后无 panic/fatal/迁移失败/数据库或 Redis 错误。主站真实 Nano Banana 分组密钥对本机与公网 `/v1beta/models` 均返回 `models/gemini-nano-banana-2.1`；未提交生产付费生图。
- **发布方式**：代码分支和 `custom-0.2.16.3` 已推送。GitHub 工作流 dispatch 仍返回 `Actions has been disabled`，按上一轮方式在本地门禁通过后直接发布已验收镜像到 GHCR；不把此次记录称作 CI 通过。
- **后续**：下次升级从 `.3` 或包含它的更新提交开始；保留两种模型目录信封、异常条目和白名单回归。下一步可修复 GitHub Actions 运行限制；不影响本次生产完成。

### `.1` 官方升级验收摘要（2026-10-14，历史证据）

- **Feature ID**：官方升级及 `image-studio`；状态：`deployed`，本地验收见 [`UPGRADE_V0_2_16_ACCEPTANCE.md`](UPGRADE_V0_2_16_ACCEPTANCE.md)，问题处理见 [`CUSTOM_UPGRADE_ISSUES.md`](CUSTOM_UPGRADE_ISSUES.md)。
- **移植来源**：`codex/image-studio-session-20261004` / `01124885d` 中 14 个功能提交；不整分支合并旧 v0.2.13，不移植旧 VERSION，不重复移植已发布的用户隔离画廊。
- **生图行为**：Nano Banana/Gemini 原生模型发现与 IMAGE 路由；Seedream 5 部署别名、模型特定尺寸/参考图/格式约束；原生 Gemini 保留 1K/2K/4K 档位且不接受自定义像素；返回图片按真实 MIME 下载。仍隐藏实际像素文字、不本地放大、不改变输出优先计费。
- **异步安全**：仅 BananaRouter 明确能力账号走异步协议；提交意图和 task ID 保存到现有 item JSONB，重启恢复轮询，取消后保留已成功图片并正常结算。未知提交结果超过 24 小时需要人工核账，不盲目重提或退款；部署前应等待旧版未持久化 task ID 的在途任务完成。
- **数据与兼容**：本次没有定制数据库迁移；追加官方 242-245 迁移，历史迁移文件名/校验值不改。浏览器 IndexedDB v2 用户隔离保持。
- **已有验证**：前端全量 401 文件/2998 项通过，最终窄修复另有 25 项回归、平台 5 项、lint、typecheck/生产构建通过；真实 PostgreSQL 批量任务/缓存/分组与异步 CAS 集成测试通过；Ent 临时目录完整生成比对无差异，Wire 生成通过。后端唯一失败的协议测试夹具修复后连续 50 次及相关回归通过。
- **Docker 验收**：`sub2api-operator-test-v0216` / `http://127.0.0.1:18086`；权限、重启持久化、mock 生图/三张 fan-out/参考图/MIME/单张与 ZIP 下载、合成结算通过。四个官方新迁移完成（318 → 322），同数据库旧镜像回滚启动/登录/模型发现通过。候选已恢复。
- **生产验收**：主站、L1、COS 容器均 healthy，重启次数为 0；`https://ai.moshu.cloud/health`、`https://api.moshu.cloud/health`、`https://cos.moshu.cloud/health` 均返回 200。三站运行版本、commit 和 digest 一致，应用日志未见 panic/fatal/迁移失败/数据库或 Redis 错误。Redis 认证连通，未重建数据服务。
- **清理**：保留当前 digest、主站旧 digest、L1/COS 旧 digest和 rollback 镜像；删除未使用的旧 Sub2API 镜像、CPA `v8.0.23`/`v8.0.8` 和可回收构建缓存。未删除卷、网络、当前 CPA、证书或数据库镜像。
- **协作注意**：根目录 `D:/projects/sub2api-custom` 有其他会话的未提交公共文档/DNS记录；不要重置或覆盖。`custom/integration` 仍落后，升级必须先核对最新发布与功能会话提交，验收后再推进集成基线。

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

> **流程生效前提（2026-10-14）**：第 3–5 节描述已实现的升级机制；Actions 恢复且 `custom/integration` 确认包含最新已验收功能之前，不得直接依赖定时升级或从旧集成基线开始。

是的，和过去直接在生产分支上叠加定制代码相比，下一次官方主线更新的合并难度会大幅降低，原因是：

1. 官方代码、长期集成基线和临时升级分支已经分离。升级前先确认 `custom/integration` 包含最近验收代码；如落后，明确选择最新已提交定制基线并记录遗漏提交，不能从过期分支开始或整合历史旧分支。
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

版本更新会话只消费已登记且可验证的定制边界：先验证 `custom/integration` 与最新已验收提交一致，再通过自动升级 PR 合并官方版本，并按日志中的测试和发布证据复核功能。基线落后时必须记录真实起点与选择性回补提交。不能把聊天内容当作已完成代码，也不能把已移除代理商功能重新套回当前版本。

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
