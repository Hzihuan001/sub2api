# 定制模块化执行记录

## 当前状态

- 2026-10-14 用户明确“执行”；授权 P0–P6 本地重构、阶段提交、测试及 Docker 验收，**不推送、不部署**。
- 工作树复用本会话已附加的 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom`；已切新分支 `codex/custom-modularization-v0.2.16`，原发布分支未移动。
- 起点 `989594099c1bd10db911c737449d15d2bb6149ed`，包含运行提交 `00e8f24a4` / `custom-0.2.16.5`。生产没有操作，继续 `.5`。
- P0、P1 完成；P2–P6 尚未完成。分阶段成功不等于最终重构或生产验收。

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
