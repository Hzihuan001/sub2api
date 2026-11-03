# 0.2.19-custom.12 渠道测智预览修复

日期：2026-11-03（Asia/Shanghai）  
状态：部分发布，生产动画问题未闭环；由 `.13` 继续修复
分支：`codex/v0.2.19-merge-20261029`  
基线：`55a62dd3c3725f3c9f4abd45f4a45607d92217a5`  
生产旧版本：`0.2.19-custom.11`，digest `sha256:f967d1f5519ff0b4f89ea595ef9618a8122810a4fde34fb73764a3fc0bc0f9f1`

## 问题与证据

- 成功作品静止：Kiro 结果中既有 JS `matchMedia('(prefers-reduced-motion: reduce)')` 后停动画，也有 CSS `@media` 同偏好停动画。系统偏好会让仅用于展示的预览只画首帧。
- 错误卡片：生产主站最近 24 小时日志里有 40 条上游读流 `connection reset by peer` / `Stream read error`。三个应用容器都健康，均运行 `.11`；错误发生在应用出站到上游 Cloudflare 地址的 TCP 读流。现有证据不能证明重置由 Cloudflare、主机网络或上游源站中的哪一方造成。

## 改动

- 只在 sandboxed `srcdoc` 预览中覆盖 JS 减动态偏好，并将内联 `<style>` 及其 `media` 属性中 `prefers-reduced-motion: reduce` 改为不匹配的查询；下载文件保持原文。
- 修复 SVG 片段在 `</svg>` 处截断而丢失后置动画脚本的问题；保留 SVG 前置样式/脚本及 DOMParser 自动移入 `head` 的资源，避免片段预览只剩静态图。
- CSP 通过 DOM 解析移除源文档实际存在的 CSP meta，再追加受限预览策略；不再用全文正则误删脚本里类似标签的字符串。预览 iframe 继续使用 `sandbox="allow-scripts"`，CSP 仍禁止网络请求，未授予同源权限或 `unsafe-eval`。
- 渠道测智仅对无响应正文且错误明确为响应读流 reset、unexpected EOF、连接超时等情况重试一次。HTTP 明确状态、部分正文、取消和确定性错误不重试。
- 风险：读流断开发生在请求发出之后，上游可能已生成并计费；重试成功可能增加一次上游费用。该重试最多一次，不能替代定位服务器至上游的根链路。
- 无数据库迁移、无价格/余额/普通用户请求行为改动。

## 验证

- `frontend/src/utils/__tests__/pelicanMarkup.spec.ts`：6/6 通过，覆盖 HTML/SVG 提取、完整文档及片段实际内联 JS RAF 帧属性变化、前后置动画资源保留、系统 reduced-motion 覆盖、CSS reduced-motion 覆盖、CSP 安全以及脚本字面量保留。
- Docker Go 1.27.3：`go test ./internal/service -run "Pelican|ScheduledTest" -count=1` 通过。
- 前端目标 lint 与 TypeScript 检查通过；`pnpm run build` 生产构建通过。
- 完整前端 Vitest：431 文件中 429 通过、2 失败；3,533 测试通过、52 失败。失败集中在未修改的 `EditAccountModal.grokUpstream.spec.ts`（50）和 `nativeControls.spec.ts`（2），不把整套全绿作为本次结果。
- `pnpm run lint:check` 无错误，存在 2 条仓库既有未使用变量 warning。
- 现有用户浏览器 UI 操作已按要求停止。另用独立临时目录的本地 headless Chrome 验证最终 utility 的隔离 iframe：JS RAF、CSS 动画以及用户 126/130/142 三份真实附件均有帧间变化、无浏览器异常；不连接用户浏览器会话。SVG SMIL 在原生 HTML 基线中也不更新动画值，因此该 runner 无法确认 SMIL，不将其标为通过。报告位于本机 `C:/Users/a/AppData/Local/Temp/moshu-pelican-browser-01OMAg/report.json`。
- 最终 Linux 镜像本地 Compose 联合验收通过（端口 18112，项目 `sub2api-operator-test-custom12-final-20261103`）：登录、角色/用户密钥权限边界、现行权限管理密钥的授权/拒绝/撤销、WebSocket、重启持久化和关键日志检查。临时验收环境已由脚本清理，仅清理其自身资源。
- 首次容器验收因脚本仍期待已停用的旧版无限权管理 Key 新建成功而失败；已将脚本改为验证旧接口拒绝和现行最小权限管理 Key，未放宽产品权限，修正后验收通过。

## 生产备份

- 已校验的备份：`/opt/sub2api-deploy-backups/20261103085654/`。
- 主站数据库使用 `sub2api`（117 张业务表，约 1.4 GB），custom-format dump 149,935,069 字节；L1/COS dump 为 18,684,243/3,054,242 字节。
- 三份 dump 均能由 `pg_restore -l` 读取；三份 `/app/data` tar 与两份 OpenResty 配置归档列表可读；全部 SHA-256 校验通过。备份不是实际恢复演练。
- 构建/部署应只更换应用容器；保留 PostgreSQL、Redis、业务目录、网络、站点环境与反代配置。应用回退目标为 `.11` digest `sha256:f967d1f5519ff0b4f89ea595ef9618a8122810a4fde34fb73764a3fc0bc0f9f1`。

## 发布结果

- 运行提交 `73348ea7793b7b58780e36a825eb5a3e6d480ff0`，tag `custom-0.2.19.12`；后续验收脚本与文档提交独立，不移动运行 tag。修复、分支和 tag 已推送。
- 镜像 `ghcr.io/hzihuan001/sub2api:0.2.19-custom.12`；不可变 digest `sha256:6cb01b9bdf763526c0c3dfe2e86cc3e2a67cd949f785933abe90a14627f2179a`。本地构建并发布，未宣称 Actions CI 成功。
- GHCR 精确 digest 拉回后与本地候选一致，内嵌版本/提交核对通过；`-UsePublishedImage` 完整隔离容器验收退出 0。验证了角色、密钥权限、WebSocket、重启持久化及日志；未做生产付费模型请求。
- 主站已于北京时间 17:49:23–17:50:25 更新：healthy、restart 0、版本/提交/digest 正确，内外 `/health`、密钥/工作台/渠道测智入口、主 JS 与测智懒加载 JS 均 HTTP 200；匿名受保护接口 401；关键启动错误 0。环境/挂载/端口/网络指纹、PG/Redis 身份及完整迁移行未变。
- L1 首次发布在公网检查遭遇 TLS 握手失败后自动回退 `.11`，回退后 healthy/restart 0；COS 当时未更新。排查确认 `api.moshu.cloud` 当前 CNAME 为 `ddos.cdn.yewcdn.com`，同一故障在旧 `.11` 仍存在；OVH 源站使用该域名直接 TLS 访问正常，原有公网入口 `api.tokenpulse.top` HTTP 200。保持 DNS/CDN/反代配置不变，通过原有域名验收 L1 同一应用容器；不将新 CDN 入口标为恢复。
- L1 再次排空等待因孤立旧 API Key 统计槽位超时，未再次变更生产；COS 未更新。该统计槽位早于回滚后进程启动，仅用于统计、等待 TTL 自愈，未删除 Redis 数据。
- **后续阻断发现**：生产父页面 nonce CSP 会被 `srcdoc` 继承，`.12` 本地 headless 的父页面没有模拟这一头部。新增同等父 CSP 后，三份真实作品的脚本均被拦截；给子脚本传递当前宿主 nonce 后动画恢复。故 `.12` 不能宣称生产动画已修复，最终由 `.13` 完成，不移动 `.12` tag。
- **用户暂停生产**：用户正在配置网络环境，已明确先不部署生产。后续不再部署/修改服务器；最新已知版本 main `.12`、L1/COS `.11`，均 healthy/restart 0。`.13` 只做本地开发验收，恢复发布前重新核对环境。
