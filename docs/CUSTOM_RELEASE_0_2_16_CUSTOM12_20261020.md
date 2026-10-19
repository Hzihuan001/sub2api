# Moshu `0.2.16-custom.12` 发布记录

## 当前状态

已完成主站、L1、COS 三站发布。用户针对明确的三站发布确认回复继续后，按本地验收 → 推送 → GHCR 发布/拉回 → 备份 → 主站 → L1 → COS 顺序执行。2026-10-20 06:26:50（Asia/Shanghai）最终复核三站 healthy、restart 0、相同不可变镜像，公网静态入口及 JS 内容与站内源站一致。

- 运行提交：`f42b02f9ea0f43c3e34d9bbf778cd9e450cb05b4`；功能提交 `5b8ee9a5e`。
- 标签：`custom-0.2.16.12`；版本：`0.2.16-custom.12`；官方基线仍为 `v0.2.16`，未额外合并新主线。
- GHCR：`ghcr.io/hzihuan001/sub2api:0.2.16-custom.12`。
- 不可变 digest / Engine image ID：`sha256:e6a53ec608abd463bfa41ccc6e939dca0175756adeb40e957ebe31a6b68c3610`（linux/amd64）。
- 交接文档是后续单独提交，不改变运行镜像提交或已发布标签。

## 变更范围

- 独立 `internal/keyrouting`、service 适配及薄 handler 接线实现 Key 主备路由、连续失败/超时/限流/状态码/TTFT/SLA 规则；按实际组调用原生调度与计费，不重新引入自定义全局账号停号。
- 编辑器按参考站排列与修改方式实现主备搜索/交换、规则折叠/稳定删除/统一冷却/保存锁和高级字段保留；仅有备用时显示故障切组。
- 独立模型策略表及多入口准入、普通/Codex 目录只减过滤；认证缓存 v28。新增迁移仅 `9001_custom_api_key_route_policies.sql`、`9002_custom_api_key_model_policies.sql`，原迁移不改。
- 已有内容、工具执行或已知用量不重放；首字等待只取消上游子请求，最后合格候选不主动截断。

## 本地证据及边界

已验收 g 的完整证据见 [`密钥编辑器验收记录`](CUSTOM_KEY_EDITOR_ACCEPTANCE_20261020.md)：前端 147 项、类型/定向 lint、六包后端定向、service 全套基线与最终工具 guard 定向、架构门禁和 Docker 生产构建通过；g 本地主站 13 个场景分两份报告完成，保留夹具失败原报告。正式产物的独立单轮验收见下段，不用它改写 g 历史。

2026-10-20 最终发布门禁已重新执行完整 service（含最后五处工具 guard）：211.873 秒、含子测试 17,311 项通过、0 失败、6 项原有条件跳过；keyrouting/handler 关键 137 项通过。原始日志 `D:/projects/key-routing-release-gate-20261020-055941/`。

正式源码从运行提交的 `git archive` 导出构建，未把工作树临时文件或凭据作为上下文。构建内 i18n 4/4、类型检查、Vite、Linux Go embed 通过；保留已有 Browserslist/分包等非阻断警告。GHCR 推送后本地与服务器都按 digest 拉回，确认版本、完整 commit、amd64 和镜像 ID 一致。

正式 GHCR 镜像单轮本地主站验收 **13/13**、exit 0，报告 `D:/projects/custom12-published-gateway-20261020/evidence.json`：模型缺失/优先级/不可调度、HTTP/SSE 502/503、已输出不重放、全部不可用零调用、主动 TTFT、冷却跳过、已知用量不重放、最后候选不截断和模型限制零调用均通过。10 次成功按实际组/账号/input10/output2/倍率验证单条 usage、dedup、用户余额和 Key 配额，合成扣款合计 `0.00001823`；无全局账号暂停。11 个临时资源精确清理均 200，验收期间服务未重启。报告为 `passed-with-findings`，唯一 finding 是已知 `MODELS_PRIMARY_ONLY`，不把它隐藏成目录并集已实现。

本地三个应用均使用正式 digest，健康 200、restart 0；六个 PostgreSQL/Redis 容器身份不变。主应用在保留 9001/9002 的同一数据库上切回 custom.11，确认健康和旧版本后恢复 custom.12；这只证明结构/启动兼容，不代表新模型权限等价。L1/COS 未重复主站完整登录矩阵。

L1/COS 本地仅健康检查通过，完整管理矩阵因本人合规确认未执行；生产可达检查不等于已登录功能全验。没有付费生产模型请求。

已知边界：`/v1/models` 非备用目录并集；计数器进程内且重启清零；旧在途配置可能重置新配置计数；TTFT 非硬实时，取消前上游可能产生用量；旧镜像不会执行新 Key 模型限制。

## 发布与备份

GitHub Actions 权限 API 显示 enabled，但本次真实 dispatch 返回 **HTTP 422 `Actions has been disabled for this repository`**，按运行提交查询无 CI run。沿用已批准的本地构建/GHCR 路径，未修改仓库设置，不能称 CI 成功或真实自动升级 PR 链路已闭环。

- 现场旧镜像三站一致为 custom.11：`sha256:022704b184874c61ecd50587252867ad109f943afa75eed2b670fd3409aa916d`，均 amd64/healthy。准备期间主站曾有在途请求，真正修改 Compose 前再次确认请求、等待槽位和图片/批量任务全部归零，未强制中断请求。
- 备份 `/opt/sub2api-backups/v0.2.16-custom.12-20261019T220600Z`：三站 pg_dump 自定义格式、pg_restore list、持久化/Compose/环境归档、反代归档和各 SHA-256 已验证可读；manifest verified=true。敏感内容只保留服务器权限隔离备份，不进入仓库。
- 初次架构检查在脏树运行，触发“generated source checkout must be clean”前置失败；没有实际生成差异，不当作通过，提交后从干净树复验。初次 staged diff 检查发现一处文档尾空行，已修正。
- 干净树复核发现架构脚本原来仅接受 `9000_custom_*`，与依次追加的 9001/9002 冲突；只把新增定制命名范围明确为 9000–9999，旧迁移不可改删及官方迁移字节校验保持，新增 10 项回归通过。9001/9002 内容及名称未改。
- 部署只读排空检查补充用户/Key/实时槽位、账号等待队列和 `image_task:*` processing 任务，避免图片结果收尾时误判空闲；不读取或输出图片正文，不修改 Redis。

### 三站生产结果

下列时刻均为 2026-10-20、Asia/Shanghai；只重建应用。三站均使用页首同一 digest/commit。

| 站点 | 更新时刻 | 迁移数量 | 结果 |
| --- | --- | --- | --- |
| 主站 `ai.moshu.cloud` | 06:23:33–06:23:41 | 324 → 326 | healthy、restart 0 |
| L1 `api.moshu.cloud` | 06:24:31–06:24:40 | 333 → 335 | healthy、restart 0 |
| COS `cos.moshu.cloud` | 06:25:13–06:25:21 | 333 → 335 | healthy、restart 0 |

- 每站旧迁移全行保持，只增加 9001/9002；配置环境、挂载、端口和网络指纹不变，数据库/Redis ID 与启动时间不变。
- 站内/公网 `/health`、`/keys`、`/image-studio`、`/docs/tools/codex` 均 200；匿名 settings、failure-policy、keys API 均正确返回 401。
- 06:26:50 有限复核三站版本/digest、配置/数据服务保持、公共入口引用与 JS 内容匹配源站、关键日志匹配 0；不是 30 分钟或全天监控。
- 生产没有现成 admin API Key，未伪造登录矩阵或新增凭据；没有生产付费模型、真实生图或故障注入。L1/COS 本地完整管理矩阵未执行的边界仍保留。
- 服务器备份目录内保存 `<site>-release-result.json`、启动日志与 `final-verification.json`；本地脱敏证据为 `D:/projects/custom12-{main,l1,cos}-production-20261020.log`、`D:/projects/custom12-final-production-20261020.json`。

## 回滚约束

仅应用镜像回退，保留新增表，不恢复旧备份覆盖上线后业务数据。9001/9002 为增量兼容结构，但旧程序忽略新 Key 模型限制；一旦启用该限制，回退不是权限等价操作，必须先停止受影响 Key 或选择已审核的修复镜像。生产失败时停止后续站点发布，按现场旧镜像回退失败站点，不改变数据库、Redis、网络、卷及反代配置。

本次生产未触发回滚。有限复核时三站新模型限制启用数均为 0；这不是日后可以免检查回滚的许可。执行前应重新检查受限 Key；确认权限安全后，把该站已核实 Compose 中应用 image 改回页上 custom.11 不可变 digest，再执行：

```sh
docker compose -f <verified-site-compose> config --quiet
docker compose -f <verified-site-compose> up -d --no-deps --pull never <application-service>
```

主站应用 service 为 `sub2api`，L1/COS 为 `api`。精确 Compose 路径与旧配置保留在服务器备份，不从别站照抄。禁止 `down -v`、数据库恢复或迁移校验绕过。回退后仍需健康、数据服务身份、旧协议与 Key 权限验证。
