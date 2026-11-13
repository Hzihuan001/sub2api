# 0.2.21-custom.4 生产发布记录（2026-11-13）

## 结果

`0.2.21-custom.4` 已按主站 → L1 → COS 顺序完成生产发布。三站最终运行同一不可变 linux/amd64 镜像，应用容器均 healthy、restart 0；PostgreSQL、Redis、业务卷、网络和 OpenResty 配置未重建。数据库迁移账本保持不变。

本次发布把图片任务“结果可找回”改为自动行为：提交已成功但结果下载遇到连接重置、EOF、HTTP/2 断流等瞬时传输错误时，前端自动继续查询同一任务并按 3、6、12、24、48 秒退避（上限 60 秒）重试下载；只使用原任务 GET，不重复 POST、不重复扣费。任务元数据和待处理状态保留在浏览器，刷新或组件卸载后可继续恢复；用户停止等待只停止当前等待，不删除任务。界面提示改为自动恢复中，用户无需手动查询。

## 制品与源码门禁

| 项目 | 结果 |
| --- | --- |
| 源码提交 | `548f4360c908a3f2b5ba18cbd47394ec6b68eaa5` |
| 版本 / tag | `0.2.21-custom.4` / `custom-0.2.21.4` |
| 分支 | `upgrade/upstream-v0.2.20-20261109` |
| GHCR digest | `ghcr.io/hzihuan001/sub2api@sha256:2f0aa58a7091303d652aa7529cdccdf692a13309669bba2f4e4908a181c4d8b8` |
| 构建元数据 | `linux/amd64`；版本、完整 commit 和 OCI revision 一致；`pg_dump`/`psql` 18.6 |
| 前端门禁 | Vitest 442 文件 / 3749 测试通过；typecheck、变更文件 ESLint、build 通过 |
| 精确 digest operator 验收 | 通过；项目 `sub2api-operator-test-custom214-20261113`、端口 `18199`，测试资源已清理 |

本次代码变更集中在图片任务自动恢复和前端状态展示，后端接口/数据库结构未新增迁移。构建时显式传入完整 commit，最终镜像内置版本与 commit 已在远端再次核对。

## 备份与逐站发布

部署前备份为 `/opt/sub2api-backups/v0.2.21-custom.4-20261113T104618Z/`，三站 PostgreSQL dump、`pg_restore --list`、应用数据、Compose/环境文件、OpenResty 配置归档和 SHA-256 manifest 均通过。

| 站点 | UTC 更新窗口 | 应用结果 |
| --- | --- | --- |
| 主站 `ai.moshu.cloud` | `2026-11-13T12:55:22Z` → `2026-11-13T12:55:32.375217Z` | `.3` → `.4`；healthy、restart 0、摘要一致 |
| L1 `api.tokenpulse.top` | `2026-11-13T12:59:18Z` → `2026-11-13T12:59:42.592375Z` | `.3` → `.4`；healthy、restart 0、摘要一致 |
| COS `cos.tokenpulse.top` | `2026-11-13T13:00:37Z` → `2026-11-13T13:00:48.423345Z` | `.3` → `.4`；healthy、restart 0、摘要一致 |

每站只执行 `docker compose up -d --no-deps --pull never` 更新应用服务。自然排空期间主站、L1、COS 使用了有界、可恢复的新推理入口 503 闸门，已有请求未被强制取消；发布完成后原 OpenResty 文件字节恢复，`nginx -t` 和 reload 通过，临时 map 已不存在。PostgreSQL/Redis 容器身份、启动时间、配置指纹和数据卷保持不变。生产迁移账本仍为主站 339、L1/COS 各 348，本次新增迁移为空。

## 页面与接口检查

四个公网地址（`ai.moshu.cloud`、`api.tokenpulse.top`、备用域名 `api.moshu.cloud`、`cos.tokenpulse.top`）分别访问 `/health`、`/keys`、`/image-studio`、`/docs/tools/codex`、`/pelican-showcase`，共 20 个请求全部 HTTP 200。三站发布脚本的 local/public 页面检查全部 200；匿名访问管理设置、故障策略和 Key API 均返回预期 401；三站精确 digest verify 全部通过。

没有发起生产付费模型调用、真实生图、故障注入或浏览器登录后交互验收；此前已通过的生图工作台/生图对话验收按用户要求没有重复执行。

## 回滚

应用回滚目标为上一已验证镜像 `.3`：`ghcr.io/hzihuan001/sub2api@sha256:a3aab14ed40f643babfb020c5f71caf431ec00aa8d5c9ca82b005a1a4a81c358`。回滚只替换应用镜像并保留当前数据库、业务数据和迁移账本；不得用旧数据库覆盖新数据。运行 tag `custom-0.2.21.4` 固定在源码提交 `548f4360c908a3f2b5ba18cbd47394ec6b68eaa5`，文档收尾另作独立提交，不移动运行 tag。