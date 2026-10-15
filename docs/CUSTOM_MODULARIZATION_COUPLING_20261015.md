# 模块化耦合清单与验收证据复核（2026-10-15）

本报告补充计划第 6 节要求的逐阶段前后清单，不替代最终候选验收报告。固定官方起点为 `01842576b3d5c1e2545b5a99ad555d36e28ed306`（v0.2.16），定制基线为 `989594099c1bd10db911c737449d15d2bb6149ed`。初次复核点 `9831e38c6`；后续独立运行修复 `8d5dba9a4`、`3e78ebe2b`，检查补强 `851b49e7a`、测试夹具 `d16ef65ce`。本报告不把业务修复算入结构收益，也不宣称最终门禁、性能或部署完成；唯一当前状态见项目状态页。

2026-10-15 后续结果：用户批准的 `.6` 本地 GHCR 发布和三站顺序部署已完成；正式镜像回拉隔离验收、三站备份和生产健康/版本/日志检查通过。坐标、逐站结果及生产检查边界统一见 [发布报告](CUSTOM_RELEASE_0_2_16_CUSTOM6_20261015.md)。下面的耦合统计仍按原提交范围阅读，后续分组规则不计入纯重构收益；发布不代表 P4 完整双跑矩阵或自动升级链路已补齐。

## 统计口径

- “宿主文件”指本阶段实际保留定制调用的文件；“登记文件”专指 `contracts.json.host_entries` 按 import 检查的文件，二者不能混同。
- “位置”按具名调用、路由挂载或独立策略实现计数，不按行数计数；同一策略的多个调用者仍然是多个接入位置。
- “依赖”区分完整实现依赖、窄接口、纯函数以及装配层允许持有的具体实现。文件仍在同一 Go package，不称包级依赖消失。
- “重复规则”只计算下表明确可比的规则，不把不同供应商、不同协议的必要分支误算为重复，也不把单纯搬迁计作减少。
- 阶段文件差异可用 `git diff <前提交> <后提交> -- <所列路径>` 重现；基线的官方差异规模统计仍见执行记录 P0，不能将其当作冲突数或本轮新增业务量。

## P1：边界可执行化

比较：`989594099 → fc16d0875`（中间 `b3dbe92d6` 为基线记录）。

| 项目 | 改造前 | 改造后 |
| --- | --- | --- |
| 后端宿主文件 | `server/custom_bootstrap.go`、`server/router.go`、`server/routes/{admin,gateway,custom}.go` 已接 custom | 同 5 个文件逐项登记，检查意外新增与导入断开；不是减少 5 个文件 |
| 前端宿主文件 | `router/index.ts`、`components/layout/AppSidebar.vue`、`views/admin/SettingsView.vue` 已接 registry | 同 3 个文件登记；Settings 增加独立读写判断、只读组件属性及官方表单提交隔离 |
| 跨边界依赖 | 接口已经存在；仅路径存在性不能约束调用 | 中立 `internal/custom` 不得导入内部宿主；authz/securityaudit 禁止 server/handler 反向依赖；image-studio/prompt-audit 前端禁止 router/Sidebar/Settings 导入 |
| 例外 | securityaudit HTTP 认证主体依赖宿主 middleware | 保留 1 个精确文件＋import 例外；不用目录级豁免 |
| 重复规则 | 无本阶段可比较的业务策略去重 | 无；本阶段收益是门禁和冲突检查，不报策略减少 |

行为证据：`server/routes/custom_contract_test.go` 使用真实 admin/gateway registrar、中间件和请求；`custom/contribution_contract_test.go` 检查重复 provider/permission/setting key；前端 `hostNavigation.integration.spec.ts` 挂载真实 Sidebar 和 Router，SettingsView 测试挂载真实贡献组件并检查读写调用。漏注册探针和缺权限元数据有负向断言，不是目录存在测试。

边界限制：`OperatorPolicyProvider` 明确是只读权限目录，真实授权仍由 `authz/operator_policy.go` 和 admin middleware 执行。`Host.RegisterPermissions/RegisterSettings` 目前没有生产调用，其通用 registrar 测试使用替身；不能称后端通用设置持久化/动态授权插件已经落地，也不应为完成接口数量而把元数据变成授权来源。现有真实角色矩阵由静态授权路径的测试证明。

## P2：导航、设置、品牌与生图纯策略

比较：`fc16d0875 → 6497dcb0b`。

| 边界 | 文件与位置：改造前 → 改造后 | 依赖与重复规则 |
| --- | --- | --- |
| 菜单 | `AppSidebar.vue` 的 roles、usage、security-audit 子项 3 类路径分派 → 1 次 `placeCustomMenuItems(baseItems, contributions)`；路径锚点在 feature 元数据中 | Sidebar 不再识别这 3 类定制路径；官方菜单仍由宿主维护 |
| 角色路由 | `router/index.ts` 1 条 `/admin/roles` 硬编码 → `custom/features/operatorRole.ts`；现有 `mergeCustomRoutes` 接口保持 | 此路由新增/修改只需 feature 声明；仍沿用真实 auth guard |
| 定制设置字段 | `SettingsView.vue` 内 `pass_auth_context` 展示和归一化 → `CustomMenuAuthContextField` 与 `normalizeCustomMenuAuthContext` 两个接入位置 | 官方自定义菜单读写 payload、整个设置页和保存 API 保留；实际业务标签贡献仍为 0，不以测试探针冒充生产标签 |
| 品牌 | `i18n/index.ts` 全局 postTranslation 替换、`stores/app.ts` 名称规则、`router/title.ts` 默认名 → 1 个 locale loader 调用＋2 个展示适配调用 | 39 个现存覆盖键逐键登记；app/title 的既有空白处理差异保留，不虚报为同一语义；未知技术 URL/变量不再全局替换 |
| 图片格式 | Studio 与 BatchGuide 两处格式目录 → `getImageOutputFormats` 单实现；Studio 两处、BatchGuide 一处格式归一化 → `normalizeImageOutputFormat` 单实现 | 目录 2 → 1；归一化 3 → 1；调用位置仍保留，Batch 的空 MIME fallback 与 Studio passthrough 差异保留 |
| 图片张数 | `api/imageStudio.ts` JSON/multipart/native Gemini 三处同一 clamp 表达式 → 3 次 `normalizeImageGenerationCount` 调用 | 同规则实现 3 → 1；HTTP 客户端未另写 |
| 参考图/能力/尺寸 | Studio 多处能力条件与参考图限额 → `getImageStudioModelPolicy`；既有 `features/image-studio/size.ts` 共享尺寸工具保留 | Studio 策略集中；Batch 参考图限额读取模型能力，语义不同，未强行合并。尺寸工具本来已共享，不计作本阶段新增去重 |

宿主 import 登记：前端 3 → 6，新增 `i18n/index.ts`、`stores/app.ts`、`router/title.ts`；后端仍为 5。图片策略另有 3 个消费者文件 `ImageStudioView.vue`、`BatchImageGuideView.vue`、`api/imageStudio.ts`，合计 8 个调用位置（3＋2＋3），不属于当前 `@/custom` import 清单。纯策略依赖 `features/image-studio/size.ts` 的模型识别函数，未新增服务调用。

行为证据：真实导航/设置/i18n 宿主测试；明确覆盖键与旧文案逐 leaf 对照；`imageGenerationPolicy.spec.ts` 的固定模型/协议输出；ImageStudio 的取消、迟到结果、Blob 清理与能力刷新回归。设置探针的只读保护证明宿主不发官方保存 API、子组件收到 readOnly；真实业务字段另有原 payload 读写断言。

## P3：后端装配与生命周期

比较：`6497dcb0b → 1e5c18ec6`。

| 项目 | 改造前 | 改造后 |
| --- | --- | --- |
| 业务路由位置 | `server/routes/admin.go` 直接注册 16 条定制路由 | 16 条声明集中到 `customapp/composition.go`；admin.go 保留 10 个具名 `RegisterAdminRoutesAt` 挂载位置，末尾通用 admin 和 gateway 注册各 1 个仍保留 |
| 装配 | `custom_bootstrap.go` 修改全局默认 provider 元数据；Wire/router 分开取宿主 | `ProvideCustomHost → customapp.NewHost` 1 个装配入口；同一个 Host 经 `http.go/router.go`、Wire Application、启动和 cleanup 传递 |
| worker | `PromptRuleService` 构造时订阅；没有应用可等待退出边界 | 构造只加载；`main.go/runMainServer` 1 次 `app.Custom.StartWorkers`；`wire.go/provideCleanup` 1 次 `customHost.ShutdownWorkers`；1 个 prompt-rule worker adapter 调用现有 service 的 Start/Shutdown |
| 依赖方向 | server 直接拥有定制业务路由；worker 生命周期隐式 | composition 允许导入 handler/service/authz；中立 custom 不依赖它们；具体服务留在既有包，不复制官方 audit/batch worker |
| 登记文件 | 后端 5，前端 6 | 后端 import 文件 8（新增 http.go、wire.go、wire_gen.go），composition 1，前端 6；另有无 custom import 的 main.go 真实调用，不能漏记 |
| 重复实现 | 16 个宿主直接路由声明、1 个隐式订阅所有者 | 宿主业务声明 16 → 0；定制声明仍为 16；worker 所有者 1 → 1，提升为可启停/等待，未虚报 worker 数减少 |

10 个具名挂载分别是 roles、user-api-keys、group-effective-models、ops-capabilities、ops-errors、usage-filters、api-keys、prompt-audit-recording、prompt-audit-export、prompt-rules，定义于 `internal/custom/admin_mount.go`。精确路由集合、顺序和中间件链见 `server/custom_bootstrap_test.go` 的 `builtinAdminRouteContracts` 与 `TestApplicationCustomRoutesPreserveOrderMiddlewareAndNativeSurface`。

生命周期单测验证一次启动、逆序等待、部分启动失败清理、超时后禁止重叠重启；实际 PromptRuleService 测试复用真实 runner。其应用测试直接调用 Host，不能单独证明 main.go 未漏调用。2026-10-15 补 `required_host_calls` 对真实 `runMainServer → app.Custom.StartWorkers` 校验；删除调用、仅留注释/字符串、移到别的未调用函数或重复调用均被夹具拒绝。此为源代码调用门禁，仍不能替代进程启动/重启验收。

## P4：批量审计窄接口

比较：`1e5c18ec6 → 4cbcaafef`（`ad24ff36a` 是前端测试契约修正，不是运行重构）。

- 宿主/适配文件为 `handler/batch_image_handler.go`、`handler/wire.go`、`handler/openai_gateway_handler.go`、`handler/security_audit_errors.go` 与生成的 `cmd/server/wire_gen.go`；新增 `handler/security_audit_gate.go`。
- 依赖 `BatchImageHandler → *OpenAIGatewayHandler`：1 → 0；替换为 `BatchImageHandler → SecurityAuditGate.Check`：0 → 1。Gate 只有 1 个方法；生产 provider 使用原 coordinator＋legacy moderation，Wire 生成调用 `ProvideBatchImageHandler`。
- 批量原有 public/download/cleanup service 依赖保留；Provider/Repository/Queue 和账务事务在基线已存在，不因本次重构重新包装或复制。完整网关实现不再通过批量构造函数传入。
- 审计协调器仍是原实现，错误输出抽取共享入口；不把复制审计或新建账务层算作成果。
- `security_audit_gate_test.go` 同时有结构断言、真实 handler/provider 调用、off/capture_only/async/blocking、去重、JSON/SSE 格式及缺组件拒绝。后续 PG/Redis/Docker 恢复证据已记录，但完整旧新任务/账务双跑矩阵仍未完成；窄接口和独立恢复测试不能替代该矩阵。

## P5：纯决策与协议适配

比较：`4cbcaafef → 57ed1ca2d → 31545ff8a`；后续 WS 身份修复单列，不归为保持 .5 不变的纯结构调整。

| 边界 | 改造前 | 改造后 |
| --- | --- | --- |
| 故障分类 | 基线已有 `gateway_failure_policy.go` 共享分类器 | 仍保留 1 个分类器，不声称从 3 个减少为 1 个 |
| failover 决策 | `openai_gateway_upstream_errors.go` 的 HTTP 1 个决策；`openai_gateway_passthrough.go` 的 passthrough/response.failed/error event 3 个决策 | 4 个入口薄委托到 1 个纯 `decideOpenAIGatewayFailure`；保留 4 种 envelope 的必要差异 |
| 重放元数据 | upstream_errors.go 的 `newOpenAIUpstreamFailoverError` 混合条件和结果构造 | 1 个 `decideOpenAIFailureReplay` 纯值决策；原函数负责传入事实、构造原错误；不是减少已有重试预算或新增重放策略 |
| 语义状态 | P5 首提交仍留有 passthrough 内 2 个旧 helper 的重复策略 | 31545ff8a 将 2 个 helper 变为 `openAIStreamSemanticStatusForClass` / `openAIStreamFailureStatusForClass` 薄调用；规则归 gateway_failure_decision.go |
| WS 副作用身份 | ctx_pool、HTTP bridge、passthrough 各自布尔观察状态 | 3 个协议文件各持有 `openAIFailureAttemptEffects`；执行仍经原 `handleOpenAIStreamTerminalAccountSideEffects` / RateLimitService，不新增全局 deduper |
| 包边界 | 策略、适配器与 service 同包 | 仍同包；纯文件无数据库/缓存/网络导入。包级依赖数未减少，也尚无专门防止纯函数未来访问服务状态的语法门禁 |

实际协议接入文件：`service/openai_gateway_upstream_errors.go`、`service/openai_gateway_passthrough.go`、`service/openai_ws_forwarder_ingress.go`、`service/openai_ws_http_bridge.go`、`service/openai_ws_v2_passthrough_adapter.go`，加 2 个纯逻辑文件。P5 没有改变 P3 的 8＋1＋6 import 清单，不能据此说故障策略没有宿主接入。

测试证据的准确含义：`TestGatewayFailureDecisionPreservesEnvelopeDistinctions` 等固定期望及真实 relay/bridge/ctx_pool 测试是行为证据。当前 wrapper/decision 比较只证明适配一致性。`851b49e7a` 新增 `TestGatewayFailurePublishedBaseline`，从验证过的 989594099 原实现只读 overlay 采集 50 组固定输出，对当前实现比较通过；覆盖分类/语义状态/8 类账号上下文/重放元数据，不替代运行冷却和时序测试。固定 fixture 来源与命令见 `backend/internal/service/testdata/gateway_failure_published_989594099.README.md`。

2026-10-15 清单补入精确现存测试名前缀 `TestOpenAIWSPassthroughIdentity`（7 项）、`TestOpenAIWSPassthroughTurnLifecycle_`（1 项）、`TestPassthroughLifecycle_`（16 项）；原 `GatewayFailureAttempt` 已包含真实身份复现。仅增加匹配范围不等于这些测试已在最终候选上通过。

## P6：需补齐的交付证据与边界演练

| 要求 | 已有证据 | 尚需的具体证据 |
| --- | --- | --- |
| 宿主重排 | 真实 Sidebar 上游菜单反序；851b49e7a 生产子路由 registrar/composition 新增4条官方路由及反序后16条定制各一次、漏挂载负例通过 | 路由演练使用合成身份/audit探针及真实授权/限流/filter/guard，并非完整JWT认证验收；后者仍由既有应用验收覆盖 |
| 官方设置新增 | `a44618924` 保留真实 Settings general 字段/API 类型/保存 patch；隔离副本 6 缺字段负控、15 正控及 typecheck 通过 | 不复制官方设置页；源码 tree、patch 及完整范围见 `custom/fixtures/settings-upgrade/README.md` |
| 模型能力变化 | 真实 ImageStudio mount 同模型切 Key 从 compatible→native、同 Key 刷新模型列表后尺寸/张数/MIME/参考图/mask 收敛，并断言实际提交 payload；后端账号映射/显式协议回归 | 对应 `ImageStudioView.sizeDisplay.spec.ts`，不外推为所有供应商均经真实付费调用 |
| Wire/Ent 生成 | d16 和 577 独立干净工作树精确提交，生成零差异、迁移无变化 | 若后续改变生成输入，按影响重新核验；不能沿用 57ed 旧坐标冒充所有新提交 |
| 完整质量与联合运行 | 完整 Go/前端、真实 PG/Redis 与 577 精确镜像角色/生图/11 故障通过 | 结果按源码范围复用；全量 lint 15 项基线欠账单列，精确工具链/命令/镜像见最终候选报告 |
| 恢复与幂等 | 577 镜像实际 3 次 SIGKILL：真实持久化图片恢复、Redis 重投账务不变、未知结果不重发并保留冻结通过 | 租约仅测试任务加速，不宣称自然 TTL 或所有极短中断窗口；工件与限制见最终候选报告 |
| 性能 | P0 阈值保持；53fd 恢复原短路求值后同源微基准通过；4,000 次 HTTP/SSE、固定 CBBC 1,200 次 SSE 及有限 DEBUG 已执行 | SSE mixed 技术结论不变；用户已接受未决风险、停止追加测试，不再阻塞本次发布。全部原始样本保留，不以 DEBUG、平均值或 unchanged classifier 改判 |
| 自动升级 | 本地脚本和 workflow 已接 contracts；离线脚本检查已补强；远端集成分支已普通快进到发布运行 `655a16868` 并核验，本轮文档收尾提交同步至发布分支与集成分支 | 正式 dispatch 仍 HTTP 422 Actions disabled，真实草稿 PR/报告链路未闭环；集成分支快进和本地 GHCR 发布不是 CI 成功证据 |

没有做相同起点/目标/功能集的真实历史合并比较；本报告不提供“冲突减少百分比”。下一次真实升级应记录冲突和耗时。

## 本次补充校验

`python -B scripts/custom_contracts.py` 通过（6 modules、7 executable groups）；`python -B -m unittest discover -s scripts -p test_custom_contracts.py` 通过（17 项）；`git diff --check` 通过。原 12 项包含正例和 parser 用例，不能全部称为 12 个独立负例；新增 5 项覆盖真实启动调用缺失、注释/字符串伪接线、错误函数、重复调用、删除 required_host_calls 配置。

本报告首轮只核查架构/夹具，当时没有远程写入或生产操作；后续全量、生成和容器证据统一以 [最终候选门禁](CUSTOM_MODULARIZATION_FINAL_GATES_20261015.md) 的源码坐标与限制为准。此次获准的远程发布和生产进展另见页首发布报告，不改写原阶段证据。
