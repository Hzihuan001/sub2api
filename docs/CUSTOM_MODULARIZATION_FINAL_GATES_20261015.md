# 模块化最终候选门禁（本地，持续更新）

这份记录用于收口，不是发布许可。目标仍为原 P0–P6 方案；没有推送、标签、GHCR 或生产操作。用户已决定无查询能力的未知生图结果保留待核对，暂不新增管理员处理入口，并暂缓发布。

## 源码与验证坐标

- 已发布源码基线：`989594099c1bd10db911c737449d15d2bb6149ed`，实际 `.5` 镜像运行提交为 `00e8f24a4`。不能混淆源码文档提交与镜像 revision。
- 首个完整单元通过候选：`d16ef65ce18e8d92f7b8f4c9e21a77fd784fac6b`，含运行修复 `8d5dba9a4`、`3e78ebe2b`，独立门禁 `851b49e7a` 与 WS 模拟协议夹具修正 `d16ef65ce`。
- 首个 Docker/恢复候选：`57721bba97234054d1e4a9c3bd13aa078967da2d`。相对 d16 仅为等价 switch 和测试类型断言错误检查；定向 WS、真实 PostgreSQL BatchImageBalance 回归通过（后者 55.762s），差异 lint 0 issues。完整单元结果归属于 d16，不冒称完整 suite 在 577 上再次执行。
- 当前运行候选：`53fd17b5b46be47a216f55381beaac24eefe667f`，恢复已发布故障策略的短路求值，避免计算调用方丢弃的语义状态和容量文案。相对 577 仅 4 个故障策略/测试文件变化；frontend、Go 依赖、Ent/迁移、Wire 输入/产物及审计/账务/恢复源码均未变。受影响回归及新增静态检查单独执行；其余结果按同源码范围复用，不冒称再次全量执行。
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
| 升级维护脚本补充 | 保留上述 17 项，增加 5 项冻结升级基线的离线 mock；最终 22 项全部通过、无 skip，3.791s。性能工具 21 项离线契约通过，含 Windows LF/历史哈希保护；没有新增模型负载 | `python -B scripts/custom_contracts.py --run-tests --group architecture-negative`，`PWSH` 指向已记录的 PowerShell 7；`D:/projects/go-tmp/modular-maintenance-contracts-20261015.log`；`deploy/tests/modular-performance-contracts.ps1` |
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

## 53fd 性能修复与受影响门禁

- 新运行提交 `53fd17b5b` 只修复重构引入的提前解析，不改变故障规则、重试预算或结算逻辑。19 个顶层 / 113 个含子测试通过，其中固定已发布输出 51 项；日志 `D:/projects/go-tmp/gateway-policy-short-circuit-regression-final-20261015.jsonl`，包耗时 1.546s。
- 对受影响 service 包执行 `golangci-lint run --timeout 5m --new-from-rev 57721bba9 ./internal/service`，0 issues；日志 `D:/projects/go-tmp/modular-53fd17-diff-lint-20261015.log`。
- 微基准在同 Go 1.27.2、GOMAXPROCS=2、固定同源夹具、count=5/100ms 下仅复测一轮；原 7 项退化消除，25 项宿主边界最大中位数变化 +5.31%（原门槛 20%），所有 30 组无新增分配。控制组 -2.21%～+4.74%。旧失败记录保留，不以换夹具或降低阈值通过；新原始证据在 `D:/projects/go-tmp/modular-microbenchmark-53fd17b5-20261015`。
- 从干净工作树精确 53fd 构建 `sub2api:modular-53fd17-local`，linux/amd64；image ID `sha256:a11840c20fa0e546ec1518f44762cb51ce448382a38beece0214f46c0e116a11`。构建日志 `D:/projects/go-tmp/modular-53fd17-docker-build-20261015.log`；前端复用同 tree 的构建缓存。没有更改生成输入，复用 577 生成零差异证据，构建后工作树仍干净。
- 精确 53fd 新镜像的 11 故障场景全部通过：overload 主账号 3 次、备用 1 次；quota/model/server/timeout/auth/rate/stream 各 1+1；safety/parameter/partial 各 1+0。命令 `pwsh -NoProfile -File deploy/tests/gateway-failover-smoke.ps1 -Project sub2api-operator-test-failover53fd -Port 18093 -MockPort 18091`，终态 exit 0，日志 `D:/projects/go-tmp/modular-53fd17-failover-smoke-20261015.log`。再次确认二进制 revision、image ID 和 `/health` 后，仅清理这个新建项目的 3 容器、合成数据卷及网络；未动恢复项目和待核对任务。
- 正常 HTTP/SSE 完成预定 4,000 次测量，全部通过 HTTP 200、自然 EOF 和预期标记校验，每个测量窗口精确 100 次上游调用、零额外 probe/reject。旧计时客户端没有独立 JSON/SSE 语法完整性校验，不能外推更强结论。HTTP 总耗时和 SSE 中位总耗时接近旧版，但 SSE 首字节 p95 的窗口中位值 23.806→32.203ms（+35.28%），不能把请求成功当性能通过。后续固定 CBBC 和用户批准的有限 DEBUG 均已结束，结论见下节及 `deploy/acceptance/MODULAR_PERFORMANCE.md`；不再追加负载或删除初次结果。

## 尚未闭环，禁止外推为完成

用户随后批准的有限 DEBUG 已完成：基线/候选各 100 次，完整 SSE、实际 usage 与逐请求日志均齐全，原日志配置已恢复。DEBUG 首字节 p95 48.869→41.849ms，可见账号选择阶段 p95 均 23ms；未复现候选更慢，但不同条件不能推翻下述 INFO / CBBC mixed。只到已有日志可观察边界，不把共享选择墙钟归因为某个 Redis/DB/GC 原因。本轮不再追加负载；完整方法、最后 JSON 证据整理错误及恢复说明见 `deploy/acceptance/MODULAR_PERFORMANCE.md`。

1. 完整 lint 首轮 17 项，其中新 2 项已修；另 15 项所在 9 文件与基线 blob 完全一致，见 [逐项归因](CUSTOM_MODULARIZATION_LINT_BASELINE_20261015.md)。没有以修改错误文案、删除容错测试或禁用 lint 掩盖它们；差异 0 issues 不等于全量绿。
2. 首轮微基准退化已由 53fd 的短路求值修复及同源复验关闭，新镜像最终 11 故障也通过；端到端 SSE 尾延迟差异仍待解释。唯一固定 CBBC 诊断完成 1,200 请求，原始样本 p95 39.921→48.522ms，两对约 +19%，按事先规则属于 mixed，仍未决。两份二进制 Go 版本/依赖/build flags 逐行相同；现有日志和合成 PG 只读关联用于定位，不因此新增负载或改变阈值。全部原始结果保留，不能套用微基准或平均值宣布性能全部通过。
3. Actions 真实可运行、获准后的集成分支推进/草稿 PR 自动链路仍未完成。本轮没有远程写授权；不要把本地脚本执行当 Actions 成功。2026-10-15 再次只读查询 permissions 为 enabled=true / all，最近运行仍为 10-08；未 dispatch，不能据权限开关宣称账号层限制已恢复。

耦合收益按 [逐阶段清单](CUSTOM_MODULARIZATION_COUPLING_20261015.md) 的真实调用/依赖/重复规则计算；没有声称零冲突或减少某个百分比。下一次真实官方升级另行指定版本并记录冲突与耗时。

## 原方案交付核对

| 原要求 | 实际实现/证据 | 状态边界 |
| --- | --- | --- |
| P0 固定基线、行为和协作归属 | 执行记录的 P0 清单、989594099 / v0.2.16、独立 codex 分支、原发布镜像及其他工作树差异 | 不合并旧功能分支，不改变远端集成分支；生产仅引用最近发布记录，不假装已现场复查 |
| P1 可执行模块契约、真实宿主、正负门禁 | `custom/contracts.json`、真实路由/Sidebar/Settings 测试、17 项脚本测试、required_host_calls | 不是只检查文件存在；metadata 不授予生产权限；通用 Host 设置/权限 registrar 不冒充动态业务插件 |
| P2 菜单/品牌/语言/图片共享策略 | frontend feature 元数据、精确语言覆盖、展示适配器、imageGenerationPolicy；完整前端和变体夹具 | 原 URL/授权与技术字段保持；生产无新业务 settings tab，真实定制字段已迁入小组件，不复制官方面板 |
| P3 统一装配、启停与生成 | `customapp/composition.go`、16 条路由、真实 main/Wire 启停、负向调用检查、干净生成及实际重启 | 复用官方审计/批量 worker，不重复所有者；没有 Ent schema 或 SQL 运行迁移变化 |
| P4 审计窄接口、计费和恢复 | `SecurityAuditGate.Check`、原 coordinator/账务事务、真实 PG、参考图/PNG/ZIP/取消/Redis 重投/进程中断证据 | 用户后续批准取消结算独立修复；未知结果保持冻结、不增加管理员解除入口、不补扣历史 |
| P5 决策与执行分离、协议边界 | 纯值 decision/replay，真实发送及 response ID attempt，固定旧版输出、HTTP/SSE/WS 测试、精确 53fd 11 故障 | 不重写转发循环，不新增故障规则/重试预算/正常请求期远程调用；纯策略仍同 service 包，限制已披露 |
| P6 联合验收和升级边界演练 | 真实 PG/Redis/Docker、官方路由重排/新增、真实设置字段 patch、真实工作台能力变化、生成零差异 | 完整质量的 skip 与基线 lint 欠账单列；端到端性能剩余结论见性能报告，不用局部成功替代 |
| 维护交付、共享日志、效果指标 | 本文件、执行记录、耦合清单、`custom/README.md`、项目状态/功能日志/升级问题记录及根目录指针 | 记录真实接入点/依赖/重复规则，不声称已证明未来冲突减少百分比 |
| 远端自动升级 | workflow / 同步脚本本地接入，权限只读核验 | 未实际触发，不能称链路完成；原方案明确允许已验证本地代码阶段单独交付 |
| 发布与生产 | 当前不执行 | 用户暂缓发布，需新授权；不以本地验收自动推送/打标签/部署 |

本表核对的是原范围，不以新增目录或接口数量作为完成标准；性能未闭环前仍不能将 P0–P6 整体验收标为完成。

## 自动升级 workflow 离线复核（2026-10-15，本地）

- 原范围复核固定 `260f3c1b6` / 运行 `53fd17b5b`，二者 backend/frontend 无差异。当前分支后续 `caa717338` 是独立分组规则功能；不能继承原完整 suite、16 路由或 Docker/性能验收作为新功能发布证据。本节只改 workflow、离线测试、契约清单与文档。
- 修复报告反引号命令替换、缺 bot 身份、switch/commit 错误继续、未提交 MERGE_HEAD 连续合并、带斜杠补丁日志路径；PR 正常标题要求 merge/check outcome 与输出均成功，异常或缺失结果明确 blocked。
- 真实 YAML Bash 在临时目录运行，Git/gh 使用 shell 替身，PATH 清空只允许本地 awk/sed/cp 包装；每场景 15 秒上限，无网络、凭据或远端写入。覆盖字面量报告、提交顺序、异常状态、补丁冲突及 no-op。
- **最终定向门禁**：Python 3.12 执行 `python scripts/custom_contracts.py --run-tests --group architecture-negative`，6 模块 / 8 可执行分组结构检查通过；原 22 项加 workflow 11 项，共 33 项、6.443 秒、0 失败 / 0 skip。日志 `D:/projects/go-tmp/upstream-workflow-contracts-20261015.log`；`git diff --check` 通过。测试包含已包含补丁不空提交且继续下一补丁的情况。
- **远端只读事实**：`actions/permissions` 为 enabled/all，workflow 默认 write、允许 PR；全仓最近执行仍 2026-10-08，升级 workflow 最近成功为 2026-10-05（run `37296118305`）。远端 `custom/integration=fd7c1b91446a38666b7f5c32ab9d493f6431d071`，到原收尾点 `260f3c1b6` 图差异 0/296；开放草稿 PR #1 仍旧 isolation 分支。未 dispatch、未推进分支；不能据权限正常声称账号层限制或整条链路已恢复。
- **P4 证据限制补充**：现有真实 PG / 候选 Docker 证明正确性与恢复，另有旧新取消缺陷复现；未找到完整同一组合夹具的旧新任务/账目逐项对照矩阵。不声称该实验已完成，也不以此追加负载；用户授权的取消修复本来就有意改变旧缺陷结果。
- 原 INFO/CBBC mixed、15 项基线 lint 和用户暂缓发布保持；没有应用源码变化，不重跑已通过的后端/前端套件，不增加性能实验。
