# 0.2.21-custom.3 生产发布记录（2026-11-13）

## 结果

`0.2.21-custom.3` 已按主站 → L1 → COS 顺序完成生产发布。三站最终运行同一不可变镜像，应用容器均 healthy、restart 0；PostgreSQL、Redis、业务卷、网络和 OpenResty 配置未重建或修改。

本次版本收紧了默认故障切换的触发范围：新建的 Key 路由故障规则只自动启用“连续失败”，默认阈值为 6 次、冷却 60 秒；状态码、TTFT、SLA、超时和限流规则仅在用户显式添加后生效，默认窗口/采样和冷却值同步放宽到 60 秒冷却。已有已保存策略保持不变。此前已实现的多分组故障轮询、可用即返回前序分组和冷却中的重试逻辑随本次镜像继续保留。

## 制品与源码门禁

| 项目 | 结果 |
| --- | --- |
| 源码提交 | `6e72928b09967df5d92bb547758bce0e99ff5227` |
| 版本 / tag | `0.2.21-custom.3` / `custom-0.2.21.3` |
| 分支 | `upgrade/upstream-v0.2.20-20261109` |
| GHCR digest | `ghcr.io/hzihuan001/sub2api@sha256:a3aab14ed40f643babfb020c5f71caf431ec00aa8d5c9ca82b005a1a4a81c358` |
| 构建元数据 | `linux/amd64`；内置版本、完整 commit 和 OCI revision 一致；`pg_dump`/`psql` 18.6 |
| 前端门禁 | Vitest 442 文件 / 3748 测试通过；typecheck、变更文件 ESLint、build 通过 |
| 后端门禁 | 精确源码归档在远程 Linux 主机执行 `go test ./internal/service ./internal/server/routes -count=1 -timeout=300s` 和 `go vet ./internal/service ./internal/server/routes`，均通过 |
| 精确 digest operator 验收 | 通过；项目 `sub2api-operator-test-custom213-20261113`、端口 `18198`，资源已清理 |

构建、拉回和 operator 验收均针对上表完整 commit 与最终 digest；没有把测试用的短 commit 镜像部署到生产。前端 build 只有仓库已有的 Browserslist 数据和大 chunk 提示，没有新增失败。

## 备份与逐站发布

部署前备份为 `/opt/sub2api-backups/v0.2.21-custom.3-20261112T165449Z/`，三站 PostgreSQL dump、`pg_restore --list`、应用数据、Compose/环境文件、OpenResty 配置归档和 SHA-256 manifest 均通过。备份验证通过但未做完整恢复演练。

| 站点 | UTC 更新窗口 | 应用结果 |
| --- | --- | --- |
| 主站 `ai.moshu.cloud` | `2026-11-12T16:56:31Z` → `16:56:40.720889Z` | 旧 `.2` 容器替换为 `.3`；healthy、restart 0、摘要一致 |
| L1 `api.tokenpulse.top` | `2026-11-12T16:58:25Z` → `16:58:51.692509Z` | 旧 `.2` 容器替换为 `.3`；healthy、restart 0、摘要一致 |
| COS `cos.tokenpulse.top` | `2026-11-12T16:59:15Z` → `16:59:27.132481Z` | 旧 `.2` 容器替换为 `.3`；healthy、restart 0、摘要一致 |

三站均使用 `docker compose up -d --no-deps --pull never` 只更新应用服务。最终容器分别为 `1Panel-sub2api-bjGj`、`api-L1`、`api-COS`，均指向上表 digest。生产迁移账本保持不变：主站 339、L1/COS 各 348；本次新增迁移为空，PostgreSQL/Redis 容器身份和数据卷未变。

## 页面与接口检查

四个公网地址（`ai.moshu.cloud`、`api.tokenpulse.top`、备用域名 `api.moshu.cloud`、`cos.tokenpulse.top`）分别访问 `/health`、`/keys`、`/image-studio`、`/docs/tools/codex`、`/pelican-showcase`，共 20 个请求全部 HTTP 200。各站匿名访问管理设置、故障策略和 Key API 返回预期 401；最终健康检查、版本/commit、重启次数、迁移账本和关键启动日志均通过。

没有发起生产付费模型调用、真实生图、故障注入或浏览器登录后交互验收；此前已通过的生图工作台/生图对话验收按用户要求没有重复执行。

## 回滚

应用回滚目标为上一已验证镜像 `.2`：`sha256:5d24cc7b12bb8da8ebc657a29a9e53041083b6d270e7e2b5075670f56430cd40`。回滚只替换应用镜像并保留当前数据库、业务数据和迁移账本；不得用旧数据库覆盖新数据。运行 tag `custom-0.2.21.3` 固定在源码提交 `6e72928b09967df5d92bb547758bce0e99ff5227`，文档收尾另行提交，不移动运行 tag。
