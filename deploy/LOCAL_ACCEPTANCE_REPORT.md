# Sub2API Operator v0.1.182 本地验收报告

日期：2026-08-25（Asia/Shanghai）

## 基线与发布坐标

- 开发分支：`feature/kiro-v0.1.182`
- 上游标签：`v0.1.182`
- 上游提交：`5a7d469622911a6b1291a692376df5fa03f9ac2e`
- 定制版本：`0.1.182-custom.1`
- 源码标签：`custom-0.1.182.1`
- 目标镜像：`ghcr.io/<fork-owner>/sub2api:0.1.182-custom.1`
- 目标平台：`linux/amd64`
- 工具链：Go 1.27.0、Node 20.20.2、pnpm 9.15.9、golangci-lint 2.13.0

本地验收镜像为 `sub2api:0.1.182-custom.1`，镜像 ID 为
`sha256:c48f41a0d197117a8a2f1535c005b5359dc37266a4d01800b5cabe8bf9f49caf`。
该镜像在合并提交前构建，OCI revision 仍是合并前提交，不能作为生产发布坐标；
生产只允许使用源码标签触发 GitHub Actions 后生成并记录的 GHCR 不可变 digest。

## v0.1.182 合并复核

- 官方 `v0.1.179..v0.1.182` 共 208 个提交、505 个文件变化；所有合并冲突均已人工解决。
- 保留 Operator 集中式默认拒绝权限层、特权账号写保护、step-up 2FA、审计和前端精确菜单。
- 保留 Kiro OAuth/API Key、模型映射、用量与 credit 计费、图片 token 估算等定制。
- 保留内置 `/docs` 用户文档和截图资源。
- Cursor 运行时代码继续移除；为存量数据库和已发布迁移 checksum 保留历史约束兼容。
- 合入官方插件管理、OpenAI quota 自动重置、Prompt Rules、模型广场及 v0.1.182 其他修复。
- 官方 `229_plugins.sql`、`230_plugin_artifacts.sql` 与已有自定义 229/230 迁移按完整文件名稳定排序；完整迁移集成测试通过。
- 插件管理未加入 Operator authz 白名单，前端仅 admin 可见；单元测试和真实容器均确认 operator 返回 `403`。
- 修复 Windows 插件包安装时 ZIP 句柄未关闭导致的 rename 失败。
- `govulncheck` 发现官方 `golang.org/x/image v0.41.0` 有 3 个可达 WebP 漏洞；已升级到 `v0.45.0`，复扫为 0 个可达漏洞。

## 已通过的质量门禁

- `go mod tidy`：通过。
- 后端完整 unit：通过。
- 后端完整 integration（隔离 PostgreSQL testcontainers）：通过。
- `golangci-lint 2.13.0`：`0 issues`。
- `govulncheck ./...`：0 个可达漏洞。
- 前端 frozen-lockfile 安装：通过。
- 前端 ESLint 与 Vue TypeScript：通过。
- 前端 Vitest：268 个测试文件、1869/1869 个用例通过。
- 前端生产构建：通过。
- `git diff --check`：通过，仅有 Windows LF/CRLF 转换提示。
- `docker compose -f deploy/docker-compose.operator-test.yml config --quiet`：通过。
- linux/amd64 多阶段 Docker 镜像构建：通过，运行时版本为 `0.1.182-custom.1`。

## 隔离容器验收

使用独立项目、独立 PostgreSQL/Redis/应用数据卷和本地端口完成两轮真实验收：

- PostgreSQL 18、Redis 8、Sub2API 健康检查和自动初始化通过。
- admin、operator、user 三角色真实登录通过。
- admin 保持完整管理权限；普通 user 的管理接口拒绝。
- operator 允许的仪表盘、运维、用户、公告、兑换码、优惠码和使用记录接口返回 `2xx`。
- 分组、渠道、订阅、账号、插件、代理、审计、完整设置和其他未登记管理接口返回 `403`。
- Admin API Key 始终映射为 admin；无效 JWT 返回 `401`。
- operator 对 admin/operator 的单次写、批量混入特权账号和特权账号 API Key 修改均原子拒绝。
- Operator WebSocket、合规确认、重启持久化和日志致命模式检查通过。
- 验收结束后，测试容器、网络和三个项目卷均已删除，无测试项目残留。

## 发布状态与晚上运维门禁

- 本轮未连接或修改腾讯云、OVH、生产数据库、生产容器或生产代理。
- 合并提交与标签推送后，必须等待 GitHub CI、Security Scan 和 Custom CI 全绿。
- 生产只使用 `custom-0.1.182.1` 生成的 GHCR digest，禁止使用本地镜像 ID或 mutable tag 直接上线。
- 晚上发布前必须重新执行 OVH 只读预检，记录当前版本和旧 digest，并完成 PostgreSQL、`.env`、持久化目录及 Compose/1Panel 配置备份。
- 若生产当前版本高于 `v0.1.182`，立即停止，禁止降级覆盖。
- 只替换 Sub2API 镜像引用并重建应用容器，不重建 PostgreSQL、Redis、卷、网络、OpenResty 或 Cloudflare 配置。
- 发布后执行健康、三角色权限矩阵、普通用户 API Key、SSE/OpenResty、5xx 和数据库/Redis 日志检查，并观察至少 30 分钟。
- 任何失败恢复旧镜像 digest；若官方迁移不可逆或旧镜像无法兼容新库，则必须同时恢复发布前数据库备份。
