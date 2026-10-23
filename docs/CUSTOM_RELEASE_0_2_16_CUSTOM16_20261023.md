# custom.16 发布记录（2026-10-23）

## 发布状态

- 版本：`0.2.16-custom.16`。
- 功能修复提交：`0ba89f7d13fa7acb639d450c84614e9c62d21b5f`；发布标签 `custom-0.2.16.16` 指向提交 `fb02af3ece9ebf2cd54633bbfb6b839c6ba0051b`，其中包含修复和本候选文档。标签/镜像已推送；生产未部署，仍按 `.14` 记录。
- 前一生产版本：`0.2.16-custom.14`，镜像 `ghcr.io/hzihuan001/sub2api@sha256:61468c96ecbafb5e79f959e06a74291773fae77db235fff130ad9d67df4f2ca7`。
- 过渡记录：`custom-0.2.16.15` 源码标签已推送，但最终检查发现两项故障恢复缺陷。为保持已推送 tag 不可变，没有移动该标签；`.15` 未构建、未发布 GHCR 镜像、未部署，现由 `.16` 修正取代。
- 数据库迁移：无。本次改动仅影响 OpenAI Responses 状态引用拒绝后的账号切换与请求恢复逻辑，不新增持久化会话内容。

## 改动与安全边界

- 多组 API Key 不再因中转站自有策略拒绝 Responses 状态请求。切换账号时仅剥离绑定旧上游凭证的不透明 `previous_response_id`、`conversation`、`item_reference` 等引用，保留当前请求实际携带的用户输入和其他可独立使用内容。
- 对首个上游明确以 HTTP 400/404 拒绝 `previous_response_id` 的情况，仅当结构化错误明确指出该引用无效、不存在或不受支持时才尝试恢复；普通参数错误、鉴权错误、通用 4xx/5xx 和不确定超时不进入此特例。
- 特例重试不会把确定的引用拒绝当作账号健康故障，不触发账号冷却或按通用错误码暂停；失败账号会加入本次请求的排除集合，避免重复选中同一账号。随后沿现有路由/切组和切换预算继续尝试，并在耗尽时优先返回最后观察到的真实上游错误。
- 不改变普通错误处理、计费预留与结算流程；不写入长期对话历史、不新增迁移、不宣称跨账号能恢复旧账号上的服务端历史。
- 如果客户端请求只携带增量内容，且剥离引用后没有足够上下文，新账号仍可能返回真实的上下文/参数错误。响应超时或断连时无法证明上游未执行，重试可能导致重复上游执行或费用；工具、副作用型操作也不能保证无感重放。

## 验证

- Docker 中使用 Go `1.27.2` 执行 `go test -tags unit ./internal/service ./internal/handler ./internal/server/middleware -count=1`：service `267.528s`、handler `42.874s`、server/middleware `0.135s`，全部通过。另有真实 API-key passthrough handler 集成回归，验证明确拒绝旧引用后账号 A 不产生账号故障副作用、请求排除 A 并由 B 成功响应。
- 回归覆盖明确 Responses 引用拒绝的窄匹配、普通 400 不误判、引用拒绝不产生账号失败副作用，以及请求内排除被拒绝账号后继续重试。
- `git diff --check` 通过。
- 正式候选从干净 Git tag 归档、根目录 `Dockerfile` 构建：`linux/amd64`，build date `2026-10-23T07:59:26Z`。镜像内 `--version` 显示版本和发布提交 `fb02af3ece9ebf2cd54633bbfb6b839c6ba0051b`；`pg_dump` / `psql` 均为 PostgreSQL `18.6`。构建不包含未跟踪的实验目录 `backend/internal/keyrouting/replay/`。
- GHCR 镜像：`ghcr.io/hzihuan001/sub2api:0.2.16-custom.16`；不可变摘要 `sha256:037d353afff1a4e18c88ff9ad59fad6735195465a1dbb60ab5ace7c5d33223e7`。已按摘要重新拉回；镜像 ID 与发布摘要一致。拉回镜像运行 `deploy/tests/operator-container-test.ps1` 全部通过，隔离 HTTP 权限矩阵、重启持久性和健康检查通过，临时容器/数据卷/网络已清理。此脚本不代替代理路由测试；路由特例以 Docker handler 集成回归为证。
- GitHub Actions 对 `.16` 标签 push 没有启动作业，手动 dispatch 返回 HTTP 422：该仓库 Actions 已禁用。因此没有成功 CI 证据，发布采用仓库既有授权的本地 Docker 构建、GHCR 推送和摘要拉回流程。
- 生产备份、Main → L1 → COS 部署和线上健康核验尚未完成。当前环境没有 `~/.ssh/config`/可解析的 SSH 主机别名，不能安全定位生产服务器；不得猜测主机或改生产容器。需提供可用 SSH host/alias 后，按现有生产顺序和备份流程继续。不得把以上单元测试或容器冒烟写成生产验收。

## 发布结果与剩余生产步骤

- Runtime fix commit / branch / tag: `0ba89f7d13fa7acb639d450c84614e9c62d21b5f` / `codex/key-account-routing-stability-20261022` / `custom-0.2.16.16` (tag points to release commit `fb02af3ece9ebf2cd54633bbfb6b839c6ba0051b`).
- GHCR immutable digest: `sha256:037d353afff1a4e18c88ff9ad59fad6735195465a1dbb60ab5ace7c5d33223e7`.
- Root Dockerfile build and immutable pull-back verified; runtime version, `pg_dump` and `psql` verified; isolated operator-container smoke passed.
- Production deployment connection target: not configured/resolved in this workspace; production rollout remains blocked until the operator supplies a valid SSH target/alias.
- 生产备份、Main → L1 → COS 健康/版本/数据服务不变性结果：待定。
- 回滚候选 `.14`：`ghcr.io/hzihuan001/sub2api@sha256:61468c96ecbafb5e79f959e06a74291773fae77db235fff130ad9d67df4f2ca7`；只允许应用镜像回退，不恢复旧业务数据库覆盖新数据。
