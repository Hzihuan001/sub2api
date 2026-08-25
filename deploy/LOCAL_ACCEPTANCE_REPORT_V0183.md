# Sub2API Operator v0.1.183 本地验收报告

日期：2026-08-26（Asia/Shanghai）

## 发布坐标

- 开发分支：`feature/kiro-v0.1.183`
- 上游标签：`v0.1.183`
- 上游提交：`e8cb019fabf8b55199436229044cbf9aa7a82564`
- 定制版本：`0.1.183-custom.1`
- 计划源码标签：`custom-0.1.183.1`
- 目标平台：`linux/amd64`

## 合并复核

- 合入 OpenAI OAuth 配额耗尽暂停、Codex `session-id` 粘性、容量溢出绑定保护及 tool-call item ID 类型修复。
- 合入 Kimi 403 临时冷却、邮箱换绑并发保护、Antigravity 64000 token 上限和 composite 频道监控聚合修复。
- 官方本次没有新增数据库迁移，也没有前端业务变更。
- 保留 Operator 集中式默认拒绝权限层、Kiro、内置文档、插件 admin-only 和历史迁移 checksum 兼容。
- 解决版本文件与 OpenAI client tool 冲突；同时保留官方 `clientItemID` 类型恢复和定制 namespace 名称恢复。
- 针对合并后发现的 `functions__exec` namespace 回归，恢复 custom tool 生命周期门控、流式事件和非流式输出映射；相关回归测试通过。

## 质量门禁

- 后端完整 unit：通过。
- 后端完整 integration：通过。
- golangci-lint 2.13：`0 issues`。
- govulncheck：0 个可达漏洞。
- 前端 frozen-lockfile、lint、typecheck：通过。
- 前端 Vitest：268 个文件、1869/1869 个用例通过。
- 前端生产构建：通过。
- Go 格式、`git diff --check`、Compose 配置和敏感信息扫描：通过。

## 隔离容器验收

- 本地镜像：`sub2api:0.1.183-custom.1`。
- 独立 PostgreSQL、Redis、应用数据卷、网络和 `127.0.0.1:18080` 健康启动。
- admin/operator/user 登录、权限矩阵、插件接口 operator 403、Admin API Key、WebSocket、特权账号保护及重启持久化均通过。
- 验收结束后测试容器、网络和三个项目卷已清理。
- 本地镜像在 merge commit 前构建，OCI revision 仍是上一个 HEAD，禁止用于生产；生产只能使用标签触发 CI 后生成的 GHCR 不可变 digest。

## 发布状态

- 本轮没有修改腾讯云或 OVH。
- OVH 当前 `0.1.182-custom.1` 保持运行；升级生产需要新的独立确认、备份、精确 digest 和 30 分钟观察。
