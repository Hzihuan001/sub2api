# Sub2API Operator v0.1.179 本地验收报告

日期：2026-08-20（Asia/Shanghai）

## 基线与构建坐标

- 分支：`custom/v0.1.179`
- 上游基线：`v0.1.179` / `75f88be5f75c27771836b586f7de1503afa0e3bc`
- 定制版本：`0.1.179-custom.1`
- 预定源码标签：`custom-0.1.179.1`
- 工具链：Go 1.26.6、Node 20.20.2、pnpm 9.15.9、golangci-lint 2.9.0
- 本地镜像：`sub2api:0.1.179-custom.1`
- 本地镜像 ID：`sha256:78748035e9c86989a58a5b3294f780aa64f7ef410bec32a8afa48cc00f31d11a`
- 镜像平台：`linux/amd64`

当前定制尚未提交，因此本地镜像的 OCI revision 仍是上游基线提交。只有提交定制代码后，由 custom CI 构建的镜像才能把实际定制提交写入 OCI revision；不得把当前本地 revision 当作发布提交证明。

## v0.1.179 同步复核

- 官方 `v0.1.179` 相对 `v0.1.178` 有 78 个提交、212 个文件变化；Operator 定制与上游仅有 8 个文件重叠，3 个文本冲突已人工合并。
- 上游没有新增管理端路由。operator 的固定 HTTP 方法 + Gin 路由模板表保持默认拒绝；未来未登记管理接口仍自动返回 `403`。
- 保留了 v0.1.179 的用户角色 `Select` 组件、Grok API Key 占位符测试和运维平台目录更新，同时叠加 operator 的角色选项与控件隐藏逻辑。
- v0.1.179 新增官方迁移 `226_add_usage_log_effective_model_indexes_notx.sql`、`227_composite_routes_add_cn_providers.sql` 和 `228_channel_pricing_multipliers.sql`；本次 Operator 定制本身仍不新增数据库迁移。
- v0.1.179 将 OpenAI 超长上下文计价开关调整为“分组或账号任一启用”。未来恢复生产发布前，必须复核生产分组和账号设置，并另行完成从当前生产版本到 v0.1.179 的合成数据迁移演练。

## 已通过的质量门禁

- 后端 Operator 专项测试通过：角色映射、JWT/Admin API Key、WebSocket、默认拒绝、合规确认、特权账号单次与批量写保护、step-up 2FA、最后管理员保护及窄化支持接口。
- `golangci-lint 2.9.0`：`0 issues`。
- 前端 `pnpm install --frozen-lockfile` 通过。
- 前端 ESLint、Vue TypeScript 检查通过。
- 前端 Vitest：240 个测试文件、1702/1702 个用例通过。
- 前端生产构建通过。
- `git diff --check` 通过（仅 Windows LF/CRLF 转换提示）。
- Docker Compose 配置校验通过。

## 上游/本机测试例外

完整 Go unit/integration 套件在当前 Windows 环境未能全绿，失败均位于未被本次定制修改的上游代码：

- 严格沙箱阻止测试在用户临时目录或包目录创建 `.entc`，导致 schema 和页面图片路径用例失败；在普通本机权限下单独重跑后两项均通过。
- 阿里云验证码 SDK 测试收到外部 `503`，4 个 repository 用例失败。
- `TestContentModerationRuntimeSnapshotRefreshFailureKeepsStaleConfig` 存在时序波动；完整套件中失败，单独重跑通过。
- TLS 指纹 integration 测试访问 `tls.peet.ws` 时，本机证书链不受信。

这些例外不影响 Operator 专项包、lint、前端全量测试或真实容器权限矩阵；但发布 CI 仍必须在 Ubuntu runner 上重新执行完整后端门禁，CI 未全绿时不得发布镜像。

## 隔离容器验收

使用项目名 `sub2api-operator-test-v01179-local`、独立 PostgreSQL/Redis/应用数据卷和 `127.0.0.1:18080` 完成真实容器验收：

- PostgreSQL、Redis、Sub2API 健康检查与自动初始化通过。
- admin、operator、user 三角色真实登录通过。
- admin 保持完整管理访问；普通 user 的管理接口全部拒绝。
- operator 明确允许接口返回 `2xx`；分组、渠道、订阅、账号、代理、审计、设置和完整运维设置返回 `403`。
- 无效 JWT 返回 `401`；Admin API Key 严格映射为 admin。
- 特权用户单次/批量写保护、API Key 所有者保护和批量原子拒绝通过。
- operator WebSocket、合规确认和应用重启后的持久化通过。
- 容器日志未匹配 panic、数据库/Redis 连接失败或迁移失败。
- 验收完成后，该项目的容器、网络及三个数据卷均已删除并复核无残留。

## 浏览器 UI 验收

使用第二套隔离项目 `sub2api-operator-test-v01179-ui` 和同一发布候选镜像完成真实浏览器验收：

- operator 登录成功并进入 `/admin/dashboard`，完成合规确认后控制台可正常使用。
- 后台侧边栏只显示仪表盘、运维监控、用户管理、公告、兑换码、优惠码和使用记录；分组、渠道、订阅、账号、代理、审计和设置等后台入口均未出现。operator 原有的个人账户菜单保持可用。
- 直接访问 `/admin/groups` 被前端路由守卫重定向回 `/admin/dashboard`。
- 运维页可查看实时指标、系统日志和告警规则；告警规则弹窗仅有查看与刷新能力，没有创建、编辑或删除入口，系统日志没有清理入口。
- 使用记录页的清理弹窗仅显示最近清理任务和刷新按钮，没有创建或取消清理任务入口。
- 用户页中普通 user 保留编辑、禁用和充值等操作；admin/operator 的批量选择被禁用，且没有编辑、禁用或充值入口。operator 创建用户弹窗没有角色字段，只能创建普通 user。
- 在登录状态下用全新标签页复核管理仪表盘，浏览器控制台没有 warning 或 error。
- 验收结束后关闭了临时浏览器标签页；第二套隔离容器、网络和数据卷随后一并清理。

## 当前发布状态

- 腾讯云 STAGING 保持原状态，未部署或修改 v0.1.179。
- OVH 生产发布保持暂停，未连接或修改任何生产资源。
- `custom/v0.1.179` 尚未创建定制提交、未推送、未创建 `custom-0.1.179.1` 标签，也未向 GHCR 发布镜像。
- 后续如需发布，应先提交并推送分支，再由 GitHub Actions 完整门禁生成 GHCR digest；是否使用腾讯云 STAGING 由用户另行决定。
- OVH 必须在新的人工确认后才能恢复发布，并且发布前仍需完成备份、迁移演练、长上下文计价设置复核和只读预检。
