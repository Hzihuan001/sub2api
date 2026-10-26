# 0.2.18-custom.3 — 批量共享16张参考图

日期：2026-10-26（Asia/Shanghai）
状态：正式源码/镜像已推送并完成回拉验收；主站/L1/COS已完成部署。

## 范围与隔离

- 官方基线仍为 `v0.2.18`；集成起点 `10b452931592de432db312ffc0a94972c1b89666`。分支 `codex/image-batch-16refs-20261026`、工作树 `D:/projects/sub2api-image-batch-16refs-20261026`。
- 只补齐批量GPT-image“整组统一参考图”16张；逐条模式原已16。其他模型按各自能力（Nano/Gemini14、Seedream10、Flash3）；不新增按钮或实际尺寸展示，不改计费/图片尺寸。
- 原始单文件10MiB、任务输出最多200张、展开后参考附件最多1000个、inline总字节128MiB仍适用。共享图会随每条Prompt及output_count重复计算，不能把16图上限理解成任意任务总量均可提交。
- 前端行为/中英文说明及回归、后端测试、版本和验收脚本更新；无后端运行时、迁移或生产配置变更。旧树未提交维护/文档修改不进入候选。
- 当前生产/兼容应用回退目标：`.18.2` / `879a99a22e47a303604927104a6f67329dcd3b38` / `ghcr.io/hzihuan001/sub2api@sha256:c90609bc4546ec309a980929ec8f28eef3cef236022dd698700920439ce78412`。回退只换应用，保留业务数据与完整迁移记录。

## 验收计划

- 前端：共享16图按顺序完整映射到每条Prompt，追加/删除/第17张/模型切换边界；相关Vitest、typecheck、定向lint、生产build和i18n完整性。
- 后端：显式 `go test -tags=unit` 覆盖共享16图展开、持久化、实际multipart生成及原总量风控；不重复调用付费模型。
- 正式镜像：根Dockerfile构建并推送，按不可变digest回拉，验证version/commit/架构及pg_dump/psql；隔离operator门禁和 `image-studio-smoke.ps1 -ReferenceImageCount 16 -VerifyNoBatchDiscount`。
- 生产：三站只读基线检查、有效备份及checksum/dump/archive验证，零在途后主站→L1→COS仅更新应用；健康/匿名权限/完整迁移/namespace/配置及实际批量页面资源核对。

## 前一版验证记录勘误

`.18.2`报告曾将两个批量相关后端测试列入通过范围，但当时命令没有 `-tags=unit`，带该build tag的批量测试文件实际未执行；multipart16图解析测试已执行。此前两组16图真实API结果及前端76项不受此说明影响。本轮必须显式启用unit标签并以本轮输出为证据，不能沿用此前命令证明批量后端通过。

## 实际结果

- 前端5文件92/92通过（批量12、原单图/策略/API76、i18n4），`vue-tsc --noEmit`、4文件ESLint、diff检查通过。共享删除至空不再回落到旧逐条参考图；并发读文件后重新计算剩余额度，防止追加/切换模型突破上限。
- Docker Go1.27.3：`go test -tags=unit ./internal/service -run 'TestBatchImageShared|TestMaxBatchImageReferenceImagesForModelSupportsGPTImage|TestValidateBatchImageReferenceLimitsForAccountResolvesModelAlias|TestAppManagedBatchImageProviderSendsGPTImageReferenceAsEdit' -count=1 -timeout=300s` 通过（0.449秒）。两Prompt以2+3输出展开，持久化与5次multipart各保留16个文件的顺序/名称/内容；992附件通过/1008拒绝，128MiB边界通过/多一输出拒绝。无后端运行时修改。
- 依赖审计按现有例外检查通过，没有新增或续期例外。没有重跑此前包含1项既有密钥路由select失败的全量前端套件，不能把本次92项称为全量测试通过。
- 三站基线已只读确认`.18.2`且healthy/restart0；本轮有效备份 `/opt/sub2api-backups/v0.2.18-custom.3-20261026T081840Z`，三份dump的restore list、配置/业务文件/代理归档及SHA256已验证。仅精确排除持续写入的运行日志，其他业务文件保留。
- custom隔离（`10b452931` / 官方`v0.2.18`）、架构契约（7模块/12执行组）和PowerShell验收脚本语法通过。没有新增或改写迁移。

## 正式产物及验收

- 运行源 `c2dbbe786e844ee1df77bfd710ebaf438f625126`；分支 `codex/image-batch-16refs-20261026`；不可变标签 `custom-0.2.18.3`，均已推送。
- 正式镜像 `ghcr.io/hzihuan001/sub2api:0.2.18-custom.3`；不可变引用 `ghcr.io/hzihuan001/sub2api@sha256:cb1c937d398989ccff2256b0bf7a7a51c7b63405f8dd5a267a1cd50f47e7b7ac`。根Dockerfile构建，Go1.27.3/Node24；正式digest已在本地与OVH拉取，linux/amd64、嵌入版本/commit/OCI revision正确，pg_dump/psql18.6可执行。
- 生产前端build通过（i18n、vue-tsc、Vite；保留既有大chunk/Browserslist提示）。源码与正式镜像经独立复核，没有发现本轮阻止发布的问题。
- 正式digest在隔离项目 `sub2api-operator-test-batch16-20261026` 通过operator权限/JWT/WebSocket/重启持久化门禁；提前等待本地PostgreSQL/Redis稳定再启动应用，本轮首次完整门禁即通过。
- `image-studio-smoke.ps1 -ReferenceImageCount 16 -VerifyNoBatchDiscount`通过：接口模型能力明确返回16；同一组16个PNG参考附件用于两个不同Prompt，2+1展开为3张输出，每次上游multipart均含完整16图，总计48附件；外加一次无参考单图请求，共4次mock上游请求，最大并发3。PNG/ZIP、幂等键、无额外批量折扣和合成费用0.03通过；无真实付费调用。内容/顺序逐项一致由后端unit与前端请求回归额外验证。
- mock证据：`C:/Users/a/AppData/Local/Temp/moshu-image-smoke-1a0b035c50`；只读资源/发布工具证据：`D:/projects/image-batch16-release-20261026`。临时凭据不写Git。
- 本地 `/batch-image` 验收入口 `index-CJ-ygdFX.js`；批量组件 `BatchImageGuideView-BQZRvQgz.js`，SHA256 `c0f319006385429922d8e46d9051631408570ee5254aadf559bfd33a15d37f95`。正式部署后已与三站实际资源比较。
- 本轮沿用已授权本地构建/GHCR回拉验收路径，没有GitHub Actions成功结论。正式运行标签不随后续文档提交移动。

## 三站部署及发布后核验

| 站点 | UTC时间（2026-10-26） | 北京时间 | 迁移条数 |
| --- | --- | --- | --- |
| 主站 | 08:36:56–08:37:04 | 16:36:56–16:37:04 | 329 |
| L1 | 08:39:49–08:39:57 | 16:39:49–16:39:57 | 338 |
| COS | 08:40:50–08:40:58 | 16:40:50–16:40:58 | 338 |

- 主站与L1等正常请求结束后，逐站只读确认请求/等待/批量/异步图片任务计数为0再更新；COS初检即空闲。未删除租约、强制取消或改业务数据库。三站仅重建应用，精确digest与嵌入版本/commit匹配，healthy/restart0；配置/挂载/端口/网络指纹、PostgreSQL/Redis身份与启动时间、namespace及完整迁移行均保持，无新增迁移。
- 每站内网及公网 `/health`、`/keys`、`/image-studio`、`/batch-image`、`/docs/tools/codex` 均200；三个匿名受保护API均401，关键启动错误匹配0。私有结果位于有效备份目录各站 `*-release-result.json`。
- 北京时间16:43:47资源比较通过：三站批量页面应用入口3/3一致，每站4个入口/单图/批量/策略JavaScript共12/12资源HTTP200、MIME及SHA256完全匹配。仅精确排除已核验Cloudflare beacon，不请求它；未知跨域仍拒绝。证据 `batch16-assets-manifest.json`、`batch16-public-assets-report.json`。
- 16:44:40只读复核再次确认healthy/restart0、配置/迁移/内外网检查/关键日志；主站、L1、COS实际观察462/290/230秒，关键错误0，三个site namespace互不相同。属于约4–8分钟短时核验，不宣称长期监控、生产登录浏览器测试、付费推理、故障注入或真实回退。
