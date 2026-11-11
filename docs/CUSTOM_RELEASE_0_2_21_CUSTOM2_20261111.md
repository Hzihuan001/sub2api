# 0.2.21-custom.2 生产发布记录（2026-11-11）

## 结果

`0.2.21-custom.2` 已按主站 → L1 → COS 顺序完成生产发布。三站最终运行同一不可变镜像，应用容器 healthy、restart 0，数据库、Redis、业务卷、网络和 OpenResty 未重建。

本次补丁没有改生图工作台交互逻辑；它补齐了生产数据配置缺口：迁移 `9009_custom_gpt_image_26_group_models.sql` 幂等地把 `gpt-image-2.6-flare` 和 `gpt-image-2.6-sunburst` 加入已有 OpenAI 生图组白名单，保留原有模型。此前 `.1` 镜像代码已支持 2.6，但主站 group 8 的白名单仍只有 2.5 与 `gpt-image-2`，因此工作台下拉框看不到 2.6。

## 制品与门禁

| 项目 | 结果 |
| --- | --- |
| 源码提交 | `dff2cb433fe219c60686a9dad3460c4e5a6cdba6` |
| 版本 | `0.2.21-custom.2` |
| GHCR digest | `ghcr.io/hzihuan001/sub2api@sha256:5d24cc7b12bb8da8ebc657a29a9e53041083b6d270e7e2b5075670f56430cd40` |
| 构建元数据 | `linux/amd64`；内置 commit 与版本一致；`pg_dump`/`psql` 保留 |
| 精确 digest operator 验收 | 通过；项目 `sub2api-operator-test-custom212-20261111`、端口 `18092`，资源已清理 |
| 已有图片工作台验收 | 沿用本会话此前已通过的前端/后端图片相关回归；按用户要求没有重复生图对话验收 |

`.1` digest `sha256:b48cb894accb0b59750c95c3404c63f1249ce82d66835e0491f23361292e46f4` 曾只更新主站应用容器，随后因发现生产组白名单缺少 2.6 被 `.2` supersede；L1/COS 未部署 `.1`。

## 备份与逐站发布

部署前备份为 `/opt/sub2api-deploy-backups/custom211-20261111T072120Z/`，覆盖三站 PostgreSQL custom-format dump、`pg_restore --list`（含 `TABLE DATA`）、应用数据、Compose/环境文件以及 OpenResty 配置；SHA-256 manifest 全部通过，备份目录约 271 MB。该备份不是完整恢复演练。

| 站点 | UTC 更新窗口 | 应用结果 |
| --- | --- | --- |
| 主站 `ai.moshu.cloud` | `08:05:00Z` → `08:05:16Z` | 旧 `.1` 容器替换为 `.2`；healthy、restart 0、摘要一致 |
| L1 `api.tokenpulse.top` | `08:07:04Z` → `08:07:17Z` | 旧 `.1` 容器替换为 `.2`；healthy、restart 0、摘要一致 |
| COS `cos.tokenpulse.top` | `08:08:13Z` → `08:08:21Z` | 旧 `.20.2` 容器替换为 `.2`；healthy、restart 0、摘要一致 |

三站应用容器更新均使用 `docker compose up -d --no-deps --pull never`，只重建应用服务；各站 PostgreSQL/Redis 容器 ID 保持不变，启动关键错误匹配为 0，内部 `/health` 均返回 `{"status":"ok"}`。

迁移后只读数据库证据显示主站 group 8 `GPT-image-超分`、L1 group 9 `GPT-image2-生图`、COS group 9 `GPT-image-生图` 均包含两个 2.6 模型；其他启用图片生成的 OpenAI 组也已幂等补齐。

## 页面检查与边界

最终公网检查在四个域名（主站、L1 主域、L1 备用域 `api.moshu.cloud`、COS）分别访问 `/health`、`/keys`、`/image-studio`、`/docs/tools/codex`、`/pelican-showcase`，共 20 个请求全部 HTTP 200。未发起生产付费模型调用、故障注入或浏览器登录后真实生成；下拉框读取的生产白名单已通过数据库复核，用户刷新页面后应能看到 2.6 模型。

## 回滚

应用回滚目标为上一已验证镜像 `.20.2`：`sha256:6d89fe835bee13987c48a60406f93b270046a4be4fa35d1813efda622f234b76`。回滚只替换应用镜像并保留迁移 `9009`、当前数据库和业务数据；不得用旧数据库覆盖新数据。若必须移除 2.6，先通过审核后的组白名单变更处理，不回滚数据库。
