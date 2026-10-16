# Moshu `0.2.16-custom.11` 发布记录

## 发布坐标

| 项目 | 值 |
| --- | --- |
| 版本 | `0.2.16-custom.11` |
| 源码提交 | `0bb65a1087bbe38cb1fa8e88b09e6c2b6f15cc43` |
| 分支 | `codex/group-failover-rules-v0.2.16` |
| 标签 | `custom-0.2.16.11` |
| 镜像 | `ghcr.io/hzihuan001/sub2api:0.2.16-custom.11` |
| 不可变 digest | `sha256:022704b184874c61ecd50587252867ad109f943afa75eed2b670fd3409aa916d` |
| 架构 | `linux/amd64` |
| 发布日期 | 2026-10-16（Asia/Shanghai） |

## 本轮内容

- 账号列表名称列在账号停止调度时直接显示“停止调度”和时间。自动冷却优先显示预计恢复时间；手动停止没有独立截止时间时回退到账号状态更新时间。
- 状态列继续保留限流、过载、临时不可调度、错误和手动停止等原因，并保留可用的恢复操作。
- 移除系统级重复过载自动停号和普通裸 `529` 的全局账号冷却。无显式分组状态码规则时，不会因为重复 `502/503/529/504` 写入 `overload_until` 或暂停账号；分组显式规则仍按状态码、窗口和阈值工作。
- 设置页移除不再生效的全局“529 过载冷却”开关；账号自定义错误码、认证失效、配额/模型限制及手动暂停仍保持原逻辑。
- 本轮无数据库迁移、无 PostgreSQL/Redis/网络/数据卷重建。

## 验证结果

- 后端定向 Go 服务测试通过，覆盖分组失败规则、普通 `529`、SSE/流式过载去除全局停号及错误策略。
- 前端 `AccountStatusIndicator` 与 `AccountsView.lite` 共 18 项 Vitest 通过；`vue-tsc --noEmit`、受影响文件 ESLint、生产构建和 `git diff --check` 通过。
- 完整 service 单测仍有既有 `gateway_failure_attempt_*` 基线失败；该失败与本轮改动无关，未将完整套件描述为全绿。
- GHCR 镜像由本地 Buildx 以 `linux/amd64` 构建并推送，生产拉回的 image ID 与上述 manifest digest 一致。GitHub Actions 本轮没有可用运行记录，不将本地构建误报为 Actions 通过。

## 生产部署

部署顺序为主站 → L1 → COS，仅重建应用服务：

| 站点 | Compose 服务 | 结果 | 健康检查 | 重启次数 |
| --- | --- | --- | --- | --- |
| 主站 `ai.moshu.cloud` | `sub2api` / `1Panel-sub2api-bjGj` | 已更新 | `{"status":"ok"}` | 0 |
| L1 `api.moshu.cloud` | `api` / `api-L1` | 已更新 | `{"status":"ok"}` | 0 |
| COS `cos.moshu.cloud` | `api` / `api-COS` | 已更新 | `{"status":"ok"}` | 0 |

三站应用容器均为 healthy，Compose 配置中的镜像 digest 一致。PostgreSQL、Redis、网络、卷和现有反代配置未重建；最近 5 分钟应用日志未发现 `panic`、`fatal`、`migration failed`、数据库或 Redis 连接异常。

## 备份与回滚

发布前备份目录为：

`/opt/sub2api-backups/v0.2.16-custom.11-pre-20261016T092300Z`

其中保存三站 Compose、环境文件副本（如存在）、容器状态和旧镜像信息。回滚只需把三站应用镜像恢复到发布前记录的旧 digest `sha256:920325bd1400a617180fe3b8c4f404ba709527c6101ec852b2bfae2e8d083128`，然后按站点执行 `docker compose up -d --no-deps`；本轮没有数据库迁移，不需要回退数据库。

本轮按用户要求完成快速健康与日志检查，不做长时间稳定性观察。
