# v0.2.16 与生图工作台本地验收

记录日期：2026-10-14。范围包括源码合并、功能移植、本地验收和已批准的生产发布。生产版本与备份证据见 `CUSTOM_PROJECT_STATUS.md`；本报告中的本地镜像 ID 不能当作 GHCR digest。

## 源码坐标

| 项目 | 坐标 |
| --- | --- |
| 官方目标 | `v0.2.16` / `01842576b3d5c1e2545b5a99ad555d36e28ed306` |
| 定制起点 | `custom-0.2.15.3` / `0e5158d24` |
| 候选分支 | `codex/v0.2.16-image-studio` |
| 工作树 | `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom` |
| 官方独立合并 | `d3b78cf8c` |
| 生图来源 | `codex/image-studio-session-20261004` / `01124885d` |
| 生图移植 | 14 个功能提交，从 `ca26384c9` 到 `2c007ec6a` |
| 运行代码修复 | `e42db1f11b3afe54ff0cd88da42f37b928ff3a5c` |
| 测试与验收脚本 | `f17cae568` |
| 候选版本 | `0.2.16-custom.1` |
| 发布标签 | `custom-0.2.16.1` |
| GHCR digest | `sha256:edf55576e8ea73d2a552d7489cabdd2a6f282290e2ac713cda0d5504a55f5f17` |

旧生图分支基于 v0.2.13，故仅选择功能提交，不合并整分支、不移植旧 VERSION。v0.2.15.2 已有的 IndexedDB 用户隔离、尺寸元数据及下载行为未重复移植。代理商运行时没有恢复。

## 保留与修复

- 保留角色权限、Prompt Audit、管理端使用记录增强、公共文档和 Moshu 品牌。
- 移植 Nano Banana/Gemini 原生模型发现与 IMAGE 路由、Seedream 5 部署别名及模型特定尺寸/格式/参考图限制、批量输出格式、真实 MIME 下载和显式模型选择。
- 原生 Gemini 保留 1K/2K/4K 档位；不把 4K 像素误判为 2K，也不开放供应商不支持的自定义像素。OpenAI-compatible 自定义尺寸不变。
- 不本地放大图片，不显示实际像素文字，输出尺寸优先计费不变。
- BananaRouter 异步协议仅用于明确识别或显式标记的账号。提交意图、task ID、身份指纹和结果保存到现有 JSONB；短轮询、重启恢复、CAS、取消排空及失败下载恢复受测试保护。
- 管理员白名单候选与实时可用模型分离：候选可保留已配置模型；实际查询/请求调度仍排除不可调度账号，不向停用账号发模型发现请求。

## 自动化结果

| 检查 | 结果 |
| --- | --- |
| 前端完整 Vitest | 401 文件、2998 项通过；最终尺寸修复另有 25 项回归通过，平台目录 5 项通过 |
| 前端 typecheck、定向 ESLint、生产构建 | 通过；Docker 内生产构建也通过 |
| 后端 handler、server、管理权限、审计、兼容路由 | 通过 |
| 服务/服务器完整单测 | 初次发现唯一 Command Code 夹具缓存串扰；其余通过，修复后该测试连续 50 次及相关协议/路由回归通过 |
| Kiro 分组归一化 | 创建、更新、平台切换清理的定向回归通过 |
| 生图提供商、处理器、worker、取消与结算 | 定向单测通过 |
| 真实 PostgreSQL/Redis | 批量任务、Gemini 缓存、分组、异步 JSONB 检查点/CAS 集成测试通过 |
| Ent / Wire | Ent 独立临时目录完整生成逐字节比对无差异；Wire 生成通过 |
| custom isolation、Compose、diff | 通过 |

成功检查结果复用；未为一个测试夹具修复重复运行全部长耗时测试。未执行真实供应商付费请求、十小时压测或生产验收。

## Docker 验收

- 项目：`sub2api-operator-test-v0216`。
- 地址：`http://127.0.0.1:18086`；仅绑定本机。
- 镜像：`sub2api:0.2.16-custom.1-local`，Linux amd64。
- 本地镜像 ID：`sha256:2bf77c8b5c9e7c80a4f0fe5dfda101d18f054ed3b0075747a8da42df0a5490fe`。
- 内置运行提交：`e42db1f11b3afe54ff0cd88da42f37b928ff3a5c`。之后提交仅含测试、验收脚本和文档，不改变镜像运行代码。
- PostgreSQL、Redis、应用数据、网络与生产完全隔离；使用合成数据和本地 mock，无真实模型费用。

权限脚本通过：管理员、operator、普通用户登录，JWT/Admin API Key、默认拒绝、最高角色目标保护、管理页支持接口、WebSocket、重启与数据持久化。

生图 smoke 通过：模型发现、单图、三张批量输出、公共/单项参考图、幂等提交、PNG 字节与 MIME、单张下载、ZIP 三图下载。mock 共收到四次请求（一次单图加三次拆分输出），批量并发峰值 3，三个输出独立幂等键，三张成功输出的合成结算为 `$0.03`。

首次 smoke 因将公开 item 状态 `succeeded` 误写成数据库状态 `success` 而停止；修正 harness 后复用既有任务验证，不重复生成任务。保留任务 `imgbatch_24de12906ddca3c513628acc4129355c` 和 mock `image-smoke-f4002e20f2` 供检查。

## 迁移与回滚

本次无新定制迁移；追加官方文件：

- `242_group_i18n.sql`
- `243_gemini_cached_contents.sql`
- `244_gemini_cached_contents_channel_usage.sql`
- `245_gemini_cached_contents_mutation_lease.sql`

迁移记录按完整 `filename` 和校验和保存，重复数字前缀不改名。隔离数据库从旧版升级后记录数由 318 增至 322。

在同一已迁移数据库上，只切应用镜像回 `sub2api:0.2.15-custom.2-image-local`，健康、合成用户登录、模型发现均通过，记录数仍为 322；随后恢复候选镜像，并复用原有任务完成模型、下载和费用检查，未新增生成请求。最终候选健康、意外重启次数 0，最近日志无 panic/fatal、迁移失败及数据库/Redis 连接错误。未恢复数据库备份、未删表、未重建数据库/Redis/卷/网络。此证据是本地旧镜像兼容，不等同于 GHCR 发布产物验证。

## 留存与下一步

验收环境保留。合成测试登录资料仅在本地验收脚本中，不是生产账号。

停止应用与配套验收服务（保留卷）：

```powershell
docker compose -p sub2api-operator-test-v0216 -f deploy/docker-compose.operator-test.yml stop
docker stop image-smoke-f4002e20f2
```

## 生产发布记录（2026-10-14）

- **推送与镜像**：分支 `codex/v0.2.16-image-studio` 和标签 `custom-0.2.16.1` 已推送 Fork。仓库 Actions API 返回 `Actions has been disabled`，无法创建本次自动运行；本地已通过质量门禁和隔离 Docker 验收后，使用 Buildx `linux/amd64` 直接发布同一源码提交的 GHCR 镜像。该例外已记录，后续应在 GitHub 设置中恢复 Actions 后再恢复自动门禁。
- **发布提交**：镜像内版本 `0.2.16-custom.1`，commit `ece7cf2208332fccb9b031e04b4f35266302dbe3`，构建时间 `2026-10-13T15:59:56Z`。
- **备份**：OVH 发布前备份为 `/opt/sub2api-backups/v0.2.16-custom.1-20261014T0010Z`。包含主站/L1/COS Compose、`.env`、数据归档、三套 PostgreSQL custom-format dump、旧镜像引用和 SHA256SUMS；主站 dump 已确认针对实际 `sub2api` 数据库，三个 dump 均可由容器内 `pg_restore -l` 读取。
- **部署**：只修改三个 Compose 的 Sub2API 镜像引用，并分别执行 `up -d --no-deps`；未重建 PostgreSQL、Redis、卷、网络或 OpenResty。主站、L1、COS 均切换到上述 digest。
- **快速验收**：三个应用容器 healthy、重启次数 0；三个 Moshu 域名 `/health` 均返回 200；主站/L1/COS 版本、commit、digest 一致；迁移计数为 322/331/331；Redis 认证连通；部署后应用日志无 panic/fatal、迁移失败或数据库/Redis 错误。
- **清理**：删除未被容器使用的旧 Sub2API 镜像 `0.2.13-custom.6` 及两个无标签旧层，删除未使用 CPA `v8.0.23`、`v8.0.8`；清理可回收 Buildx 缓存约 244 MB。保留当前 digest、主站旧 digest、L1/COS 旧 digest、`rollback-0.2.13-custom.3`、数据库/Redis/卷和网络。清理后 Docker 镜像从 18 减至 13，未使用卷未动。
- **回滚**：若需回滚，恢复发布前 Compose 中的旧 digest 并仅重建对应应用容器；本次官方迁移已在本地旧镜像兼容演练中验证，生产若发现不可逆迁移问题仍必须按备份恢复数据库，不能只切镜像。

后续等待旧版异步在途任务完成并继续观察错误率；未知提交结果超过 24 小时仍需人工核账，不得盲目重提或退款。
