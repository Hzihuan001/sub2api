# 0.2.18-custom.3 — 批量共享16张参考图

日期：2026-10-26（Asia/Shanghai）
状态：候选，验证中，尚未部署。

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
- 完成后补齐源码/镜像坐标、正式镜像验收、部署及有限观察范围。GitHub Actions不可用时沿用已授权本地构建/GHCR回拉验收路径，不宣称CI成功。
