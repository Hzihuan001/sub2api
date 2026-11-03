# 0.2.19-custom.13 渠道测智 CSP 动画修复

日期：2026-11-03（Asia/Shanghai）

状态：用户已完成网络配置并恢复发布；镜像、源码标签、本地与发布产物容器验收完成。主站仍有在途请求，首次排空超时后未修改生产，正在确定本次维护方式。DNS/CDN/反代未改动。

- 运行源码：`c46975de0a3fe6649e0fbe421521bad948a1839c`；标签 `custom-0.2.19.13`，分支 `codex/v0.2.19-merge-20261029`，均已推送。
- 镜像：`ghcr.io/hzihuan001/sub2api:0.2.19-custom.13`；不可变发布引用 `ghcr.io/hzihuan001/sub2api@sha256:433a235815bbc61ae215578238323cb92500984e9a22df899244ba3a15f9ca15`，Linux/amd64，已在本地和 OVH 拉取，版本和完整提交匹配。
- 正式入口：主站 `ai.moshu.cloud`、L1 `api.tokenpulse.top`、COS `cos.tokenpulse.top`。旧 `*.moshu.cloud` 分站入口不再作为本轮发布门禁，也不擅自修改其解析。

## 根因与最小修复

生产父页面通过 HTTP CSP 的 `script-src 'self' 'nonce-随机值'` 授权脚本。iframe `srcdoc` 继承该策略，子页面的 meta CSP 无法放宽父策略；因此 `.12` 即使保留作品脚本，它们仍会被浏览器拦截。此前独立本地浏览器未模拟父 CSP，覆盖不足，不能用其结果声称生产动画恢复。

本次仅在 `frontend/src/utils/pelicanMarkup.ts` 为预览脚本传递宿主当前 nonce：从 `document.scripts` 的 `.nonce` 属性读取（不使用被浏览器隐藏的 `getAttribute`），通过 DOM 属性赋值安全覆盖所有作品脚本和启动脚本，包括 SVG 内部脚本。完整 HTML 和片段共用 DOM 流程，保留片段头尾动画依赖。没有全局开放 `unsafe-inline`、`unsafe-eval` 或外部网络；iframe 仍是 `sandbox="allow-scripts"`。下载原文不变。

沿用 `.12` 的片段提取、减少动态偏好和有限读流重试修复。响应读取重试可能发生在上游已处理之后，仍有额外一次上游计费风险，不能承诺网络失败完全消失。

## 本地验收

- `pelicanMarkup.spec.ts`：10/10 通过，覆盖实际 RAF、片段前后脚本/样式、宿主 nonce 传递与转义、源 nonce 覆盖、SVG 内部脚本及 DOMParser 兜底。
- 定向 ESLint、`vue-tsc --noEmit`、`git diff --check` 通过。
- 独立临时目录的 Chrome headless 使用生产同等父 HTTP CSP，调用最终源码自动获取宿主 nonce，没有在测试后手工补 nonce。6 个场景（含用户 126/130/142 三份真实作品）各采样 5 次，RAF/CSS/作品 SVG 属性变化均符合预期；CSP 违规、页面和作品 JS 异常均为 0。
- 浏览器报告：`.tmp/pelican-browser-csp-final-report.json`；本机脚本 `.tmp/pelican-browser-csp-final.cjs`。这些是本地临时验收文件，不是生产依赖，也不连接用户浏览器。
- SMIL 未标为通过：当前 runner 的原生 HTML 对照也未更新其几何属性，无法据此判断产品是否有问题。
- `pnpm run build` 前端生产构建通过（含 i18n 4/4、TypeScript 构建）；只有既有 Browserslist/chunk 体积提示。本轮不因小范围前端修复重跑未修改后端全套。上轮完整前端已有 52 项既有失败（Grok 50、nativeControls 2），没有宣称全量测试全绿。
- 根 Dockerfile 本地镜像、GHCR 精确 digest 拉回后的隔离 PostgreSQL/Redis 容器验收均通过；验证 operator 权限、受限管理密钥允许与拒绝、重启持久化、撤销后 401。两个临时验收项目均已自动清理，仅删除各自新建的资源，未清理其他会话容器。
- GitHub Actions 仍不可用，本次使用已授权的本地构建、GHCR 发布及精确产物验收，不声称 CI 成功。

## 本次备份与发布门禁

有效新备份：`/opt/sub2api-deploy-backups/custom13-20261103T104359Z/`。三站 PostgreSQL custom-format dump 分别为 main 149,480,915、L1 18,711,098、COS 3,056,452 字节；`pg_restore -l`、站点挂载归档、Compose/环境配置、OpenResty 配置及 SHA-256 校验通过。备份目录和文件分别限制为 700/600，不在文档输出凭证。

第一次归档活跃日志时 tar 检测文件变化并停止，未修改生产；该不完整目录 `custom13-20261103T104113Z` 不能当作恢复点。修正为复制静态文件快照后归档，数据库仍用一致性 pg_dump；没有声称演练了完整数据库恢复。当前镜像及每站旧 digest 均保留以便只回滚应用。

发布脚本只更新应用镜像：核对旧镜像可在本地解析、完整备份、Compose 归属及排空期间无并发配置变更；更新后核对环境/挂载/端口/网络指纹、完整迁移记录和 PostgreSQL/Redis 实例身份均保持。检查失败恢复该站旧 Compose 和旧应用镜像，不恢复旧数据库覆盖新业务数据。

首次主站自然排空等待约 240 秒，持续有普通请求槽位，后台图片、批量和定时测试队列均为空，门禁正常退出，未执行应用重建。源码 `backend/cmd/server/main.go` 的 HTTP Shutdown 仅等待 5 秒，不能将 SIGTERM 当作长请求无损排空；是否接受本次短暂维护中断待用户选择。

## 部署前基线与上一轮历史

| 站点 | 最近核对版本 | 镜像 digest | 状态 |
|---|---|---|---|
| 主站 ai.moshu.cloud | 0.2.19-custom.12 | `sha256:6cb01b9bdf763526c0c3dfe2e86cc3e2a67cd949f785933abe90a14627f2179a` | healthy，restart 0 |
| L1 | 0.2.19-custom.11 | `sha256:f967d1f5519ff0b4f89ea595ef9618a8122810a4fde34fb73764a3fc0bc0f9f1` | 首次公网检查失败后回滚，healthy，restart 0 |
| COS | 0.2.19-custom.11 | 同 L1 | 本轮未更新，healthy，restart 0 |

上一轮 `.12` 排查时 `api.moshu.cloud` CNAME 为 `ddos.cdn.yewcdn.com`，TLS 握手失败；同域名直达 OVH 源站正常，原有 `api.tokenpulse.top`、`cos.moshu.cloud` 入口正常。随后用户自行完成网络配置，并明确本轮使用页首三个正式入口；旧域名的历史故障不作为当前正式入口故障结论。本轮未改 DNS/CDN/反代。

上一轮 `.12` 的 L1 再次排空因旧进程残留的 API Key 统计槽位超时，未再次更新应用。只读证据确认其写入早于当前进程启动 17.681 秒、无账号/用户/实时/WS 配对槽位，同 Redis namespace 应用仅 L1；这类槽位源码明确仅供统计、TTL 自愈。没有删除它或绕过在途请求检查。本轮恢复发布前该槽位已自然过期，L1/COS 初次只读排空为零，尚未执行 `.13` 重建。

## 部署前剩余步骤

构建、镜像发布和精确 digest 容器验收已完成，无新代码或失败时不重做。待本次维护方式确定，部署前刷新实际生产版本、排空、备份和旧镜像可回滚状态，执行主站 → L1 → COS，每站检查通过后继续。使用 `.13` 的独立发布脚本，分别校验 main `.12` 与 L1/COS `.11` 的基线；不要续跑 `.12` 脚本，也不要恢复旧数据库覆盖新业务数据。
