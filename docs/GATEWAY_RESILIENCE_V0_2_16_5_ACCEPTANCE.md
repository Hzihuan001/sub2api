# 网关故障切换验收 — 0.2.16-custom.5

## 发布坐标

- 官方基线：v0.2.16；上一生产版本：0.2.16-custom.4。
- 运行代码：`00e8f24a40eba481d1e61cfebb5f4ab3f9401a2b`。
- 计划源码标签：`custom-0.2.16.5`，指向上述运行提交；后续验收脚本和文档提交不改变运行产物。
- 工作分支：`codex/v0.2.16-image-studio`。
- 本条记录时本地验收通过，尚未推送镜像或部署生产；发布结果在本文末尾追加。

## 行为与边界

- 同一 OpenAI 账号滚动 5 分钟累计 3 次明确 overload，复用既有过载冷却配置，默认 10 分钟；跨请求计数，一次上游尝试只计一次。冷却后按原有健康、权限、模型和优先级/评分选择备用，不改高级调度权重。
- 硬额度不足、凭据无效、模型不可用不继续原账号盲重试；额度没有 reset 时采用至少 10 分钟可恢复冷却，凭据保留原刷新/失效策略。
- 普通瞬时 OAuth 429 原账号恢复窗口最多 8 秒，尊重 Retry-After；等待较长时换号。普通 5xx 使用既有账号×模型 10/45 秒冷却，pool 不再忽略；520–524 首错 90 秒，504/52x 不反复等待同一账号。
- HTTP、透传、SSE、WS 共享错误语义保护。参数、上下文、内容安全拒绝不通过换号重放；真实内容已经输出、客户端取消、插件已提交等边界不放宽。
- 不改变计费、分组授权、模型权限、生图和数据库结构；没有新迁移。其它提供商原生协议保留原策略，不宣称全部协议已重写。
- Redis 原子滚动计数，现有 overload_until 持久化；正常请求不新增远程查询。缓存故障使用有界本地备用状态。

## 本地验证（2026-10-14）

受影响 Go service/handler/repository/custom 测试通过；HTTP、429、过载、SSE 心跳与输出边界、WS 配对事件、Redis 并发/窗口/恢复均覆盖。扩大回归时发现旧 403 ip_blocked 与 Codex plan-gated 模型兼容用例，已修复并通过定向回归。四包 go vet、gofmt、git diff --check 通过；定制隔离检查通过，新增 9 个保护路径。生成代码、迁移和前端无本次改动；未重复声称完整前端/全量 CI 新跑通过。

Linux/amd64 正式版本 Docker 构建通过；镜像内版本及 commit 与上述坐标一致。隔离项目 `sub2api-operator-test-v0216`，应用 `http://127.0.0.1:18086`，独立 PostgreSQL/Redis，模拟上游不产生真实模型费用。

运行 `deploy/tests/gateway-failover-smoke.ps1` 的结果：

| 场景 | 主账号尝试 | 备用尝试 | 结果 |
| --- | ---: | ---: | --- |
| overload | 3 | 1 | 成功；持久化冷却，下一请求不再调用主账号 |
| quota | 1 | 1 | 成功 |
| model not found | 1 | 1 | 成功 |
| server 502 | 1 | 1 | 成功 |
| timeout 524 | 1 | 1 | 成功 |
| auth 401 | 1 | 1 | 成功 |
| rate 429 / Retry-After 30 | 1 | 1 | 成功 |
| SSE 首内容前 server_error | 1 | 1 | 成功 |
| safety | 1 | 0 | 未重放 |
| parameter | 1 | 0 | 未重放 |
| partial output + failed | 1 | 0 | 已有内容保留，未重放 |

测试夹具独立处理能力探测，不将自动 probe_ping 算作用户调用；等待默认一秒调度 outbox 进入快照后再注入故障，避免把新建账号异步可见性误判为故障切换失败。合成账号/用户/分组通过 API 清理，mock 容器移除；不清理既有数据卷。

本地证据：`D:/projects/go-tmp/gateway-failover-docker-smoke.log`、`gateway-resilience-final.log`、`gateway-resilience-final-compat.log`、`gateway-resilience-docker-build.log`。这些是本机文件，不含生产凭据，不作为仓库附件上传。

## 备份和回滚边界

发布前备份：`/opt/sub2api-backups/v0.2.16-custom.5-20261014T120809Z`，三站 PostgreSQL dump、应用数据和部署配置均已保存；pg_restore 目录、tar 列表、SHA256SUMS 校验通过。

旧镜像：`ghcr.io/hzihuan001/sub2api@sha256:a4b7d421e233a1484c92b02780fd2a2421d4ea450fc2a184c82ac253289c045e`。

仅重建应用，数据库、Redis、网络、卷及反代保持。回滚只恢复旧镜像引用并 `up -d --no-deps` 对应应用服务，不用旧数据库覆盖新业务数据。临时冷却状态到期自动恢复；现有管理恢复操作可清除状态。

## 已知限制

- 上游已输出内容后无法无缝切换续写，本次不自动重放造成重复输出/计费。
- 没有健康、授权且支持同一模型的备用账号时仍可能失败；不跨分组兜底或绕过权限。
- 持久化失败时本实例仍暂避，DB 补偿依靠后续 overload 信号，未增加后台扫描。
- 当前三站各单应用实例；未来多副本运行时，手动清理不会广播其它进程的本地冷却，它们可能保持到原截止。
- 新账号变更通过已有 outbox 异步进入调度快照；此次没有扩大为调度缓存重构。
