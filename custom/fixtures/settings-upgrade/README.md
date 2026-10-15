# P6：官方设置新增演练

这两份 patch 仅用于隔离副本演练，不应应用到活动开发目录或作为生产功能提交。`official-field.patch` 在真实 `SettingsView.vue` 的 general 页添加 `upstream_support_notice` 输入框、表单类型、默认值和保存字段，并补齐 API 读写类型；加载和保存回显继续使用原通用逻辑。`rehearsal-tests.patch` 复用真实 SettingsView mount 与 custom registry 贡献探针。

来源与已验证结果（2026-10-15）：

- frontend tree：`9abed689f5b2b6c33afc8e5cfa8a1ee3864d3183`。
- 原始 SettingsView blob：`476602609d0c8d5c838b08a083e9de4224ed0cdf`。
- 工具链：Node 20.20.2 / pnpm 9.15.9。
- 负控：仅应用测试 patch，6 个用例均失败于 `real official general field must exist`。
- 正控：应用官方字段 patch 后，15 个定向用例通过（新增 6 个＋原宿主契约 9 个；其余 43 个按名称过滤），`pnpm run typecheck` 通过。
- 活动 frontend tree 与原 SettingsView blob 均保持不变。

新增用例覆盖管理员、scoped/global 操作员的新字段加载、编辑、保存及服务端回显，原有设置和 custom-menu 数据保留，定制面板读取和保存，缺失/null 默认值，以及只读操作员两类保存均不写入。测试 patch 另补齐真实邮件编辑器的 `getEmailTemplates` 读取 mock，以保留“无错误提示”的断言；未替换 SettingsView 或 custom 宿主。

## 在隔离工作树重跑

以下 PowerShell 命令从仓库根目录执行。先启用上述 Node/pnpm 版本；目标目录必须尚不存在。先核对 tree/blob，若源码已升级，应审阅并重定位 patch 上下文，不使用强制应用。

```powershell
git rev-parse HEAD:frontend
git rev-parse HEAD:frontend/src/views/admin/SettingsView.vue
git worktree add --detach ../sub2api-settings-rehearsal HEAD
Copy-Item -LiteralPath custom/fixtures/settings-upgrade/official-field.patch,custom/fixtures/settings-upgrade/rehearsal-tests.patch -Destination ../sub2api-settings-rehearsal
Set-Location ../sub2api-settings-rehearsal
pnpm --dir frontend install --frozen-lockfile

# 负控：只有测试，没有新官方字段；预期退出码 1，恰好 6 个缺失字段失败。
git apply --check --directory=frontend rehearsal-tests.patch
git apply --directory=frontend rehearsal-tests.patch
pnpm --dir frontend run test:run src/views/admin/__tests__/SettingsView.spec.ts -t "P6 official general field upgrade rehearsal" --maxWorkers=1 --minWorkers=1

# 正控：添加真实官方字段；预期 15 个定向测试和类型检查均通过。
git apply --check --directory=frontend official-field.patch
git apply --directory=frontend official-field.patch
pnpm --dir frontend run test:run src/views/admin/__tests__/SettingsView.spec.ts -t "registered custom settings host contract" --maxWorkers=1 --minWorkers=1
pnpm --dir frontend run typecheck
```

Windows 深目录下若 Node 20 无法解析实际存在的 pnpm 依赖长路径，安装时追加 `--config.virtual-store-dir-max-length=60`，仍保持 `--frozen-lockfile`。无需重跑全量 Vitest。演练工作树会保留临时修改，检查后按本地工作树管理方式清理，勿把演练字段合并到生产分支。

## 本机原始证据

已实跑脚本位于 `D:/projects/go-tmp/p6-settings-20261015/run-rehearsal.ps1`，支持 `SourceRoot`、`OutputRoot`、`ToolchainRoot` 和 `ExpectedFrontendTree` 参数；它创建独立源码副本并复用现有依赖，不属于本目录的必需运行框架。

最终日志均在 `D:/projects/go-tmp/p6-settings-20261015/verified-run/`：

- `negative-control.log`：6 个预期缺失字段失败。
- `candidate-tests.log`：15 个通过。
- `candidate-typecheck.log`：类型检查成功。
- `summary.log`：工具链、来源 tree 与活动源码未变校验。

该临时目录只作为本机证据；维护者重跑只需本目录两份 patch、对应源码和项目工具链。
