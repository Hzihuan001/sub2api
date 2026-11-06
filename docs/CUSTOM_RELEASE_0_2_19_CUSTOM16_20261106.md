# 0.2.19-custom.16 发布记录（2026-11-06）

## 结果

本次发布完成“本站实际测试结果”修复和默认提示词更新。默认提示词精确为：

> 创建一个HTML，内容是SVG绘制一个鹈鹕骑自行车的2D动画，你不需要任何测试。

其中“不需要任何测试”是发送给模型的题目文本；软件发布仍执行了源码、镜像、容器和生产健康检查。

定时渠道测智计划现在可以保存并选择 `default`、`low`、`medium`、`high`、`xhigh` 推理强度。请求映射会根据上游和模型能力安全处理，不支持的模型继续使用上游默认强度。迁移 `9007_custom_scheduled_test_thinking_effort.sql` 为加法迁移，已有计划兼容。

本站失败诊断补充了上游 523/524、502/503、读流 reset/EOF 等类别；Responses 兼容流在客户端断开后继续消费上游以完成 usage 结算；无首事件的安全 failover、OpenAI `response.incomplete` 和 Claude 缺失 `message_stop` 均保留可读失败原因，不再误标成功。

## 发布坐标

- 源分支：`codex/v0.2.19-merge-20261029`
- 源码提交：`8dd0cb3c135d53162e7adc26ef1ea37238ccf703`
- Git tag：`custom-0.2.19.16`
- 镜像：`ghcr.io/hzihuan001/sub2api@sha256:a3f3a1e9cc5d51cfe262407faf3e32197a27b4b3f227eaaf39a4389d294aff30`
- 生产回滚应用基线：`.15`，`sha256:c949104a7e9a0393d73c8b2c17b5ab0297926399dfae979724975932c06b7af1`
- 备份：`/opt/sub2api-deploy-backups/custom16-20261106T044446Z/`

Git tag 和提交已推送到 `origin`。Actions 没有为该 tag 产生新的运行记录，因此本次使用已授权的本地构建 → GHCR 推送 → 按 digest 拉回验收路径；这不宣称 CI 通过。

## 验证

- Go 定向服务测试、流式回归测试、`go build ./...`：通过。
- 前端定向测智结果组件测试：8/8 通过；此前完整前端 Vitest 为 3,547 通过、50 个失败，失败集中在未改动的 `EditAccountModal.grokUpstream.spec.ts`，不写成全量通过。
- 服务全量单元套件复跑：18,753 通过、12 个失败。12 个失败均为既有定价/搜索/图片计费/调度 fixture 或预期与当前产品行为不一致的项目，未归因于本次测智改动；图片计费两项仍保留为后续独立问题，未在本发布中扩大范围。
- `python scripts/custom_contracts.py`：8 个模块、16 个可执行测试组通过。
- 本地 linux/amd64 发布镜像按不可变 digest 拉回；版本、提交、架构、`pg_dump`/`psql` 均核对通过。
- 已授权 operator 容器验收通过；测试容器、卷和网络已清理。
- 备份 custom-format dump、`pg_restore --list`、应用数据/Compose/env/反代归档及 SHA-256 校验通过。

没有调用真实模型、付费生产接口、故障注入或额外生产 SSE 压测。

## 生产发布

严格按主站 → L1 → COS 顺序，仅重建应用容器，保留 PostgreSQL、Redis、业务卷、网络、环境和反代配置。每站更新前都重新检查自然排空；有效账号/用户槽、实时租约、WebSocket ingress、等待队列和 Redis `image_task:*` 处理任务均为 0。历史 API Key 统计槽位按有效时间窗过滤，不作为活动请求。

| 站点 | 应用容器 | UTC 更新窗口 | 迁移数 | 结果 |
| --- | --- | --- | ---: | --- |
| 主站 | `1Panel-sub2api-bjGj` | 05:59:27–05:59:45 | 333 → 334 | healthy，restart 0 |
| L1 | `api-L1` | 06:07:16–06:07:25 | 342 → 343 | healthy，restart 0 |
| COS | `api-COS` | 06:08:21–06:08:30 | 342 → 343 | healthy，restart 0 |

三站运行版本均为 `0.2.19-custom.16`，提交和镜像 digest 完全一致；三站 9007 均已应用，近启动日志没有 panic、fatal error 或 migration failure。公开健康检查返回 200：`ai.moshu.cloud/health`、`api.moshu.cloud/health`、`cos.moshu.cloud/health`。

发布过程中主站第一次门禁因把 Redis 中过期历史槽位按 `ZCARD` 统计而安全阻断，未修改容器；改为按 Redis 有效时间窗复查为 0 后继续。L1 第一次脚本在临时文件写入前因 `os.close` 参数错误退出，未修改 Compose 或容器；修正后重新执行并成功。

## 回滚与观察

如需回滚，只回滚发生问题站点的应用 Compose image 到上述 `.15` digest，保留 9007 和现有数据，不恢复旧数据库覆盖新迁移。继续观察三站健康状态、restart、启动关键日志和测智失败原因；生产观察不等同于真实模型付费测试。
