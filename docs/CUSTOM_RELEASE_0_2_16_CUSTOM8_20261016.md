# Moshu 0.2.16-custom.8 发布记录

日期：2026-10-16，Asia/Shanghai。公共使用文档和 Key 提示删除已统一发布 `.8`。源码、标签和 GHCR 已推送；同一镜像本地/OVH 回拉一致，主站 → L1 → COS 顺序部署全部 passed、exit 0，三站最终 healthy、重启 0。

## 发布内容

- 基于已上线 `.7` 的最新活动分支，仅移植旧文档预览树的 `DocumentationView.vue`、对应测试、14 张 integrations 图片和文档深链接一行变更。原预览树的旧角色静态路由不移植，角色模块保持注册入口。
- 八篇工具指南顺序：Codex、Claude CLI、Claude Desktop、WorkBuddy、Trae、Cherry Studio、Zcode、OpenCode。桌面独立菜单、手机单一选择器，保留创建令牌、CCSwitch 和原深链接；CC Switch 改用官方下载页。
- 删除主备 Key 编辑器常驻范围提示，保留实际组计费提示及后端限制。此次不扩展模型目录并集，也不变更 GPT 账号映射。
- 后端仅版本元数据变化，无运行逻辑、依赖、锁文件或数据库迁移变化。教程沿用用户文档中的模枢主站地址，截图历史供应商地址已有以文字配置为准的提示。

## 验证

- 本树 Vitest：文档页面 20、Key 编辑器 13、真实消息宿主 4、路由 62、文档入口安全 7，共 106 项通过。
- 类型检查、受影响文件非修复 ESLint、diff check 通过；14 张源图与移植文件 SHA-256 一致，共 4,608,268 字节。单独复核图片未发现明文密钥。
- 原文档会话的桌面/手机浏览器结果作为原补丁证据保留，不冒称本轮重跑浏览器。精确镜像的本地 HTTP 及三站公网资源核验均通过，见下文。
- 未变后端复用 `.7` 已有测试；本轮不追加付费模型调用或 SSE 压测。10 月 15 日 GPT 上游失败和模型目录仅主组的边界继续保留。

## 发布坐标与执行结果

| 项目 | 状态 |
| --- | --- |
| 源码分支 | `codex/group-failover-rules-v0.2.16` |
| 发布版本 / 标签 | `0.2.16-custom.8` / `custom-0.2.16.8`，已推送 |
| 运行提交 | `46083426af6cf993b0dfd86767fc35df9977dd22` |
| 镜像 tag | `ghcr.io/hzihuan001/sub2api:0.2.16-custom.8` |
| 镜像 digest / 本地及 OVH image ID | `sha256:7eb13edccde298ea2b57721fd2581c8552291b81c65268c1b3f812a073d7647b` |
| 架构 / 构建时间 | `linux/amd64` / `2026-10-15T16:59:54Z`（北京时间 10 月 16 日 00:59:54） |
| 发布及集成分支 | 已 atomic 推送运行提交至发布分支和 `custom/integration`；后续文档收尾不改变运行标签 |
| 本地容器验收 | 同一镜像 b 环境角色/JWT/API Key/WebSocket/两次重启/日志检查 exit 0 |
| 发布前备份 | `/opt/sub2api-backups/v0.2.16-custom.8-20261015T165932Z`，三站 verified |
| 主站 / L1 / COS | 全部部署与快速检查通过，无生产回滚 |

源码从本地提交的独立 `git archive` 导出后构建，没有从旧预览分支全量发布。正式镜像已推送 GHCR，本地及 OVH 按上方 digest 拉取后 image ID、完整提交、版本、架构和构建时间一致。运行标签指向该提交，后续文档提交不移动标签。

## 容器和文档资源验收

- 第一轮角色项目 `sub2api-operator-test-custom8-roles-20261016` 遇到 PostgreSQL 首次初始化的 TCP 就绪竞争，应用初次连接被拒绝并自动恢复；后续功能断言通过，但最终日志检查保留该错误而 exit 1，不能记为首次全绿。完整失败日志 `D:/projects/go-tmp/custom8-role-acceptance-20261016.log`。
- 先启动独立 b 项目 PostgreSQL/Redis，并显式用 TCP `pg_isready -h 127.0.0.1` 确认接受连接后，复验同一镜像；完整角色/JWT/API Key/WebSocket/重启/日志验收 exit 0。没有修改应用源码、测试断言或重建镜像；结果摘要 `D:/projects/go-tmp/custom8-local-acceptance-20261016.json`，应用日志 `custom8-role-container-b-20261016.log`。
- 本地实际服务的文档 chunk 为 `/assets/DocumentationView-DHOL5pBz.js`，SHA-256 `571d4cbec1e9a620231b09f2668d70a487a98375d9565154dbfbd5bf16bcfb51`。验证八篇工具标识、CC Switch 新下载链接、旧固定下载链接移除，以及 14 张实际图片响应与源文件哈希完全一致；证据 `D:/projects/go-tmp/custom8-doc-assets-local-20261016.json`。
- 两个本次专属角色项目及其合成数据库、卷和网络已按精确项目标签回收；上述日志/JSON 保留，原先其他验收项目、生产数据和回滚镜像未清理。

## 生产备份和部署结果

备份目录使用 UTC 时间命名，实际在北京时间 10 月 16 日创建。三站 dump、Compose/env/持久化目录归档与 OpenResty 配置均已备份，`pg_restore --list`、tar 读取及 SHA-256 校验通过。dump 大小为主站 96,199,085、L1 11,578,157、COS 3,086,874 字节，manifest 最终 `verified=true`；这不是完整恢复演练。

| 站点 | 北京时间 2026-10-16 | 结果 |
| --- | --- | --- |
| `ai.moshu.cloud` | 01:11:28–01:11:42 | passed，healthy，restart 0 |
| `api.moshu.cloud` | 01:12:09–01:12:28 | passed，healthy，restart 0 |
| `cos.moshu.cloud` | 01:12:52–01:13:17 | passed，healthy，restart 0 |

各站部署前在途批量、请求槽位和等待槽位均为 0。只替换对应应用的镜像引用并 app-only 重建，环境/端口/挂载/网络指纹、PostgreSQL/Redis 身份与启动时间、全部迁移全行记录保持一致；新增迁移为 0。内网及公网 health、工作台和三篇文档深链接均 200，匿名设置与分组规则接口均 401，关键启动日志匹配为 0。

三个公网入口均实际加载新文档 chunk 并逐张核验全部 14 张图片哈希；不以 SPA 的 HTTP 200 代替资源验收。生产没有新建管理员凭证、执行付费请求或逐角色登录，完整角色矩阵以本地同一镜像为据。逐站结果、资源清单和备份 manifest 已脱敏复制至 `D:/projects/go-tmp/custom8-production-evidence-20261016/`；服务器备份中的同名结果保留。收尾 inspect 再次确认三站使用上方 digest、healthy、重启 0。

## Actions 和后续边界

本轮沿用已批准的本地验收、GHCR 与按 digest 部署路径。显式查询个人 Fork 的 Actions API，运行提交的 `total_count=0`，不称 CI 通过。当前仓库权限 API 返回 enabled、custom-release workflow 为 active，不能继续把先前 HTTP 422 当作当前禁用状态；自动发布/升级 PR 链路仍需另行验证。

发现本机 `gh` 的默认仓库原为上游 `Wei-Shaw/sub2api`，已校正为 `Hzihuan001/sub2api`；后续 Actions 查询仍应显式带 `-R Hzihuan001/sub2api` 或使用完整 API 路径，避免误看上游运行。未修改远端 Actions 设置。原 SSE mixed、全量 lint 基线、P4 双跑和未知图片结果的边界保持，未因本轮前端发布改写为通过。

## 生产预检与回退

北京时间 00:55，只读确认三站均 `.7` / `f36de3d4a00400c66994a02c13c5d23f6a7feac8`，healthy、重启 0，在途批量/请求/等待槽位均为 0；服务器可用约 44 GB。部署前再次检查在途请求。

回退目标为 `ghcr.io/hzihuan001/sub2api@sha256:ec8860583ac3603fadc108e90b2036abf352d395b5035ee8f4cb51d1de66ac42`（`.7`）。只恢复对应应用 Compose 的镜像引用并 `up -d --no-deps` 重建应用；保留数据库、Redis、卷、网络和反代。当前版本无新增迁移，发布验证要求迁移全行记录完全相同，不再沿用 `.7` 首次增加 9000 迁移的判断。
