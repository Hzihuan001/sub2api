# Moshu `0.2.16-custom.14` 发布记录

## 发布坐标

- 官方基线：`v0.2.16`；源码提交：`09e885d4eb2098a48758b9b5309e166bd85737ce`
- 运行标签：`custom-0.2.16.14`
- 镜像：`ghcr.io/hzihuan001/sub2api:0.2.16-custom.14`
- 不可变摘要：`sha256:61468c96ecbafb5e79f959e06a74291773fae77db235fff130ad9d67df4f2ca7`
- 架构：`linux/amd64`；上一版回退摘要：`sha256:9d79c4378f3e4dc6dd4d1648cff7420947ccb7984a33d7bb2cbd11030c32c14f`

本轮发布的是受限的 OpenAI Responses 连续会话恢复适配器与既有多分组路由稳定性修复。恢复逻辑只在明确支持且可验证的官方 OpenAI API Key/已存储 Responses 场景启用；不支持的 provider-managed 状态、工具/文件/图片、OAuth/Codex、超时或部分输出继续 fail closed，不静默删除状态标识重试。无数据库迁移变更。

## 门禁与产物

- Docker 定向后端回归、恢复适配器单测、`git diff --check` 通过。
- 本地三站隔离 Compose 验收通过：`12/12` 合成网关请求，覆盖同组账号切换、后续分组、实际组计费、exactly-once 结算、重复配置与权限隔离；证据：`D:/projects/key-route-recovery-20261023-r2/evidence.json`。
- 根 `Dockerfile` 重新构建并写入完整提交元数据；镜像拉回后版本输出为 `0.2.16-custom.14 (commit: 09e885d4eb2098a48758b9b5309e166bd85737ce)`。Actions 仍未运行，本轮使用已授权的本地构建与 GHCR 路径，不称 CI 通过。
- 发布前在 OVH 生成并验证备份：`/opt/sub2api-backups/v0.2.16-custom.14-20261022T195945Z`；三站 PostgreSQL dump、持久化归档、Compose/环境和反代配置均通过可读性/校验验证。备份迁移计数为主站 `328`、L1/COS `337`。

## 生产部署结果

严格按主站 → L1 → COS 顺序，仅重建应用容器；没有重建 PostgreSQL、Redis、卷、网络或反代，也没有付费模型调用或故障注入。

| 站点 | 部署时间（北京时间，2026-10-23） | 结果 | 迁移变化 |
| --- | --- | --- | --- |
| 主站 `ai.moshu.cloud` | 04:15:31–04:15:39 | 通过，healthy / restart 0 | 无 |
| L1 `api.moshu.cloud` | 04:16:03–04:16:14 | 通过，healthy / restart 0 | 无 |
| COS `cos.moshu.cloud` | 04:16:35–04:16:43 | 通过，healthy / restart 0 | 无 |

三站部署前在途请求、等待、批量任务和图片处理均为 0。每站均通过本地与公网 `/health`、`/keys`、`/image-studio`、`/docs/tools/codex`（200）及受保护匿名接口（401）检查；版本/完整提交/镜像摘要一致，关键启动日志为 0，PostgreSQL/Redis 身份与启动时间保持不变。部署后只读复核仍显示三站健康、所有在途计数为 0。

## 回退

若后续发现问题，按站点停止后续发布并只把失败站点应用回退到上一版 `.13` 摘要；保留新增兼容表和上线后业务数据，不恢复旧数据库覆盖新账目。回退命令必须使用已记录的 `.13` 不可变摘要，并重新执行版本、健康、迁移和路由作用域检查。

## 限制

本记录不宣称所有模型商、所有有状态端点、付费生产推理、150 用户容量或长期稳定性已完成验收；不支持的跨 provider 状态迁移仍应向客户端返回明确错误。
