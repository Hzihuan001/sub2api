# 定制功能日志

> 本文件是 append-only 变更账本。每次定制、官方升级、验收和发布都追加记录，
> 不覆盖旧记录。详细操作流程见 [`CUSTOM_PROJECT_STATUS.md`](CUSTOM_PROJECT_STATUS.md)。
>
> 记录从 `custom-0.1.179.1` 开始。早期代理商/分站功能属于历史阶段；自
> v0.2.8 后代理商运行时已移除，不能在后续升级中误恢复。

## 当前状态（2026-10-11）

- 最近发布：`custom-0.2.15.2`，官方基线 `v0.2.15`。
- 当前生产镜像：`ghcr.io/hzihuan001/sub2api@sha256:7b49d3d555c3427917fbfc9315497214a74df3ddb49c6a9c17ee5ecf13726eab`。
- 当前升级工作分支：`codex/v0.2.15-image-studio-sync`；已补回 image-studio 独立补丁并完成生产部署。
- 长期集成基线：`origin/custom/integration`，当前为 `fd7c1b914`。
- 本文件和项目状态文档不包含服务器凭据、API Key、私钥、`.env` 或生产数据库内容。

### 2026-10-11 — `custom-0.2.15.2` — v0.2.15 生图工作台补丁回补与发布

- **原因**：v0.2.15 合并与发布时遗漏了独立生图会话中的工作台改动。
- **处理**：按功能边界移植 1K/2K/4K/自定义尺寸、批量尺寸持久化与重试、Gemini `imageConfig` 透传、API-key 图片实际像素内部解析、按用户隔离的 IndexedDB v2 画廊，以及不在界面展示实际尺寸的行为；未带入旧版本号、旧发布记录或代理商代码。
- **提交来源**：`045d95cbb`、`7a676681b`、`efc19f645`（仅选择生图代码和测试，按 v0.2.15 当前结构合并）。
- **数据库/迁移**：无新增后端迁移；沿用批量 item `input_payload` 保存尺寸，旧任务无尺寸时回退 1K。
- **验证**：后端 image/Gemini 定向测试通过；前端生图定向测试 `19/19`，全量 Vitest `2915/2915`，typecheck、lint、生产构建、custom isolation 和本地 Docker 联合验收均通过。
- **发布**：提交 `eb509b8ef`，标签 `custom-0.2.15.2`，GHCR 镜像 digest 为 `sha256:7b49d3d555c3427917fbfc9315497214a74df3ddb49c6a9c17ee5ecf13726eab`；GitHub Actions 因 Fork 工作流限制未能 dispatch，使用已通过同等质量门禁的本地构建镜像发布。
- **生产部署**：OVH 主站已只重建应用容器；`https://ai.moshu.cloud/health`、L1、COS 健康检查均返回 `{"status":"ok"}`，主站容器 healthy、重启次数为 0，PostgreSQL/Redis 容器 ID 保持不变。备份目录：`/root/sub2api-release-backups/custom-0.2.15.2-20261010T182500Z`。
- **回滚**：恢复生产 Compose 备份中的旧 digest 后仅重建主站应用容器；本次无新增数据库迁移，不需要回滚数据库。

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

### 2026-10-04 — 未发布 — image-studio 账号隔离与 Gemini 尺寸转发

- **需求**：本会话只处理 `image-studio`，在独立工作树中修复并验证，不覆盖其他会话或父工作树的修改。
- **Feature ID**：`image-studio`
- **官方基线**：`v0.2.13` / `3040209f2`；本会话分支 `codex/image-studio-session-20261004` 基于 `07bf51ba0`。
- **实现边界**：`frontend/src/features/image-studio/library.ts` 使用版本化、按认证用户编码的 IndexedDB 命名空间；`frontend/src/views/user/ImageStudioView.vue` 在账号切换、异步加载、生成保存、删除和清空时校验作用域并清理陈旧内存结果；`backend/internal/service/batch_image_provider_gemini.go` 将 `aspectRatio`/`imageSize` 写入 Gemini 原生 `generationConfig.imageConfig`，并保留旧请求在未提供尺寸时的省略行为；`ResponseMimeType` 不写入该图像配置；对应回归测试位于 `frontend/src/features/image-studio/__tests__/library.spec.ts` 和 `backend/internal/service/batch_image_provider_gemini_test.go`。
- **用户可见行为**：不同登录用户只读取自己的本地画廊；旧的 v1 全局画廊数据库保留但不自动迁移或读取，避免旧数据跨账号显示。Gemini 批量生图会按请求传递比例和尺寸配置（参见 [Gemini Batch API](https://ai.google.dev/gemini-api/docs/batch-api) 与 [GenerateContent API](https://ai.google.dev/api/generate-content)）。
- **数据库/迁移**：无后端数据库迁移；仅新增浏览器 IndexedDB v2 命名空间，旧 v1 数据不删除。
- **测试**：image-studio 前端 3 个测试文件 `14/14`；定制回归 4 个测试文件 `36/36`；`pnpm@9.15.9 run typecheck`、定向 ESLint、隔离检查均通过；Docker `golang:1.27` `go test -tags=unit ./internal/service -run 'BatchImage|Gemini' -count=1` 与 `go vet -tags=unit ./internal/service` 均通过。
- **提交/PR**：仅本地未提交，未推送、未创建 PR；父工作树 `codex/isolation-refactor-v0.2.13` 保持 clean。
- **发布镜像**：无；继续使用已记录的 `custom-0.2.13.6` 镜像，未产生新发布坐标。
- **部署站点**：未部署，未执行生产操作。
- **回滚**：放弃本会话分支的未提交改动即可回滚；旧 v1 浏览器数据库未改动，无后端数据库恢复要求。
- **备注**：`.github/upstream-sync-manifest.yml` 的 `patch_branches` 仍保持 `[]`，待独立补丁分支从 `custom/integration` 重建并完成验收后再登记；宿主机无 Go，后端测试使用 Docker 完成。

### 2026-10-04 — 未发布 — image-studio 返回尺寸可验证性与 4K 工作台优化

- **需求**：结合 CPA 实测低于请求尺寸的情况，优化工作台，使请求尺寸、实际返回像素和原生 4K 能力不再混淆；本会话仍只处理 `image-studio`，不覆盖其他会话修改。
- **Feature ID**：`image-studio`
- **官方基线**：`v0.2.13` / `3040209f2`；本地分支 `codex/image-studio-session-20261004`，基于 `07bf51ba0`。
- **实现边界**：前端 `ImageStudioView.vue` 增加 3840×2160/2160×3840 预设、`xhigh/max` 质量选项、请求/实际尺寸状态徽标和预览说明；`features/image-studio/size.ts` 增加像素尺寸核验；API 测试锁定 `gpt-image-2.5-sunburst`、4K 尺寸和 `max` 原样透传；后端 `openai_images.go` 对 API-key 非流式 `b64_json`/内联 data URL 解析实际尺寸并覆盖错误的尺寸回显。
- **用户可见行为**：作品卡片和预览同时显示请求尺寸与返回文件实际像素；尺寸不匹配会提示上游未按请求返回，工作台不会超分或插值放大；尺寸匹配仅证明最终文件像素匹配，不宣称模型内部原生生成。
- **数据库/迁移**：无后端数据库迁移；继续使用按用户隔离的 IndexedDB v2 命名空间。
- **测试**：前端 image-studio/API/size `3 files / 16 tests passed`；前端 typecheck、定向 ESLint 通过；Docker `go test -tags=unit ./internal/service` 图片/Gemini 定向用例、`go vet -tags=unit ./internal/service`、`gofmt` 和 `git diff --check` 通过。
- **提交/PR**：仅本地未提交，未推送、未创建 PR；父工作树保持 clean。
- **发布镜像**：无；未改变已记录的 `custom-0.2.13.6` 镜像。
- **部署站点**：未部署，未执行生产操作。
- **回滚**：放弃本会话未提交改动即可回滚；无新增后端迁移或不可逆数据操作。
- **备注**：CPA/ChatGPT OAuth 上游是否真正返回原生 3840×2160 仍需用实际出站请求和返回文件验证；本优化只提高透传、观测和诚实展示，不把超分结果标成原生 4K。

### 2026-10-04 — 未发布 — image-studio 尺寸链路收敛

- **需求**：聚焦请求尺寸与实际图片质量，不增加复杂功能按钮或尺寸展示负担；解决“用户请求尺寸”和“实际返回像素”不一致时的误判。
- **Feature ID**：`image-studio`
- **官方基线**：`v0.2.13` / `3040209f2`；本地分支 `codex/image-studio-session-20261004`，基于 `07bf51ba0`。
- **实现边界**：补充 `gpt-image-2.5-sunburst` + `3840x2160` + `max` 的 OAuth 出站契约测试；工作台保留实际像素检测和元数据记录，尺寸不匹配时只显示一次告警；不做本地插值或超分，不新增复杂尺寸徽标。
- **用户可见行为**：结果按上游返回的原始文件保存，元数据保留用户请求尺寸；若上游返回不同像素尺寸，提示“请求尺寸 → 实际尺寸”，并明确工作台未放大图片。
- **数据库/迁移**：无；继续使用按用户隔离的 IndexedDB v2 命名空间。
- **测试**：前端 image-studio/API/size `3 files / 16 tests passed`；`pnpm@9.15.9 run typecheck`、定向 ESLint 通过；Docker `golang:1.27` 图片/Gemini 定向 Go 测试（含 Sunburst 尺寸质量契约）通过；目标 Go 文件 `gofmt -d` 与 `git diff --check` 通过。
- **提交/PR**：仅本地未提交，未推送、未创建 PR；父工作树保持 clean。
- **发布镜像**：无；未改变已记录的 `custom-0.2.13.6` 镜像。
- **部署站点**：未部署，未执行生产操作。
- **回滚**：放弃本会话分支的未提交改动即可回滚；无新增后端迁移或不可逆数据操作。
- **备注**：当前代码已覆盖 API-key、OAuth 直调和 Responses 兼容路径的尺寸/质量透传，但 CPA/ChatGPT 上游仍可能忽略或改写 4K 请求。要保证原生 `3840x2160`，需要 CPA 适配器的出站/返回证据或直接使用支持该尺寸的官方 Images API-key 路径；工作台不会把超分结果标成原生 4K。

### 2026-10-04 — 未发布 — image-studio 隐藏实际尺寸显示

- **需求**：工作台不向用户显示生成文件的实际像素尺寸，保留内部校验和元数据能力。
- **Feature ID**：`image-studio`
- **官方基线**：`v0.2.13` / `3040209f2`；本地分支 `codex/image-studio-session-20261004`，基于 `07bf51ba0`。
- **实现边界**：`ImageStudioView.vue` 移除实际尺寸和不匹配数字提示；保留 `actualSize` 的内部检测与 IndexedDB 元数据；移除不再使用的实际尺寸 i18n 文案；新增视图回归测试确认生成、作品库和预览均不渲染实际像素。
- **用户可见行为**：工作台只显示用户请求的尺寸；不展示 `1672x941` 等上游返回尺寸，不弹出“请求尺寸 → 实际尺寸”提示。
- **数据库/迁移**：无；无服务端数据结构变化。
- **测试**：视图/接口/图库/尺寸前端测试 `4 files / 18 tests passed`；类型检查、定向 ESLint、现有后端图片/Gemini 测试保持通过；待完整发布 CI 再记录 GHCR digest。
- **提交/PR**：本地改动待提交；未推送、未创建新 PR。
- **发布镜像**：无；目标版本 `0.2.13-custom.7` / `custom-0.2.13.7`。
- **部署站点**：未部署。
- **回滚**：恢复上一个已验证镜像即可；无新增迁移。
- **备注**：实际尺寸仍只用于内部质量核验和调试元数据，不改变上游输出，也不执行本地放大。

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
