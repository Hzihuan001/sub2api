# 定制模块化执行记录

## 当前状态

- 2026-10-15 最新状态以 [项目状态页](CUSTOM_PROJECT_STATUS.md) 页首为唯一当前快照。结构主体已落地但 P0–P6 尚未整体验收；用户选择未知结果保留待核对、不新增管理员入口并暂缓发布，本地验证继续。以下阶段结论均按其提交/日志范围阅读，不能外推到最新源码。
- 后续独立提交与精确门禁见 [最终候选报告](CUSTOM_MODULARIZATION_FINAL_GATES_20261015.md)：完整 Go/前端、真实 PG 审计与账务、最终生成，以及 577 候选 Docker 角色/生图/11 故障/真实中断恢复均已有通过证据。性能仍在收尾，全量 lint 基线欠账与 Actions 外部限制单列；不外推为本轮已发布。
- 逐阶段真实接入点、依赖和重复规则的前后口径见 [耦合清单](CUSTOM_MODULARIZATION_COUPLING_20261015.md)。不以目录数或接口数代表冲突减少，不把业务修复混算为结构收益。

- 2026-10-14 用户明确“执行”；授权 P0–P6 本地重构、阶段提交、测试及 Docker 验收，**不推送、不部署**。
- 工作树复用本会话已附加的 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom`；已切新分支 `codex/custom-modularization-v0.2.16`，原发布分支未移动。
- 起点 `989594099c1bd10db911c737449d15d2bb6149ed`，包含运行提交 `00e8f24a4` / `custom-0.2.16.5`。生产没有操作，继续 `.5`。
- 2026-10-15 完成度复核：P1–P3 和 P4 窄接口完成；P5 三条 WS scope 已实际接入并保持 .5 语义。普通兼容批量“部分成功后取消”在候选与原基线均复现漏结算，触发停止条件，P4/P6 不能整体验收。WS 跨轮身份、完整门禁、性能和真实恢复也仍有缺口。详见 [复核及停止项](CUSTOM_MODULARIZATION_REVIEW_20261015.md)；未推送、未部署。

## P0：基线证据

### 分支及协作

- 远程发布分支经 ls-remote 为 `989594099`，`origin/custom/integration` 为 `fd7c1b914`；祖先比较 `0 / 270`，可在将来获准时快进。此次没有移动远程、本地集成分支或改变默认分支。
- 仍开放的 PR #1 是旧 `codex/isolation-refactor-v0.2.13 → custom/integration`，仅作为历史背景，不在本轮编辑/合并它。
- `codex/image-studio-session-20261004` / `01124885d` 不是新发布基线；其功能已按之前的移植记录进入当前发布，不能将旧 VERSION 和整个旧分支再次合入。`codex/release-custom-0.2.15.3` 无 HEAD 之外提交。
- 根目录 `D:/projects/sub2api-custom` 的公共文档页及测试改动属于其它会话，本轮不触碰；两份共享文档属于跨会话入口，按追加进度维护。其它旧/分离工作树不删除、不复位。
- 本工作树开始时仅有本轮计划和两份共享日志未提交，没有未提交运行代码。

### 自动化状态

- 只读 Actions permissions API 返回 enabled=true；运行列表最新仍为 2026-10-08。
- `.5` 刚发布时实际 dispatch 返回 422 Actions disabled，证据保存在该版本发布报告。只读 enabled 标记不能证明账号层执行能力已经恢复，也不足以断言具体是计费还是平台限制。
- 本轮不再次 dispatch、不修改权限或计费。需仓库所有者处理运行限制并授权验证；自动升级远程链路暂不能记为完成，本地开发可继续。

### 差异规模（统一口径）

命令：`git diff --name-status --no-renames v0.2.16 989594099 -- backend/internal frontend/src`。仅统计两个目录；排除 cmd、backend/ent、依赖文件、SQL 等；测试按 `_test.go`、`/__tests__/` 和 spec 命名分类。它衡量历史差异规模，不代表文件全属于本轮，也不等于未来冲突数。

| 类别 | 修改已有文件 | 新增文件 |
| --- | ---: | ---: |
| 后端业务/支持代码 | 171 | 57 |
| 后端测试 | 133 | 82 |
| 前端业务/支持代码 | 142 | 52 |
| 前端测试 | 46 | 29 |

### 主线接入与功能冻结清单

| 功能 | 当前接入/耦合点 | 基线守门 |
| --- | --- | --- |
| 角色/使用记录 | authz、admin middleware；router/sidebar/settings；用户/API Key handlers | Operator/Custom/权限和设置测试，后续 HTTP 角色矩阵 |
| 品牌 | i18n/index、app store、标题和布局默认值 | 品牌/语言/标题测试；仅展示字段变化 |
| 生图 | ImageStudio + BatchImageGuide 共享规则重复；batch Provider/worker；BatchImageHandler 依赖完整 OpenAI Handler | BatchImage 单测、后续 PG/Redis + Docker 原有三图与下载验收 |
| 审计 | securityaudit 包；handler audit helper；定制 recording/export 路由 | PromptAudit/PromptCapture、capture_only/阻断/去重测试 |
| 网关韧性 | failure classifier、overload breaker、HTTP/SSE/WS、ratelimit 和 Redis | `.5` 单测、11 场景 Docker、同一微基准 |
| 装配 | custom.Host/metadata、routes/admin、Wire/cleanup | custom/host、server/bootstrap 和后续真实装配测试 |

重要范围核查：官方 v0.2.16 已包含批量网关基础路由、PromptAudit 原生命周期，以及设置页的 overload/429/倍率探测/Ollama/面板限流/Claude 提示/OpenAI Fast 卡片。**不能为模块化把这些全部变成依赖定制注册的功能。** 只抽定制新增路由/策略和必要适配；可选空定制集必须保持官方基础行为。

审计启动失败的官方策略是记录降级并继续服务，已配置 blocking 的 fail-closed 仍生效；统一生命周期时不能一律改成致命失败或绕过阻断。前端实际待隔离差异主要是权限包装、品牌和自定义菜单认证上下文，不为完成“面板数量”强拆官方卡片。

### 基线验证（2026-10-14）

- Go `1.27.2 windows/amd64`，pnpm `9.15.9`；Docker Linux engine `29.8.2`。当时 C 可用约 40 GB、D 约 92 GB（十进制），未清理已有数据。
- 受影响 Go 基线：custom/authz/securityaudit/server/handler/service/repository 七包全部通过，使用 `-tags=unit -count=1`；regex 覆盖 Custom、Operator、PromptAudit、PromptCapture、BatchImage、GatewayFailurePolicy、OpenAIRecoverableFailover、AccountOverload、OpenAIRepeatedOverload、OpenAIQueueHeartbeat、CountsOverloadOnce、SameAccount、ClearAccountError、RateLimitService_Clear。
- 前端基线：custom/authz/image-studio/prompt-audit + SettingsView + ImageStudio 尺寸展示，13 文件 / 144 项通过，26.87 秒。已有 Settings mocks 的组件 warning 保留，不当作失败掩盖。
- 日志：`D:/projects/go-tmp/modular-p0-backend.log`、`modular-p0-frontend.log`。
- 不把这组定向基线称为全量测试或最终 Docker 通过；P6 仍需候选完整门禁。

### 性能冻结

新增仅测试的 `BenchmarkCustomFailureClassification`，后续继续通过同一宿主调用测同一输入；不能改用更简单夹具制造改善。命令：`go test ./internal/service -run '^$' -bench '^BenchmarkCustomFailureClassification$' -benchmem -count=5 -benchtime=100ms`。

| 输入 | 五次中位数 ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| overload | 3806 | 328 | 10 |
| quota | 2889 | 216 | 5 |
| model | 2742 | 216 | 5 |
| request | 2056 | 224 | 5 |
| safety | 1829 | 224 | 4 |

日志 `D:/projects/go-tmp/modular-p0-benchmark.log`。候选同机串行测量，中位数退化超过 20% 或分配次数增加时先复测定位，不直接通过；不得出现新增请求期远程调用。微基准不能证明完整 API 延迟，P6 另在相同 mock 负载下核对 HTTP/SSE 时序与资源；避免把调度夹具的固定等待算作业务耗时。

## 后续阶段

P1 起按阶段追加实际文件、提交、测试、负向验证与指标，不提前填入“通过”。如发现新的官方边界，记录证据后保持本方案范围，不扩大为整套官方组件重写。

## P1：架构与真实接入门禁

- `custom/manifest.yml` 引用可执行 `custom/contracts.json`：6 个功能所有者、入口、依赖、7 组 argv 测试；`custom_contracts.py` 实际读取并校验。升级 CI 和本地同步脚本均接入真实 contract tests，前端不再只有安装/类型检查；本轮未触发远程 workflow。
- 架构规则约束中立 Go 契约包、审计/权限模块和前端功能的反向依赖，核对 5 个后端、3 个前端接入文件；跨 feature 相对路径与 alias 同样检查。旧 `securityaudit/prompt_handler.go → server/middleware` 仅登记单文件单 import 例外（读取认证主体），不放开整个目录。
- 12 个独立负向夹具通过：缺入口、文件存在但断开导入、未登记接入点、反向依赖、重复模块、空测试门禁、过期例外、路径逃逸、相对路径隐式功能依赖等。静态检查不替代行为测试。
- 后端 provider ID、permission、setting key 冲突及 typed-nil 拒绝；真实 `RegisterAdminRoutes`/`RegisterGatewayRoutes` 验证鉴权/审计中间件、operator 未知路由默认拒绝、请求体限制和重复路由。可选空 Host 仍保留官方 10 条批量和审计基础路由；漏注册探针返回 404。
- 前端 registry 对 routes/alias/name、菜单和 settings ID 原子校验；Settings 使用贡献 labelKey，显式独立 read/write 和 readOnly。未知/缺失权限不放行，沿用已有 settings 全局权限继承；自定义表单不能误调用官方全表保存。未复制官方设置页或新增虚构业务 tab。
- 真实 Sidebar/router/Settings 挂载探针验证已接入与未注册两种状态；权限包含仅读、仅写、读写和全局授权。后端依旧是最终授权边界，组件必须遵守 readOnly 保存契约。
- 验证：Go custom/server/routes 全部 unit 通过；前端相关 10 suites 182 项通过，最后增补一例后 adapters 11 项全过（总计 183）；vue-tsc、相关 ESLint、12 个 Python 架构测试、隔离/迁移/manifest 和 diff 检查通过。
- 范围：仍保留现有 worker Start 测试；完整关闭/等待契约在 P3 实际装配时补充。messages 的真实 loader 接入随 P2 落地，不把尚未实现的部分记为通过。生成代码无改动，最终独立 generate 验证在 P3/P6。
- P1 不改变公开路由、数据库、已有业务权限或生产版本；仅本地阶段提交。后续 P2 开始前确认本阶段 commit。

## P2：前端声明、品牌与图片策略

- P1 提交 `fc16d0875`。P2 由 `operatorRole` 声明原角色路由；菜单通过 parent/before/after 元数据放置，移除 Sidebar 内 roles/usage/audit 三类定制路径特判。原 URL、顺序、图标与读写权限保持，缺锚点或宿主重名会显式失败。
- 只抽取官方自定义菜单行中的 `pass_auth_context` 定制字段为小组件/归一化函数；仅读不 emit 更新、不更改保存 API。没有复制整个 SettingsView 或发明新标签页；其余官方字段省略/false 的原 payload 行为保持。
- `messages` loader 真正被 `i18n/index.ts` 调用。新增键必须在声明 namespace 内，覆盖须逐键批准；重复、未知覆盖、父子覆盖和原型键拒绝，失败不发布半个 locale。真实 i18n loader 测试通过，不以 mock loader 代替。
- 品牌明确覆盖 39 键（30 展示、3 历史示例兼容、6 已生成 MOSHU_API_KEY 配置说明）；两种 locale 所有现存 leaf 与旧展示输出逐项对照相同。新/未知 URL、变量、存储键及插值不再被全局正则改写。app site_name 与 router title 共用窄展示适配器，保留两者既有空白/名称优先级差异。
- 工作台、原批量页和 API 三种生成方式共用 MIME/格式/参考图/张数纯策略；保留 Seedream、原生 Gemini 和兼容模型的不同限额，不重新封装 HTTP，不改任务/账务。尺寸显示、取消、迟到响应丢弃与 Blob 清理回归保留。
- 门禁：31 个定向文件共 400 场景通过（首轮 399 通过，新增 Settings 测试错误期待省略字段为 false；按旧 payload 修正测试后整个 Settings 51 项通过）；图片子组 77 项通过。vue-tsc、定向 ESLint 和生产构建通过（1138 modules / 45.32s）。构建仅已有 browserslist、静态/动态 import、chunk 大小提示。
- 静态架构检查通过；前端显式宿主入口由 3 个增为 6 个（补登记实际 i18n/app/title），不是掩盖新增接入点。收益是取消分散品牌替换、菜单特判和重复图片规则，不以目录/文件数量减少宣称性能或冲突百分比。
- 新增 `custom/README.md` 维护指引。未推送、未部署，P3 后端阶段随后开始。

## P3：后端装配与生命周期（2026-10-14）

- 提交：`1e5c18ec6`（未推送、未部署）。
- `internal/customapp/composition.go` 成为定制 composition root：显式组合 16 条现有定制管理路由、operator 权限提供者和 PromptRule 订阅；不引入 service locator、整套 Handler 或第二个官方 worker。
- `AdminMount` 将原有定制路由安装到原 admin 子路由的具名位置，`routes/admin.go` 保持 admin 认证、审计、限流与注册顺序；空 Host 仍保留官方基础路由。可选扩展仍可使用原始注册接口。
- `PromptRuleService` 初始数据库/缓存加载仍在构造阶段同步完成；Redis 订阅改为 `Start(ctx)` 持有、`Shutdown(ctx)` 取消并等待回调/连接退出。`custom.Host` 对 worker 统一提供一次启动、逆序关闭、超时保留运行状态，防止重复订阅或半退出后重启。
- Wire 由锁定 Go 工具生成 `cmd/server/wire_gen.go`，未手改生成文件；应用把同一个 Host 注入 router、启动和 cleanup。`wire_gen_test` 的最小 cleanup 夹具同步传入空 Host。
- 定向证据：`go generate ./cmd/server` 通过；`go test -tags=unit ./internal/custom ./internal/customapp ./internal/server ./internal/server/routes ./cmd/server -run 'Custom|Operator|PromptRule|Application|Route|Host|Cleanup' -count=1` 通过；PromptRule service/repository 定向测试通过；`python scripts/custom_contracts.py` 与 `git diff --check` 通过。
- 本阶段没有数据库迁移、公开路由/字段/设置键、账务、官方批量/审计 worker 或生产配置变化。前端 3 个已有平台清单契约测试另以 `ad24ff36a` 固化，属于门禁基线修复，不改变运行代码。

## P4：生图与审计接口（2026-10-14）

- 提交：`4cbcaafef`（本地，未推送/未部署）。`BatchImageHandler` 由完整 `OpenAIGatewayHandler` 改为 `SecurityAuditGate` 窄接口；生产 Wire 使用现有 coordinator 与内容审核服务组装 gate，未复制网关或改变账务/队列。
- 审计顺序、HTTP 去重、`capture_only` 的 fail-open、阻断模式 fail-closed、错误 JSON/SSE 格式和缺依赖拒绝均保留。批量入口审计失败不会绕过原请求边界。
- `go test -tags=unit ./internal/handler ./internal/securityaudit -run 'BatchImageAudit|BatchImagePromptGuard|SecurityAuditShared|PromptAuditGatePrecedes|PromptGuard|SecurityAudit|CaptureOnly' -count=1` 通过；`cmd/server` 与 handler 编译通过。
- 全量 handler 宽回归仅有基线已有的 AstraPro 两例失败（预期 2 次账号尝试、实际 1 次），与 P4 差异无关。

## P5：网关故障策略（2026-10-14）

- 提交：`57ed1ca2d`（本地，未推送/未部署）。新增无状态故障分类、重放元数据和单次真实尝试效果对象；HTTP、普通透传、`response.failed` 和 error event 适配器改为薄委托。WS 适配器保留现有共享副作用路径，避免重复计数/改变协议语义。
- 没有改变重试预算、账号冷却、模型选择、账务、响应体或请求级远程查询；纯策略与旧实现交叉矩阵验证通过。
- `go test ./internal/service -run '^TestGatewayFailure(Decision|Replay|Attempt)' -count=1` 通过；相关 resilience manifest 定向测试通过。
- 宽回归发现的 5 个失败在 P4 提交前基线已复现：两个流式 403 credential 状态测试、三个 OAuth 透传 502/503/504 旧期待；本轮未改对应语义，不能把它们列为新回归。

## P6：本地 Docker 联合验收（2026-10-15）

> 完成复核：本节记录已经通过的局部证据，不代表 P6 全部完成。首次报告遗漏 WS attempt 真实接线和完整质量/性能验收，现按原方案继续补齐；没有追加生产授权。

- 静态门禁：`custom_contracts.py`、12 个架构负例、迁移 append-only、manifest/isolation 全部通过；独立干净工作树执行 `--run-go-generate` 后 generated sources clean。
- 后端门禁：宿主/权限/生命周期、审计、批量、故障策略定向 Go 测试通过；真实 PostgreSQL 18 + Redis 8 integration tests 通过（25.084s）。
- 前端门禁：`vue-tsc`、定向 ESLint、41 个 Vitest 文件/442 个测试通过；生产构建 1138 modules 通过。仅保留既有 Browserslist、动态导入和 chunk 大小提示。
- Docker 候选镜像：`sub2api:operator-modular-local`，linux/amd64，image ID `sha256:253c6aaa8bd54ebeacc64d829e4500773677e182cea8851eb4dd2d74b6b4935f`，版本构建参数 `0.2.16-custom.modular`、提交 `57ed1ca2d`。隔离项目 `sub2api-operator-test-v0216` 端口 `18086` 健康、重启 0；容器日志无 panic/fatal/migration/database/redis 错误。
- Docker 烟测：生图单图/批量/参考图/PNG/ZIP/重试幂等/完整计费 3 张实际成本 `0.03` 通过；网关 overload（同账号三次冷却）、quota、model、server、timeout、auth、rate、stream 及安全/参数/部分输出不重放共 11 场景通过。另以全新隔离项目 `sub2api-operator-test-modular` 端口 `18089` 完成角色、API Key、审计和重启验收并保留供人工检查。
- 上述局部本地验收通过；完整候选验收未通过。未推送、未建标签、未构建 GHCR、未改主站/L1/COS。生产继续运行 `.5`。

## 2026-10-15 最终复核补充（停止收尾）

- 本地阶段提交：`31545ff8a`（WS 接线）、`d596bae70`（边界测试/门禁）、`fa677d301`（保留失败的旧账务复现）。未推送，本分支不得作为已通过候选发布。
- 三条 WS 生产路径使用 attempt scope；passthrough 保持 `response.created` 的原轮次语义，未采用会误归属迟到帧的发送时重置方案。真实 relay/HTTP bridge/旧 ctx_pool 定向测试通过；无 created 新轮及跨轮迟到身份仍需独立修复。
- 前端三组真实边界演练新增至 3 文件 / 80 tests，类型与定向 lint 通过。Settings 用例只验证扩展 reserved-ID 接入，不冒称实现新官方面板。
- contracts 的 batch 门禁补入 BananaAsync，audit 补 SecurityAudit/CaptureOnly，resilience 包含真实 attempt adapter 测试，避免测试名不匹配而漏跑。
- 完整 Go unit 24,100 个测试/子测试 pass，14 个顶层测试失败；在 4cbcaafef 全部同样失败，47 条错误签名一致。三条 PgDumper 在子进程 PATH 补 sh 后通过；未削弱断言、修改历史迁移或称全量通过。
- 新增取消账务复现在候选及 989594099 均失败：已成功图片没有结算，整笔 hold 释放、无 usage。仅保留失败回归测试，未修改账务运行代码；按方案停止条件等待独立修复范围确认。
- 不再重建候选或继续性能/恢复验收来绕过停止项；现有 Docker 镜像仍是 57ed1ca2d 的局部验收产物，不是最终候选。详细证据和后续顺序见复核报告。
- 收尾差异 lint `golangci-lint run --timeout 10m --new-from-rev 989594099` 0 issues；架构清单、12 个负向测试、diff check 通过。未把差异 lint 当全量静态或账务门禁通过。

## 2026-10-15 独立批量取消修复（本地，尚不可发布）

- 本地代码提交 `55765c473`；不变更版本号，不推送部署。
- 用户确认已发出的请求取消后仍正常结算，上游明确生成失败不收费；未发出项不收费。普通 fan-out 复用已有 CAS/取消字段，不新增表或失败收费体系。
- 部分成功 + 在途成功 + 排队未发出的真实 provider/processor/settlement 合成用例通过：发送 2 次、成功 2 项、失败/未发出 1 项，原预留 0.75、实扣 0.50；重复取消/处理只捕获一次、usage 一次，结果文件保留。完全未发出的普通任务仍 cancelled 并释放预留。
- 定向服务测试覆盖普通/异步 provider、取消公共 API/processor、分页 101/501 项、明确失败、结果未知、损坏检查点；`D:/projects/go-tmp/batch-cancel-final-tests.log` 为 67.275s PASS。最后窄用例另行复测，`go vet -tags=unit ./internal/service` 通过；前端取消文案定向 ESLint 与 vue-tsc 通过。
- Docker Desktop 起初未运行，本轮已启动；`CI=true go test -tags=integration ./internal/repository -run '^TestBatchImageRepository_Async' -count=1 -v` 在真实 PostgreSQL 18.1 / Redis 8.4 上通过（58.503s）。覆盖 CAS、取消禁止新提交、在途状态保留、终态不可再改；不是完整应用镜像或真实账务全流程验收。
- 已知限制：普通同步上游 claim 后丢失响应无法自动找回，保留冻结但不重发/不当失败退款，人工对账/解冻未闭环；结算重试耗尽/usage 恢复仍有独立风险。旧版在途请求需先排空再升级。不能据局部通过关闭整个 P6。
- WS 新增两项真实失败复现，候选和 989594099 一致；运行代码未改，身份修复继续保留停止门禁。日志和详情见复核报告。
- 没有推送、标签、GHCR 或生产操作；不补扣/重算历史费用。
