# 0.2.19-custom.13 渠道测智 CSP 动画修复

日期：2026-11-03（Asia/Shanghai）

状态：本地候选，用户配置网络期间暂停生产部署。没有发布 `.13` 镜像或 tag，没有继续修改服务器及 DNS/CDN。

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

## 生产真实状态及暂停指令

| 站点 | 最近核对版本 | 镜像 digest | 状态 |
|---|---|---|---|
| 主站 ai.moshu.cloud | 0.2.19-custom.12 | `sha256:6cb01b9bdf763526c0c3dfe2e86cc3e2a67cd949f785933abe90a14627f2179a` | healthy，restart 0 |
| L1 | 0.2.19-custom.11 | `sha256:f967d1f5519ff0b4f89ea595ef9618a8122810a4fde34fb73764a3fc0bc0f9f1` | 首次公网检查失败后回滚，healthy，restart 0 |
| COS | 0.2.19-custom.11 | 同 L1 | 本轮未更新，healthy，restart 0 |

排查时 `api.moshu.cloud` CNAME 为 `ddos.cdn.yewcdn.com`，TLS 握手失败；同域名直达 OVH 源站正常，原有 `api.tokenpulse.top`、`cos.moshu.cloud` 入口正常。本轮未改 DNS/CDN/反代；用户正在其他会话配置网络，后续状态以其完成后实际核对为准。

L1 再次排空因旧进程残留的 API Key 统计槽位超时，未再次更新应用。只读证据确认其写入早于当前进程启动 17.681 秒、无账号/用户/实时/WS 配对槽位，同 Redis namespace 应用仅 L1；这类槽位源码明确仅供统计、TTL 自愈。本轮没有删除它或绕过在途请求检查。

## 恢复发布前

用户明确恢复部署后，重新读取共享文档，核对 DNS/CDN、实际生产版本、站点入口和备份。以 `.13` 最终源码构建镜像并完成隔离容器及精确发布 digest 验收，再执行主站 → L1 → COS。不要直接续跑 `.12` 的临时脚本或假设三站仍使用同一基线；不要恢复旧数据库覆盖新业务数据。
