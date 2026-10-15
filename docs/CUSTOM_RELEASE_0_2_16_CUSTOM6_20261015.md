# Moshu 0.2.16-custom.6 发布记录

日期：2026-10-15，Asia/Shanghai。三站发布已完成；源码、标签、镜像与以下生产结果已核验。文档收尾提交不改变运行镜像。

## 范围与授权

- 用户批准本轮满足上线条件后发布，并在 GitHub Actions 实测禁用后明确批准沿用本地构建、GHCR、回拉验收和三站部署流程。
- 本轮包含定制层模块化、批量取消结算及恢复修复、分组错误切换规则和手动恢复；不包含 API Key 主备分组，不合并新官方版本。
- SSE mixed 风险已接受，不再追加压测；不是宣称已证明零性能差异。

## 不可变坐标

| 项目 | 值 |
| --- | --- |
| 官方基线 | v0.2.16 / `01842576b3d5c1e2545b5a99ad555d36e28ed306` |
| 发布版本 / 标签 | `0.2.16-custom.6` / `custom-0.2.16.6` |
| 源码分支 | `codex/group-failover-rules-v0.2.16` |
| 运行提交 | `655a16868fc310d6dcc742c4bb60a2f740456a58` |
| 镜像 | `ghcr.io/hzihuan001/sub2api:0.2.16-custom.6` |
| 不可变 digest | `sha256:cb074f11766d9190f4e5f3479a0750c7a759a562bfc142aa3067a0d4bfbfc42e` |
| 架构 / 构建时间 | linux/amd64 / `2026-10-15T09:36:44Z` |
| 回滚版本 | `0.2.16-custom.5` / `00e8f24a40eba481d1e61cfebb5f4ab3f9401a2b` |
| 回滚 digest | `sha256:3a2f925574266819adc3cddad9595170cd71404519f315326f83d61098b6a80b` |

标签指向运行提交；后续文档提交不改变该镜像。本次没有新 SQL/Ent 迁移或依赖升级。

## 已完成验证

- 复用与当前运行源码匹配的完整及定向门禁、真实 PostgreSQL/Redis、批量账务恢复、分组规则和协议测试，详见 [最终门禁](CUSTOM_MODULARIZATION_FINAL_GATES_20261015.md)。两项独立只读复核未确认新增 P1/P2 正确性阻断。
- 从干净 `git archive 655a16868` 构建，标准 Dockerfile 复用已验收前端缓存，重新编译 release 二进制。版本、commit、镜像标签和 amd64 架构核对通过。
- GHCR 发布后按 digest 回拉，回拉产物的 image ID 与本地已验收产物一致，manifest digest 与记录值一致。执行 `pwsh -NoProfile -File deploy/tests/operator-container-test.ps1 -Project sub2api-operator-test-release6-20261015 -Port 18097 -Image ghcr.io/hzihuan001/sub2api@sha256:cb074f11766d9190f4e5f3479a0750c7a759a562bfc142aa3067a0d4bfbfc42e -UsePublishedImage`：角色/JWT/Admin API Key、允许拒绝、混合批量原子拒绝、WebSocket、重启持久化与关键日志通过，退出 0。
- 本次临时角色验收容器、卷和网络由脚本按唯一项目清理，仅含合成数据；此前分组、性能、恢复项目及证据未删除。没有真实付费模型请求或新性能压测。
- GitHub `custom-release.yml` 实测 dispatch HTTP 422 `Actions has been disabled for this repository.`，没有 CI 运行。用户明确批准的本地路径不冒称远端 CI 成功。

## 生产备份

路径：`/opt/sub2api-backups/v0.2.16-custom.6-20261015T093931Z`，仅服务器 root 可读，凭据不进入 Git。

- 三站 PostgreSQL 均 `pg_dump -Fc`，随后实际 `pg_restore --list` 可读并含 TABLE DATA。
- 三站 Compose、`.env`、应用持久化目录以及 OpenResty 配置/证书目录已归档，`tar -tzf` 通过；各文件 SHA-256 记录在 `manifest.json`，`verified=true`。
- 保存旧容器信息、镜像 digest、迁移指纹和批量状态。备份为当前线上快照，不恢复旧库覆盖发布后的业务数据；此处只验证备份可读，不冒称执行过整库恢复演练。

| 站点 | 数据库 dump 字节 | 应用/配置归档字节 |
| --- | ---: | ---: |
| 主站 | 95,737,806 | 183,152,432 |
| L1 | 11,496,042 | 10,624,171 |
| COS | 3,073,198 | 2,938,382 |

## 逐站部署结果

发布前只读现场三站 `.5` 均 healthy、重启 0；主站批量状态仅 completed/failed/output_deleted，分站无批量任务。应用重建前再次读取请求槽位及等待队列，等待空闲而非主动中断已有请求。

主站实际等待请求槽位从 1–2 降为 0 后更新，没有强制中断正在处理的请求；L1、COS 开始前槽位与等待队列均为 0。严格主站通过后进入 L1、L1 通过后进入 COS。

| 站点 | 北京时间 | 应用服务 | 结果 |
| --- | --- | --- | --- |
| `ai.moshu.cloud` | 17:49:57–17:50:04 | `1Panel-sub2api-bjGj` / `sub2api` | passed |
| `api.moshu.cloud` | 17:50:36–17:50:44 | `api-L1` / `api` | passed |
| `cos.moshu.cloud` | 17:51:06–17:51:17 | `api-COS` / `api` | passed |

三站均仅替换 Compose 应用镜像为同一 digest，并执行 `up -d --no-deps --pull never <应用服务>`。逐站核验：

- healthy、重启计数 0，二进制版本/完整 commit 与上述发布坐标一致。
- 环境变量、端口、挂载、网络指纹不变；PostgreSQL/Redis 容器 ID 与 StartedAt 不变。迁移记录数量及全行指纹不变。
- 本地和公网 `/health`、`/image-studio` 均 HTTP 200；健康正文 status=ok。
- 匿名访问 `/api/v1/admin/settings` 与新 `/api/v1/admin/groups/1/failure-policy` 均 HTTP 401。
- 启动日志 panic/fatal/迁移失败/数据库或 Redis 连接失败匹配 0。逐站受限原始结果为备份目录中的 `main-release-result.json`、`l1-release-result.json`、`cos-release-result.json`。
- 三站没有现成的全局 Admin API Key，因此没有新建密钥或伪造生产登录。完整角色/JWT/API Key/WebSocket 权限验收来自最终 GHCR 镜像的本地容器，不冒称生产管理员/普通用户已逐一登录，也没有实际付费模型或生图调用。
- 17:54:31（北京时间）再次复核：三站同 digest、healthy、重启 0，启动后关键错误匹配仍为 0。只是针对性快速验收，不是长时间稳定性观察。

## 保留限制

- 发送后结果未知且无上游查询能力时，继续保留待核对与预留金额，不自动重发、扣款或退款；暂不增加管理员确认入口。
- 原 SSE INFO/CBBC 为 mixed，有限 DEBUG 未复现稳定退化；用户接受该未决风险，原始数据不改写。
- 全量 lint 的 15 项基线欠账仍单列，差异 lint 通过不等于全量绿。
- P4 未补完整同组合夹具的旧新账目逐项双跑矩阵；已有实际正确性/恢复证据，但不把有意修复的旧取消缺陷包装成完全等价。
- GitHub Actions 和真实自动升级 PR 链路尚未完成验证；未追新版本、未做生产故障注入或长时间负载测试。

## 回滚

仅将相应应用服务镜像恢复为上述 `.5` digest，再在对应目录运行应用单服务重建：

```sh
# 先将 docker-compose.yml 中该应用的 image 精确恢复为上方 .5 digest。
cd /opt/1panel/apps/sub2api/sub2api
docker compose config --quiet
docker compose up -d --no-deps sub2api

cd /opt/api-L1
docker compose config --quiet
docker compose up -d --no-deps api

cd /opt/api-COS
docker compose config --quiet
docker compose up -d --no-deps api
```

只回退需要回退的站点；关键检查失败先停止后续站点。无需改数据库、Redis、卷、网络或反代；不要恢复备份覆盖新业务。旧镜像与备份保留。

## 本地证据索引

- `D:/projects/go-tmp/custom6-release-build-20261015.log`
- `D:/projects/go-tmp/custom6-ghcr-push-20261015.log`
- `D:/projects/go-tmp/custom6-ghcr-container-20261015.log`
- `D:/projects/go-tmp/custom6-production-backup-20261015.log`
- `D:/projects/go-tmp/custom6-main-deploy-20261015.log`
- `D:/projects/go-tmp/custom6-l1-deploy-20261015.log`
- `D:/projects/go-tmp/custom6-cos-deploy-20261015.log`
- `D:/projects/go-tmp/custom6-final-check-20261015.json`
- 逐站结果与受限启动日志已保存在上述服务器备份目录。

## 文档与下一会话交接

- `origin/custom/integration` 已从旧 `fd7c1b914` 普通快进到本次发布运行提交 `655a16868`，远端 `ls-remote` 核验一致，没有强推。本报告所在的文档收尾提交同时同步至 `codex/group-failover-rules-v0.2.16` 和 `custom/integration`；运行标签固定在上述运行提交，不随文档移动。
- 本轮同步 15 份仓库文档：项目状态、功能日志、本发布报告、模块化方案/执行/复核/最终门禁/耦合清单、批量取消恢复、WS 尝试身份、性能验收、定制层 README、主线同步说明、发布运行手册、升级问题日志。
- 另同步 `D:/projects/sub2api-custom/docs/CUSTOM_PROJECT_STATUS.md` 和 `CUSTOM_FEATURE_LOG.md` 两个本机共享入口。旧根目录源码及其他会话改动不提交、不覆盖；完整文档以活动工作树和上述远端分支为准。
- 文档提交不包含 backend/frontend 运行源码变化，无需为文档重新构建或部署。精确文档提交用下列只读命令核验，避免在文件中记录自身提交哈希形成循环：

```sh
git ls-remote origin refs/heads/codex/group-failover-rules-v0.2.16 refs/heads/custom/integration
git log -1 --format=%H -- docs/CUSTOM_RELEASE_0_2_16_CUSTOM6_20261015.md
git diff --exit-code custom-0.2.16.6 -- backend frontend
```

两远端分支应指向同一文档收尾提交，标签仍为 `655a16868`。下一会话先读项目状态和本报告，再开展新的定制；自动升级 PR 的真实运行验证仍是后续维护项，不因分支已同步而自动完成。
