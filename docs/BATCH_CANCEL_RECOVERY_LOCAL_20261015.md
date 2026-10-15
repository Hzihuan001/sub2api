# 批量取消与结算恢复：本地验证记录

日期：2026-10-15。此记录属于模块化验收发现问题后的独立修复，不代表 P0–P6 全部通过，也不是推送或生产发布授权。

## 用户确认的边界

- 未发出的请求停止，不收费；已经发出的请求不因取消退款，按结果正常结算；上游明确生成失败不收费。
- 已发送但结果不可确认、又无上游查询接口时，保留待核对和预留金额，不自动重发、不擅自扣款或退款。
- 用户明确暂不增加管理员人工确认接口/按钮，并暂缓发布。本次未新增此入口、数据表或迁移，不补扣或重算历史费用。

## 本次独立恢复补丁

- 在 `55765c473` 的取消保留结果实现上，结算顺序改为：原幂等 capture → 确认使用记录持久化 → 标记任务完成。写入使用记录失败必须继续恢复，不能吞掉错误后把任务标为完成。
- 所有 settling 任务始终重试相同 capture ID。零成功时 capture 金额为 0，原事务一次性返还预留；错误次数达到旧阈值 5 也不另发 full release，避免 capture 已提交但确认丢失时二次退款。
- 原账务事务内以任务和 API Key 为范围串行化 reserve/capture/release，锁等待最多 5 秒。现有 dedup/live/archive 记录用于阻止 capture 与 release 交叉执行；同操作重放仍幂等。不改变普通网关请求路径或正常请求期远程调用。
- 正预留 capture 必须有该任务自己的 reserve；已经释放的旧任务不能用其他任务的冻结余额再扣一次。零金额免收费任务仍无需 reserve。
- 历史 completed 任务保持只读，未重写价格快照、余额或旧使用记录。旧版 release 已提交但状态未完成的异常任务会拒绝 capture 并保留待核对，不伪造恢复完成。

## 真实数据库证据

工具链：Go 1.27.2 Windows，Docker Desktop Linux 29.8.2；临时 PostgreSQL 18.1-alpine3.23、Redis 8.4-alpine；合成用户、余额、Key、账号和 mock HTTP 上游，无生产连接或真实生图费用。

```text
CI=true go test -tags=integration ./internal/repository -run '^(TestBatchImageCancellation_PostgresAccountingRecovery|TestBatchImageBalance)' -count=1 -v
```

- PASS，21.284 秒。日志：`D:/projects/go-tmp/batch-cancel-terminal-pg-20261015.log`。
- 完整 provider → processor → settlement 路径：取消后迟到成功、明确失败、capture 提交确认丢失、usage 提交确认丢失、完成状态提交确认丢失，共 5 个场景通过。
- 第 6 个场景在旧重试阈值边界注入零成功 capture 确认丢失，并让同一用户另有 0.75 预留：该任务完成且费用为 0，另一任务的 0.75 原样保留，没有 release 记录。
- 仓储终态互斥覆盖 8 组 live/archive × 零/正费用 × capture/release 顺序，以及无 reserve、免费任务和 12 个并发终态操作。只有一个终态资金操作生效，其余同操作重放或异操作拒绝；未动用其他任务的预留。
- 这证明真实数据库提交后的恢复和幂等，不冒称已经完成 OS 进程强杀、Redis 重投、完整应用重启或最终镜像验收。

## 单元回归

```text
go test -json -p 2 -tags=unit ./internal/service ./internal/repository ./internal/handler -run 'BatchImage' -count=1 -timeout 10m
```

三个包全部通过，113 个顶层测试 / 含子测试 316 项、失败 0：service 18.192 秒、repository 0.585 秒、handler 0.576 秒。日志：`D:/projects/go-tmp/batch-cancel-candidate-unit-20261015.jsonl`。另有 settlement 定向回归通过（1.498 秒）；前序取消输出、101/501 项分页和未知结果测试仍保留。

初次新增 PG 夹具错误地用带连字符的 UUID 拼接任务 ID，capture request ID 超出 usage 表 64 字符限制。已改用生产 `NewBatchImageID()` 并断言长度；没有修改生产字段限制来迁就夹具。

## 剩余门禁

最终候选完整静态/测试、应用进程恢复与同负载性能、最终 Docker 镜像仍需验证。未知上游结果与旧异常已释放任务保持待核对；不增加未经批准的管理功能。本轮没有推送、标签、GHCR 或生产操作。
