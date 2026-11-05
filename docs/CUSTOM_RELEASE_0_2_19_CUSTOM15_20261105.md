# 0.2.19-custom.15：SSE 心跳后的安全故障切换

日期：2026-11-05
状态：主站、L1、COS 已部署并通过基础生产验收

## 问题与行为

部分上游会先发送 SSE 心跳，随后在真正的模型内容到达前断开。旧逻辑将心跳误认为已经向客户端发送了模型输出，因此停止账号切换，并将读流错误直接返回给用户。

本次把 SSE keepalive 与语义内容输出区分开：如果上游读流在模型内容开始前失败，允许按现有路由策略切换账号继续请求；一旦已经向客户端发送语义内容，就不重放请求，避免重复或拼接损坏的回答。兼容转换的 Responses 流也应用相同边界。

这解决的是“内容尚未开始输出时，心跳后读流失败导致无法切换”的代理侧问题。上游 Cloudflare 返回的 524（例如其 120 秒读取窗口超时）仍属于上游处理时间限制；本次不能让该上游请求无限延长，也不会在已经发送部分回答后重放。

## 代码与测试

- 源码提交：`898097ae9ea04a2c8ef3cf407331caf37054117c`（`fix gateway failover after SSE keepalive`）
- 版本：`0.2.19-custom.15`
- 镜像：`ghcr.io/hzihuan001/sub2api@sha256:c949104a7e9a0393d73c8b2c17b5ab0297926399dfae979724975932c06b7af1`
- 新增/运行的目标回归覆盖普通 SSE 与 Responses 流在语义输出前可切换、语义输出后不重试；覆盖 OpenAI 兼容流和切换次数边界。
- 定向 Go 回归测试通过；本地隔离 PostgreSQL/Redis 容器中，以拉回的不可变镜像完成 API Key、网关和健康检查验收。
- 官方 GitHub Actions 全量工作流未作为本次通过证据：当前工作流中的 Go 小版本锁定与构建环境不一致，且本次没有触发版本标签工作流。不要将本次描述为完整 CI 全绿。
- 本次发布范围不包含测智计划思考强度配置；当前计划新增/编辑表单没有该设置项。

## 生产发布与验收

三个站点顺序发布同一不可变镜像：

| 站点 | 地址 | 容器 | 结果 |
|---|---|---|---|
| 主站 | `ai.moshu.cloud` | `1Panel-sub2api-bjGj` | `running / healthy`，重启数 0 |
| L1 | `api.tokenpulse.top` | `api-L1` | `running / healthy`，重启数 0 |
| COS | `cos.tokenpulse.top` | `api-COS` | `running / healthy`，重启数 0 |

三站公网 `/health`、`/` 和 `/pelican-showcase` 均返回 HTTP 200；更新后最近启动日志中未见 panic、fatal 或迁移错误标记。每站部署前检查在途请求和队列归零，按主站 → L1 → COS 顺序只重建应用容器。PostgreSQL、Redis、业务卷、网络及反向代理均未修改。

## 备份与回滚

- 部署前备份目录：`/opt/sub2api-deploy-backups/custom15-gateway-20261105T054114Z/`
- 旧镜像回滚 digest：`sha256:2f76a571730141b6aec90638d814372d16552be3032303b1e401c8603719a922`
- 回滚时仅将对应站点 Compose 的应用镜像恢复到旧 digest 并重建应用容器，不恢复旧数据库、不覆盖 Redis 或业务卷。
