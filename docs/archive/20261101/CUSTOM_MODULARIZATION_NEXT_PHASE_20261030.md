# 定制功能深度解耦与主线合并计划

## 1. 当前判断

当前项目已经有一层有效的定制边界：

- 前端有 `frontend/src/custom/` 注册表、菜单/路由适配和 feature 目录。
- 后端有 `backend/internal/custom/`、`custom_bootstrap` 和若干独立的 service/repository 包。
- `custom/manifest.yml`、升级脚本、保护路径检查和 Docker 验收已经形成基础门禁。
- 鹈鹕展示页、模型广场、提示词审计和批量生图已经存在相对独立的模块边界。

当前仍然存在高冲突接入：

1. `operator` 仍需要官方 handler、路由、前端权限和角色页面的接入点。
2. `key-routing` 仍与 API Key 认证、中间件、网关重试和计费快照共享核心调用链。
3. `gateway-resilience` 的失败分类、冷却和重试预算分布在 HTTP、SSE、WebSocket、账号测试和限流服务中。
4. `image-studio`/批量生图仍有工作台、官方批量入口、handler、计费和队列之间的交叉依赖。
5. `pelican-showcase` 当前使用官方 `scheduled_test_plans`、`ScheduledTestRunnerService` 和 `AccountTestService`；展示服务独立，但执行入口还没有完全抽出来。
6. `wire_gen.go`、官方路由注册和少数主线页面仍是定制改动容易产生冲突的文件。

结论：当前是“编译期模块化 + 少量官方接入点”的阶段，已经能降低普通更新冲突，但还没有达到“主线文件只保留稳定 hook、定制业务全部在 custom 层”的目标。

## 2. 目标架构

以后每个定制功能都由五层组成：

1. **Contract**：稳定的输入、输出、权限和事件接口，放在 `backend/internal/custom/contracts/` 或 feature 自己的 `contract.go` 中。
2. **Core**：定制业务服务、策略和状态机，只依赖 contract 与官方公开 service port。
3. **Adapter**：把官方账号、网关、计费、路由或页面数据转换成定制 contract；官方文件只保留薄适配代码。
4. **Surface**：定制路由、handler、前端页面、菜单和设置贡献，统一从 custom registry 注册。
5. **Verification**：每个 feature 自带单元、契约、集成和合并烟囱测试，并在 manifest 中声明拥有的路径和迁移。

定制模块之间通过接口或事件交互，禁止直接读取另一个模块的内部 repository、组件状态或数据库表。普通请求路径不得因为展示、审计或同步功能增加远程查询。

## 3. Feature 边界与优先级

| 优先级 | Feature | 当前边界 | 下一步抽取目标 |
| --- | --- | --- | --- |
| P0 | `pelican-showcase` | 展示 API/页面/聚合服务已独立；计划和执行仍共享官方定时测试 | `PelicanTestPlan`、`PelicanTestAdapter`、独立结果端口；官方 runner 只调用 adapter |
| P0 | `key-routing` | `backend/internal/keyrouting` 已存在，认证中间件、网关和计费仍有直接分支 | `RouteDecision`、`AttemptBudget`、`BillingSnapshot` contract；官方入口只调用一个 resolver |
| P0 | `gateway-resilience` | 错误分类、冷却和重试跨多个协议 | 统一 `FailureClassifier`、`CooldownStore`、`RetryPolicy` port；HTTP/SSE/WS 仅负责协议包装 |
| P1 | `operator` | 前端 registry 和后端权限基础已存在，官方菜单/路由仍有冲突面 | `PermissionResolver` 与 `RouteContribution`；移除分散的角色特判 |
| P1 | `image-studio` | 工作台、批量队列、官方图片入口和计费共享核心文件 | `ImageRequestPolicy`、`BatchFanoutPort`、`ImageResultPresenter`；页面只消费策略结果 |
| P1 | `prompt-audit` | 审计包独立，网关/设置/记录仍有多处接线 | `AuditDecision` 和 `AuditEvent` contract；请求入口只调用统一 facade |
| P2 | `branding` / `usage-extras` | 主要是前端和展示字段覆盖 | 迁移到 registry/adapter，减少官方页面条件分支 |

优先顺序按“主线冲突频率 × 业务风险 × 可测试性”确定。先处理 P0 的运行路径，再处理页面和低风险展示层。

## 4. 分阶段执行

### 阶段 A：盘点与冻结（只读和文档）

- 为每个 feature 建立 `custom/features/<id>/` 目录清单。
- 给每个 feature 记录入口、出口、数据库表、设置键、迁移、官方接入文件和测试命令。
- 对当前未提交代码先完成工作树快照和行为基线，不在盘点阶段扩大功能范围。
- 将 `custom/manifest.yml` 扩展为“拥有路径、接入路径、迁移、测试、回滚键”的清单。

完成标准：每个定制功能都能回答“从哪里进入、改了什么、由谁计费、如何回滚、主线更新会冲突在哪”。

### 阶段 B：统一 contract 与 adapter

- 为 P0 功能定义不可变 DTO、错误码和事件名。
- 把官方 service 的调用包在 adapter 中，业务代码不再直接依赖官方具体实现。
- 所有定制副作用通过显式 port 传递，例如 `AccountTestPort`、`GroupVisibilityPort`、`BillingSnapshotPort`。
- 不改变现有外部 API、数据库字段语义或用户权限。

完成标准：可以用内存 fake 替换官方 service，跑完 feature 的 service 测试。

### 阶段 C：迁移高冲突实现

- 先迁移 `pelican-showcase`：让计划归属、执行适配、结果存储和展示 API 独立；官方普通定时测试继续使用原路径。
- 再迁移 `key-routing` 和 `gateway-resilience`：把多组选择、失败切换、冷却、计费快照合成单一决策结果。
- 最后迁移 `image-studio`、`prompt-audit` 和 `operator` 的主线接入，保留最薄 hook。
- 每完成一个 feature，删除旧分支逻辑，避免两套实现同时生效。

完成标准：官方主线文件中的定制分支数量下降，feature 目录可以独立测试和回滚。

### 阶段 D：升级模拟与门禁

- 每次官方升级先运行 `range-diff` 和 manifest ownership 检查。
- 检查生成代码、迁移顺序、路由重复、前端 route/菜单冲突和 API contract。
- 使用一个官方 tag 做“合并后不发布”演练，输出冲突报告和各 feature 测试结果。
- 只有所有 feature 通过后，才创建发布镜像；本地 Docker 验收与生产发布继续分离。

## 5. 约束规则

- 不拆微服务、微前端或独立仓库；目标是降低合并成本，不增加运行时网络依赖。
- 不把定制逻辑塞进官方大文件的通用分支；新增功能优先新增 custom 文件和单一 hook。
- 不重复实现官方认证、计费、账号选择、模型目录和队列；使用 adapter/port 复用官方能力。
- 数据库迁移只追加；定制表和字段统一使用 `9000_custom_*.sql`，每个迁移必须有幂等测试和旧镜像读取测试。
- 不用整文件 `ours/theirs` 解决冲突；任何冲突必须按 feature manifest 逐段处理。
- 不把“代码能编译”写成“功能通过”；每项必须有对应行为测试和 Docker/HTTP 证据。
- 未发布代码、候选镜像和生产状态必须在共享文档中分开描述。

## 6. 验收门禁

每个 feature 完成后至少通过：

1. contract 单元测试和 fake adapter 测试。
2. 受影响后端包与前端类型/构建测试。
3. 路由、权限、计费、迁移和回滚兼容测试。
4. `scripts/check-custom-isolation.py`、manifest ownership 和 `git diff --check`。
5. 隔离 Docker Compose 的健康检查、关键 HTTP/SSE 路径和启动错误扫描。
6. 一次官方 tag 合并演练，保留冲突报告和 range-diff。

生产发布仍按主站 → L1 → COS，使用同一不可变镜像摘要；本计划本身不授权生产部署。

## 7. 本轮执行顺序

1. 先完成现状文档和 manifest 盘点。
2. 把鹈鹕测试执行从官方 `ScheduledTestRunnerService` 抽成 `PelicanTestAdapter`，保持现有分组结果语义。
3. 给 `key-routing` 和 `gateway-resilience` 建立 contract，不改变运行行为。
4. 为每个 feature 补一份最小 fake/contract 测试。
5. 用官方下一版本做一次本地合并演练，确认冲突面是否下降。
6. 验收通过后再决定是否提交、推送和部署。

当前未执行本计划的代码迁移；本文件是后续会话的执行基线。
