# Sub2API Operator 本地验收报告

日期：2026-08-20（Asia/Shanghai）

## 基线与构建

- 分支：`custom/v0.1.178`
- 上游基线：`v0.1.178` / `e0c48a19ed794a565e3858662520afe0a1f9f0ba`
- 定制版本：`0.1.178-custom.1`
- 预定源码标签：`custom-0.1.178.1`
- 工具链：Go 1.26.6、Node 20.20.2、pnpm 9.15.9、golangci-lint 2.9.0
- 本地镜像：`sub2api:0.1.178-custom.1`
- 本地镜像 ID：`sha256:cf48b466677064d606a48f1661100a6896f50f9bbbed15a083cf801fe93d44f6`
- 镜像平台：`linux/amd64`

本地代码尚未提交，因此本地镜像的 OCI revision 仍是固定基线提交。提交定制代码后，custom CI 会把实际定制提交写入 GHCR 镜像标签；不得把当前本地 revision 当作发布提交证明。

## 通过的质量门禁

- 后端 operator authz、管理认证、目标保护、角色授予与最后管理员测试通过。
- `golangci-lint 2.9.0`：`0 issues`。
- 前端 frozen-lockfile 依赖可用于构建。
- 前端 ESLint、Vue TypeScript 检查通过。
- 前端 Vitest：231 个测试文件、1656/1656 用例通过。
- 前端生产构建通过。
- `git diff --check` 通过（仅 Windows LF/CRLF 提示）。
- Docker Compose 配置校验通过。

## 容器验收

最终镜像在隔离 Compose 项目中完成以下验收：

- PostgreSQL、Redis、Sub2API 健康检查与初始化。
- admin、operator、user 三角色真实登录。
- admin 全量访问、普通 user 管理接口拒绝。
- operator 明确允许接口返回 2xx；分组、渠道、订阅、账号、代理、审计、设置及完整运维设置返回 403。
- 无效 JWT 返回 401；Admin API Key 严格映射为 admin。
- 特权用户单次/批量写保护、API Key 所有者保护和批量原子拒绝。
- operator WebSocket、合规确认与重启持久化。
- 容器日志未匹配 panic、数据库/Redis 连接失败或迁移失败。

验收完成后，隔离容器、网络、PostgreSQL/Redis/应用数据卷均已删除。

## 浏览器验收

- operator 登录默认进入 `/admin/dashboard`。
- 后台侧边栏只显示仪表盘、运维监控、用户管理、公告、兑换码、优惠码、使用记录。
- operator 的个人 API Key、个人用量、订阅、兑换和资料入口保留。
- 禁止后台 URL 直接访问时重定向到管理仪表盘，后端同时返回 403。
- admin/operator 用户行无编辑、充值、禁用或删除入口，批量选择框禁用；普通 user 保持可管理。
- 仪表盘不显示通往分组管理的快捷入口。
- 运维页不显示配置或日志清理控件；告警规则只读。
- operator 不读取完整 `ops/advanced-settings`，仅使用窄化 capabilities 接口。
- 使用记录页可查看清理任务历史，但不能创建或取消清理任务。

## 上游/本机环境例外

完整 integration 套件中仍可复现与本次定制无关的上游或本机环境问题：外部 TLS 指纹站点证书链不受信、阿里云验证码外部接口 503、一个日期断言受本地时区影响，以及既有 service 时序型用例偶发失败。operator 相关单元、集成和真实容器矩阵均通过。

## 尚未执行的外部门禁

- 未配置个人 Fork `origin`，未提交、推送或创建源码标签。
- 未向 GHCR 发布，因此没有远端不可变 digest。
- 未获得腾讯云和 OVH 的只读检查/备份/部署访问方式。
- 未执行腾讯云 STAGING、30 分钟观察、OpenResty/SSE 验收。
- 未触发独立 OVH 生产发布确认，也未改动任何生产资源。

后续发布必须严格按照 `CUSTOM_RELEASE_RUNBOOK.md` 使用同一 GHCR digest，并在腾讯云全部通过后单独确认 OVH 生产发布。
