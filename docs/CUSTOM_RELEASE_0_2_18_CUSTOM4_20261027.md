# 0.2.18-custom.4 批量参考图请求修复

日期：2026-10-27（Asia/Shanghai）。Feature ID：`image-studio`。当前为候选，尚未部署；生产仍为 `.18.3`。

## 问题与范围

用户反馈昨天共享参考图从1张提升至16张后，GPT批量返回 `BATCH_IMAGE_INVALID_ITEMS`。此前前端将同一组共享Base64重复放进每条prompt；handler对所有JSON绑定失败统一返回该错误，无法区分体积、类型、Base64或上传中断。

旧真实handler复现：16图合法请求1542字节可通过绑定并到达401；16图shape扩成22854字节、limit1543时返回400/`BATCH_IMAGE_INVALID_ITEMS`，非法data-URL Base64也返回相同错误。这个确定缺陷与参考图增加相关，但缺少用户那次完整请求，不能声称已经确认那次线上触发原因。最初将provider风险直接称为根因证据不足，已在共享日志勘误。

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

## 发布门禁（待执行）

使用根Dockerfile构建 `.18.4` linux/amd64，提交/标签/不可变digest在验收后登记。需精确digest operator权限/重启验收及mock：共享16图只上传一次、无provider选managed、4K请求原样、2prompt展开3单图、PNG/ZIP、同格式/旧格式幂等、正常单图费率无批量折扣。没有生产付费模型调用。

OVH三站沿用已授权备份→只读排空→主站/L1/COS顺序仅更新应用；保留数据库/Redis/配置/卷/namespace/完整迁移。兼容应用回退目标 `.18.3` / `ghcr.io/hzihuan001/sub2api@sha256:cb1c937d398989ccff2256b0bf7a7a51c7b63405f8dd5a267a1cd50f47e7b7ac`，不恢复旧业务数据库。当前无生产更改或部署成功声明。
