# 0.2.19-custom.12 渠道测智预览修复

日期：2026-11-03（Asia/Shanghai）  
状态：本地验证完成，生产发布验收中  
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

待完成：运行提交、tag、镜像 digest、精确发布镜像验收、主站/L1/COS 部署时间、容器状态、版本和页面/健康检查结果。若任一站点失败，停止后续站点并只回退失败站点的应用镜像，不恢复数据库或业务卷。
