# v0.2.18-custom.1 合并验收记录

日期：2026-10-24
状态：代码已合并，本地 Docker/数据库验收通过；源码、发布标签和 GHCR 镜像已推送，三站生产已按主站→L1→COS 顺序部署并验收

## 版本与基线

- 官方 tag：`v0.2.18` / `ac1b8f1271be0d674fa6f074403c1410f30a3a81`
- 定制基线：`0.2.16-custom.16` / `4356fafb7707ab0334338a56ef4ff02e004164db`
- 合并提交：`885edb605702a4af52b0bb8ac0060b58698c872d`
- 路由语义修复：`a688883f1bc09fbce2c38a7253be7e1577793678`
- 流桥接/Gemini 推理计费修复：`305afd7f9`
- 功能清单测试合同修复：`8b5726b0c`
- 发布源提交：`fc0d798bf2d6a08a074970d0485c567291e02660`；当前分支后续仅追加文档收尾提交，不改变发布标签和镜像。
- 候选版本：`0.2.18-custom.1`
- 发布标签：`custom-0.2.18.1`
- GHCR 镜像：`ghcr.io/hzihuan001/sub2api:0.2.18-custom.1`
- 不可变 digest：`sha256:d7f365d1ba40b6c159d4155f7fbc2773d01f0fe6015af7906ebbece500ba1837`

## 合并处理

真实三方 merge 探针发现 16 组语义冲突，涉及版本、README、Chat/Responses/Gemini 网关、账号/分组管理、计费测试、OpenAI 转发、定价服务和前端管理页。冲突均按定制功能清单人工处理；没有用整文件 ours/theirs 静默覆盖。

官方 v0.2.18 的配额与通知、Gemini/Antigravity 图像分组门禁、流断开结算收口、Cloudflare 1010 分类、限流并发安全、Gemini 工具兼容、DeepSeek 图片载荷修复、图片下载 SSRF 防护及 Grok 4.7 Fast 定价修复已保留。Chat/Responses 的 Key 路由价格、提示词注入、分组上下文、账号级故障切换和备用分组接线已复核；合并后发现的选择失败语义回退已在 `a688883f1` 修复。另修复流桥接迁移残留的旧 `scanner` 引用、Kiro 错误传递、Gemini Chat 实际 thinking 档位计费记录，以及官方新增构造器参数造成的测试调用缺口。

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
- Docker Go 1.27.3：`go test ./internal/... -count=1 -timeout=900s` 全部通过；其中 service、handler、middleware、key-routing 和 repository 回归均通过。

## 隔离 Docker 与数据库验收

- 构建镜像：`sub2api:0.2.18-custom.1-acceptance`，镜像 ID `sha256:7a3eaee680b7708628cd1f8565818d22e830b836676b35d099f640e3397b0dbc`；容器内版本输出为 `Sub2API 0.2.18-custom.1`。
- Compose 项目：`moshu-reseller-acceptance-v018c1`；主站/L1/COS 分别使用 `18280/18281/18282`，独立 PostgreSQL 18、Redis 8 数据卷和网络，配置校验通过。
- 三站最终状态：应用、PostgreSQL、Redis 均 healthy；三个 `/health` 均返回 HTTP 200 `{"status":"ok"}`；三套管理员登录均返回 HTTP 200；三套 Redis 均返回 `PONG`。
- 迁移：三站 `schema_migrations` 各 329 条，最新定制迁移为 `9004_custom_key_route_site.sql`；官方 246 与定制 9000–9004 迁移保持原文件名，迁移并发锁、幂等性、定制 Key 路由/站点命名空间和批量生图不折扣迁移测试全部通过。
- 持久化：重启三站应用后仍 healthy；主站合成管理员用户数量重启前后均为 1；重启后 45 秒内无新的错误、panic 或连接失败日志。
- 发布产物回拉：按上述 digest 从 GHCR 回拉后 image ID 仍为 `sha256:d7f365d1ba40b6c159d4155f7fbc2773d01f0fe6015af7906ebbece500ba1837`，内置版本为 `0.2.18-custom.1`、提交为 `fc0d798bf2d6a08a074970d0485c567291e02660`，架构为 `amd64`；`pg_dump`/`psql` 18.6 存在。
- 启动观察：首次并行 `up` 时主站和 COS 各出现一次 PostgreSQL 就绪竞态并退出，依赖健康后重新启动即恢复；重启和最终健康检查均通过。该现象属于验收 Compose 首次启动时序，不是代码构建或迁移失败；生产发布前应按“依赖 healthy 后启动应用”的顺序执行。

以上完成了本地 PostgreSQL/Redis/Compose 门禁；本轮未做生产付费模型调用、故障注入或数据库回退演练。

## 生产发布结果（2026-10-24）

- 发布前只读排空通过：主站/L1/COS 均无活动并发槽、等待队列或 `image_task:*` 在途任务；主站的常驻调度器索引键不计为在途请求。
- 发布前备份目录：`/opt/sub2api-backups/v0.2.18-custom.1-20261024T000000Z`。包含三站环境/Compose、应用数据、PostgreSQL custom-format dump、恢复清单和反代配置；SHA256、gzip 完整性和 dump 的 `TABLE DATA` 清单均已核验通过。
- 主站约 `2026-10-24T00:13:04Z`、L1 约 `00:13:59Z`、COS 约 `00:14:53Z` 依次仅重建应用容器；PostgreSQL、Redis、卷、网络和反代均未重建。
- 三站当前统一运行 `0.2.18-custom.1`，镜像均为 `ghcr.io/hzihuan001/sub2api@sha256:d7f365d1ba40b6c159d4155f7fbc2773d01f0fe6015af7906ebbece500ba1837`；应用容器 healthy、restart 0。
- 内网与公网健康检查均通过：`https://ai.moshu.cloud/health`、`https://api.moshu.cloud/health`、`https://cos.moshu.cloud/health` 全部 HTTP 200 `{"status":"ok"}`；部署后关键启动错误匹配为 0。
- 生产 `schema_migrations`：主站 329 条、L1/COS 各 338 条；三站均保留定制 `9000–9004`，并在启动时幂等应用官方 `246_enable_gemini_image_generation_groups.sql`。生产数据服务启动时间未因本轮应用更新改变。
- 本轮没有调用付费模型，不据此宣称所有 provider、长连接或生产容量已完成长期验收。应用回退目标为 `.16` digest `sha256:037d353afff1a4e18c88ff9ad59fad6735195465a1dbb60ab5ace7c5d33223e7`；回退只更换应用镜像，不覆盖上线后的数据库。

## 迁移与回滚

官方新增 `246_enable_gemini_image_generation_groups.sql`；已发布 `9000–9004` 定制迁移保持原名和原内容。官方和历史定制存在重复数字前缀，运行器按完整文件名排序，发布前必须在 PostgreSQL 环境确认升级、重启和回退，不得修改生产校验值。实验性未跟踪目录 `backend/internal/keyrouting/replay/` 不属于本候选。

迁移为前向幂等结构变更，没有自动 down 迁移；旧镜像可以启动并忽略新增结构，但 `9000_custom_batch_image_no_discount.sql` 对历史批量折扣的归一化属于数据语义变更，回退旧镜像不会反向恢复旧折扣。生产回滚只能更换应用镜像并保留新增表/触发器，不能用旧备份覆盖上线后的业务数据库。

## 发布决定

生产三站已运行 `0.2.18-custom.1` 及上述不可变 digest，现场备份、顺序发布和健康验收均已完成。后续如需回退，按本记录保留新增迁移结构，只回退应用镜像到 `.16`，不得用旧备份覆盖新业务数据。
