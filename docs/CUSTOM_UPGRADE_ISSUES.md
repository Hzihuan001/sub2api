# 定制升级问题记录

> 本文档是官方主线升级过程的 append-only 记录。每次遇到冲突、定制行为回归、
> 测试失败或验证限制时追加一条；问题解决后补充处理结果和验证证据。
> 文档不得包含 API Key、密码、JWT、SSH 私钥、`.env` 内容或生产数据。

## 当前升级任务

- **记录日期**：2026-10-10
- **官方目标**：`v0.2.15` / `f2669c8cf62555cd92389b3f55920e9e6e7c6ff2`
- **定制基线**：`origin/custom/integration` / `fd7c1b914`
- **候选分支**：`codex/v0.2.15-merge-integration`
- **工作目录**：`D:\projects\sub2api-custom-upgrade-v0.2.15`
- **当前状态**：代码合并与本地质量门禁已完成；Docker 验收完成后进入推送、镜像发布和分阶段部署
- **生产状态**：保持不变

## 记录规则

1. 先记录现象和影响，再记录修复方式和测试结果。
2. 官方新能力优先保留；定制功能按 `custom/manifest.yml` 的 feature ID 复核，不能
   通过“整文件选 ours/theirs”静默覆盖。
3. 数据库迁移只允许追加；本轮未确认迁移前不得推送或部署。
4. 每条记录必须说明是否已经验证，未验证不得写成已解决。

## 2026-10-10 — v0.2.15 首次合并检查

### 1. 合并冲突集中在平台和账号宿主边界

- **现象**：从 `custom/integration` 合并官方标签时出现 39 个未合并文件，集中在
  后端平台/账号/监控/计费文件和前端账号表单、平台常量、设置页、渠道页。
- **原因**：官方 v0.2.15 将平台清单、多协议账号表单、转发和探测逻辑改为动态注册；
  旧定制也曾直接修改这些核心文件，冲突不是普通文档或隔离层冲突。
- **影响**：不能使用全量 `ours` 或 `theirs` 自动解决，否则会丢失 operator、管理员
  使用记录增强、品牌、监控兼容或官方 Cline/Command Code。
- **处理结论**：按功能边界逐文件合并；平台注册采用官方动态清单，并显式复核仍需保留
  的兼容项。未完成前不提交。

### 2. 直接采用官方文件会丢失定制权限和分组接口

- **现象**：临时候选编译发现 `RoleOperator`、`SettingKeyOperatorRolePolicy`、
  `AdminService.GetGroupEffectiveModels` 和 `GroupHandler.GetEffectiveModels` 缺失。
- **原因**：这些符号属于当前定制层，而官方同名文件有较大重构。
- **处理**：恢复 operator 常量、动态权限设置键和有效模型接口，同时保留官方平台
  验证与模型清单；修复正在进行。
- **验证状态**：已恢复部分；后端目标包仍需重新编译通过。

### 3. 国产供应商模型候选函数被覆盖

- **现象**：合并后 `DefaultCNProviderModelIDs` 未定义，导致管理端分组模型候选编译失败。
- **处理**：从定制基线恢复 Kimi、智谱、DeepSeek、MiniMax 的保守候选列表；该列表
  仅用于管理端候选，不作为网关授权。
- **验证状态**：函数已补回；等待后端测试复核。

### 4. 依赖与 Go 工具链差异

- **现象**：官方 `go.mod` 要求 Go `1.27.2`，本机当前便携运行时为 `1.27.0`；
  官方依赖变更后还缺少定制 Kiro/分词代码需要的 `github.com/pkoukk/tiktoken-go`。
- **处理**：候选分支保留官方 Go 版本要求，并补回 tiktoken 直接依赖；本地只能用
  `GOTOOLCHAIN=local` + 1.27.0 做临时诊断，不能将该诊断结果当作正式质量门禁。
- **验证状态**：依赖图已能继续编译；正式 Go 1.27.2 门禁已完成。

### 5. 前端旧 Kiro 字段残留

- **现象**：官方前端类型移除了部分 `kiro_*` 字段，但旧定制的状态单元格、账号页和
  分组页仍引用这些字段，类型检查失败。
- **处理结论**：当前功能清单不再把 Kiro 作为独立定制 feature；清理旧前端展示引用，
  不删除 operator、branding、usage-extras 和 image-studio。后端兼容代码是否保留需
  以编译和平台清单结果为准。
- **验证状态**：清理完成，前端 typecheck、lint、生产构建和全量测试均通过。

### 6. 组合调度测试与保留平台清单不一致

- **现象**：首次运行受影响后端测试时，`TestCompositeGroupSchedulerHasAllCanonicalPlatformBuckets`
  失败，结果比测试期望多出 `kiro` 平台。
- **原因**：本次合并保留了现有 Kiro 账号、额度和组合路由兼容；动态平台清单因此会把
  Kiro 纳入调度快照。官方 v0.2.15 的测试期望只覆盖官方平台，不能直接覆盖定制平台清单。
- **处理**：将该回归测试的 canonical bucket 期望同步到当前定制平台清单，未删除 Kiro
  后端兼容实现，也未改动数据库结构。
- **验证状态**：定向组合调度测试和正式 Go 1.27.2 下的完整受影响后端测试均通过。

### 7. 动态平台合并后英文界面缺少旧 Kiro 文案

- **现象**：前端生产构建的 i18n 完整性检查发现英文 locale 缺少仍由账号重新授权、
  Kiro 编辑和兼容流程引用的 38 个 Kiro 文案键；构建因此中止。
- **原因**：官方 v0.2.15 移除了旧 Kiro 前端字段/文案，而候选分支仍保留后端兼容和
  少量旧前端授权流程，造成静态引用与 locale schema 不一致。
- **处理**：只补回实际生产代码引用的中英文文案键，并补齐 Claude Code 转发设置的
  中英文文案；没有恢复已清理的旧 Kiro 展示组件或改变官方动态平台目录。
- **验证状态**：i18n 完整性 3/3 通过，前端生产构建通过。

### 8. 发布坐标和正式工具链校验不一致

- **现象**：官方 `v0.2.15` 标签中的版本文件仍为 `0.2.14`，而定制发布工作流要求
  版本文件与 `custom-X.Y.Z.N` 标签严格一致；工作流原先还校验 Go 1.27.0，和官方
  `go.mod` 的 Go 1.27.2 要求不一致。
- **处理**：候选发布版本固定为 `0.2.15-custom.1`，同步版本文件、工作流 Go 校验和
  manifest 中的管理员使用记录测试路径。
- **验证状态**：正式 Go 1.27.2 门禁已通过；工作流已同步校验 Go 1.27.2，待推送后复核 Actions。

### 9. 正式 Go 1.27.2 首次测试缺少 x/image 校验和

- **现象**：使用正式 Go 1.27.2 运行受影响后端测试时，编译阶段提示
  `golang.org/x/image/webp` 和 `golang.org/x/image/draw` 缺少 `go.sum` 条目。
- **原因**：`go.mod` 已声明 `golang.org/x/image v0.41.0`，但合并后的校验和文件只保留了
  另一版本的间接记录，Go 的默认只读模块模式拒绝继续编译。
- **处理**：在候选工作树运行 `go mod tidy`，补齐与当前 `go.mod` 一致的校验和并清理过期条目；
  未修改生产环境或数据库。
- **验证状态**：使用自动下载的 Go 1.27.2 重新运行受影响后端测试，全部通过。

## 已完成的当前轮次验证（截至 2026-10-10）

- 前端 `typecheck`：通过。
- 前端全量 Vitest：389 个文件、2907 个测试全部通过。首轮 33 个旧断言与官方
  v0.2.15 动态平台/组件契约不一致，已只维护测试契约，未改变生产代码。
- 前端定向 Vitest：99/99 通过，覆盖设置页、operator 菜单/权限、管理员和用户使用记录、
  账号状态等定制边界。
- 前端 lint：通过；仅保留 2 条既有测试文件未使用变量 warning，无 error。
- 后端 `internal/handler/admin`、`internal/server` 及相关路由：通过。
- 后端组合调度 canonical bucket 定向测试：通过。
- 后端 `internal/service`：正式 Go 1.27.2 运行，全部通过（141.730s）。
- 后端 `internal/handler/admin`、`internal/server`、`internal/server/middleware`、
  `internal/server/routes`：正式 Go 1.27.2 运行，全部通过。
- `git diff --check`：通过；候选工作树无未合并冲突。
- `go mod verify`：通过。
- 本地 Docker 验收：Docker Desktop Linux 引擎、PostgreSQL、Redis 和应用容器均健康；
  `/health` 返回 200；合成管理员登录、`/auth/me`、应用重启后再次登录均通过；Compose
  配置校验通过，最近日志无 panic/fatal/migration/database/redis 错误，重启次数为 0。
- 正式 Go 版本门禁：Go 1.27.2 通过。

## 待完成门禁

- [x] 后端完整编译和受影响单元测试（Go 1.27.2）
- [x] 前端 frozen-lockfile、typecheck、全量 Vitest、生产构建
- [x] `scripts/check-custom-isolation.py` 和 `git diff --check`
- [x] 复核 operator、Prompt Audit、Moshu 生图/批量生图、branding、管理员使用记录
- [x] 检查 v0.2.15 新增 Cline/Command Code 不被定制平台过滤逻辑误伤
- [x] 本地 Docker 验收
- [ ] 人工确认后才推送、构建镜像或部署

## 10. 本轮前端全量测试维护说明

- **现象**：官方 v0.2.15 更新动态平台目录、账号创建表单和通用布局后，首轮全量
  Vitest 有 33 个旧断言失败，集中在已移除的 Kiro 前端展示、平台颜色、账号状态和
  对话框交互假设。
- **处理**：仅更新测试夹具、断言和允许的原生控件清单，使其匹配当前官方组件契约；
  未恢复被官方移除的旧展示，也未修改生产运行逻辑。后端 Kiro 兼容代码保持不变。
- **结果**：全量 389 个测试文件、2907 个测试通过；typecheck、lint 和生产构建通过。

## 11. 本地 Docker 验收记录

- 项目名：`moshu-reseller-acceptance`；应用端口：`18080`。
- 使用候选源码构建的 `sub2api:0.2.15-custom.1-local`，独立 PostgreSQL、Redis 和
  数据卷，不连接任何生产凭据或数据库。
- 健康、合成管理员登录、`/auth/me`、应用重启持久化、Compose 配置和关键日志检查均通过。
- 推送前需再用最终 GHCR 镜像替换应用容器进行一次健康检查；验收环境不删除卷，发布后
  可用 `docker compose -p moshu-reseller-acceptance -f deploy/docker-compose.operator-test.yml down`
  停止（不带 `-v`）。
