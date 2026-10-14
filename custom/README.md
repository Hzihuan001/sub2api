# 定制模块维护入口

本目录是定制边界的**清单和门禁**，不是第二套应用、动态插件系统或独立服务。

开始前先读 `docs/CUSTOM_PROJECT_STATUS.md`，核对活动工作树、最新生产提交和其它会话改动，再读 `docs/CUSTOM_MODULARIZATION_EXECUTION.md` 的实际阶段证据。不要从根目录旧版本源码直接发布。

## 一次定制改动应怎样接入

1. 复用一个已有 feature ID；只有确实新增独立功能时才新增 ID。同步 `custom/manifest.yml`、`.github/custom-features.json` 与后端 `internal/custom/features.go`。
2. 在 `custom/contracts.json` 登记所有者、真实入口、显式功能依赖和可执行测试组。接口放在使用方或中立契约包；只抽需要的能力，不传整个 handler/service 容器。
3. 宿主接线只放在清单登记的入口；新增入口要解释为什么现有入口不够用，并补实际宿主测试。禁止为通过门禁添加整个目录的无理由例外。
4. 前端 routes/menus/settings 通过 `frontend/src/custom` 接入。设置贡献必须声明独立 read/write permission；无读取声明不显示、无写入声明只读。贡献元数据不能给后端授予权限，服务端默认拒绝仍必须测试。
5. 对官方已有能力保留宿主实现，只抽定制差异；不能复制整个设置、调度、计费或批量系统来“减少冲突”。API、持久化键、历史迁移和结算事务边界不能因目录调整而变化。
6. 增加正向和漏接/拒绝测试，核对旧行为；单独提交并在 `CUSTOM_FEATURE_LOG.md` 追加提交、测试、是否推送/部署。当前状态只在 `CUSTOM_PROJECT_STATUS.md` 更新，根共享目录保留同步入口。

## 本地门禁

仓库根目录执行；Go、pnpm、Python 使用执行记录中的锁定环境：

```text
python scripts/custom_contracts.py
python scripts/custom_contracts.py --run-tests --group architecture-negative
python scripts/custom_contracts.py --run-tests --group backend-host
python scripts/custom_contracts.py --run-tests --group frontend-host
python scripts/check-custom-isolation.py --base-ref <验收基线> --upstream-ref <固定官方标签>
git diff --check
```

`--run-tests` 不指定组时执行清单内全部组；组的 argv/cwd 真正被工具消费，不是备注。静态导入检查不能证明运行接线，故必须保留实际 router/Sidebar/Settings 测试及未注册负例。只运行目录存在性或类型检查不算功能验收。

生成产物由锁定工具生成、不能手改；新增定制迁移使用约定命名，但本轮模块化不需要迁移。`--skip-generated` 仅在另一步已验证或明确推迟到候选完整验证时使用，报告中不能称作已完成生成验证。

## 合并更新与发布

- 下一次官方升级使用最新验收集成点和明确官方标签；不要重新合并长期旧功能分支恢复已删除代码。
- 本地同步脚本与 upstream-sync workflow 均运行架构/真实宿主测试；缺接线、权限、消息或新依赖失败时先定位，再改适配器，不能削弱断言。
- 保持阶段提交分离：上游合并、结构重构、业务变更不要混在同一提交。
- 静态检查、定向测试、完整候选门禁和隔离 Docker 验收分层执行；同源码成功结果可复用，不反复无原因运行昂贵测试。
- 本地成功不代表 CI/生产成功。本轮未授权 push、远程 PR/标签或部署；Actions 的实际执行限制仍需单独恢复和验证。
- 发布时重新核对现场版本/长任务、备份和不可变镜像 digest，只重建应用容器，失败立即停止后续站点。不要用旧数据库备份覆盖新业务来回退纯重构。

## 明确保留的例外

- `securityaudit/prompt_handler.go` 读取宿主认证主体的单一 middleware 导入已逐项登记；不因此允许整个功能反向导入 server/handler。
- 官方已拥有审计和批量 worker 生命周期；不要再注册第二个所有者。
- 本轮不是一次新版本升级，也不扩展 Kiro 供应商改造。历史供应商适配差异不能因为目录整理被误删或回退。

阶段完成情况与剩余项以执行记录及 `docs/CUSTOM_MODULARIZATION_REVIEW_20261015.md` 为准。当前账务停止项未解决，不能把局部已通过证据当成 P0–P6 全部交付；不要跳过复现失败的取消测试来发布。
