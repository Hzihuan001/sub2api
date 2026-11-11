# 图片任务断线找回与本地结果存储（候选实现，待发布）

本文记录候选版本 `0.2.20-custom.3` 的 `2026-11-11` 工作树实现。它尚未作为本说明编写时的动作发布到生产环境；生产预检只读完成，三站的运行配置与挂载证据见仓库根目录 `.tmp/image-recovery-production-preflight.json`。

## 目标与边界

图片工作台对 OpenAI 和 Grok 的异步 generations/edits 请求先返回任务 ID。浏览器关闭、网络切换或页面重载后，客户端可以在同一个用户和同一个 API Key 的 IndexedDB 任务库中继续轮询并保存结果。服务端保留任务元数据 24 小时，生成文件的本地清理窗口为 48 小时（惰性清理，在后续保存时触发）。

服务端记录的执行截止时间默认 30 分钟，结果持久化另有独立的 5 分钟预算；异步任务的上游 drain 已标记为继承该绝对执行截止时间，不会再追加普通同步请求的断开等待。超过执行截止时间且在保存宽限期内没有结果时，轮询会得到 `image_task_result_unknown`；服务端不会因为客户端重试而再次调用模型。已经完整保存的结果可以在 Redis 元数据短暂不可用时由本地 checkpoint 恢复，晚到的成功结果可以恢复“结果未知”状态，但不会覆盖真实的 completed 或真实 failed 终态。

这项机制恢复的是任务查询和已产生结果的取回。上游 HTTP POST 已经发出后，如果响应在网络中断时丢失，不能声称支持上游请求的断点续传；再次提交可能重复生成或计费。客户端只对轮询和受限的首次提交确认做短暂重试，无法确认的提交不会自动切换到同步生成。

## 幂等合同

`POST /v1/images/generations/async` 与 `POST /v1/images/edits/async` 接受可选 `Idempotency-Key`。键为 1–128 个 ASCII 字母、数字、`.`、`_`、`:` 或 `-`，并按 **用户 ID + API Key ID + 键** 隔离。

- 同一个作用域、同一个键和同一个规范化请求会返回已有任务，不会再次执行模型。
- 同一个作用域复用键但请求指纹不同会返回 `409 IMAGE_TASK_IDEMPOTENCY_CONFLICT`。
- Redis 使用原子 `CreateIfAbsent` claim；进程锁不能替代它。残留 claim 找不到任务时 fail closed，不会放行第二次生成。
- JSON 指纹会压缩空白并保留对象键顺序；multipart 指纹忽略 boundary 和不同字段的排列，包含字段名、文件名、内容类型、大小和字节 SHA-256，同名重复文件的顺序仍然有意义。

客户端应在一次逻辑提交的短重试中复用同一个键和同一份序列化请求。若第一次提交结果不明确，客户端不会回退到同步接口，避免重复生成。

## 存储选择

候选版本优先使用已配置且完整的 S3 兼容对象存储；上传完成后 Redis 只保留紧凑 URL 结果，不保留原始 `b64_json`。上传失败会将任务标记为 `image_task_result_storage_failed`，并明确提示不要自动再次生成。

从未保存过后台 `image_storage_config` 且配置文件 fallback 没有启用对象存储时，候选版本使用应用 `DATA_DIR/image-tasks` 的本地存储。文件通过受任务所有权保护的 `GET /v1/images/tasks/{task_id}/images/{index.ext}` 提供，原子写入并在 48 小时后惰性清理。已保存过的后台设置优先：后台 `enabled=false` 明确拒绝新任务；后台设置不完整也不会静默改用本地回落。配置文件或环境中的 `image_storage.enabled=false` 只关闭该 S3 绑定；在没有后台设置且未启用 fallback S3 的站点，本地结果存储仍然可以工作。

本地文件和 checkpoint 必须位于持久卷，不能依赖容器可写层。结果下载继续进行任务 ID、用户和 API Key 所有权校验；不向任意外部 URL 自动附加 API Key。

## 已做的生产只读预检

预检时间为 `2026-11-11T02:47:15Z`，未修改服务器状态、未调用模型、未读取或输出密钥。

| 站点 | 应用端口 | 数据目录挂载 | UID 1000 写入 | 应用默认图片间隔 | 应用响应头 | 反代读/写 |
| --- | ---: | --- | --- | ---: | ---: | --- |
| main `ai.moshu.cloud` | 8080 | `/opt/1panel/apps/sub2api/sub2api/data` → `/app/data` | 是 | 900 秒 | 600 秒（OpenAI 0，Grok 120） | 10h / 10h |
| L1 `api.tokenpulse.top`、`api.moshu.cloud` | 3000 | `/opt/api-L1/data` → `/app/data` | 是 | 900 秒 | 600 秒（OpenAI 0，Grok 120） | 10h / 10h |
| COS `cos.tokenpulse.top`、`cos.moshu.cloud` | 3001 | `/opt/api-COS/data` → `/app/data` | 是 | 900 秒 | 600 秒（OpenAI 0，Grok 120） | 10h / 10h |

三站均有 `/app/data` 持久 bind mount，当前可用空间约 38.5 GB。主站的 `nocf.moshu.cloud` 是独立旁路代理：静态入口的上游超时为 3 秒，转发应用的读写超时为 10h；它不改变应用任务的 30 分钟执行预算。

## 端点限制与插件边界

异步任务只覆盖 OpenAI/Grok 的 generations 和 edits。同步接口仍按原有请求生命周期工作；Composite、Gemini 原生图片、批处理及其他供应商不能被这套异步任务合同宣称为可恢复，前端对它们保留同步路径或现有工作流。流式图片请求被拒绝，因为任务轮询只返回一个最终 JSON。

这项能力属于宿主的任务、所有权、Redis claim、结果存储和安全下载边界。插件可以实现自己的传输或供应商协议，但不能绕过宿主的 API Key 所有权、幂等 claim、结果保留和本地文件鉴权；插件协议中的 `ForwardResponseError.request_sent` 仍须准确表示上游是否可能已收到请求。是否将其他供应商接入异步任务，需要另行扩展协议、路由、计费和测试，不由本候选实现自动获得。

## 发布前检查

本候选已通过后端 `go test -tags=unit ./... -count=1`、`go test -p=1 -tags=integration ./... -count=1`、golangci-lint，以及前端 442 文件/3714 项测试、lint、typecheck 和 build；日志位于 `.tmp/full-unit-recovery-final.log`、`.tmp/full-integration-recovery-final.log` 及前端最终门禁日志。发布候选版本仍应检查三站 `DATA_DIR` 持久挂载和非 root 写权限，并用不产生真实付费模型请求的合成上游验证：相同键并发只创建一个任务、断 Redis 后 checkpoint 可取回、错误不会重复执行模型、关闭后台存储设置后既有任务仍可轮询。生产验收应明确区分健康/HTTP 路径验证与真实模型调用；本说明不把后者宣称为已完成。
