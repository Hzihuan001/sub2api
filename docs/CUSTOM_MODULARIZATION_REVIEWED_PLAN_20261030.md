# 定制功能深度模块化执行方案（四轮审核定稿）

## 0. 目标和边界

目标是让官方主线升级后，定制功能可以通过少量稳定接入点重新组合，减少冲突、人工逐文件合并和回归范围，同时保持当前用户行为、计费、权限、数据和三站部署方式不变。

本方案只做代码结构、接口边界、测试和升级流程改造。它不新增业务功能，不拆微服务，不改变外部 API，不改生产数据，不恢复已移除的代理商运行时。

当前生产基线为 `0.2.19-custom.2`。工作区的鹈鹕测智分组归属改造仍是未发布候选，不能作为生产基线。

## 1. 现状基线

已有的可复用基础：

- 前端 `frontend/src/custom/` 已有 feature registry、菜单、路由和设置贡献入口。
- 后端 `backend/internal/custom/`、`custom_bootstrap`、`internal/authz`、`internal/securityaudit` 已有定制边界。
- `custom/manifest.yml`、`custom/contracts.json`、升级脚本、隔离检查和 Docker 验收已经存在。
- `key-routing`、`gateway-resilience`、`image-studio`、`prompt-audit` 已有独立目录或独立 service/repository。
- 上游同步工作流会创建升级分支和草稿 PR，但 `patch_branches` 仍为空，不能直接重放历史功能分支。

仍然容易和主线冲突的地方：

1. Key 路由仍进入 API Key 认证、中间件、网关、计费和账号调度核心路径。
2. 容错失败分类、冷却、重试预算分布在 HTTP、SSE、WebSocket、账号测试和限流服务。
3. 生图工作台、批量队列、官方图片入口和计费共享主线 handler/service。
4. Operator 角色仍需要官方路由、handler、设置页和前端菜单接入。
5. 鹈鹕测试展示层已独立，但计划、执行器和账号测试仍共享官方定时测试模型。
6. `wire_gen.go`、官方路由注册和少数大型前端页面仍可能造成高冲突。

## 2. 目标结构

每个定制 feature 统一采用五层：

| 层 | 责任 | 依赖规则 |
| --- | --- | --- |
| Contract | DTO、错误码、权限、事件和 port | 不依赖具体 handler、数据库实现或页面 |
| Core | 策略、状态机、业务编排 | 只依赖 Contract 和官方公开 port |
| Adapter | 官方 service/handler/数据模型转换 | 负责兼容主线变化，不承载业务决策 |
| Surface | 路由、handler、页面、菜单、设置 | 只做输入校验、授权和结果呈现 |
| Verification | 单测、契约测试、集成测试、升级门禁 | 与 feature 一起维护 |

推荐目录：

```text
backend/internal/custom/features/<feature>/
  contract.go
  service.go
  adapter.go
  ports.go
  errors.go
  *_test.go
frontend/src/custom/features/<feature>/
  registry.ts
  api.ts
  components/
  __tests__/
```

官方文件只保留一层薄 hook：注册 feature、调用 port、传递官方上下文。禁止在官方大文件中继续扩散业务判断。

## 3. Feature 拆分和优先级

### P0：运行路径

**`pelican-showcase`**

- 抽取 `PelicanTestPlan`、`PelicanTestAdapter`、结果仓储 port 和展示 DTO。
- 官方普通定时连接测试继续走原路径。
- 鹈鹕计划的分组归属、提示词、模型、Cron 和结果状态完全由 custom adapter 管理。
- 展示接口只依赖 `PelicanResultReader`，不直接读取官方计划 repository。

**`key-routing`**

- 定义 `RouteDecision`、`AttemptBudget`、`GroupCandidate`、`BillingSnapshot`。
- 认证中间件只负责解析 Key 和调用 resolver。
- 网关只消费 `RouteDecision`，不重新实现主备/模型/失败判断。
- 计费只读取已确认的 effective group 和价格快照。

**`gateway-resilience`**

- 定义 `FailureClassifier`、`CooldownStore`、`RetryPolicy`、`RecoveryEvent`。
- HTTP、SSE、WebSocket 只负责协议转换和流关闭，不各自维护失败计数。
- 分组规则、账号暂停、Key 备用分组和最终错误返回必须使用同一套分类结果。

### P1：高冲突界面和任务

**`image-studio`**：抽取图片能力策略、批量 fan-out、参考图处理和结果呈现 port；官方图片入口只调用策略。

**`operator`**：抽取 `PermissionResolver`、`RouteContribution`、`SettingsContribution`；移除散落的角色字符串判断。

**`prompt-audit`**：统一 `AuditDecision`、`AuditEvent` 和保存 port；请求入口只调用一个 facade。

### P2：低风险展示

**`branding` / `usage-extras`**：迁移到前端 registry 和后端 response adapter，减少官方页面条件分支。

## 4. 执行阶段

### 阶段 1：盘点和冻结

不改业务行为，完成：

- 每个 feature 的入口、出口、官方接入文件、数据库表、迁移、设置键、测试命令和回滚方式。
- `custom/manifest.yml` 增加 owned paths、host hooks、migrations、tests、rollback keys。
- `custom/contracts.json` 增加显式依赖和禁止反向依赖。
- 对当前未提交候选建立行为基线；未通过的候选不进入 patch stack。

**通过条件**：每个 feature 都能明确回答“谁调用、调用谁、保存什么、如何测试、怎样回滚”。

### 阶段 2：P0 contract 和 adapter

执行顺序固定为：

1. `pelican-showcase`
2. `key-routing`
3. `gateway-resilience`

每个 feature 都必须先有 contract、fake adapter 和测试，再替换运行接线。旧路径保留兼容 wrapper，待新旧行为对比通过后再删除旧分支。

**通过条件**：service 测试不需要启动官方 handler、真实数据库或完整网关容器；adapter 测试覆盖官方边界错误。

### 阶段 3：P1/P2 宿主瘦身

- 将生图、权限和审计的官方接入收敛到单一 hook。
- 把大型页面中的定制逻辑移到 feature components/composables。
- 把 `wire_gen.go` 的人工变化限制为生成结果，不在其中写业务代码。
- 删除旧的重复分支，禁止两套实现并行生效。

**通过条件**：主线升级时，定制业务目录可独立复用，官方核心文件只出现登记过的 hook 变化。

### 阶段 4：升级演练和发布

- 固定一个官方 `vX.Y.Z` tag，创建升级分支。
- 运行 range-diff、manifest ownership、生成代码、迁移、前后端测试和 Compose 验收。
- 记录冲突文件数量、冲突原因、手工修改行数和每个 feature 的回归结果。
- 本地 Docker 通过后才能推送；最终 GHCR 镜像回拉后再复用同一环境检查。
- 生产仍按主站 → L1 → COS，使用同一 immutable digest。

### 阶段执行的兼容性不变量

- 外部 API、错误码、计费顺序、用户余额、已结算消耗和现有权限矩阵不因目录迁移改变。
- 纯结构重构不得新增迁移；确需新增字段时必须可空或有安全默认值，旧版本能够启动并读取历史数据。
- 迁移只追加，不修改已执行 SQL，不回写历史业务记录，不用旧备份覆盖新业务数据进行回滚。
- 日志、指标和审计事件继续保留原 request ID、user ID、group ID、account ID 和 billing snapshot。
- adapter 失败必须返回明确错误码或降级结果，不能通过 `nil`、默认价格或默认分组静默绕过。

## 5. 四轮审核与优化记录

### 第 1 轮：架构边界审核

**发现**：原计划只写“新增模块”，没有明确官方代码和模块之间的依赖方向；鹈鹕展示已独立，但执行仍直接调用官方计划和账号测试。

**优化**：增加 Contract/Core/Adapter/Surface/Verification 五层；要求 P0 feature 使用 port 和 fake adapter；明确鹈鹕要抽出独立执行适配器。

### 第 2 轮：主线合并审核

**发现**：只说“减少冲突”，没有定义哪些文件允许被修改；`wire_gen.go`、官方路由和大型前端页面仍可能成为隐性冲突点。

**优化**：增加 owned paths、host hooks、manifest ownership、range-diff 和生成代码门禁；禁止整文件 ours/theirs 解决冲突；`patch_branches` 在独立验收前保持为空。

### 第 3 轮：数据和运行稳定性审核

**发现**：模块移动可能误改计费、迁移、权限和回滚行为；仅通过编译不能证明业务正确。

**优化**：增加价格快照、权限、迁移幂等、旧镜像读取、HTTP/SSE/WS、Docker 健康、备份和回滚门禁；要求生产只替换应用镜像，不用旧数据库覆盖新业务。

### 第 4 轮：执行成本和交付审核

**发现**：原计划阶段较大，容易一次改太多，失败后难以定位；也没有明确停止条件。

**优化**：按 P0 → P1 → P2 分批，每个 feature 先 contract/fake，再接线，再删旧分支；任意阶段出现行为差异、迁移异常、宿主接线缺失或 Docker 关键日志错误，立即停止，不进入推送和部署。

## 6. 统一验收门禁

每个 feature 必须通过：

1. contract、fake adapter、边界错误和权限测试。
2. 受影响 Go 包、前端 typecheck、定向测试、生产构建。
3. `scripts/custom_contracts.py`、`scripts/check-custom-isolation.py`、manifest ownership 和 `git diff --check`。
4. 迁移重复执行、历史数据读取、旧镜像启动和回滚兼容。
5. 隔离 Docker Compose：PostgreSQL、Redis、应用 health、关键 HTTP/SSE、启动错误和重启持久化。
6. 一次官方 tag 合并演练，并保存冲突报告、range-diff 和测试清单。

### 可量化的完成标准

- 每个 P0/P1 feature 至少有一个独立 core service、一个官方 adapter 和一组 fake/contract 测试。
- feature 核心包不能直接 import 官方 handler、server router 或具体数据库实现；例外必须登记为 host hook。
- `custom/manifest.yml` 能列出该 feature 的 owned paths、host hooks、迁移、测试和回滚键；未登记路径变更应使门禁失败。
- 官方 tag 合并演练必须记录冲突文件、冲突原因、手工修改行数和各 feature 回归结果，并与上一轮比较。
- 纯结构重构的迁移数量为零；有迁移的功能必须通过重复执行、旧镜像启动和回滚读取测试。
- Docker 验收必须证明普通连接测试、鹈鹕测试、Key 路由、计费和权限的旧行为没有互相污染。

发布前还必须记录：源码提交、标签、镜像 digest、三站备份标识、部署顺序、健康结果和回滚目标。

## 7. 明确不做的事情

- 不拆微服务、微前端或独立仓库。
- 不复制官方设置、账号、计费、队列或调度系统。
- 不为模块化重写外部 API、历史迁移或业务数据。
- 不把未验收的模块加入自动升级 `patch_branches`。
- 不因为合并困难而删除既有功能或放宽测试断言。

## 8. 下一步

1. 先审核并冻结 `custom/manifest.yml` 的 feature ownership。
2. 为 `pelican-showcase` 建立 `PelicanTestAdapter` 和 `PelicanResultReader` 的最小 contract。
3. 在隔离 fake 下验证普通连接测试和鹈鹕测试互不影响。
4. 再开始 `key-routing` contract，不改现有运行逻辑。
5. 每完成一个 P0 feature 就更新共享状态和功能日志，再进入下一项。

本方案四轮审核已完成，当前只形成执行基线；没有因为写方案而推送、部署或修改生产环境。
