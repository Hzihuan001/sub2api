# 0.2.18-custom.4 批量参考图请求修复

日期：2026-10-27（Asia/Shanghai）。Feature ID：`image-studio`。正式镜像已发布并通过精确digest验收，OVH三站已部署 `.18.4`。

## 问题与范围

用户反馈昨天共享参考图从1张提升至16张后，GPT批量返回 `BATCH_IMAGE_INVALID_ITEMS`。此前前端将同一组共享Base64重复放进每条prompt；handler对所有JSON绑定失败统一返回该错误，无法区分体积、类型、Base64或上传中断。

旧真实handler复现：16图合法请求1542字节可通过绑定并到达401；16图shape扩成22854字节、limit1543时返回400/`BATCH_IMAGE_INVALID_ITEMS`，非法data-URL Base64也返回相同错误。这个确定缺陷与参考图增加相关，但缺少用户那次完整请求，不能声称已经确认那次线上触发原因。最初将provider风险直接称为根因证据不足，已在共享日志勘误。

线上只读诊断仅确认北京时间10:05:21、10:05:37两次 `POST /v1/images/batches` 返回400，耗时52.775/49.738秒。旧日志没有请求大小、绑定异常类型或请求体，不能锁定就是用户这次请求或其具体原因。另33条200包含查询，不代表33次生成成功。未发现显式body-limit覆盖只能按代码默认256MiB推断，不能称已实测生产阈值。

## 实现

- 新增顶层 `shared_reference_images`，共享图片只传一次；`supports_shared_reference_images` 模型能力为true时新前端才使用，旧服务保留逐item传图以防静默丢失。显式空数组清除旧item图。
- 后端在原validate流程中将共享图规范为每个item，清除顶层字段后再做哈希/展开/持久化；保持旧格式幂等和每张输出16图完整性。不新增存储结构或迁移。
- 前端按 `output_count` 预检整组1000附件/128MiB解码总量，仍保留每文件10MiB、GPT16图、每条4输出/整组200输出。一次上传不等于放宽后台持久化/附件总量。
- 请求体过大返回413/`BATCH_IMAGE_REQUEST_TOO_LARGE`；JSON、字段类型、Base64、读体错误分类，日志只记安全结构字段，不记录prompt、图片或原始解码错误。
- GPT-image、Seedream 5.0 Pro、Grok Imagine明确使用app-managed普通单图并发；请求provider原值保留以避免改变旧哈希。Gemini/Vertex原生批量和Nano Banana单图异步轮询保留；没有改取消、余额、费率、质量、尺寸或超分行为。

## 本地验证

- 前端99项（参考图25、locale4、API22、policy39、size9）、最终typecheck、定向ESLint通过。首轮新增Base64计数断言误计前缀，已改为完整JSON字符串计数并重跑通过。
- Docker Go1.27.3-alpine显式 `-tags=unit`：共享16图展开/持久化/普通edits multipart保真、共享/旧格式哈希一致、清空共享、1000/128MiB边界、public service回归通过；provider测试同时注册native/managed，覆盖12种provider/尺寸组合。
- 真实handler定向（绑定、Key模型准入、审计）通过，包含共享一次上传在相同limit可绑定/重复上传413、JSON/类型/Base64、未知Content-Length和日志无payload。Go1.27 Base64错误包在类型错误内，调整分类顺序后重跑通过。
- custom isolation、custom contracts、prod依赖审计例外检查和diff-check通过；没有新增依赖、安全例外或成功Actions CI声明。
- 本机Docker起初未启动，随后恢复并补跑上述Go测试。未运行全量前端，保留既有无关select测试基线记录。

## 正式产物与隔离验收

- 版本 `0.2.18-custom.4`；运行源 `1524aa0985f432df19c9144c726224d2852308d6`，不可变源码标签 `custom-0.2.18.4`；分支 `codex/image-batch-16refs-20261026`。运行提交与文档收尾分开，标签不移动。
- 根Dockerfile构建时间 `2026-10-27T02:41:13Z`；发布并回拉镜像 `ghcr.io/hzihuan001/sub2api@sha256:80adc97b50213ba840e68d6c13b932701cff294f04fb5dcd291ffe12b63f49e6`。本地与OVH均按该digest拉取；本地嵌入版本/完整提交、OCI revision、linux/amd64、PostgreSQL18.6的pg_dump/psql均核对通过。
- 正式digest在隔离项目 `sub2api-operator-test-batchfix-20261027`、端口18098通过operator权限矩阵、Admin API Key、WebSocket与重启持久化验收。
- 同一digest的mock验收通过：共享16图只上传一次、请求不指定provider仍选 `app_managed`/`managed_fanout`、`3840x2160` 原样转发，两prompt按2+1展开3个单图任务，每个上游请求完整16图。PNG/ZIP、同格式及旧逐item格式幂等通过；合成费用0.03、无额外批量折扣。总上游4次包含另一个正常单图调用，最大并发3。mock输出是小PNG，只证明请求尺寸转发，不代表真实模型原生4K成品验收；没有新增生产付费模型调用。
- 本轮独立数据库已预热再启动应用，未出现此前空库就绪竞态。验收后只清理本轮项目的3容器、3卷和1网络，既有12个运行测试容器保留。

## OVH发布（已完成）

已验证备份 `/opt/sub2api-backups/v0.2.18-custom.4-20261027T024304Z`：三站dump均可由pg_restore列出，业务/配置/代理归档均可读，SHA256核对通过；归档只排除精确运行日志目录。dump体积主站102966128、L1 13690674、COS3052194字节。生产保留数据库/Redis/配置/卷/namespace/完整迁移，主站329、L1/COS各338条，无新增迁移。

沿已授权只读排空→主站/L1/COS顺序，仅更新应用服务。主站与L1在持续请求后自然排空，未强制取消或删除并发slot；逐站全部在途/等待/图片任务为零后才更新。

| 站点 | 北京时间2026-10-27 | 迁移记录 |
| --- | --- | --- |
| 主站 | 11:00:16–11:00:29 | 329，完整旧行保持 |
| L1 | 11:01:41–11:01:49 | 338，完整旧行保持 |
| COS | 11:02:14–11:02:22 | 338，完整旧行保持 |

三站运行同一正式digest/版本/完整提交，healthy、restart0，配置、数据库/Redis身份、卷/网络和namespace保持。各站内外网health、Keys、单图/批量页、工具文档均200；匿名管理设置、分组故障策略、Keys API均401；关键启动错误0。

11:02:46短时复核通过，主站/L1/COS实际观察148/64/32秒，三个namespace互不相同。该结果只覆盖有限观察窗口，不代表长期稳定性或生产登录浏览器/付费模型实测。

11:03:04公网资源核对通过：批量页面入口3/3一致，入口JS、单图/批量/策略chunk共12/12的HTTP状态、JavaScript MIME和SHA256与本地正式digest相同。仅忽略已知Cloudflare beacon，不修改代理配置。私有核对清单保存在 `D:/projects/image-batch-fix-release-20261027/`，不将测试凭据、备份内容或原始业务日志写入Git。

兼容应用回退目标 `.18.3` / `ghcr.io/hzihuan001/sub2api@sha256:cb1c937d398989ccff2256b0bf7a7a51c7b63405f8dd5a267a1cd50f47e7b7ac`，不恢复旧业务数据库。没有生产付费模型调用或实际回退。

## 17时后补充的业务日志证据

本轮后续只读检查发现：反代在部署窗口11:00:18–11:00:21记录3条 `/v1/responses` 连接重置/提前关闭/拒绝连接。此前“在途归零”指切换前只读瞬时采样，未阻止采样后新请求进入；“关键启动错误0”指指定启动日志匹配器，不包含这些反代业务错误。因此不能将原验收概括为部署零中断；本次仅补事实，不改写此前采样结果或其他功能实现。

17:43三站仍healthy/restart0、正式digest和配置/完整迁移保持。生图发布后3批量任务共6条目实际1成功/5上游502，用户另反馈过程中的 `Failed to fetch`；网络显示与生成条目失败尚未锁定同一根因。详细窗口、分类、建议和未确认边界见[生图工作台诊断](CUSTOM_IMAGE_STUDIO_DIAGNOSTICS_20261027.md)。此次只同步文档，不移动运行标签或重新部署。
