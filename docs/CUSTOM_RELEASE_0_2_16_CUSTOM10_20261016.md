# Moshu 0.2.16-custom.10 发布记录

日期：2026-10-16（Asia/Shanghai）。本次发布修复分组状态码切换规则的共享计数、账号暂停状态展示及删除规则后的运行时清理。

## 发布坐标

| 项目 | 值 |
| --- | --- |
| 源码提交 | `dbe323f47` |
| 源码分支 | `codex/group-failover-rules-v0.2.16` |
| 源码标签 | `custom-0.2.16.10` |
| 版本 | `0.2.16-custom.10` |
| 镜像 | `ghcr.io/hzihuan001/sub2api:0.2.16-custom.10` |
| 不可变 digest | `sha256:920325bd1400a617180fe3b8c4f404ba709527c6101ec852b2bfae2e8d083128` |
| 架构 | `linux/amd64` |

## 本次内容

- Redis 共享计数器支持并严格校验 `status_100`–`status_599`，`502`、`503` 等状态码保持独立滚动计数。
- 分组规则达到阈值后按账号暂停调度；账号列表显示“暂停调度”和恢复入口。
- 删除分组最后一条规则后清理该分组账号的规则计数及过载冷却，不清理独立限流或模型限额。
- 管理员手动恢复同时清除本地计数、Redis 计数和暂停冷却。
- 成功 failover 的中间错误继续按原有日志逻辑处理，不进入最终用户错误列表。

## 验证

- 后端分组规则/运行时测试通过。
- Redis 状态码计数器测试通过。
- 前端相关 Vitest 34 项、类型检查和 `git diff --check` 通过。
- 镜像本地回拉确认 digest、`amd64/linux` 与发布坐标一致。
- 三份 Compose 配置校验通过。

## 生产部署

部署顺序为主站 → L1 → COS，仅重建应用容器，未重建 PostgreSQL、Redis、网络或数据卷。

| 站点 | 容器 | 结果 |
| --- | --- | --- |
| 主站 `ai.moshu.cloud` | `1Panel-sub2api-bjGj` | healthy，restart 0，`/health` 200 |
| L1 `api.moshu.cloud` | `api-L1` | healthy，restart 0，`/health` 200 |
| COS `cos.moshu.cloud` | `api-COS` | healthy，restart 0，`/health` 200 |

三站容器配置镜像引用和实际 image digest 均为上述 digest；最近 5 分钟日志未发现 panic、fatal、migration failed、database error 或 redis error。

部署前快照：`/opt/sub2api-backups/v0.2.16-custom.10-pre-20261016T060000Z`，包含三份 Compose、容器清单和旧镜像坐标校验。

本次为快速发布验收，未进行长时间稳定性观察；如需回滚，只恢复三份 Compose 中的旧 digest 并按同样的 `up -d --no-deps` 逐站重建应用容器。
