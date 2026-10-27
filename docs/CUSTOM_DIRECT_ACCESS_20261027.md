# 主站受控直连入口（2026-10-27，已上线）

## 当前入口与使用

- `nocf.moshu.cloud`：Cloudflare **DNS only**，A 记录指向 OVH `15.204.82.242`，TTL 300；客户端直接 TLS 连接 OVH，不经过 Cloudflare 数据代理。
- API Base URL：`https://nocf.moshu.cloud/v1`。原 API Key、余额、分组、模型、计费及路由保持；兼容原生 Gemini 等已有 API 路径，不提供前端/管理接口。
- 已为本次受影响用户 ID 4 开通，其他用户默认不允许。管理员在主站「用户管理 → 编辑用户 → 直连 API 权限」勾选「允许直连接入」；撤销后下一个请求即时拒绝，在途请求继续完成。
- 普通管理员是否可操作，沿用现有 `users.write` 和目标用户保护；普通用户无属性写接口，不能自行授权。

## 实现与发布坐标

- 本轮工作树 `D:/projects/sub2api-direct-access-20261027`，分支 `codex/direct-access-20261027`，基于集成 `28159e883499df18d6f0ef7819e500eab95a5896`；未从旧根工作树构建。
- 入口源码提交 `4e746e82d`、不可变标签 `direct-access-20261027.1` 已推送；功能分支与 `custom/integration` 同步，后续文档收尾不移动入口标签或主应用运行标签。
- 独立镜像 `ghcr.io/hzihuan001/moshu-direct-access:20261027.1`，不可变 digest `sha256:43175cca0723263ad44798a1f245454bc0fc3a3808936a15f8f873989b884508`，复用本地验收通过的构建产物。
- 主站及两分站应用继续运行 `.18.4`，运行源 `1524aa0985f432df19c9144c726224d2852308d6`，digest `sha256:80adc97b50213ba840e68d6c13b932701cff294f04fb5dcd291ffe12b63f49e6`；没有重建/重启应用、PostgreSQL 或 Redis，原主站反代文件哈希保持。
- 独立 Go 入口在 `backend/cmd/direct-access`，部署文件在 `deploy/direct-access`。无主程序接线、前端改动、新依赖或业务表结构/迁移改动；登记为 `custom/manifest.yml` 的部署扩展并纳入升级保护路径与可执行单元测试清单。
- 复用原有自定义用户属性 `moshu_direct_access`，严格只接受 `["enabled"]`。新增布尔授权函数与受限角色；函数所有者 `NOLOGIN`、精确列只读、空 `search_path`，侧车只有函数 `EXECUTE`，不能查询业务表/枚举 Key 或修改数据。
- 侧车仅本机 `127.0.0.1:18190`，4 个数据库连接、2 秒查询超时、无许可缓存、依赖异常拒绝。不会读取模型请求正文；原生鉴权继续核查余额、IP、并发、配额、模型及分组。
- 独立 OpenResty vhost：请求/响应不缓冲，发送/读取 10 小时，200 MiB 请求上限、相邻上传读取 600 秒；清除伪造身份/IP头，保留 SSE 与 WebSocket 透传。生产全局无 real-IP 重写，自定义转发 IP 头列表为 `[]`。
- 日志只保存不带查询串的路径/状态/耗时；关闭此 vhost 原始错误日志，防止 Gemini query Key 被 Nginx 错误信息带入。SSO 属性映射已确认不使用该保留键。

## 验收事实与边界

- Go 单元测试、`go vet` 及真实 PostgreSQL 18 集成通过；集成包含 23 个状态/撤销/严格 JSON 子场景，验证受限角色不能读写业务表、建表或切换函数 owner，撤销函数执行权即时拒绝。
- 隔离 Compose `moshu-direct-access-acceptance` 联合验收 **48/48** 通过：许可/撤销、重复与冲突凭据、失效 Key/用户/属性、管理及内部路径、真实 IP、数据库故障与恢复及日志净化。
- 80 MiB JSON 传输哈希一致；127 秒上传空窗后正常完成；130 秒静默响应正常完成；130 秒 SSE 的 14 次心跳及完成事件完整，无模型费用。使用实际模板渲染，仅去掉本地 TLS/重定向。
- 生产免费验收：授权前 403，授权后 `/v1/models` 200/15 模型，与主站原生目录一致；其他用户 403、匿名 401、冲突凭据 401、管理路径 404。外网独立解析/有效 HTTPS 确认远端 IP 为 OVH，响应没有 CF-Ray。
- 三站应用与数据库身份、镜像、启动时间及重启次数保持；Redis 没有 Docker healthcheck，核实为运行且身份不变。首次证据收集错误地要求 Redis 有 healthcheck，修正检查后通过，没有重启 Redis 或额外授权。
- Let's Encrypt 证书 `moshu-nocf` 到期日 **2027-01-25**；独立续期 hook 已安装，`certbot renew --cert-name moshu-nocf --dry-run` 成功。
- 本地证据在忽略目录 `deploy/direct-access/.acceptance/evidence.json`，生产摘要保存在独立部署目录。未保留或提交用户请求正文/生产 Key/数据库密码。

此前主站的 `Failed to read request body` 为约 74.3 MB 上传、约 125 秒处截断，不能仅凭日志断言一定由 CF 主动断开。直连入口去掉 CF 这一中间层，但不保证客户端网络或真实模型上游永不失败。127/130 秒为合成验收，并非用户原始大上下文实测；新权限仅管 `nocf`，未改变原主站 Host 的源站访问策略。

## 运维与回退

见 [部署说明](../deploy/direct-access/README.md)。需要停用时先保留 nocf TLS vhost 并改成 API 返回 503，校验后 graceful reload，再停止独立 Compose；不得删除 vhost 后让域名落入无门禁的默认站点。无需降级业务表、恢复旧业务数据库或重建应用。

隔离验收环境暂留本机 `127.0.0.1:18390`，只含合成数据；停止命令为 `docker compose -p moshu-direct-access-acceptance -f deploy/direct-access/compose.acceptance.yml down`，不要全局 prune 或删除其他会话的数据卷。
