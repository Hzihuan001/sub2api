# 定制功能日志

> 本文件是 append-only 变更账本。每次定制、官方升级、验收和发布都追加记录，
> 不覆盖旧记录。详细操作流程见 [`CUSTOM_PROJECT_STATUS.md`](CUSTOM_PROJECT_STATUS.md)。
>
> 记录从 `custom-0.1.179.1` 开始。早期代理商/分站功能属于历史阶段；自
> v0.2.8 后代理商运行时已移除，不能在后续升级中误恢复。

### 2026-10-14 — 实现与本地验收通过 — 统一取消批量额外折扣（`.4` 发布中）

- **授权 / 边界**：用户明确所有提供商都不再有批量五折，并要求直接推送部署。新建任务价格快照版本 2、批量倍率固定 1；分组/用户/账号等原有倍率保留。只按成功的请求子任务收费，不对上游额外返回图片自动补扣。
- **实现**：分组新建/编辑/复制兼容旧请求但批量倍率统一为 1，冻结倍率至少 1（大于 1 的配置保留）；移除前端折扣控件。v2 使用日志原始金额为基础单价乘成功数，实际金额单独保留。v0/v1 快照、在途任务及历史账目不重新计价。
- **迁移**：新增 `9000_custom_batch_image_no_discount.sql`，仅规范现有分组的批量折扣/冻结字段和未来插入默认值；不修改旧迁移校验值、不更新现有任务/用户余额/历史使用记录。
- **验证**：后端 batch/group/duplicate 单测与 vet、真实 PostgreSQL 幂等迁移及旧记录逐字段保护测试通过；前端 33 项、typecheck、定向 ESLint 通过。Docker 完整生产构建及 mock 三图并发/参考图/单张和 ZIP 下载通过，三图总价 $0.03、usage 原始/实际金额均 $0.03、倍率 1，未调用付费上游。
- **发布状态**：候选 `0.2.16-custom.4`；生产尚为 `.3`，后续发布结果另行追加。GitHub Actions 仍有仓库禁用限制，沿用本地门禁后直发 GHCR，不宣称 CI 通过。

### 2026-10-14 — 只读诊断 — 批量生图计价与图片数

- **状态**：发现问题，未修复/未发布/未调整生产费率或历史扣费；用户尚未确认具体批次，以下为最近生产记录与代码交叉核对。
- **已验证样本**：主站 14:57（Asia/Shanghai）`gpt-image-2`，使用记录 #190308：2 项成功，每项 1 张，基础单价 $0.03，批量折扣 0.5，结算单价 $0.015，总金额 $0.03，使用记录图片数为 2。因此该笔不是漏乘 2，而是应用了五折；一批仅写一条汇总记录。
- **原因**：`batch_image_public.go` 的价格快照对 app-managed 单图请求队列也沿用批量折扣，`batch_image_settlement.go` 按成功 item 数乘快照单价。历史默认批量折扣为 0.5；不能据此推断上游普通请求也提供折扣。
- **额外发现**：主站另一批次明细的 image_count 合计 7，而汇总/用量记为 6；代码按成功子任务计数而不是图片数。批量 usage 的 total_cost 已含折扣但 UI 又标为原始金额，展示口径也需要校正。
- **建议待确认**：应用层拆单批量默认与普通单图同价，折扣必须显式配置；统一请求张数、返回张数、收费张数与日志金额语义。上游超额返回图片不能未经确认自动追加收费；历史记录不自动补扣。定位证据来自只读事务和源码，未发起付费测试。

### 2026-10-14 — 运维清理 — 无引用旧镜像

- **范围 / 结果**：按精确镜像 ID 删除本地 Docker 15 个无容器引用的旧应用/验收镜像，以及 OVH 的 2 个旧发布镜像（`0.2.15-custom.2`、`0.2.15-custom.1`）。不使用全局 prune 或强制删除；每次删除前重新检查所有运行和停止容器的引用。
- **保留**：三站当前 `a96e2a5…`、直接回滚 `edf55576…`，以及历史显式 rollback `0390d87…`；所有容器引用镜像、宣传站、CPA、数据库/Redis、基础工具镜像均保留。没有删除容器、卷、网络、备份或构建缓存。
- **验证**：本地 16 个容器、OVH 14 个容器的镜像与运行/停止状态前后未变。三站仍 healthy、重启 0；OVH 根盘可用空间从 48,357,138,432 增至 48,699,949,056 字节，约增加 327 MiB。本地未把镜像标称大小相加当作实际回收量，也未压缩 Docker 虚拟磁盘。
- **恢复 / 记录**：已发布旧镜像可从 GHCR 按原 tag/digest 重新拉取；本地临时验收镜像需由对应源码重建，原构建产物已删除。本次仅清理及同步本地文档，没有新版本或部署；本日志尚未另行推送。

### 2026-10-14 — 共享进度同步 — 发布完成后的多会话交接

- **阅读优先级**：历史条目中的“当前”“未发布”“发布中”均指记录当时；实时进度以 `CUSTOM_PROJECT_STATUS.md` 和最近完成的发布条目为准，历史记录不改写。
- **完成坐标**：三站 `0.2.16-custom.3`，运行提交 `a1eb0aaac`；完整发布文档已推送为 `da654fb2c`。根工作树共享入口已同步本工作树与最新生产快照，旧根目录源码未更换。
- **本次范围 / 待办**：仅本地文档同步，未另行提交推送，也没有改代码或重新部署。Actions disabled 和集成分支落后仍是后续事项；待恢复和核查其他功能会话后推进，不认为自动升级已恢复。

### 2026-10-14 — 已部署 — `0.2.16-custom.3` Nano Banana 目录兼容

- **Feature ID / 来源**：`image-studio`，官方基线 `v0.2.16`。修复提交 `5db535f6c`、异常载荷过滤加固 `a1eb0aaac70cda552eb92882305e6f5241c95418`；分支 `codex/v0.2.16-image-studio`、标签 `custom-0.2.16.3` 已推送。`.2` 仅源码标签，未发布镜像或部署，历史标签未改写。
- **行为**：兼容第三方 `/v1beta/models` 返回 OpenAI `data[].id`；后端逐条转换后应用分组白名单，前端读取两种格式及 Gemini `displayName`。空原生目录不回填、错误条目不跳过白名单、不注入静态模型。生图/调度/计费/下载逻辑保持，无迁移。
- **验证**：后端相关 service/handler 单测与 vet、前端 25 项/typecheck/定向 ESLint、生产构建及隔离规则检查通过。真实 Docker 网关验证异常兼容目录中的 Nano Banana 允许与拒绝均正确；测试夹具中的用户余额和 mock 启动方式修正后通过，未为夹具问题改业务逻辑。
- **镜像**：`ghcr.io/hzihuan001/sub2api:0.2.16-custom.3`；`sha256:a96e2a5a52e0d7a83a9829371715359b2afcc0a02168fe8bded77b55dcfdeec3`，linux/amd64，内嵌 commit `a1eb0aaac70cda552eb92882305e6f5241c95418`。GitHub dispatch 仍报 Actions disabled；本地门禁通过后发布已验收镜像，拉回检查 digest 一致。
- **部署 / 结果**：主站 `ai.moshu.cloud`、L1 `api.moshu.cloud`、COS `cos.moshu.cloud` 在 13:28–13:30（Asia/Shanghai）统一更新；只重建应用。三站 healthy、重启 0、内外健康/工作台入口 200、无关键启动错误。真实主站分组密钥经内网和公网发现 Nano Banana 模型，未进行付费生成。
- **备份 / 回滚**：完整备份 `/opt/sub2api-backups/v0.2.16-custom.2-20261014T031705Z` 已验证三份 pg_restore 目录、数据 tar 及 SHA256SUMS；切换前配置分别为 `v0.2.16-custom.3-config-20261014T052752Z-main`、`...T052905Z-l1`、`...T052946Z-cos`。回退镜像 `sha256:edf55576e8ea73d2a552d7489cabdd2a6f282290e2ac713cda0d5504a55f5f17`，仅重建对应应用，无需恢复数据库。
- **共享同步**：根工作树保留其他会话改动，仅更新状态入口和本发布日志；下一轮以 `.3` 为最低功能基线，不能从旧 `custom/integration` 丢失本补丁。

### 2026-10-13 — 本地验收通过，未发布 — v0.2.16 与最新生图工作台

- **需求 / Feature ID**：官方升级及 `image-studio`，保留 `operator`、`prompt-audit`、`branding`、`usage-extras`。
- **基线**：从 `custom-0.2.15.3` / `0e5158d24` 合并官方 `v0.2.16` / `01842576b3d5c1e2545b5a99ad555d36e28ed306`；工作树为 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom`，分支 `codex/v0.2.16-image-studio`。
- **实现**：选择性移植 `codex/image-studio-session-20261004` 的 14 个功能提交：Gemini/Nano 原生发现与路由、Seedream 5 约束、输出格式/MIME、异步 fan-out 和显式模型选择；不带入旧版本或代理商运行时。补强异步任务 JSONB 检查点、CAS、取消排空、轮询恢复和下载恢复，修复原生 Gemini 4K 档位及 Kiro 分组归一化。
- **边界**：不本地放大、不显示实际像素文字、不改输出优先计费；实时模型查询/调度仍过滤不可调度账号，管理员配置候选保留已配置模型。
- **迁移**：无新定制迁移，追加官方 242-245 四个文件；历史迁移原样保留。本地升级记录数 318 → 322，旧镜像使用同一新数据库的启动、登录、模型查询通过。
- **验证**：前端全量 2998 项及最终窄回归通过，类型/构建通过；后端受影响回归和真实 PostgreSQL 集成通过。唯一协议测试时钟缓存串扰修复后重复 50 次通过。Docker 权限、重启持久化、三张并发输出、参考图、幂等、MIME、单张/ZIP 下载与 `$0.03` 合成结算通过，无真实模型费用。
- **提交**：官方 merge `d3b78cf8c`，生图移植止于 `2c007ec6a`，运行修复 `e42db1f11`，验收脚本 `f17cae568`；仅本地。详细结果和限制见 [`UPGRADE_V0_2_16_ACCEPTANCE.md`](UPGRADE_V0_2_16_ACCEPTANCE.md)。
- **镜像 / 部署**：只有 `sub2api:0.2.16-custom.1-local` 本地镜像；未推送、未创建标签、未发布 GHCR、未部署生产。主站仍为 `0.2.15-custom.2`，L1/COS 为 `0.2.15-custom.1`。
- **后续**：发布前等待旧版异步在途任务排空，未知提交超过 24 小时需人工核账；推进长期集成基线前先核对所有会话提交，不能直接从仍落后的 `custom/integration` 开始。

### 2026-10-14 — 已发布 — v0.2.16-custom.1 生产更新与旧镜像清理

- **需求 / Feature ID**：发布已完成本地验收的官方 v0.2.16 合并和 `image-studio` 移植；`operator`、`prompt-audit`、`branding`、`usage-extras` 保持。
- **来源**：分支 `codex/v0.2.16-image-studio`，标签 `custom-0.2.16.1`，提交 `ece7cf2208332fccb9b031e04b4f35266302dbe3`。
- **镜像**：`ghcr.io/hzihuan001/sub2api:0.2.16-custom.1`，不可变 digest `sha256:edf55576e8ea73d2a552d7489cabdd2a6f282290e2ac713cda0d5504a55f5f17`，linux/amd64。因仓库 Actions API 报 `Actions has been disabled`，本次完成本地门禁后由 Buildx 直接发布；后续应重新启用 Actions。
- **部署**：OVH 主站、L1、COS 只替换 Sub2API 应用镜像并执行 `up -d --no-deps`；没有重建数据库、Redis、卷、网络或 OpenResty。备份标识 `/opt/sub2api-backups/v0.2.16-custom.1-20261014T0010Z`。
- **验收**：三应用均 healthy、重启次数 0，`ai.moshu.cloud`、`api.moshu.cloud`、`cos.moshu.cloud` 的 `/health` 均 200；三站版本和 digest 一致，迁移计数 322/331/331，Redis 认证连通，日志无关键错误。
- **清理**：删除未使用的旧 Sub2API 镜像、CPA `v8.0.23`/`v8.0.8` 及可回收 Buildx 缓存约 244 MB；保留当前和必要回滚镜像，不动卷、网络和其他运行服务。Docker 镜像数从 18 降至 13。
- **回滚**：恢复备份中的旧应用 digest 后仅重建对应应用容器；若生产迁移出现不可逆问题，按 PostgreSQL 备份恢复，禁止只切旧镜像。

### 2026-10-12 — 运维记录同步 — 下游 Moshu 域名

- 从根工作树保留的共享记录同步：L1/COS 新入口为 `api.moshu.cloud`、`cos.moshu.cloud`，指向 OVH，HTTPS SAN 证书与续期钩子已配置，两条 DNS 均为 Cloudflare `Proxied`。旧 tokenpulse 入口保留对照。
- 原记录证据：新入口证书和 `/health` 通过；本机应用约 1 ms，测试网络代理后首页约 3.5–4.7 秒，直连同类检查曾超过 25 秒。不是本轮升级重新进行的生产验收。
- 应用、数据库、Redis、卷和网络未因域名切换改变；保留 10 小时读写超时及关闭响应缓冲。回滚仅恢复原 DNS/vhost/证书配置，不回退应用数据库。

## 当前状态（2026-10-11）

- 最近发布：`custom-0.2.15.1`，官方基线 `v0.2.15`。
- 当前生产镜像：`ghcr.io/hzihuan001/sub2api@sha256:ec2e0efb603d8b364cdfccacb8dfa2449d8071116853487849abb9f540b4511e`。
- 当前升级工作分支：`codex/v0.2.15-image-studio-sync`；在 `codex/v0.2.15-merge-integration` 上补回 image-studio 独立补丁，尚未发布。
- 长期集成基线：`origin/custom/integration`，当前为 `fd7c1b914`。
- 本文件和项目状态文档不包含服务器凭据、API Key、私钥、`.env` 或生产数据库内容。

### 2026-10-11 — 未发布 — v0.2.15 生图工作台补丁回补

- **原因**：v0.2.15 合并与发布时遗漏了独立生图会话中的工作台改动。
- **处理**：按功能边界移植 1K/2K/4K/自定义尺寸、批量尺寸持久化与重试、Gemini `imageConfig` 透传、API-key 图片实际像素内部解析、按用户隔离的 IndexedDB v2 画廊，以及不在界面展示实际尺寸的行为；未带入旧版本号、旧发布记录或代理商代码。
- **提交来源**：`045d95cbb`、`7a676681b`、`efc19f645`（仅选择生图代码和测试，按 v0.2.15 当前结构合并）。
- **数据库/迁移**：无新增后端迁移；沿用批量 item `input_payload` 保存尺寸，旧任务无尺寸时回退 1K。
- **验证**：后端 image/Gemini 定向测试通过；前端生图定向测试 `19/19`、typecheck、lint、生产构建和 custom isolation 检查通过。Docker 镜像联合验收待本地构建完成后记录。
- **发布状态**：未推送、未创建新标签、未部署；当前生产仍使用 `custom-0.2.15.1`。
- **回滚**：丢弃本分支即可恢复到已发布 v0.2.15；无新增数据库恢复要求。

## 当前功能清单

| Feature ID | 当前行为 | 代码/测试边界 | 迁移要求 |
| --- | --- | --- | --- |
| `operator` | operator 认证、默认拒绝的管理路由、角色权限配置、菜单和页面守卫；admin 仍保留严格超级管理员语义 | `backend/internal/authz`、`backend/internal/server/middleware`、`frontend/src/authz`、角色/权限测试 | 已有迁移保持不变；新增迁移只能使用 `9000_custom_` 前缀 |
| `prompt-audit` | `off`、`capture_only`、`async_audit`、`blocking`；捕获事件、类型筛选、详情、导出、删除和保留策略 | `backend/internal/securityaudit`、`frontend/src/features/prompt-audit`、对应后端/前端测试 | 追加式；不得修改已发布迁移 |
| `image-studio` | Moshu 生图工作台、批量任务、应用层单图拆分、队列、参考图、单张下载和 ZIP 下载；默认生图尺寸为 2K | `backend/internal/service/batch_image_*`、`frontend/src/features/image-studio`、用户工作台视图、批量测试 | 追加式；对象/任务数据需兼容旧镜像 |
| `branding` | 对客页面、公共文案和图片工作台统一使用 Moshu，避免把上游项目名暴露给用户 | `frontend/src/i18n`、品牌工具、公共页面测试 | 无独立数据迁移 |
| `usage-extras` | 管理员使用记录显示缓存命中率、分页跳转等增强；用户侧不显示管理员专属字段 | 管理员使用记录 handler/view 与前端测试 | 无破坏性迁移 |

## 版本与功能时间线

### 2026-10-14 — `0.2.16-custom.3` — Nano Banana 异常目录防御（发布中）

- **Feature ID / 基线**：`image-studio`；同官方 `v0.2.16`，补齐 `custom-0.2.16.2` / `5db535f6c`。
- **发布前复核**：`.2` 的完整数组解码遇到单个数字 ID 会原样透传，前端仍可能展示未经过白名单的兼容目录。生成权限独立校验，问题仅涉及目录可见性；`.2` 未发布镜像或部署。
- **修复**：兼容目录按条目解析，异常项跳过；错误格式的原生信封可转为兼容目录，之后统一经过分组白名单。原生合法空数组保持空。新增 handler 允许/拒绝验证混合异常条目，避免一次坏条目跳过过滤。
- **发布约束**：沿用 `.1` 回滚 digest，没有数据库迁移。最终镜像和生产验收后追加记录，已发布标签 `.2` 不改写。

### 2026-10-14 — `0.2.16-custom.2` — Nano Banana 模型目录兼容（verified，未部署）

- **需求 / Feature ID**：修复 Nano Banana 分组密钥在生图工作台无法发现 `gemini-nano-banana-2.1`；`image-studio`。
- **基线**：官方 `v0.2.16` / `01842576b3d5c1e2545b5a99ad555d36e28ed306`，定制 `custom-0.2.16.1` / `ece7cf220`，分支 `codex/v0.2.16-image-studio`。
- **根因与边界**：上游原生模型路径返回 OpenAI `data[].id`。`backend/internal/service/gemini_models_endpoints.go` 仅归一化 HTTP 200 兼容目录，保留原生信封、错误和额外信封字段；删除转换源 `data`，让分组白名单统一作用于 `models`。`frontend/src/api/imageStudio.ts` 兼容信封、去重并读取原生 `displayName`，不回填服务端过滤后为空的原生目录。
- **不变 / 迁移**：生图请求、路由、密钥、调度、计费、尺寸、参考图和下载不变；没有数据库迁移，不添加静态假模型或发现请求。
- **测试**：service/handler 的兼容信封、去重、空值/非法载荷、原生保留、白名单允许/拒绝及响应头回归通过；前端 API/工作台 25 项、typecheck、定向 ESLint 通过。
- **提交 / 镜像 / 部署**：发布中；待补充最终 commit、tag、digest 与三站验收，不把本条验证误认为已部署。
- **回滚**：上一版三站共同 digest `sha256:edf55576e8ea73d2a552d7489cabdd2a6f282290e2ac713cda0d5504a55f5f17`；只回退应用镜像，无需改库。

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
