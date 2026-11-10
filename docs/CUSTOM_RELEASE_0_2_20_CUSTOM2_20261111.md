# 0.2.20-custom.2 发布记录（2026-11-11；三站生产部署完成）

> 状态：全部源码门禁、精确发布镜像验收及主站 → L1 → COS 生产部署已完成，三站运行相同提交/版本/digest，healthy、restart 0。逐站 HTTP/匿名权限以及最终公网 CSP、静态资源 SHA、别名和迁移账本复核均已通过。浏览器视觉/交互及真实付费模型测试未完成，不宣称全部页面功能通过。

## 变更与边界

- **部署前与回退基线**：三站部署前均为 `0.2.20-custom.1`，运行提交 `eb069def5b0dbcdd8f85f4294fa20434a6537015`，镜像 `ghcr.io/hzihuan001/sub2api@sha256:f3a21007c4a9a2e5941435dffadaf5467b2dbd2a4db400baad0e165150578693`。现已按主站 → L1 → COS 更新到下方 `.2` 坐标；`.1` 保留为兼容的应用回退目标。
- **图片同账号重试**：`/v1/images/generations` 和 `/v1/images/edits` 遇到瞬时 HTTP 5xx、连接重置、EOF/超时，使用账号级有限重试（未配置时重试上限为 3 次）；固定首次选中的账号和并发槽位。耗尽后返回最后的上游错误，避免重新选号导致“无可用兼容账号”误报。
- **图片状态与重放边界**：上述瞬时错误不写账号停用/冷却；明确的鉴权、配额、图片工具不可用仍保留既有处理。DNS NXDOMAIN 不重试；客户端取消或已写出实际图片内容时不重发。上游可能已处理丢失响应的图片 POST，重试可能重复生成/计费。
- **DeepSeek Pro 计费**：按已核对的[官方更新日志](https://api-docs.deepseek.com/updates/)纠正旧新闻稿预定降档逻辑；`deepseek-v4-pro` 保持独立 Pro 档，不再仅因到达旧计划日期而按 Flash 档计费。
- **Terra 定价**：补齐 `gpt-6.1-terra` 的 fallback 识别、缓存写入策略和 priority/Fast 档规则，避免已有模型支持与定价路径脱节。
- **按图计费优先级**：专用图片端点已启用 `image_count` 时，按张计费优先于渠道 token 定价；混合消息请求继续保留 token 计费路径。按次/图片的分组及渠道定价仍走相应配置。
- **提交后隐私副作用**：账号刷新中的 OpenAI/Antigravity 隐私设置改用 `AfterAccountCommit`，将外部副作用放在账号事务提交之后，避免失败/回滚事务先触发隐私操作。
- **停用计划租约**：运行中计划被停用后，完成路径只清理匹配本次 lease/run 的遗留所有权，不重新排队或恢复下一次执行时间。
- **Grok 前端恢复**：恢复上游合并时遗漏的 OAuth/API-key 模型编辑器、目录诊断面板及统一同步入口，保留草稿、过期请求隔离和上游 0.2.20 的 Kiro 等新功能。
- **测试与静态检查维护**：修复过时构造函数、SQL/权限/用量等测试夹具及静态检查问题；Vitest 改用包含消息编译器的 i18n 构建来处理测试中的原始字符串语言包，原生控件基线纳入已有 MCP 授权页面。
- **9008 迁移审查通过**：新增 `9008_custom_auth_invalidation_scope.sql`，仅追加/替换鉴权缓存失效函数，不删除表、列或业务数据；区分真实路由/权限变更与仅 revision/展示变更，覆盖主/备分组和用户允许分组权限。完整集成及兼容性审查已通过，`.1` 可在保留 9008 的数据库上应用级回退；三站部署证据均确认只新增 9008、全部旧迁移行/checksum 不变；最终独立实测 main/L1/COS 为 335→336、344→345、344→345。
- **生产证据边界**：三站应用、健康、HTTP 路径及匿名权限检查已完成。CUA 无法确认 URL，浏览器视觉/交互检查已中止；真实付费模型请求未用于本次验收。最终公网 CSP、静态资源 SHA 和备用域名检查均已通过。历史少量日志不代表当前完整错误分布。

## 已锁定源码与发布制品

| 项目 | 当前状态 |
| --- | --- |
| 源码分支 | `upgrade/upstream-v0.2.20-20261109` |
| 最终运行提交 | `a90e15c1c1444c3f81b4ab9dc847b2a77ed60cac` |
| `backend/cmd/server/VERSION` | `0.2.20-custom.2` |
| 自定义 tag / 远端源码 | `custom-0.2.20.2` 指向上述提交；tag 和源码分支已推送；收尾前 `git fetch origin` 后当前分支左右计数 0/0 |
| GHCR 不可变 index digest | `ghcr.io/hzihuan001/sub2api@sha256:6d89fe835bee13987c48a60406f93b270046a4be4fa35d1813efda622f234b76` |
| 平台 / 构建时间 | `linux/amd64`；`2026-11-10T19:06:27Z` |
| 镜像嵌入版本、提交、OCI revision | 已匹配 `0.2.20-custom.2` / `a90e15c1c1444c3f81b4ab9dc847b2a77ed60cac` |
| 完全相同 digest 的拉回及容器验收 | 已通过；`pg_dump` / `psql` 均为 18.6；详见下表 |
| 构建来源 / 本地发布证据 | 对运行 HEAD 执行 `git archive` 导出干净上下文，以根 `Dockerfile` 构建；`.tmp/custom202-image-build.log`、`.tmp/custom202-image-push.log` |
| Actions 状态 | `gh run list --commit a90e15c1c1444c3f81b4ab9dc847b2a77ed60cac` 返回 `[]`，没有工作流运行；本地发布不等于 Actions 成功 |

## 已验证门禁与待办

前端使用仓库锁定的 Node `20.20.2` / pnpm `9.15.9`；后端 lint 为 golangci-lint `2.15`（以 Go `1.27.3` 编译），使用 `backend/.golangci.yml` 默认配置且未额外指定 build tags，CI 工具版本同步至 2.15。后端命令在 `backend/` 执行，以下 `.tmp-full-*` 日志位于仓库根目录。源码、精确发布镜像及生产 HTTP 证据分别记录；浏览器视觉/交互及付费模型验证的限制单独保留。

| 门禁 | 结果与证据 |
| --- | --- |
| 后端全量 unit | `go test -tags=unit ./... -count=1`，exit 0；67 个含测试包全部通过，service 248.257s；`.tmp-full-unit-final.log` |
| 前端 `pnpm run lint:check` | exit 0；0 errors、2 条已有测试文件 unused 警告；`.tmp/release-frontend-lint-final.log` |
| 前端 `pnpm run typecheck` | exit 0；`.tmp/release-frontend-typecheck-final.log` |
| 前端 `pnpm run test:run` | exit 0；441/441 文件、3677/3677 测试通过，294.28s；`.tmp/release-frontend-tests-final.log` |
| 前端 `pnpm run build` | exit 0；含 i18n 完整性 4/4，Vite 58.77s；保留既有 chunk-size/Browserslist 提示；`.tmp/release-frontend-build-final.log` |
| Grok 模型编辑定向回归 | 62/62 通过，随后完整前端套件也已通过；`.tmp/frontend-grok-restoration.log` |
| `git diff --check` | 当前文档更新后复核通过；发布提交前仍须再检 |
| 后端完整 integration | `go test -p=1 -tags=integration ./... -count=1`，exit 0；service 155.170s；`.tmp-full-integration-final.log` |
| 后端完整 `golangci-lint` | `golangci-lint run --timeout=15m --max-issues-per-linter=0 --max-same-issues=0 ./...`，exit 0、`0 issues.`；`.tmp-full-lint-final2.log` |
| 9008 迁移/权限失效/回退兼容性审查 | 已通过；function-only 追加迁移，`.1` 应用回退兼容，保留 9008 和历史 checksum |
| 精确发布镜像 operator-test | exit 0；使用上述 GHCR digest，项目 `sub2api-operator-test-custom202-20261111`、端口 18197，权限/重启持久化验收通过；`.tmp/custom202-operator-test.log`；仅清理该项目测试资源 |
| 三站备份 | `/opt/sub2api-backups/v0.2.20-custom.2-20261110T184754Z` 已验证三站；旧迁移账本 main/L1/COS 为 335/344/344 |
| 三站生产部署 / HTTP 与匿名权限 | 主站 → L1 → COS 全部完成；三份 `*-release-result.json` 均 `passed: true`，详情见下表 |
| 最终公网 CSP / 静态资源 SHA / 别名 | exit 0；四域每域 5 个 URL（health + 4 个 HTML 页面）均 200、7 个 JS 资源 SHA 与 origin 一致、CSP nonce 匹配；`.tmp/custom202-public-postdeploy.log` |
| 迁移总数最终复核 | main/L1/COS 实测 335→336、344→345、344→345；旧行/checksum 全部不变，仅新增 9008；`.tmp/custom202-final-evidence.json` |
| 浏览器视觉检查 | CUA 无法确定页面 URL，当前工具检查受阻；不得据此声称视觉/交互验收通过 |

图片回归覆盖 HTTP 524、TCP 重置后同账号成功、重试耗尽保留上游错误且不跨账号/组、DNS 不存在不重试，并随完整源码门禁验证旧错误透传、账号状态及计费规则。同一运行提交构建的精确发布 digest 验收和三站生产应用/HTTP 验收均已通过；浏览器交互及真实付费模型调用仍不在已完成结论内。

## 三站生产部署证据

备份 `/opt/sub2api-backups/v0.2.20-custom.2-20261110T184754Z` 覆盖三站 PostgreSQL、应用数据、Compose/环境和 OpenResty，部署时重新核对了快照及配置。各站仅通过 `docker compose up -d --no-deps --pull never <application-service>` 更新应用；配置、挂载、网络及 PostgreSQL/Redis 身份不变。旧迁移行与 checksum 全部保留，每站仅新增 `9008_custom_auth_invalidation_scope.sql`，critical 启动日志匹配均为 0。

三站共同运行 `ghcr.io/hzihuan001/sub2api@sha256:6d89fe835bee13987c48a60406f93b270046a4be4fa35d1813efda622f234b76`、`0.2.20-custom.2`、提交 `a90e15c1c1444c3f81b4ab9dc847b2a77ed60cac`。以下时间均为 2026-11-10 UTC，对应北京时间 2026-11-11 凌晨；`*-release-result.json` 位于上述备份目录。

| 站点 | 应用更新 UTC 开始 → 完成 | 排空方式 | 发布结果 | 迁移账本 |
| --- | --- | --- | --- | --- |
| 主站 `ai.moshu.cloud` | 19:32:27 → 19:32:42.506646 | 临时对新推理入口返回 503，已有流自然完成后归零 | `main-release-result.json`：`passed: true`；healthy、restart 0 | 335→336；旧行/checksum 不变，仅增 9008 |
| L1 `api.tokenpulse.top` | 19:34:49 → 19:35:21.609990 | 临时对新推理入口返回 503，已有流自然完成后归零 | `l1-release-result.json`：`passed: true`；healthy、restart 0 | 344→345；旧行/checksum 不变，仅增 9008 |
| COS `cos.tokenpulse.top` | 19:35:59 → 19:36:09.544514 | 自然零在途，无需入口围栏 | `cos-release-result.json`：`passed: true`；healthy、restart 0 | 344→345；旧行/checksum 不变，仅增 9008 |

主站/L1 的临时 503 围栏没有强制终止已有流。围栏 UTC 时间分别为主站 19:31:49.661551→19:32:44.062267（54.4s）、L1 19:34:23.123466→19:35:23.536827（60.4s），均为 2026-11-10。完成后原始 nginx 配置文件按字节恢复、SHA 一致，临时 map 不存在，`nginx -t` 通过。最终配置一致性不代表整个更新期间未使用入口围栏。

每站 local + public 的 `/health`、`/keys`、`/image-studio`、`/docs/tools/codex`、`/pelican-showcase` 均 HTTP 200；匿名 `/api/v1/admin/settings`、`/api/v1/admin/groups/1/failure-policy`、`/api/v1/keys` 均 401。以上验证说明路径可访问和匿名授权边界生效，不包含浏览器内交互或真实付费模型功能验收。

最终公网检查 `.tmp/custom202-public-postdeploy.log` exit 0：`ai.moshu.cloud`、`api.tokenpulse.top`、L1 备用域名 `api.moshu.cloud`、`cos.tokenpulse.top` 每域上述 5 个 URL（`/health` + 4 个 HTML 页面）均 200；每域 7 个 JS 资源 SHA 与 origin 一致，CSP nonce 匹配。最终证据 `.tmp/custom202-final-evidence.json` 时间为 `2026-11-10T19:39:58.627173Z`（北京时间 2026-11-11 03:39:58），再次确认三站 healthy、restart 0、精确 digest 和上述迁移账本结果，以及围栏恢复状态。CUA 因无法确认 URL 中止，浏览器视觉/交互验收未完成。未执行生产故障注入、生产回退或真实付费模型请求。

运行 tag `custom-0.2.20.2` 始终固定在 `a90e15c1c1444c3f81b4ab9dc847b2a77ed60cac`；收尾前已 `git fetch origin`，当前分支本地/远端左右计数 0/0；文档收尾使用独立提交，不移动运行 tag。兼容应用回退目标为上述 `.1` digest，回退前也须自然排空，保留 9008、当前数据库及新业务数据，不恢复覆盖旧数据库。
