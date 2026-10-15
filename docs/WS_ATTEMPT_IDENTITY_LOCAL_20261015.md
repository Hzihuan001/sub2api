# WebSocket 尝试身份：本地验证记录

2026-10-15。P5 的真实尝试幂等边界补充；本报告记录独立修复时的本地证据，当时未推送、未部署，不表示最终候选通过。

后续完整门禁见 [最终候选报告](CUSTOM_MODULARIZATION_FINAL_GATES_20261015.md)。本修复已纳入正式 `.6` 运行提交 `655a16868` 并部署三站；源码/标签和镜像已推送，回拉同一产物的隔离验收、三站备份及生产部署检查通过，见 [发布报告](CUSTOM_RELEASE_0_2_16_CUSTOM6_20261015.md)。正式镜像的 WebSocket 矩阵在本地执行，不冒称生产执行了相同测试，也不将发布结果倒写为本阶段额外测试证据。

## 实现边界

- passthrough 由真实 `response.create` 发送建立尝试作用域，以 `response.id` / `response_id` 关联后续帧，不再仅以 `response.created` 重置故障计数。
- 已知旧响应的迟到帧保留原始字节转发，但不再修改新轮次的用量、完成回调、账号冷却、输出标记或超时。
- 后续轮次无身份的 error/terminal 无法安全归属：透传该帧后以 1013 请求重新连接，不猜测归属、不自动重放，也不继续接受新的发送。
- 连接内保留最多 1024 个响应身份，达到上限要求重连，不驱逐旧身份后把迟到帧误当新请求。
- 延迟到达的 created 若属于同一真实尝试，不提前结算待配对 error；后续 failed 提供权威 usage，只回调一次。
- deadline 更新与尝试身份切换遵循相同锁顺序，前轮 terminal 不能清除刚发送的新轮截止时间。二进制帧保留原来的不透明语义及文本专用首输出超时行为。
- 未改变错误分类、overload 阈值、重试预算、原始响应载荷或已输出后不重放规则。新增重连保护属于身份不可确定时的显式边界，不宣称客户端可以无缝继续旧连接。

## 证据

旧问题复现提交 `3856b469a` 在候选和基线 `989594099` 同样失败：下一次发送缺 created 时计数被压制；已完成旧 ID 的迟到终态会重复完成回调/误计冷却。

```text
go test -json -tags=unit ./internal/service ./internal/service/openai_ws_v2 -run 'TestGatewayFailureAttempt|TestOpenAIWSPassthrough|TestPassthroughLifecycle|TestRelay|TestRunEntry|TestPassthroughBilling|TestPassthroughUsageMeta' -count=1 -timeout 180s
go test -tags=unit ./internal/service/openai_ws_v2 -count=1 -timeout 60s
```

- 81 个顶层测试 / 含子测试 111 项通过；service 20.608 秒，relay 3.091 秒；完整 relay 包另跑通过 3.092 秒。
- 日志：`D:/projects/go-tmp/ws-identity-focused-tests.json`。
- 审查后新增确定性交错：identity 先观察旧终态，writer 真正发送下一轮，再执行旧终态的 deadline 更新。修复前 generation 2 从 armed=true 错变 false；修复后保留新截止时间并实际产生有界的首输出超时。
- 最后交错修复的影响范围复跑：`go test -json -tags=unit ./internal/service -run '^Test(OpenAIWSPassthroughIdentity|GatewayFailureAttemptPassthrough|PassthroughLifecycle)' -count=1 -timeout 120s`，30 个顶层 / 含子测试 41 项通过（20.058 秒）。
- 前后证据：`D:/projects/go-tmp/ws-identity-deadline-interleave-before.log`、`ws-identity-deadline-interleave-after.json`。

工具链 Go 1.27.2 Windows。格式与 diff 检查通过；宿主未安装 GCC，`CGO_ENABLED=1` 的 race 编译失败，未宣称 race 验证通过。最终全量门禁、应用镜像与恢复/性能验证另行记录。
