# 0.2.18-custom.2 — GPT-image 16 张参考图

日期：2026-10-26（Asia/Shanghai）
状态：源码、标签、正式镜像已推送且回拉验收通过；生产已按主站→L1→COS完成部署。

## 范围与基线

- 官方 `v0.2.18` / `ac1b8f1271be0d674fa6f074403c1410f30a3a81`；集成起点 `e5d4e8c16388a5f061a261997f5840f53ed65f38`。
- 隔离分支 `codex/image-studio-16refs-20261026`，工作树 `D:/projects/sub2api-image-studio-16refs-20261026`；旧活动树未提交内容不进入发布。
- 只提高单生图 GPT-image 明确模型的参考图上限至16，保留其他模型能力、文件大小、批量限制、图片尺寸/格式和计费行为。
- 前端策略与回归测试变化；后端只补 multipart16图解析测试，没有后端运行时或数据库迁移变更。
- 原生产/兼容应用回退目标：`0.2.18-custom.1` / `fc0d798bf2d6a08a074970d0485c567291e02660` / `ghcr.io/hzihuan001/sub2api@sha256:d7f365d1ba40b6c159d4155f7fbc2773d01f0fe6015af7906ebbece500ba1837`。

## 已完成的真实 API 测试（本次修改之前）

2026-10-26 12:32–12:33，两组分别通过主站 `/v1/images/edits` 请求 `gpt-image-2.5-sunburst`；各16张不同的256×256 PNG（合计88,934字节），`n=1`、1K、medium。超分组37秒、原生组39秒，均HTTP200并返回1张可解码图片。全部16个编号、图形和随机字符经独立视觉对照匹配，包含第13–16张；字符不在提示词中，核对拼图也未上传。

共2次真实生成，无自动重试；首次本地工具FormData缺失在发送前失败，不构成额外模型调用。未据接口token值推算账单；分组名不证明上游内部原生/超分方式。本测试仅证明本模型、本参数、小图单次链路，不保证其他模型、大文件、4K、并发或每个上游账号。

## 验收和发布门禁

- 前端：policy、上传边界、模型切换、multipart序列及image-studio相关测试；typecheck、定向lint、生产build。
- 后端：16个 `image` / `image[]` multipart项的内容/顺序；现有图片转发与批量参考图上限定向回归。
- 仓库：custom isolation/contracts、Compose校验、diff check。
- 正式镜像：根Dockerfile、准确version/commit、GHCR不可变digest回拉、隔离PostgreSQL/Redis容器角色和工作台验收；不将本地检查称作Actions通过。
- 生产：主站/L1/COS均已只读确认 `.18.1`；发布前新备份验证、逐站只读排空、仅应用更新；保留数据服务、卷、代理、配置及完整迁移记录，失败停止后续站点。
- 后续追加实际检查结果、源码与镜像坐标、备份、发布时间及观察范围。只记录实际完成的检查，不沿用旧报告冒充本次通过。

## 文件大小限制

单生图每张10MiB、后端单multipart部分20MiB及总体body限制保持不变；代理/上游可能有更低总量限制。批量内联总量默认128MiB，与图片数量上限独立，本次不放宽。没有增加新的按钮或工作台实际尺寸显示。

## 本轮验证结果

- 前端定向：3个文件76/76通过，覆盖模型策略、16张上传提交、第17张拦截、模型切换降限及multipart顺序。`vue-tsc --noEmit`、定向ESLint通过；全量lint为0错误/2条既有未使用变量警告。生产前端build通过（含i18n 4/4、类型检查、Vite构建；有既有大chunk提示）。
- 全量Vitest：424个文件，3414项通过/1项基线失败。失败为 `nativeControls.spec.ts` 检测 `KeyRouteFailureRulesEditor.vue` 的原生select；该组件与测试相对基线均无差异，blob分别为 `815ce4aa1551791a8563839162e596697d9dd45c` 与 `b1af29aa1785fdf00d1d5a9910facd37f5d3cc4e`。未修改其他功能或豁免测试，不能称全量测试通过。
- 后端：Docker Go1.27.3定向service测试通过（0.270秒），包含16项 `image` / `image[]` 原始内容与顺序、既有API-key图片编辑转发、GPT批量参考图上限及别名解析。后端运行时代码未变。
- 隔离/契约：`check-custom-isolation.py --skip-generated --base-ref e5d4e8c16 --upstream-ref v0.2.18`、`custom_contracts.py`（7模块/12文件组）、Compose配置及diff检查通过。
- 生产依赖审计按现有 `.github/audit-exceptions.yml` 验证通过；本轮没有续期或添加例外。未触发或宣称GitHub Actions成功，沿用运行手册授权的本地构建/GHCR回拉验收路径。

## 源码与备份

- 运行提交：`879a99a22e47a303604927104a6f67329dcd3b38`；分支 `codex/image-studio-16refs-20261026`；不可变源码标签 `custom-0.2.18.2`，均已推送。
- 三站完整有效备份：`/opt/sub2api-backups/v0.2.18-custom.2-20261026T050710Z`，三份PostgreSQL dump均通过 `pg_restore --list`，业务/配置归档、代理配置归档可读且SHA256已验证；清单 `verified=true`。
- 最初两次备份中止，未用于发布；第二次的私有stderr确认唯一错误为L1的运行日志在tar读取时变化。第三次按实际 `/app/data` 挂载只排除各站 `logs` 目录，记录精确排除项；没有忽略tar错误或排除业务文件，没有停服务、截断或改写日志。失败备份保留，不作为可用恢复点。
- 本轮只做应用更新，不恢复数据库；若需应用回退，使用上面的 `.18.1` digest并保留现有迁移与业务数据。

## 正式镜像验收

- 标签 `ghcr.io/hzihuan001/sub2api:0.2.18-custom.2`；不可变引用 `ghcr.io/hzihuan001/sub2api@sha256:c90609bc4546ec309a980929ec8f28eef3cef236022dd698700920439ce78412`。
- 根Dockerfile构建：Go1.27.3、Node24、pnpm9.15.9；`linux/amd64`。回拉后嵌入版本/完整运行提交及OCI revision一致；`pg_dump`、`psql` 均18.6可执行。OVH已拉取同一digest。
- 隔离项目 `sub2api-operator-test-image16-20261026` / 本地18098：正式digest的operator权限矩阵、JWT、WebSocket、重启持久化验收通过。首次空库启动因临时PostgreSQL就绪窗口出现一次connection refused，初次门禁失败；数据库稳定后仅重建本地应用容器，重跑完整门禁通过。首次日志保留于仓库外证据目录，未将首次启动写成成功；生产使用既有稳定数据库。
- `image-studio-smoke.ps1 -VerifyNoBatchDiscount` 通过：mock上游单生图1次、批量3张，共4次请求，最大并发3，PNG/ZIP下载和合成计费0.03通过；夹具清理完成。该测试没有真实上游账单，16张能力由前端/后端回归和先前真实API测试证明。
- 本地正式镜像实际服务的policy包含GPT-image 16、Gemini14、Seedream10、未知4；独立执行实际HTTP返回的纯策略模块也验证这些值。资源清单为 `/assets/index-CKAmrhW3.js`、`/assets/ImageStudioView-BASW-DLC.js`、`/assets/imageGenerationPolicy-L7NRaLhF.js`；部署后已完成三站SHA256比较。
- 仓库外本地证据目录：`D:/projects/image16-release-20261026`；mock生图证据目录：`C:/Users/a/AppData/Local/Temp/moshu-image-smoke-d405537b29`。不要将测试凭据、生产配置或私有备份上传Git。

## 三站部署结果

| 站点 | UTC时间（2026-10-26） | 北京时间 | 迁移条数 |
| --- | --- | --- | --- |
| 主站 | 05:24:23–05:24:31 | 13:24:23–13:24:31 | 329 |
| L1 | 05:25:06–05:25:17 | 13:25:06–13:25:17 | 338 |
| COS | 05:26:03–05:26:14 | 13:26:03–13:26:14 | 338 |

主站因持续正常流量只读等待约10分钟后归零；L1也等待了请求结束，COS初检即空闲。发布前再次检查所有请求/等待/批量/异步图片任务计数为0，没有强制取消、删除租约或停数据库。三站只重建应用服务，均healthy/restart0，嵌入版本/完整commit匹配正式镜像；环境/挂载/端口/网络指纹及PostgreSQL/Redis容器ID和启动时间保持不变，namespace值和全部迁移行不变、无新增迁移。

每站内网与公网 `/health`、`/keys`、`/image-studio`、`/docs/tools/codex` 均200；三个受保护匿名API检查均401；关键启动日志匹配0。部署结果保存于有效私有备份目录各站 `*-release-result.json`。本轮为有限发布验收，没有生产登录后的浏览器交互、付费推理、故障注入、真实回退或长时间容量测试。

## 发布后资源与短时复核

- 北京时间13:29:15，三站 `/image-studio` 的应用入口3/3与本地正式镜像一致；入口、视图、策略共9/9个JavaScript资源HTTP200、MIME正确且SHA256一致。policy SHA256为 `7e27b39756f51820f49c2d59da1ad8ebaa9ad18f5a08318984a7c7e7041711d3`。
- 初版只读资源脚本把Cloudflare额外注入的 `static.cloudflareinsights.com` module beacon误当应用入口，因此根入口检查失败，但3个应用资源已匹配。仅在私有验收脚本中精确识别并记录该HTTPS beacon路径，未知跨域仍拒绝，未请求beacon、未修改生产。脚本自检13/13；旧失败报告保留，最终证据为 `image16-assets-manifest-v2.json` 与 `image16-public-assets-report-v2.json`。
- 13:29:35再次只读复核三站healthy/restart0、正式digest、配置指纹、完整迁移记录、内外网检查及关键日志；主站/L1/COS观测时间分别310/268/212秒，关键错误匹配0，三个site namespace互不相同。该范围是约3.5–5分钟的短时观察，不代表30分钟或长期监控。
- 本次没有新增全局引用图大小限制、代理限制调整或服务端放大。当前发布仅开放已验证的GPT-image16张能力，复杂图片理解质量和其他上游账号能力仍由实际模型决定。

## 2026-10-26 后端测试范围勘误

本报告此前“后端定向”中列出的批量参考上限/别名两个目标位于带 `//go:build unit` 的文件，当时命令没有 `-tags=unit`，它们实际未执行。已执行的multipart16图解析及API-key编辑转发、前端和真实API证据保持有效；不能用此前无tag命令证明批量测试通过。后续 `.18.3` 显式带unit标签重测，包括共享16图展开与真实multipart构造，结果见 [`custom.3记录`](CUSTOM_RELEASE_0_2_18_CUSTOM3_20261026.md)。
