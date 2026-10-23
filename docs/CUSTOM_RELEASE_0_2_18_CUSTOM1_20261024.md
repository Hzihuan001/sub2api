# v0.2.18-custom.1 合并验收记录

日期：2026-10-24  
状态：代码已合并，待后端/Docker 验收；未推送、未打发布标签、未部署

## 版本与基线

- 官方 tag：`v0.2.18` / `ac1b8f1271be0d674fa6f074403c1410f30a3a81`
- 定制基线：`0.2.16-custom.16` / `4356fafb7707ab0334338a56ef4ff02e004164db`
- 合并提交：`885edb605702a4af52b0bb8ac0060b58698c872d`
- 路由语义修复：`a688883f1bc09fbce2c38a7253be7e1577793678`
- 候选版本：`0.2.18-custom.1`

## 合并处理

真实三方 merge 探针发现 16 组语义冲突，涉及版本、README、Chat/Responses/Gemini 网关、账号/分组管理、计费测试、OpenAI 转发、定价服务和前端管理页。冲突均按定制功能清单人工处理；没有用整文件 ours/theirs 静默覆盖。

官方 v0.2.18 的配额与通知、Gemini/Antigravity 图像分组门禁、流断开结算收口、Cloudflare 1010 分类、限流并发安全、Gemini 工具兼容、DeepSeek 图片载荷修复、图片下载 SSRF 防护及 Grok 4.7 Fast 定价修复已保留。Chat/Responses 的 Key 路由价格、提示词注入、分组上下文、账号级故障切换和备用分组接线已复核；合并后发现的选择失败语义回退已在 `a688883f1` 修复。

## 检查结果

通过：

- `frontend/src/features/key-routing`：9 个测试文件，94 项通过
- 前端 typecheck、生产 build
- custom isolation / append-only 检查
- architecture-negative：34/34
- custom contracts：18/18
- upstream sync：5/5
- upstream workflow：11/11
- `git diff --check`

未执行：本机当前没有可用 Go，Docker Desktop daemon 也未运行，因此未执行 `go test ./...`、真实 PostgreSQL/Redis 集成、迁移回退演练及隔离 Docker Compose。未执行项是发布门槛，不应解释为后端已通过。

## 迁移与回滚

官方新增 `246_enable_gemini_image_generation_groups.sql`；已发布 `9000–9004` 定制迁移保持原名和原内容。官方和历史定制存在重复数字前缀，运行器按完整文件名排序，发布前必须在 PostgreSQL 环境确认升级、重启和回退，不得修改生产校验值。实验性未跟踪目录 `backend/internal/keyrouting/replay/` 不属于本候选。

## 发布决定

生产三站仍为 `0.2.16-custom.16`，镜像 digest `sha256:037d353afff1a4e18c88ff9ad59fad6735195465a1dbb60ab5ace7c5d33223e7`。本记录只说明代码合并，不代表镜像可上线。下一步：恢复 Go/Docker → 执行后端及数据库/Redis/Compose 验收 → 构建并拉回不可变镜像复验 → 再由用户明确批准推送、打标签和部署。
