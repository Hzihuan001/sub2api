# Moshu `0.2.16-custom.9` 发布记录

## 摘要

本次发布将分组切换规则改为可编辑的 HTTP 状态码规则列表。每条规则可以填写多个状态码、累计次数、统计窗口和暂停时长；同一条规则中的状态码分别计数，重复状态码会被拦截，并继续兼容历史四类错误配置。

## 发布坐标

| 项目 | 值 |
| --- | --- |
| 官方基线 | `v0.2.16` |
| 版本 | `0.2.16-custom.9` |
| 提交 | `51732a2d93ebcebf2dde5e127e305da5273c0556` |
| 标签 | `custom-0.2.16.9` |
| GHCR 镜像 | `ghcr.io/hzihuan001/sub2api:0.2.16-custom.9` |
| 镜像 digest | `sha256:26a27d9793baf2bf074d73efc57b76ddc1c65852489b84578bd854cac0640db3` |

## 自动化与本地门禁

- 后端受影响包全量测试：`go test -tags=unit -count=1 ./internal/service ./internal/handler/admin`。
- 前端状态码规则、组件和分组入口共 30 项 Vitest 通过；`pnpm typecheck`、定向 ESLint、生产构建、`scripts/custom_contracts.py` 和 `git diff --check` 通过。
- GitHub Actions 因仓库级禁用未运行；本次使用本地 Docker 构建、推送并以不可变 digest 发布，未将 Actions 误报为通过。

## 生产部署与验收

- 部署顺序：主站 `ai.moshu.cloud` → L1 `api.moshu.cloud` → COS `cos.moshu.cloud`。
- 三站均只重建应用容器，PostgreSQL、Redis、网络、数据卷、Compose 其他服务和 OpenResty 配置保持不变。
- 三站容器均 healthy、restart 0；本地和公网 `/health` 均返回 `{"status":"ok"}`；OpenResty 配置检查通过。
- 最近容器日志未发现 `panic`、`fatal`、迁移失败、数据库错误或 Redis 错误。
- 发布前备份目录：`/opt/sub2api-backups/v0.2.16-custom.9-20261016T014500Z`。回退目标为上一镜像 `0.2.16-custom.8`，本轮未触发回滚。

## 后续维护

后续更新应先读取 `CUSTOM_PROJECT_STATUS.md`、本记录、`UPSTREAM_SYNC.md` 和 `custom/manifest.yml`。状态码规则改动继续保持在 gateway-resilience 模块及其适配层内，避免把定制逻辑散落到主线路由和计费核心。
