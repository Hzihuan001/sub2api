# 0.2.19-custom.14 定时测试与渠道测智修复发布记录

日期：2026-11-04（Asia/Shanghai）

状态：已推送并部署主站、L1、COS；生产容器及入口检查通过。

## 修复内容

1. 定时测试启用时，PostgreSQL 报 `column "next_run_at" is of type timestamp with time zone but expression is of type text`。更新语句将启用分支的参数显式转换为 `timestamptz`，停用分支显式转换为 `NULL::timestamptz`。
2. 渠道测智遇到短暂空流断开时，扩展受控重试：最多三次总尝试，退避 250ms、750ms；包括 Responses 流在收到 `response.completed` 之前结束的情况。只有没有任何有效正文的瞬时 EOF/reset/timeout 才重试。HTTP 错误、部分响应、取消和非瞬时错误不会自动重发。

上游可能已经收到并处理断流请求，因此空响应重试仍存在少量重复上游计费风险。已返回部分内容后不重试，避免重复正文或覆盖已生成内容。

## 代码和镜像

- 提交：`333faad73fc7f7ea9f258c0deccc943e4ea33d2c`
- 分支：`codex/v0.2.19-merge-20261029`
- 标签：`custom-0.2.19.14`
- 镜像：`ghcr.io/hzihuan001/sub2api@sha256:2f76a571730141b6aec90638d814372d16552be3032303b1e401c8603719a922`
- 版本文件：`0.2.19-custom.14`

## 验证结果

- 真实 PostgreSQL 18：定时计划启用/停用集成回归通过，确认时间戳值可写、停用后为 NULL。
- 后端定向测试：定时测试仓储与测智读流重试回归通过，覆盖重试上限、瞬时断流后恢复、Responses 空流提前结束、部分输出不重试、取消后停止重试。
- 整个 `backend/internal/service` 测试包并非全绿：`TestOpenAIAccountScheduler_SkipsAccountBlockedForRequestedModel` 一项失败，失败代码不在本次修改文件；单独重跑仍复现。该失败已记录，不能据此宣称全量 service 测试通过。
- 本地 Docker 构建及 GHCR 镜像按不可变 digest 拉回后的隔离容器验收通过。
- 三站生产发布后容器均 healthy、重启数为 0；公网及源站的健康检查、`/pelican-showcase` 页面、静态入口资源和 CSP nonce 核验通过。生产数据库、Redis、业务卷、网络、反代配置和迁移记录未变。

## 生产发布和恢复信息

按主站 → L1 → COS 发布。每站等现有请求和定时测智租约自然归零后才重建应用容器，没有中断在途请求。

正式站点及当前镜像：

| 站点 | 地址 | 版本 |
|---|---|---|
| 主站 | `https://ai.moshu.cloud` | `0.2.19-custom.14` |
| L1 | `https://api.tokenpulse.top` | `0.2.19-custom.14` |
| COS | `https://cos.tokenpulse.top` | `0.2.19-custom.14` |

- 部署前备份：`/opt/sub2api-deploy-backups/custom14-20261103T200305Z/`，校验通过。
- 主站和 L1 应用回滚镜像：`sha256:433a235815bbc61ae215578238323cb92500984e9a22df899244ba3a15f9ca15`。
- COS 应用回滚镜像：`sha256:f967d1f5519ff0b4f89ea595ef9618a8122810a4fde34fb73764a3fc0bc0f9f1`。
- 回滚只替换应用镜像，不把旧数据库备份覆盖到新业务数据上。

## 浏览器页面核查边界

生产渠道测智页和必要静态资源的 HTTP 响应、CSP nonce 已在服务器和公网入口核对通过，并已请求 Codex 面板打开主站 `https://ai.moshu.cloud/pelican-showcase`。本轮 Windows Computer Use 因无法可靠判定 Edge 当前 URL 而停止，没有截图级视觉验收结论；页面已发起面板导航，但需要在 Codex 面板实际呈现后由用户或后续视觉验收确认最终布局。
