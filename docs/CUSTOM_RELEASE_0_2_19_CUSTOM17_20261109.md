# 0.2.19-custom.17 发布记录（2026-11-09）

## 变更

本版处理 `.16` 测智结果中“模型声称已完成，但实际只回复没有文件工具”的问题。

- 每次测智本来就是独立 API 请求；Claude 测试请求会生成新的 session UUID。默认题补充要求模型把完整 HTML/SVG 源码直接写在回复中，不查找或沿用目录/文件，不声称创建了本地文件。
- 空默认题、旧英文默认题和 `.16` 中文默认题都会升级为新指令。默认题回复只有包含完整 HTML 标签、`<svg>` 和闭合标签才计为成功；自然语言解释保留在 `response_text` 中并改标失败，不会触发成功后的账号自动恢复。
- 公开结果卡对旧的无源码成功记录显示“未交付源码”，不再显示“已完成”，组状态也会显示失败。
- Pelican 测智结果按应用配置时区每日 00:05 清理当天零点以前的记录；进程启动时会补清理当天零点以前漏掉的结果。清理只删 Pelican 结果，不删计划或普通连接测试结果。

## 源码和镜像

- 分支：`codex/v0.2.19-merge-20261029`
- 运行时源码提交：`051925f844eb3ec8dfd742bce70a04c145b59514`
- tag：`custom-0.2.19.17`
- 镜像：`ghcr.io/hzihuan001/sub2api@sha256:167ef6a933f2e36a25d26678050be4e4d3bffab8c27588cb7287b2cf5d894b43`
- 镜像元数据和程序内版本：`0.2.19-custom.17`，提交 hash 与源码一致，`linux/amd64`。
- 前一版生产镜像 `.16`：`sha256:a3f3a1e9cc5d51cfe262407faf3e32197a27b4b3f227eaaf39a4389d294aff30`。

提交和 tag 已推送，GHCR 镜像已按 digest 拉回。Actions 没有出现这次 tag 的新运行，因此不宣称 CI 通过。第一次推送的候选缺少 OCI version/revision labels，未用于生产；最终 digest 已补齐 labels 并重新拉回验收。

## 验证

- Go：`go test ./internal/service ./internal/repository -run 'Test(DefaultPelicanTestPrompt|NormalizePelicanTestPrompt|ValidatePelicanTestResult|PelicanResultsCleanupCutoff|ScheduledTestRunner.*Pelican)' -count=1` 通过；服务测试通过，仓储包编译通过（此选择器在仓储包没有直接匹配用例）。
- 前端：渠道测智展示回归 3/3、TypeScript 检查、目标文件 ESLint 和 Vite 生产构建通过。
- 最终 GHCR digest 容器验收通过；容器、网络和测试卷已清理。镜像内 `pg_dump`/`psql` 为 18.6。
- 没有真实模型请求、付费生产请求或额外生产 SSE 压测。

## 生产状态

用户在发布包验收后要求“暂不更新生产”。因此三站均保持 `.16`，没有替换 Compose 镜像、重启应用、运行生产清理或修改代理配置。主站 → L1 → COS 的应用发布尚未执行；恢复时仍需按顺序重新做自然排空检查。

更新前的只读排空检查显示 L1/COS 活动账号和用户槽为 0，主站仍持续出现活动请求槽（按 30 分钟槽位有效期统计，约 1–4 个）；等待队列、异步图片处理任务和批量图片运行任务均为空。没有为发布强制中断请求。

更新前三站备份已验证，位置：`/opt/sub2api-deploy-backups/custom17-20261109T034556Z/`。PostgreSQL custom dump 的 `pg_restore --list`、app-data/Compose-env/proxy 归档读取和 SHA-256 校验通过。数据库 dump 大小分别为主站 201,448,800、L1 27,744,942、COS 3,069,020 字节；restore list 行数为 1403、1448、1448。备份不是完整恢复演练。

恢复生产部署前应重新确认当前版本仍不高于 `.16`、备份有效、当前应用 digest 仍为 `.16`，并在每站变更前重做请求/队列/任务排空检查。用户后续通知前不执行生产更新。没有数据库 schema migration；首次启动 `.17` 会按约定清理当天零点以前的 Pelican 结果。
