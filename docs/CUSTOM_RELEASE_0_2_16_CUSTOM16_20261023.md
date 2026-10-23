# custom.16 发布记录（2026-10-23）

## 发布状态

- 版本：`0.2.16-custom.16`。
- 功能修复提交：`0ba89f7d13fa7acb639d450c84614e9c62d21b5f`；发布标签 `custom-0.2.16.16` 指向提交 `fb02af3ece9ebf2cd54633bbfb6b839c6ba0051b`，其中包含修复和本候选文档。标签/镜像已推送；三站现已完成生产部署。
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
- 部署前只读探测三站公网 `/health` 均返回 HTTP 200；该接口不返回版本号，因此单凭这项探测不作为镜像版本证据。

## 生产部署与验收（2026-10-23）

- 生产目标为已确认的 OVH `15.204.82.242`。部署前备份位于 `/opt/sub2api-backups/0.2.16-custom.16-20261023T091138Z`：三站 PostgreSQL custom dump/角色导出、Compose 与环境文件、应用数据归档、主站 1Panel PostgreSQL 配置及 OpenResty 实际挂载配置均已生成；`pg_restore --list`、tar 读取及 SHA256 校验通过。备份目录仅 root 可读写，含轮换前配置，属于敏感数据，勿外传。没有执行 live restore 演练。
- 此前运行日志曾在工具输出中包含数据库凭据，因此部署前轮换了主站 PostgreSQL 管理角色及 L1/COS 数据库角色口令，并同步更新相应配置；不在本报告记录任何口令。PostgreSQL 服务按新配置重建并 healthy，L1/COS Redis 未重建，原数据库与应用数据目录保持。
- 按主站 → L1 → COS 顺序，仅更新 Sub2API 应用镜像为同一个不可变 digest；没有改反代规则、端口、网络、Redis 或业务数据。为同步轮换后的连接配置，L1/COS PostgreSQL 容器亦使用原数据目录按原 Compose 重建。三站均报告版本 `0.2.16-custom.16`、发布提交 `fb02af3ece9ebf2cd54633bbfb6b839c6ba0051b`、镜像 `sha256:037d353afff1a4e18c88ff9ad59fad6735195465a1dbb60ab5ace7c5d33223e7`，容器 healthy / restart 0。
- 公网 `ai.moshu.cloud`、`api.moshu.cloud`、`cos.moshu.cloud` 的 `/health`、`/keys`、`/image-studio`、`/docs/tools/codex` 均返回 200，匿名管理 API 返回 401。部署后数据库活动查询为 0；最近 15 分钟关键启动错误匹配为 0。迁移与 `.14` 完全一致：主站 `328` 条、最大 `9004_custom_key_route_site.sql`、指纹 `631b9a14b22231de548f93b4e1e5a506`；L1/COS 各 `337` 条、最大迁移相同、指纹 `85f0448525583d4cff64f88d982936c0`。无 schema 迁移。
- 未执行付费模型调用、生产故障注入、登录后的 UI 矩阵或长时间观察；生产基础健康通过不等于全部供应商、端点及并发容量均已验收。

## 发布结果与剩余生产步骤

- Runtime fix commit / branch / tag: `0ba89f7d13fa7acb639d450c84614e9c62d21b5f` / `codex/key-account-routing-stability-20261022` / `custom-0.2.16.16` (tag points to release commit `fb02af3ece9ebf2cd54633bbfb6b839c6ba0051b`).
- GHCR immutable digest: `sha256:037d353afff1a4e18c88ff9ad59fad6735195465a1dbb60ab5ace7c5d33223e7`.
- Root Dockerfile build and immutable pull-back verified; runtime version, `pg_dump` and `psql` verified; isolated operator-container smoke passed.
- Production deployment target: confirmed OVH `15.204.82.242`; Main → L1 → COS deployment completed on 2026-10-23. All three sites report the exact `.16` digest/version, healthy with restart 0; public health/page checks passed.
- Backup verification: `/opt/sub2api-backups/0.2.16-custom.16-20261023T091138Z`; database dumps, app/config and proxy-mounted archives passed list/read/SHA checks. No live restore drill was performed.
- 回滚候选 `.14`：`ghcr.io/hzihuan001/sub2api@sha256:61468c96ecbafb5e79f959e06a74291773fae77db235fff130ad9d67df4f2ca7`；只允许应用镜像回退，不恢复旧业务数据库覆盖新数据。
