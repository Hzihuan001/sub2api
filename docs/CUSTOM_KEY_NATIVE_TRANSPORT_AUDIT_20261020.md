# Key 账号级路由：原生运输层核查记录（2026-10-21）

本文件只记录该阶段的原生运输层局部证据，不表示整个功能已通过验收。活动源码为 `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom`。未推送、未发布、未调用生产模型。下方“仍须闭环”小节保留历史审计边界；最新测试结果以本页顶部补记和共享执行台账为准。

## 2026-10-21 最新局部复核

- WSv2、Grok caption、Kiro typed failure、部分 usage 及 provider transport 定向测试已通过：`go test ./internal/service -run 'TestKeyAccount(WS|Grok|NativePartial|Kiro|Provider)|TestKeyAccountAttempt|TestKeyAccountInferenceTransport' -count=1 -timeout=180s`，5.360 秒。
- 这组测试只证明运输层的 attempt/usage/replay 边界，不证明三站 Docker、真实 PG/Redis、多实例、账务核账或生产镜像。Docker Desktop Linux 引擎当前未运行，新候选尚未启动。

## OpenAI 插件与普通 HTTP

实现位置：`backend/internal/service/openai_plugin_transport.go`、`plugin_dispatch_reservation.go`，相关测试为 `key_account_openai_transport_test.go` 与 `plugin_dispatch_reservation_test.go`。

- 普通推理必须由调用方传入最终映射模型；scope 开启后缺失实际模型时本地拒绝，不能猜测为公开模型。
- 插件命中/未命中从同一冻结路由快照决策。插件本地 readiness 与 runtime reservation 先执行，之后才进入真实发送的预算与尝试生命周期。runtime 为空、client/api 缺失、draining、已退出、输入校验或最终准入拒绝不产生派发样本。
- reservation 只持有原生 runtime，不是模型推理。未发送时释放一次；成功后保留原生 `pluginResponseBody.Close` 的释放所有权；重复 cancel/dispatch 不会重复释放。
- `pluginRuntime.roundTrip` 在 `api.Forward(ctx)` 前只有 request/URL/account 校验。header map 与 URL 字符串构造不返回错误；正文读取在 Forward 和请求元数据发送之后。正文读取失败因此属于已进入真实 RPC 的不确定尝试，而不是“本地零请求”。
- 插件明确 `RequestSent=true` 时禁止内层和外层重放；取消可能抹去 typed error，已调用插件后返回的非 typed 错误同样保守禁止。只有插件明确 `RequestSent=false` 保留安全重试机会，不制造 HTTP 状态码。
- 旧 body 未读完/Close 前不能进入下一真实发送；同一个 Forward 内的多次真实发送具有不同 AttemptID，使用对应映射模型与独立样本。无 scope 与 count_tokens 保留原生路径。

局部定向测试以受控 HTTP/内存 gRPC 帧替身完成；唯一 subprocess 是立即退出的本地测试子进程，用来验证 go-plugin 自身的 `Exited` 状态。没有模型费用。并发 cancel/dispatch 使用确定性门闩循环测试；这不是 `-race` 检测结果。

稳定源码定向命令与结果：

```text
go test ./internal/service -run 'TestKeyAccountOpenAITransport|TestOpenAIGatewayPluginRouting|TestPluginRequestSentError|TestPluginManagerRouting|TestKeyAccountPluginReservation|TestKeyAccountInferenceTransportB2NativeWrapper' -count=1 -timeout=60s
ok github.com/Wei-Shaw/sub2api/internal/service 0.338s
```

离线 Go 1.27.2 工具链；该命令覆盖本次 13 个 OpenAI/reservation 顶层测试及已有插件回归与 wire-context 包装测试。`git diff --check` 同时通过。未进行整套测试、模型实测或生产发布。

## 仍须闭环：HTTP 入站到原生 upstream WSv2

普通 `/responses` HTTP 请求也可能进入原生 WSv2。不能以“客户端 WebSocket 不参与多组路由”排除这条实际路径。

核查锚点：

- `openai_gateway_forward.go` 的 `OpenAIUpstreamTransportResponsesWebsocketV2` 分支。
- `openai_ws_forwarder_v2.go`：lease Acquire；真实 generation `lease.WriteJSONWithContextTimeout(ctx, payload, ...)`；`ReadMessageWithContextTimeout`；usage、首有效内容与 terminal error 处理；defer `MarkBroken/Release`。
- `openai_ws_forwarder_support.go` 的 `performOpenAIWSGeneratePrewarm`：`generate=false` 预热与实际 generation 是不同语义。底层 lease Write 被预热、其他 WS 共用，不能全局装饰成推理。

所需验收：真实 generation 每次独立 attempt；实际映射模型正确；未知帧/usage/tool/reasoning/已有输出单调 veto；写入错误不能假定服务器未收到；WS close code/error type 不能冒充 HTTP 503；计时取消必须绑定这次 attempt 与完整无内容前缀证明；reader 退出和 lease 释放后才允许重放。上述局部测试已覆盖这些运输层边界，但本文件不代替三站联合验收。

## Grok 图片 caption：保留原生计费、媒体链路不自动重放

核查锚点：

- `openai_gateway_chat_completions_raw.go` 在主生成前调用 `bridgeGrokComposerImageInputs`。
- `openai_gateway_grok.go` 的 `bridgeGrokComposerImageInputs`、`describeGrokComposerImage`；caption 使用独立模型 `grok-build-0.1`。
- 新增独立薄适配 `key_account_grok_auxiliary.go`；主运输层 scope 保留辅助派发/清理状态，不复制原生 provider 或另建账务。

本轮采用经确认的保守边界，替代此前未实施的 memo/独立结算 token 草案；本节最新状态优先于历史设计讨论：

- 每个真实 caption Do 使用独立 scope、AttemptID、实际映射模型及端点，正文/usage 观测只归属该次尝试；不能将辅助成功当作主生成完成。
- 本地构造/准入失败尚未发送时不污染主 scope。caption 真正派发后才设置常驻媒体 replay veto；caption 的正文和原生 Close 完成、独立 scope Finish/CleanupAck 成功之后，才允许首个主生成。
- 首个主生成保留原生行为，但不启用 TTFT 主动取消；其内层真实重试及外层换账号/换分组均阻断。caption 不确定、未知 2xx、正文无效、清理失败也不能借重试重复付费辅助调用。纯文本 Grok 不受这一媒体边界影响。
- caption 的已确认真实 usage 合入原生同账号、同分组、同 Composer 计价结果一次。主请求构造/准入/网络/HTTP/解析失败，或后续 caption 失败，也保留已取得的部分 usage；普通 error 去掉原生 failover 分类，供已有 partial-result 结算入口处理。不新增 usage 表、独立辅助扣费或价格 override，不臆造未知调用的 token。
- 主结果合并 caption 前，主请求的原生 usage 独立观测；parent Finish 不把合并后的 caption 用量错误归属主 HTTP 的样本。原始 wire reader 对主 usage 的保护仍有效，其余平台的 native-result usage 保护不变。
- 无 scope 继续使用原生请求/解析/usage 行为。图片、工具与其他媒体链路的自动重放范围没有扩大；不实现跨账号 caption memo，也不宣称多步媒体请求能像无状态文本一样自动故障切换。

局部验收使用 `key_account_grok_auxiliary_test.go`，覆盖独立 caption/main 样本及映射模型、TTFT 不取消首个主生成、主失败与后续 caption 失败的部分用量、未知派发、nil response/body、Close 失败、辅助未清理禁止主发包、主内部重试零二次发送，以及成功/主失败两条路径各一次原生 `RecordUsage` 合并命令与金额。原生未开启 scope 的桥接回归一并运行；最新命令见本页顶部。

稳定源码最终定向测试结果见下一次收尾记录；此前已完成的局部命令通过不替代 handler 槽位、真实数据库幂等及本地 Docker 联合验收。

## 局部通过不等于发布放行

这些测试不覆盖 handler 账号槽位、原生预算耗尽、跨组价格、真实 PostgreSQL/Redis、多实例、完整生产构建及回滚。只有合成链路整体核账、资源屏障、所有承诺路径覆盖及本地 Docker 人工验收完成后，才可以称功能可上线。
