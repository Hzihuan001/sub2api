# 定制项目状态与官方升级说明

> 本文是给后续维护会话、代码审查者和自动化工具使用的项目交接说明。
> 先读本文，再读 [`CODEX_HANDOFF.md`](../CODEX_HANDOFF.md)、
> [`UPSTREAM_SYNC.md`](UPSTREAM_SYNC.md) 和
> [`custom/manifest.yml`](../custom/manifest.yml)。
>
> 本文不保存密码、Token、私钥、`.env` 内容或数据库备份路径中的敏感信息。

## 1. 当前快照

### 2026-10-14 — 模块化实施中（本地，不发布）

- 用户已明确执行 P0–P6；在本工作树切新分支 `codex/custom-modularization-v0.2.16`，起点 `989594099`。生产/发布分支仍 `.5`，未修改远程或生产。
- P0 `b3dbe92d6`、P1 `fc16d0875`、P2 `6497dcb0b`、P3 `1e5c18ec6`、P4 `4cbcaafef`、P5 `57ed1ca2d` 已本地提交并通过对应门禁；P6 本地 Docker/PG/Redis 联合验收已通过。尚未推送、建标签或部署。
- 前端完整扫描 409 文件后，修复 3 个测试契约并定向复跑 48 项通过（`ad24ff36a`）；其余 406 文件通过，运行源码未变，复用已通过的类型/构建结果。完整证据见执行记录。
- 当前执行入口：[方案](CUSTOM_MODULARIZATION_PLAN.md)、[逐阶段证据](CUSTOM_MODULARIZATION_EXECUTION.md)。根目录旧源码不修改，推送/部署另行确认。

### 2026-10-15 — P0–P6 本地实施完成（不发布）

- 运行工作树 `codex/custom-modularization-v0.2.16` 已完成 P0–P6。候选镜像和 Docker 验收只使用合成数据、独立 PG/Redis/网络/卷；生产三站仍保持 `.5`。
- 详细阶段提交、测试、已知基线失败及 Docker 证据见 [逐阶段执行记录](CUSTOM_MODULARIZATION_EXECUTION.md)。下一步必须由用户单独确认推送/镜像/生产发布；本轮没有进行任何远程写操作。

### 2026-10-14 — 方案编写阶段（历史，后已获准实施）

- 已编写 [定制层解耦与主线升级减负执行方案](CUSTOM_MODULARIZATION_PLAN.md)，以当前 `.5` 为基线，分 P0–P6 推进基线/门禁、前端/品牌、后端装配、生图/审计接口、故障策略及联合验收。
- 本轮仅文档：不改运行代码、不提交推送、不修改远程设置、不构建或部署。生产继续 `.5`，下面发布记录保持有效。
- 目标是收窄实际依赖和主线接入点，不机械搬目录，不新增业务或拆微服务；API、权限、计费及数据库行为保持。审批执行后先做 P0，推送与生产发布需后续明确授权。

### 2026-10-14 — `.5` 三站发布完成（最新）

- **完成**：`gateway-resilience` 统一可恢复故障切换已发布；运行提交 `00e8f24a40eba481d1e61cfebb5f4ab3f9401a2b`，标签 `custom-0.2.16.5` 已推送。本页下方“准备中 / 未发布”是此前阶段记录，不是当前状态。
- **行为**：同账号 5 分钟累计 3 次明确 overload，默认冷却 10 分钟；额度/凭据/模型不可用及可恢复上游故障按类别限次并换号。参数/安全拒绝、已输出流不自动重放。保留原调度权重、分组权限和计费，不保证无健康备用账号时仍可成功。
- **验证**：相关 Go 单测及 vet、隔离检查、生产构建通过；Docker 11 类合成故障验证通过，无真实模型费用；正式 GHCR 产物回拉与验收 image ID 一致。Actions 仍 disabled，使用本地验收产物直发，不声称 CI 通过。
- **生产**：20:32–20:33（Asia/Shanghai）主站/L1/COS 顺序更新同一 digest；20:34 复核三站 healthy、重启 0，健康与静态入口 HTTP 200、关键启动错误 0。仅重建应用，无新迁移，数据库/Redis/反代保持。
- **备份 / 回滚**：`/opt/sub2api-backups/v0.2.16-custom.5-20261014T120809Z` 已验证。回退 `.4` digest `sha256:a4b7d421e233a1484c92b02780fd2a2421d4ea450fc2a184c82ac253289c045e`，只回退应用，不恢复旧库。
- **完整交接**：[验收及回滚说明](GATEWAY_RESILIENCE_V0_2_16_5_ACCEPTANCE.md)。下一轮从包含 `.5` 的本分支继续，不从落后的集成分支或根目录旧源码发布。

### 2026-10-14 — 网关故障切换统一（`.5` 发布准备中）

- **授权**：用户已要求按错误分类改造、推送、部署三站并更新日志；生产目前仍为 `.4`，以下不是部署成功记录。
- **范围**：保留既有 overload 5 分钟 3 次冷却；在 OpenAI-compatible HTTP/透传/SSE/WS 共用错误语义，明确安全/参数/上下文拒绝不重试，凭据/硬额度/模型不可用直接换号。原生其它提供商继续原有协议，不重写所有网关。
- **策略**：OAuth 普通瞬时 429 同账号恢复窗口从 2 分钟缩至 8 秒；尊重 Retry-After，较长等待直接换号；硬额度无 reset 使用至少 10 分钟可恢复冷却。普通 5xx 复用账号×模型 10/45 秒冷却，pool 不再忽略；520–524 首错 90 秒，504/52x 不原账号反复等待。WS 同次尝试配对错误去重。
- **不变**：分组/模型权限、原调度优先级/评分、真实输出后不自动重放、插件已经提交不盲目重提、计费与数据结构。无新迁移。保留 `.4` 镜像供 app-only 回滚。
- **验证**：相关单测正在完成整合复核；新增 `deploy/tests/gateway-failover-smoke.ps1` 通过隔离容器模拟主备故障及不应重试情景。尚未记录 Docker/生产通过；完成后在下方追加验收记录。

### 2026-10-14 — 本地待发布：跨请求 overload 冷却

- **状态**：`verified`（仅相关本地代码测试）；基于 `custom-0.2.16.4`，改动保留在本工作树 `codex/v0.2.16-image-studio`。没有提交、推送、打标签、构建发布镜像或部署；三站生产继续运行 `.4`。
- **需求**：用户选择同一账号短时间跨请求累计 3 次 overload 后暂时避开。实现为 OpenAI 网关已识别的容量错误在滚动 5 分钟内累计 3 次，冷却复用现有 `overload_cooldown_settings` 开关/时长（未配置默认启用、10 分钟）。首次及第二次保留原请求级重试；第三次不再原账号重试，按既有健康、权限、模型及优先级调度备用。成功请求不清零窗口；一次上游尝试的配对错误帧只能计一次。
- **边界**：HTTP 错误和 HTTP 200 内的流式失败都累计；已输出内容不自动重放，但仍计数以保护后续请求。修正排队心跳被算作输出的独立缺陷，不放宽未知上游数据帧的重放规则。没有承诺已经输出一半的流可无缝续写。
- **保存及恢复**：Redis 原子滚动计数，现有 `accounts.overload_until` 持久化，正常请求不新增 Redis/数据库查询；冷却不因在途错误续期，到期自动恢复。人工恢复同步清本实例计数和 Redis；缓存故障使用有界进程内备用状态。
- **兼容**：新增 `gateway-resilience` 定制登记及升级保护路径；无数据库迁移、生成代码变化、计费或模型权限改动。其它平台原有 529 策略保持。
- **限制**：DB 写入失败时本实例仍暂避账号，持久化补偿由后续 overload 触发，未加后台任务。若以后同一站扩展多应用实例，人工清除没有跨实例本地缓存广播，其它实例可能继续冷却到原截止；当前每站单实例。不要把本地验证写成已部署。
- **验证完成**：服务/handler 相关 overload、重试、SSE 心跳和 runtime-block 回归通过（29.164s/6.994s）；Redis 原子计数、滚动窗口、租约补偿等 9 组通过。管理员恢复与并发清除、WS 三类配对错误回归通过。最终组合 `go test -tags=unit ./internal/custom ./internal/service ./internal/handler ./internal/repository -run 'TestAccountOverload|TestOpenAIRepeatedOverload|TestOpenAIQueueHeartbeat|TestProxyResponsesWebSocketFromClient_CountsOverloadOncePerUpstreamTurn|TestAdminService_ClearAccountError|TestRateLimitService_Clear|TestBuiltInFeatureManifests|TestSameAccount'` 全部通过；四包 `go vet` 通过。迁移/manifest/保护路径检查通过；未改生成文件，完整生成检查因工作树未提交要求 clean 未通过，未声称全量 CI 通过。完整发布门禁、Docker 和生产验收尚未执行。

> **最新进度（2026-10-14）**：`0.2.16-custom.5` 已推送并部署主站、L1、COS，统一故障切换验收通过；包含 `.4` 批量不额外打折和 `.3` Nano Banana 修复。原计费及历史账目不改。详细证据见最新功能日志和验收文档。

> **最近运维（2026-10-14）**：清理本地 15 个、OVH 2 个无引用旧镜像，当前版本和回滚镜像保留；没有删除容器、数据库、卷或缓存，三站仍 healthy、重启 0。详见功能日志的“运维清理”记录，生产版本不变。

### 2026-10-14 批量计费 `.4` 发布完成交接

- **已推送记录**：计费修复 `29945161c`，发布运行提交 `c9f6829730c8f74bbf171e03b7e384e93e2f9ea2`，标签 `custom-0.2.16.4`；发布结果文档随后追加在本分支，文档提交不改变镜像 commit。
- **共享入口**：`D:/projects/sub2api-custom/docs/CUSTOM_PROJECT_STATUS.md` 和 `CUSTOM_FEATURE_LOG.md` 已同步生产快照及本工作树位置；根目录源码仍是旧分支，不得覆盖其中其他会话的未提交修改。
- **当前状态 / 下一步**：本次修复及三站部署完成。后续另行恢复 GitHub Actions，并核对其他功能会话后推进落后的集成分支；恢复前不能认为定时升级可用，不能从旧集成引用丢失 `.3`/`.4` 修复。
- **验证范围**：复用已通过的相关单元/真实 PostgreSQL/前端检查；正式 GHCR 镜像拉回确认 digest 一致，Docker 三图结算/参考图/下载验收通过。生产只重建应用，未发起付费生图，未重建数据库、Redis、网络或卷。

| 项目 | 当前值 |
| --- | --- |
| 记录日期 | 2026-10-14 |
| 官方基线 | `v0.2.16` / `01842576b3d5c1e2545b5a99ad555d36e28ed306` |
| 当前定制版本 | `0.2.16-custom.5`（统一故障切换；包含 `.4` / `.3` 修复） |
| 最近生产版本 | 主站、L1、COS 均为 `0.2.16-custom.5` |
| 最新已发布基线 | `custom-0.2.16.5` / `00e8f24a40eba481d1e61cfebb5f4ab3f9401a2b` |
| 当前工作分支 | `codex/v0.2.16-image-studio` |
| 升级工作树 | `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom` |
| 官方合并 / 生图移植提交 | `d3b78cf8c` / `2c007ec6a`；运行代码修复 `e42db1f11`，验收脚本 `f17cae568` |
| 长期集成基线 | `origin/custom/integration` / `fd7c1b91446a38666b7f5c32ab9d493f6431d071` |
| 当前隔离改造 PR | 草稿 PR #1（如仍未合并）：`codex/isolation-refactor-v0.2.13` → `custom/integration` |
| 官方远程 | `upstream = Wei-Shaw/sub2api` |
| Fork 远程 | `origin = Hzihuan001/sub2api` |
| 主站生产镜像 digest | `sha256:3a2f925574266819adc3cddad9595170cd71404519f315326f83d61098b6a80b` |
| L1/COS 生产镜像 digest | `sha256:3a2f925574266819adc3cddad9595170cd71404519f315326f83d61098b6a80b` |
| 当前发布状态 | `.5` 已发布并部署三站，快速验收通过；旧 `.2` 仍仅保留源码标签 |

生产健康检查最近确认通过的入口：

- `https://ai.moshu.cloud/health`
- `https://api.moshu.cloud/health`
- `https://cos.moshu.cloud/health`

2026-10-12 两个下游入口切换到 Moshu 子域名并开启 Cloudflare 代理；旧 `tokenpulse.top` 入口保留。OVH OpenResty 继续使用 HTTPS、10 小时读写超时和关闭响应缓冲。域名运维记录见功能日志。

本次生产三站统一使用同一镜像 digest：`sha256:3a2f925574266819adc3cddad9595170cd71404519f315326f83d61098b6a80b`。`.5` 备份和回退 `.4` 的命令见本页最新验收文档；此前 `.4` 备份 `/opt/sub2api-backups/v0.2.16-custom.4-20261014T074350Z` 继续保留。所有历史批量任务与价格快照不改，不还原旧库覆盖新业务。

### 批量无额外折扣（2026-10-14，deployed）

- **新任务**：所有 provider 额外批量倍率固定 1，冻结倍率至少 1；普通收费倍率不移除，v2 使用日志区分基础和实际金额。分组 UI 不再显示批量折扣控件。
- **历史保护**：迁移只更新分组两项批量策略和未来记录默认值，不回写任务/使用记录/余额。生产主站 17 个旧任务价格快照和已结算金额与备份一致，L1/COS 原本无批量任务。
- **生产结果**：15:52–15:54（Asia/Shanghai）主站/L1/COS 顺序部署；36/12/12 个分组的折扣均为 1、冻结倍率均至少 1，新迁移每站执行一次；三站 healthy、重启 0，版本/commit/digest/Compose 一致，健康及工作台入口 200，关键启动错误 0。
- **后续边界**：一批使用记录按成功请求子任务汇总；上游超额返回图片的展示数量问题未扩展重构，不自动追加收费。已在途的旧任务仍沿用其创建时价格，不追溯补扣。

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
| `gateway-resilience` | OpenAI 跨请求 overload 冷却及安全重试边界 | `overload_failure_breaker`、Redis 扩展、OpenAI 网关窄接入点及回归 | 本地 verified，未推送/部署 |

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
