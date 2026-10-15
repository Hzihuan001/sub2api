# 模块化最终候选门禁（本地，持续更新）

这份记录用于收口，不是发布许可。目标仍为原 P0–P6 方案；没有推送、标签、GHCR 或生产操作。用户已决定无查询能力的未知生图结果保留待核对，暂不新增管理员处理入口，并暂缓发布。

## 源码与验证坐标

- 已发布源码基线：`989594099c1bd10db911c737449d15d2bb6149ed`，实际 `.5` 镜像运行提交为 `00e8f24a4`。不能混淆源码文档提交与镜像 revision。
- 首个完整单元通过候选：`d16ef65ce18e8d92f7b8f4c9e21a77fd784fac6b`，含运行修复 `8d5dba9a4`、`3e78ebe2b`，独立门禁 `851b49e7a` 与 WS 模拟协议夹具修正 `d16ef65ce`。
- 当前候选：`57721bba97234054d1e4a9c3bd13aa078967da2d`。相对 d16 仅为等价 switch 和测试类型断言错误检查；定向 WS、真实 PostgreSQL BatchImageBalance 回归通过（后者 55.762s），差异 lint 0 issues。完整单元结果归属于 d16，不冒称完整 suite 在 577 上再次执行。
- frontend tree：`9abed689f5b2b6c33afc8e5cfa8a1ee3864d3183`。前端完整验证过程中无源码、配置或 lockfile 变化。
- 工具链：宿主 Go 1.27.2 windows/amd64、Node 20.20.2 / pnpm 9.15.9、golangci-lint 2.14.0、Docker Desktop Linux。容器构建沿用原 Dockerfile，其 Node builder 为 Node 24，不把宿主 Node 20 验证冒充镜像 builder 版本。

## 已取得的具体证据

| 门禁 | 结果与边界 | 原始证据 |
| --- | --- | --- |
| 后端完整 unit | d16ef65：63 包通过，12,537 顶层 / 24,232 含子测试通过，0 失败，22 skip | `D:/projects/go-tmp/modular-d16-full-unit-20261015.jsonl` |
| 前端完整 | frozen-lockfile、typecheck、lint:check（0 error / 2 warning）、409 文件 / 3176 Vitest、生产 build 全通过 | `D:/projects/go-tmp/modular-frontend-final-20261015-summary.log` 及该索引中的各日志 |
| Wire / Ent / 迁移 | 独立干净工作树精确 d16ef65；go generate 后全仓零差异，SQL 相对基线无变化，append-only/清单/保护路径通过 | `D:/projects/go-tmp/modular-p6-generation-d16ef65-20261015.log` 及 `-verification.log` |
| 最终生成复核 | 独立干净工作树精确 57721bba9；Wire/Ent generated sources clean、迁移 append-only、清单/隔离检查通过 | `D:/projects/go-tmp/modular-p6-generation-57721-20261015.log` |
| 真实审计集成 | 新建本地临时 PG/Redis，11 个需外部地址的集成用例实际通过；全包 119 顶层 + 135 子例，0 skip/失败，9.285s；临时容器已按归属清理 | `D:/projects/go-tmp/modular-securityaudit-real-20261015-summary.log` 及同名前缀 JSONL/lifecycle 日志 |
| 新增差异静态检查 | 57721bba9 相对 989594099，0 issues；不是全量 lint 零问题 | `D:/projects/go-tmp/modular-d16-followup-diff-lint-20261015.log` |
| 架构检查 | 6 模块 / 7 可执行组，17 项脚本测试（含正例与解析器，不全是负例）通过 | `851b49e7a`；`python -B scripts/custom_contracts.py`，`python -B -m unittest discover -s scripts -p test_custom_contracts.py` |
| 官方路由变体 | 生产子路由 registrar/composition：4 条官方路由新增、反序后 16 条定制路由各一次；漏挂载检测、默认拒绝、实际限流在审计前通过 | `backend/internal/server/routes/custom_upgrade_variation_test.go`；3 顶层 / 3 布局子例通过 |
| 官方设置新增 | 临时副本真实增加 general 字段及 API 类型；6 项负控准确失败、15 项正控及类型检查通过；活动 frontend 不变 | `custom/fixtures/settings-upgrade/` 两份 patch 与复跑说明；`D:/projects/go-tmp/p6-settings-20261015/verified-run/` |
| 独立 `.5` 故障对照 | 验证原 989594099 源码后只读 overlay 采集 50 个固定输入；当前 wrapper 对照通过，不使用新 decision 自产期望 | `backend/internal/service/testdata/gateway_failure_published_989594099.README.md` |
| 取消结算恢复 | 真实 PG：正常/明确失败、capture/usage/完成确认丢失、旧重试阈值、live/archive、capture-release 并发及别的任务 hold 保护通过 | [账务恢复报告](BATCH_CANCEL_RECOVERY_LOCAL_20261015.md)；这不是进程 kill/restart 或最终应用镜像验证 |

完整 Go 命令：

```text
GOMAXPROCS=4 go test -json -p 2 -tags=unit ./... -count=1 -timeout 20m
```

Windows 测试进程 PATH 包含锁定 Go 和 `G:/PC/Git/bin`，后者满足 PgDumper 的 sh 依赖。22 个 skip 包括 11 个需要独立 PG/Redis 地址的 securityaudit 集成用例、外部 TLS/OpenAI/插件环境用例、只在显式基线采集时运行的 capture 测试以及已有 skip；不能将 skip 算作通过。其中 11 个真实审计集成已按上表另行验证，其余环境性 skip 保留。

首次完整 Go 的 4 个 WS 失败原因：模拟上游对 `session.update` 返回了假的 `response.completed`。修正只动两份测试文件，回 `session.updated` 且仅 response.create 生成 usage；原白名单/1008 断言保留，并增加无 create 的孤立终态必须 1013 的负例。相关 61 个顶层通过后才重跑完整 suite。

前端首次构建：Node 20 无法解析实际存在、长度 264 的 pnpm 嵌套依赖路径。使用 `pnpm install --frozen-lockfile --config.virtual-store-dir-max-length=60` 后，同 Node/pnpm 构建通过；没有更改锁文件、换 Node 或删断言。

## 最终源码镜像与真实容器（2026-10-15）

- 候选运行源码：`57721bba97234054d1e4a9c3bd13aa078967da2d`；独立干净工作树生成后构建 `sub2api:modular-57721-local`，linux/amd64。镜像 ID `sha256:264242a7a7664b8e285cebd5c464825d3dafd8e709e230123ee30f61270424eb`，容器 `/app/sub2api --version` 返回同一 commit。构建日志 `D:/projects/go-tmp/modular-57721-docker-build-20261015.log`。
- 角色：新建 `sub2api-operator-test-modular-final` / 18094，admin/operator/user、API Key、允许/拒绝接口、混合批量原子拒绝、WS 与重启持久化通过。结束后仅该新建合成项目的容器、网络、卷按脚本清理，未删除既有项目。日志 `modular-57721-role-container-20261015.log`。
- 生图：在既有隔离 `sub2api-operator-test-modular` / 18089 仅重建应用后，单张/批量、参考图、PNG/ZIP、重试幂等与取消批量五折验收通过；批量 3 张实扣 `0.03`，合成上游共 4 次（含单图），批量最大并发 3。日志 `modular-57721-image-smoke-20261015.log`，工件 `C:/Users/a/AppData/Local/Temp/moshu-image-smoke-98962ab010`。
- 故障：同候选镜像完成 overload/quota/model/server/timeout/auth/rate/stream/safety/parameter/partial 共 11 场景；overload 主账号 3 次后备用 1 次，安全/参数/部分输出均无备用重放。日志 `modular-57721-failover-smoke-20261015.log`。
- 上述日志均位于 `D:/projects/go-tmp/`。最终应用在 3 次故意 SIGKILL 后恢复 healthy；03:55:20Z 之后日志无 panic/fatal/迁移失败/数据库或 Redis 连接失败匹配。没有访问生产或真实付费上游。

### 实际中断恢复，不用替身代替进程

验收脚本与设置新增演练工件提交 `a44618924`，没有运行源码变化。恢复脚本复审补齐原子队列快照、实际 PG/Redis 卷归属与全合成 key usage 核验；28 项无 Docker 契约测试通过后才执行。

执行 `deploy/tests/image-studio-recovery.ps1 -Execute -ExpectedImageId sha256:264242a7a7664b8e285cebd5c464825d3dafd8e709e230123ee30f61270424eb`：2026-10-15 03:58:09–03:59:20 UTC，退出码 0、status=passed；同一应用容器 3 次 SIGKILL 均验证退出 137，并以新的进程启动时间恢复。

| 场景 | 实际结果 |
| --- | --- |
| 图片已 fsync、结算前被杀 | 从真实 `.jsonl.partial` 恢复成功，PNG 经公开下载接口可读；上游仅 1 次，capture/usage 各 1 次，实扣 `0.01`，冻结归零 |
| 完成后真实 Redis 重复投递 | 运行时 2 次、停止后 3 次投递并重启；完整账务/结果快照不变，无重复调用、扣款或 usage |
| 已发出但没有任何结果 | 真实连接因进程退出中断；恢复及额外投递后仍不重发、不 capture、不 release、不生成 usage，保留 `0.01` 待核对冻结；全 key 只有前一成功任务的 1 条 usage |

证据：`C:/Users/a/AppData/Local/Temp/moshu-image-recovery-eed2b3abd8/evidence.json`、`durable-before-kill.jsonl`、`durable-recovered.png`、`upstream-final.json`；聚合日志 `D:/projects/go-tmp/modular-57721-recovery-20261015.log`。成功 batch `imgbatch_2f6af3baba6be6fff5ed02de78968c53`，未知 batch `imgbatch_fbe8afe8bf9dde0a7f23dacce63efa68`。测试账号/任务和 mock 保留，未知任务没有被退款、删除或伪造完成；不要直接重跑脚本，它会拒绝其他运行留下的未完成任务。

边界：仅在应用停止后加速这两个测试任务的 Redis 过期租约，不声称自然 TTL 等待通过；数据库行锁只控制真实 fsync 后的时间窗口，没有手造成功状态。capture→usage 的极短进程中断窗口未直接注入，相关确认丢失/并发由独立真实 PG 测试覆盖；未知结果并未自动核对完成。旧 57ed 镜像的局部验收不再冒充最终候选。

## 尚未闭环，禁止外推为完成

1. 完整 lint 首轮 17 项，其中新 2 项已修；另 15 项所在 9 文件与基线 blob 完全一致，见 [逐项归因](CUSTOM_MODULARIZATION_LINT_BASELINE_20261015.md)。没有以修改错误文案、删除容错测试或禁用 lint 掩盖它们；差异 0 issues 不等于全量绿。
2. 首轮性能门禁未通过：同源 overlay 两轮计时发现宿主决策的提前解析增加分配，普通 HTTP overload 中位数 +71.98%、10→11 alloc，request 的流式失败 +53.03%、6→9 alloc；未变分类器控制组基本持平。详见 `deploy/acceptance/MODULAR_PERFORMANCE.md`。只恢复原短路求值，保持纯策略和既有行为；修复后再复验，不更改阈值或基准输入。正常 HTTP/SSE 容器对照尚未执行。577 的既有正确性结果保留，不等于性能通过或新候选已验。
3. Actions 真实可运行、获准后的集成分支推进/草稿 PR 自动链路仍未完成。本轮没有远程写授权；不要把本地脚本执行当 Actions 成功。2026-10-15 再次只读查询 permissions 为 enabled=true / all，最近运行仍为 10-08；未 dispatch，不能据权限开关宣称账号层限制已恢复。

耦合收益按 [逐阶段清单](CUSTOM_MODULARIZATION_COUPLING_20261015.md) 的真实调用/依赖/重复规则计算；没有声称零冲突或减少某个百分比。下一次真实官方升级另行指定版本并记录冲突与耗时。
