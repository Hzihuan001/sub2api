# 受控直连 API 入口

`https://nocf.moshu.cloud` 是主站的独立 API 入口，绕过 Cloudflare，继续使用原有 API Key。它不提供登录、后台管理或系统设置页面，不修改主站应用镜像及两个分站。

## 权限与鉴权

在主站「用户管理 → 编辑用户」中的「直连 API 权限」勾选「允许直连接入」。复用现有自定义属性，保留键名 `moshu_direct_access`、类型 `multi_select`，选中值为 JSON 数组 `["enabled"]`。未授权用户默认拒绝，取消勾选后下一次新请求立即重新校验；已经通过鉴权的在途请求不主动断开。

侧车每次用请求中的 Key 查询其真实归属用户，不信任客户端声明的用户 ID、Host 或授权标记，不读取请求正文。仅允许 active、未删除、未过期 Key，且其用户 active、未删除、属性定义启用且未删除、属性值严格为 `["enabled"]`。空值、损坏 JSON、额外选项及重复选项均不授权。

此检查只决定能否使用直连入口。主站原有 Key、IP 白黑名单、余额、配额、分组、模型、并发及计费检查仍完整执行。普通接口使用 Bearer、`x-api-key` 或 `x-goog-api-key`；原生 Gemini 兼容路径另支持原有 `key` 查询参数。重复凭据头、冲突凭据或重复查询 Key 会拒绝。

权限只控制新增 `nocf` 域名；此次没有改变原有主站 Host 的源站访问策略。生产未配置全局 real-IP 重写；后续新增自定义可信 IP 头时，也必须在此入口清除或用真实连接 IP 重写。

业务表结构和迁移不变。`authorization.sql` 添加独立数据库角色及布尔授权函数：侧车账号只有函数 EXECUTE 权限，不能 SELECT 业务表、枚举 Key 或修改数据；无登录函数 owner 仅获必要列的读取权限。`attribute.sql` 创建现成管理表单使用的属性定义。

## 部署配置

生产目录：`/opt/moshu-direct-access`。Compose 只创建独立 `auth` 侧车，连接主站现有数据库网络；不要重建主应用、数据库或 Redis，不改分站。

| 配置 | 用途 |
| --- | --- |
| `DIRECT_ACCESS_IMAGE` | 独立侧车的不可变镜像 digest |
| `DIRECT_ACCESS_DB_NETWORK` | 主站 PostgreSQL 所在的现有 Docker 网络名 |
| `./secrets/auth.dsn` | 只读挂载的数据库 DSN 秘密文件；账号为 `moshu_direct_access_auth` |
| `DIRECT_ACCESS_DSN_FILE` | 容器内秘密路径 `/run/secrets/auth.dsn`，不从明文环境变量读取数据库密码 |
| `DIRECT_ACCESS_LISTEN_ADDR` | 容器内监听 `:8080`，宿主机仅映射 `127.0.0.1:18190` |

秘密文件须允许 Compose 指定的 UID `65532` 读取，其他用户不可读取。数据库连接池最多 4 个连接，单次查询超时 2 秒，无授权缓存；数据库、函数或侧车故障一律拒绝，绝不绕过。侧车日志不写 Key、DSN、请求 URI 或正文。

Nginx 模板独立渲染为新 vhost：`__DOMAIN__=nocf.moshu.cloud`，`__AUTH_ORIGIN__=http://127.0.0.1:18190`，`__MAIN_ORIGIN__` 指向现有主站内部地址；`__CERT_DIR__`、`__ACME_ROOT__` 使用本机证书及 ACME webroot。`/_nocf_auth` 仅内部可访问，不转发正文；转发给主站前清除客户端伪造的直连标记及不可信客户端 IP 头。

API 反代读取、发送和客户端响应发送超时为 10 小时；请求及响应缓冲、缓存均关闭，保留 SSE/WebSocket 透传。请求体上限 200 MiB、读取请求体的相邻读取超时 600 秒。10 小时是相邻 I/O 超时设置，不承诺客户端网络或上游会一直保持连接。

访问日志只写 `$uri`，不含查询参数；此 vhost 的 Nginx 错误日志写 `/dev/null`，避免原生 Gemini 查询 Key 被原始 URI 记录。排障使用净化后的访问状态、耗时和侧车健康状态，不临时开启原始 URI 日志。

## 当前生产与验收

镜像：`ghcr.io/hzihuan001/moshu-direct-access@sha256:43175cca0723263ad44798a1f245454bc0fc3a3808936a15f8f873989b884508`。

2026-10-27 已部署，用户 ID 4 已授权。隔离本地验收 48/48 通过，含长连接；生产免费模型目录检查返回 200、15 个模型，未授权 Key 返回 403、匿名请求返回 401、管理页面返回 404。未以此宣称真实上游长期请求永不失败。

常用检查（在生产目录执行）：

```sh
docker compose --env-file .env -f compose.yml ps
curl --fail http://127.0.0.1:18190/healthz
curl --fail https://nocf.moshu.cloud/health
docker exec 1Panel-openresty-ZmgO nginx -t
```

不要在命令行、截图或共享日志中粘贴生产 Key。`/health` 是入口存活检查，侧车 `/healthz` 还检查数据库连接；均不代表某个上游模型一定可用。

证书名为 `moshu-nocf`，当前到期日 2027-01-25。Certbot 自动续期及成功后校验、重载 Nginx 的 hook 已安装。检查定时任务，并可用 `certbot renew --cert-name moshu-nocf --dry-run` 验证续期；修改 hook 后先确保 `nginx -t` 成功。

## 回滚

1. 先将新增 `nocf` vhost 改成保留证书、对 API 统一返回 503 的停用配置；容器内 `nginx -t` 成功后 reload。不要直接删除 vhost 让此域名落入其他默认站点；保留主站及分站原 vhost。
2. 再在生产目录执行 `docker compose --env-file .env -f compose.yml down`，仅移除此项目侧车，不加 `--volumes`、不执行全局 prune。
3. 可选：取消所有用户该属性授权，或停用此属性定义。需要彻底移除数据库授权函数/角色时另行核对依赖后处理；无需业务表降级或恢复数据库备份。

回滚不重建主应用，不修改主站现有账户、余额、Key、配置及分站。DNS 与证书可暂留以便恢复，不能让旧直连地址回退到不带权限检查的通配 vhost。
