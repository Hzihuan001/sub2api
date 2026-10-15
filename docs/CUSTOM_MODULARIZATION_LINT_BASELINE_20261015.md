# 模块化全量 lint：源码基线归因（2026-10-15）

结论：d16 全量日志的 17 项中，本轮新增 2 项已由 `57721bba97234054d1e4a9c3bd13aa078967da2d` 修正；其余 **15 项所在的 9 个文件，在源码基线 `989594099`、候选 `d16ef65`、当前 `57721bba9` 三者 blob 完全一致**。本清单没有发现剩余项属于本轮新增回归；**全量 lint 仍未取得全绿证据**，不能把“基线已有”写成门禁通过。

证据来源：`D:/projects/go-tmp/modular-d16-full-lint-20261015.log`，SHA256 `c7b4dcfcc38d6dee3c7a26dbdb501c552c12cf88e053166a501e30b5f608a130`。执行了逐文件 `git rev-parse <ref>:<path>`、基线 `git show` 上下文、基线/当前 `git grep` 引用核对，以及只读 `gofmt -d`；未修改运行代码、未重跑全量 lint。下表路径均相对 `backend/internal/service/`，行号与日志/基线一致；短 blob 可用 Git 解析为完整对象。

| # | 文件:行 | 告警 | 三版本相同 blob | 归因 / 风险性质 |
| --- | --- | --- | --- | --- |
| 1 | `openai_ws_ingress_overload_test.go:98` | errcheck / `conn.CloseNow` | `bc3a158412dcf` | 基线测试清理未处理关闭错误；可能掩盖清理失败，不是已证实运行时缺陷。 |
| 2 | `openai_ws_ingress_overload_test.go:122` | errcheck / `clientConn.CloseNow` | `bc3a158412dcf` | 同上，客户端清理路径。 |
| 3 | `batch_image_public.go:167` | gofmt | `27066bb05e6f` | 纯字段/初始化项对齐；只读格式 diff 无语义变化。 |
| 4 | `batch_image_size.go:10` | gofmt | `6230ccf06e29` | 纯常量对齐。 |
| 5 | `composite_platform_test.go:117` | gofmt | `aed56829ac5f` | 纯构造参数注释对齐。 |
| 6 | `openai_account_runtime_block_fastpath.go:101` | QF1001 | `df0b69c5c8cc` | 德摩根等价写法建议，不是条件计算错误的证据。 |
| 7 | `openai_images.go:258` | ST1005 | `49ddcfbf928b` | `Seedream…n=1` 错误字符串首字母风格；不改变请求校验。 |
| 8 | `openai_images.go:261` | ST1005 | `49ddcfbf928b` | `Seedream…quality` 字符串风格。 |
| 9 | `openai_images.go:264` | ST1005 | `49ddcfbf928b` | `Seedream…input_fidelity` 字符串风格。 |
| 10 | `overload_failure_breaker_test.go:232` | SA1012 | `8f50d9675bde` | 故意测试 nil receiver / nil Context 的容错返回。 |
| 11 | `overload_failure_breaker_test.go:234` | SA1012 | `8f50d9675bde` | 故意测试 nil receiver 的清理操作安全返回。 |
| 12 | `overload_failure_breaker_test.go:253` | SA1012 | `8f50d9675bde` | 故意以 nil Context 测设置禁用/配置/读取失败回退。 |
| 13 | `ratelimit_service.go:2622` | QF1001 | `ce948384ca6a` | 双否定的等价简化建议；不是已证实调度回归。 |
| 14 | `channel_monitor_checker.go:359` | unused / `callProvider` | `8051683d9942` | 基线已无调用点的遗留函数；基线与当前全仓引用均只有定义。 |
| 15 | `ratelimit_service.go:1507` | unused / receiver wrapper | `ce948384ca6a` | 基线已有测试使用的包装方法；生产调用的是无 receiver 的同名函数，不能混为被删掉的业务调用。 |

语义交叉核验：#10–12 的测试名是 `TestAccountOverloadBreakerSettingsAndNilDependencies`；`overload_failure_breaker.go:40–81` 对 nil receiver 提前返回，并将 nil Context 替换为 `context.Background()`，该实现基线/当前 blob 同为 `4100cb5f209a`。#15 的 wrapper 只转调包级函数，8 处调用在带 `//go:build unit` 的 `ratelimit_service_openai_test.go`，其基线/当前 blob 同为 `ba4324d7f9ee`；本次未将该 lint 日志未记录的构建参数臆断为根因。故这 15 项包含真实的测试清理错误检查欠账、格式/风格、刻意的容错测试和遗留/测试包装函数，不能笼统称作“全是格式”，也没有足够证据把它们升级成业务 bug。

新增且已修的 2 项：`openai_ws_forwarder_ingress_session_test.go:1284` 的直接类型断言改为 `session, ok` + 显式断言（测试失败可诊断性）；`repository/usage_billing_repo.go:159` 的 QF1003 改为 tagged switch（等价风格）。`git show 57721bba9` 可核对仅这两处；`modular-d16-followup-diff-lint-20261015.log` 为 `0 issues.`，它是增量门禁结果，不是当前完整 lint 的替代证据。

复核方式（对上表每个路径分别执行；基线完整提交 `989594099c1bd10db911c737449d15d2bb6149ed`，候选 `d16ef65ce18e8d92f7b8f4c9e21a77fd784fac6b`）：

```powershell
git rev-parse 989594099:backend/internal/service/<file> d16ef65:backend/internal/service/<file> 57721bba9:backend/internal/service/<file>
git show 989594099:backend/internal/service/<file>
git grep -n -E 'callProvider\(|calculateOpenAI429ResetTime\(' 989594099 -- backend
git grep -n -E 'callProvider\(|calculateOpenAI429ResetTime\(' 57721bba9 -- backend
```
