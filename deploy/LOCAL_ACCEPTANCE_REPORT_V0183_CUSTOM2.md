# Sub2API Operator v0.1.183-custom.2 本地验收报告

日期：2026-08-27（Asia/Shanghai）

## 发布坐标

- 开发分支：`codex/cache-hit-rate-v0.1.183`
- 上游基线：`v0.1.183` / `e8cb019fabf8b55199436229044cbf9aa7a82564`
- 定制版本：`0.1.183-custom.2`
- 源码标签：`custom-0.1.183.2`
- 目标平台：`linux/amd64`

## 功能范围

- 在 admin/operator 后台“使用记录”的 Token 列中，将缓存命中率显示在缓存读取数量之后。
- Token 悬停明细新增“缓存命中率”一行。
- 口径统一为：`缓存读取 ÷（普通输入 + 缓存写入 + 缓存读取）× 100%`。
- 百分比固定保留两位小数；有缓存写入但无缓存读取时显示 `0.00%`。
- 没有任何缓存统计证据的历史记录在悬停明细中显示 `--`，不误报为零命中。
- 普通用户自己的“使用记录”页面没有新增该展示。
- 直接使用现有 usage log Token 字段计算，无数据库迁移，不改写历史数据。

## 自动化验证

- 缓存命中率工具与后台表格针对性测试：25/25 通过。
- 前端 frozen-lockfile 安装、lint、typecheck：通过。
- 前端完整 Vitest：269 个文件、1876/1876 个用例通过。
- 前端生产构建：通过。
- 后端 Go 1.27.0 unit：通过。
- 后端 Go 1.27.0 integration：通过。
- golangci-lint 2.13.0：`0 issues`。
- `git diff --check`、Compose 配置与敏感信息扫描：通过；扫描仅命中已有支付测试中的假 PEM 固定夹具。

## 隔离容器验收

- 本地验收镜像：`sub2api:0.1.183-custom.2`。
- 独立 PostgreSQL、Redis、应用数据卷、网络及 `127.0.0.1:18080` 环境健康启动。
- admin/operator/user 登录、Operator 权限矩阵、Admin API Key、WebSocket、特权账号保护及重启持久化全部通过。
- 验收结束后测试容器、网络和项目数据卷已清理。
- 本地镜像 OCI revision 是提交前的旧 HEAD，仅用于本地验收，禁止用于生产。

## 生产发布门禁

- 生产只能使用 `custom-0.1.183.2` 标签触发 GitHub Actions 后生成的 GHCR 不可变 digest。
- OVH 更新前必须只读核对当前镜像与服务状态，并备份 PostgreSQL、环境配置、持久化目录及部署配置。
- 只替换 Sub2API 应用镜像；数据库、Redis、卷、网络、OpenResty 和 Cloudflare 配置保持不变。
- 上线检查失败时恢复发布前镜像 digest，并只重建 Sub2API 应用容器。
