# 密钥编辑交互对齐及本地验收（2026-10-20）

> 当前状态：`0.2.16-custom.12` 已推送并部署主站/L1/COS。以下 g/d 的失败与补验证据保持原样；正式 digest 单轮验收和生产发布独立记录在本节及 [custom.12 发布记录](CUSTOM_RELEASE_0_2_16_CUSTOM12_20261020.md)，不倒写历史。

## 正式发布产物验收与当前本地环境

- 运行 `f42b02f9ea0f43c3e34d9bbf778cd9e450cb05b4`，标签 `custom-0.2.16.12`，三站同一 `ghcr.io/hzihuan001/sub2api@sha256:e6a53ec608abd463bfa41ccc6e939dca0175756adeb40e957ebe31a6b68c3610`（linux/amd64）。
- 发布前完整 service（含最后工具 guard）17,311 项/关键后端 137 项通过；正式 GHCR 拉回后单轮本地主站 **13/13** HTTP/SSE/TTFT/模型准入与账务场景通过，报告 `D:/projects/custom12-published-gateway-20261020/evidence.json`。10 次成功均单条 usage/dedup、实际组扣费；已输出/已有用量不重发、最后组不截断、拒绝场景零调用。报告 `passed-with-findings` 的已知项为目录仍主组，非备用并集。
- 三个本地应用均为该正式镜像，healthy、restart 0，六个数据服务未重建；保留 9001/9002 的同库 custom.11 主应用启动健康回退/恢复通过，不能据此声称模型限制权限等价。
- 生产主站→L1→COS 已完成 app-only 更新；2026-10-20 06:26:50 有限复核健康、版本/commit/digest、公共 JS 与源站一致及关键日志 0。无生产付费模型、没有生产全角色登录矩阵；L1/COS 本地完整管理矩阵仍未执行，不代本人合规确认。Actions 实际 dispatch 422，无 CI run。
- 本地入口仍为 `http://127.0.0.1:18180/keys`、L1 `18181`、COS `18182`。保留验收数据；若本人完成本地合规后补测 L1/COS，用当前 digest 的 `-ExpectedImageId`，不使用下文历史 d/g ID。

停止当前验收环境（保留数据卷）：

```powershell
$env:SUB2API_IMAGE = 'ghcr.io/hzihuan001/sub2api@sha256:e6a53ec608abd463bfa41ccc6e939dca0175756adeb40e957ebe31a6b68c3610'
docker compose -p moshu-reseller-acceptance-c -f deploy/acceptance/docker-compose.isolation.yml stop
```

不要使用 `down -v`。下文 g/d 的关闭参数和补测 ID 仅代表历史阶段，当前以上述正式坐标为准。

## 历史 g 扩展：TTFT、SLA 与 Key 模型限制（当时本地验证完成、未发布）

用户继续要求完成参考站的密钥编辑功能后，本轮补齐了以下真实后端能力，而非只加 UI 选项。最终 g 容器复验结果见下一节；后文 d 镜像证据保留为历史，不能代替新后端验证。

- 首个有效内容（TTFT）：明确配置才启用，最低 5 秒；有可用备用才主动中断上游，最后一个合格分组不按阈值截断。按 Key/组/模型/端点记录慢首字最近 N 样本，阈值触发后续请求冷却。没有隐藏的系统默认 TTFT。上游 dispatch 才开始计时，原生排队不含在内；原生 backoff 的退出仍可能带来延迟，不承诺硬实时。
- SLA：最近 N 个可统计分组结果收满后，成功率严格低于百分比才冷却；成功结果可以触发后续冷却，但绝不重放成功请求。成功率等于阈值不触发。
- 计费/重放安全：独立 guard 只取消本组上游，不取消入口 context；bounded SSE prelude 最多 64 KiB；有文字/思考/工具启动、已知正数 usage 或未知/超大协议时保守禁止重放。原生 parser 隐藏的 usage 由只读原始 body observer 提前识别；统一 switch 门禁同时尊重 usage veto，不能因状态码规则再次发请求。仍沿用官方计费路径，不承诺上游被取消请求免输入费。
- provider 执行工具、未知工具、图片、接续请求、WS/后台任务不参与跨组重放；普通客户端 function/custom 工具仍可路由，已有输出后不重放。
- 自动注入/服务端模拟工具：在独立 `key_route_hosted_tools` helper 与五个原生薄发送边界处理 Codex 图片桥、OAuth/Grok 搜索、Responses Lite additional_tools、Web Search 模拟和 Kiro MCP tools/call。普通 text/function/custom 与 tools/list 不受影响。原始 usage 已知但 native parser 返回 nil result + typed failover 时，保留消息和原生 partial settlement，阻止同组/同账号及跨组二次发送；不合成上游状态码。取消重放后 writer 继续原生心跳，usage/终态/未知帧不伪记为有效首字。
- Key 模型限制：高级设置内开关、搜索/勾选结果、chips、清空、精确 ID 手填；启用必须至少一项、最多 200 项，不支持通配。独立扩展表 9002，CRUD/认证快照 v28；省略保留、null/关闭移除。各请求入口、WS 首帧/后续帧和 session.update、批量/异步创建均校验。不可验证的 Live/非 Responses Realtime 对受限 Key 明确拒绝；已拥有任务的查看/取消不受阻。
- 模型目录：普通列表、单模型与 Codex 分支按 Key 策略只减过滤，保留原组/平台限制和 metadata，ETag 按过滤结果计算。尚未合并备用目录，`/v1/models` 仍来自主组；不能用模型限制名单伪造可用模型。UI 候选只公开所选分组已启用白名单的精确 ID，无候选时可手填。
- 目录可见性边界：Gemini 原生 `/v1beta/models` 及 Antigravity 的 `/antigravity/models`、`/antigravity/v1beta/models`、`/antigravity/v1/models` 暂沿用原组目录，未增加 Key 只减过滤；实际模型调用仍在认证后执行 Key 精确限制。该 UI/metadata 差异不等于能够绕过调用权限，不能声称所有平台目录已完全统一。
- 模块：纯 `internal/keyrouting` guard/observer/规则、独立 service 策略适配、独立 handler writer/admission/listing，原生入口薄接线；不复制官方调度器、计费器或 Ent schema。扩展迁移仍在官方软删除边界注册清理触发器，需要后续主线合并复核。
- 回滚边界：新扩展表不破坏旧列；但旧镜像不认识 Key 模型限制，**启用受限 Key 后不能把回滚称作权限等价**。长连接使用认证时策略快照，不宣称编辑后实时撤销已建立会话。规则计数为有界进程内状态，重启清空、多个实例不共享。
- 已知并发边界：旧在途请求仍可能携带旧策略快照，规则指纹惰性清理可使新策略刚形成的计数/冷却重置；不会跨 Key/模型污染或额外扣款，但不承诺保存新规则后所有在途请求严格按新版本执行。后续需要带版本的计数 namespace 再治理，不在本轮复制一套配置版本系统。

## g 最终本地验收（2026-10-20）

### 产物和环境

- 本地镜像 `sub2api-custom:key-routing-20261020g`，版本 `0.2.16-custom.11`，构建标识 `local-key-editor-20261020g`。
- OCI index / Docker Engine image ID：`sha256:d972c627b4107cdfb57c892f77bd7ebfbf5fd1d4a58efcb1ef23e0af2324ef78`；Linux amd64 manifest：`sha256:cdd56177eeacbe878a584a299a93bf3a5addee30673777338fdf46326305a51a`。没有 GHCR 发布或新 Git 标签。
- 原隔离项目 `moshu-reseller-acceptance-c` 的三个应用更新为同一镜像，均 healthy、restart 0，`18180/18181/18182` 健康检查均 200，启动关键错误匹配均 0。六个数据容器 ID 与更新前一致，未重建数据库、Redis、网络或卷。迁移 9002 的独立表已存在。
- 用户入口：`http://127.0.0.1:18180/keys`。关闭仍用本文末尾的 Compose `stop`，把 `SUB2API_IMAGE` 改为本节 g 标签，不使用 `down -v`。

### 自动化门禁

- 最终前端 **11 文件 147/147**（包括真实父级 v-model、删除、保存竞态、五默认种子、模型策略），`vue-tsc --noEmit`、定向 ESLint 通过。
- handler/keyrouting/middleware/routes/repository/DTO 六包最终定向全部通过；service 完整基线 **144.928 秒、exit 0**，最后追加的原生工具边界另以定向用例复验通过（0.333 秒），不冒称旧全套结果在追加源码后重新运行。完整 native Forward 与 handler 的已知 usage + nil result/typed failover 回归通过，只允许一次上游调用。
- Go vet（handler/service/keyrouting 等受影响包）、架构门禁（7 模块、10 测试组）、差异空白检查、夹具 Node 语法及 PowerShell 语法通过。
- 最终 g Docker 生产构建内 i18n **4/4**、类型检查、Vite 和 Linux Go embed 编译通过。已有 Browserslist、动态/静态混合导入及大 chunk 警告保留；Windows 无 CGO 编译器，没有宣称 `-race` 通过。

### 真实 HTTP/SSE 与账务

两个原始报告联合覆盖 13 个不同场景，不把失败报告改成通过：

1. `D:/projects/key-route-existing-9b235a49ed8c/evidence.json`：前 **10 个场景通过**；第 11 次在合成 `slow_usage` 的二次 `writeHead` 处导致模拟上游崩溃，报告状态保留 `failed`。不是把断流当作业务通过。
2. 修复夹具为已发送头时不再 `writeHead`，不修改应用源码或重建 g；仅通过 `-RemainingSafetyCasesOnly` 补验余下 **3/3**，报告 `D:/projects/key-route-existing-fd4cc97c440d/evidence.json` 为 `passed-with-findings`。已通过场景未无因重跑；总计 14 次本地入口调用，含那次夹具失败，不调用生产模型。

| 场景 | 最终证据 |
| --- | --- |
| 主组缺模型、备用优先级、主账号不可调度 SSE | 依次命中正确备组；按 0.5/0.75 倍率单次结算 |
| 最终 502 HTTP、输出前 503 SSE | 每次主/备各调用一次、最终一条 usage + dedup |
| 已输出后失败 | 仅调用主账号，不重放；此合成无 usage 失败不扣款 |
| 全部账号不可调度 | 503、零上游调用、零扣款 |
| TTFT 静默 HTTP、只有 prelude/心跳的 SSE | 5 秒规则下分别约 5.464/5.466 秒完成备用返回 |
| 达慢首字阈值后的下一请求 | 直接走备用，仅一次上游调用 |
| 开始已知 usage、内容延迟 | 约 7.507 秒完成主组，超过 6 秒阈值但不截断，只有一次调用/usage/dedup，实际主组 2 倍率扣款正确 |
| 最后合格备用组延迟 | 约 7.367 秒完成，超过 6 秒阈值不截断，单次结算 |
| Key 模型限制 | API 保存回读；目录只减过滤；被禁模型 404、零上游调用、余额/配额不变 |

所有完成场景均核对实际 account/group、input=10/output=2、usage/dedup、用户余额与 Key 配额；自定义全局暂停字段没有被写入。补验两次成功总扣款 `0.00000675`、usage=2/dedup=2。

`passed-with-findings` 的 finding 仅为已记录的 **MODELS_PRIMARY_ONLY**：备用模型可调用，但列表尚非并集，不隐瞒为完整实现。SLA 的样本完整性、严格百分比、成功不重放、规则修订及身份隔离由自动化验证，未额外制造收费 SLA 请求。

夹具崩溃使其自身容器内 HTTP 清理不可用：先逐一核对旧报告 ID 的合成前缀/所属环境，再经本机合成管理员 API 删除该次 11 个临时资源；补验 9 个临时资源自动清理均 200。脚本现有独立临时 HTTP client 的清理后备，避免未来夹具退出留下资源。两个验收期间均未重启原有九个服务；原 Key `1`、专用 UI 验收 Key 和生产数据未删除。

L1/COS 使用同一 g 镜像且健康，但管理功能矩阵仍受用户本人合规确认前置限制，**未执行、不代确认、不冒称三站功能全验通过**。补验必须在本人确认后显式传 g 的准确 image ID。

### 浏览器实测

- 万模只查看新建/编辑的未保存草稿，最后均取消，现有 Key 不变。核对其新草稿五条默认：TTFT 15 秒/20 次中 3 次、SLA 20 次低于 90%、连续 5 次、超时及限流各 20 次中 10 次，冷却 120 秒；本地首次主动启用空草稿已相同。已有策略和关闭后的本次草稿不被覆盖。
- 最终 g UI 保存专用 `codex-key-editor-acceptance-20261020`（仍 $1 上限，合成余额 $0，未调用模型），重开确认：连续 5、限流 20 次中 3 次、TTFT 8 秒/20 次中 3 次、SLA 95%、冷却 240、精确模型 `gpt-4o-mini` 开启，全部回读正确。
- 清空中间规则样本后第一次点击移除即删除该规则，其他规则不变；关闭/再开保留剩余草稿；取消不保存这些删除。新建草稿的无备用隐藏/有备用显示、五条默认顺序和值已真实检查，新建未提交。
- 桌面 1280×960 和手机 390×844 真实检查；手机弹窗外层宽度/scrollWidth 均 390，规则可换行、底部取消/更新可访问；没有把底层官方表格称作整页零横向溢出。临时视口已恢复，最终 browser console error `[]`。
- 截图：`D:/projects/key-editor-g-defaults-20261020.png`、`D:/projects/key-editor-g-desktop-20261020.png`、`D:/projects/key-editor-g-mobile-20261020.png`。页面已保留供用户验收。

**当时交付状态（g 阶段）**：本地候选已备好，尚未推送或部署；后续已获批准并发布 custom.12，以上方正式坐标为准。不是宣称所有万模内部实现均完全一致，范围及已知边界仍保留。

## d 镜像阶段结论与范围（历史）

本轮继续完善已有主备分组、四类 Key 故障规则的编辑功能，参考万模登录态下的未保存草稿逐项操作对比。没有修改、保存或删除万模现有 Key。仅本地候选，尚未提交、推送或部署生产，须用户本地验收。

下方为上一轮 d 镜像的历史结果，当时尚无主动 TTFT、SLA 与 Key 模型限制；最新实现以上方扩展与最终产物复验为准。`/v1/models` 备用目录合并仍未实现。

## 已完成的编辑能力

| 项目 | 当前实现与验收 |
| --- | --- |
| 主/备排列 | 主组卡片、最多四个有序备用卡片；上移/下移/编辑/移除；首备可与主组互换 |
| 选择/更换 | 名称、编辑、添加均一键打开统一搜索弹层；选择已用分组时安全交换，避免重复；失效旧组不能被交换到其他位置 |
| 真实分组信息 | 复用官方 `GroupOptionItem` 展示名称、说明、平台、右侧倍率、专属零倍率及现有峰谷信息；不按平台猜模型清单 |
| 故障区显示 | 有备用才出现；标题、状态、开关同行；编辑/收起规则；关闭再开启保留本次未保存草稿 |
| 添加/移除规则 | 自动加入未使用语义类型；可增加多条不重叠状态码规则；稳定草稿 ID 删除；按下动作防失焦布局移动，空字段后首次删除准确 |
| 冷却 | 一个统一输入；协议仍逐规则保存。旧规则异值时明确提示“多值，保留原配置”；只有用户输入新值才统一，打开/切换类型不覆盖旧值 |
| 校验 | 错误仅出现在受影响规则，重复状态码只标记参与规则；新增状态码规则不自动插入已用建议码；原后端重复/权限校验保持 |
| 高级设置 | 默认折叠；原自定义 Key、IP、额度、速率及有效期字段不销毁；摘要反映实际额度；隐藏原生校验失败自动展开并聚焦 |
| 保存 | 提交重入保护，保存期间禁止关闭、取消或切换 Key；失败保留草稿并解锁；避免旧请求关闭新草稿 |
| 弹层 | 搜索自动聚焦；Escape 先关闭分组选择，第二次才关闭 Key 编辑；返回后保留未保存数据 |

规则仍为连续失败、最近 N 次已有上游超时、最近 N 次上游 429、高级状态码窗口；统计 Key/分组/模型/端点，不因自定义规则全局停用账号。继承/关闭/覆盖及安全重放边界见 [持续设计](CUSTOM_KEY_FAILOVER_RULES_20261019.md)。

## 模块与后续合并

- 新独立组件 `frontend/src/features/key-routing/KeyGroupPicker.vue`、`KeyAdvancedSettings.vue`；主备与规则编辑仍在同一 feature 目录。
- `KeysView` 仅组合组件、保留原字段/提交路径、增加提交锁和摘要。不修改官方 `Select`、`BaseDialog`、`GroupOptionItem`。
- UI 统一冷却不扩展 API、Ent、数据库或价格/计费协议。本轮后端生产逻辑未追加改变。
- 修正三个旧测试文件中的隐式全局停号预期，保留请求身份/迟到帧/usage 等检查，增加 `SetOverloaded`、`SetTempUnschedulable` 零调用断言；不是恢复已废弃停号行为。
- 新增可复用本地脚本 `deploy/tests/key-routing-acceptance-existing.ps1`。默认只读预检，显式执行只在指定现有隔离项目创建带 UUID 的合成资源；按记录 ID 清理，不重建应用、数据库或卷；默认不接受合规协议。

## 自动化结果

- 受影响前端 9 文件 **131/131** 通过；另管理员 Key 弹窗与 API 路由序列化 **12/12** 通过。覆盖父级真实 v-model、保存竞态、关闭/继承、异值冷却、真实双层弹窗和高级字段保存回读。
- 定向 ESLint、前端类型检查通过；Docker 生产构建内 4 项 i18n、`vue-tsc -b`、Vite 和 Go embed 编译通过。
- 完整 `internal/service` 测试 **143.783 秒、exit 0**。此前 5 个顶层失败均为已废弃全局停号预期，保留历史失败证据并以本节更新当前门槛。
- keyrouting/repository/middleware/routes/handler/admin/DTO 七个受影响后端包定向测试通过。
- 架构契约门禁通过（7 模块、10 测试组），`git diff --check` 通过。构建已有 Browserslist 过期和较大 chunk 警告，不冒称这些警告消失。

## 真实本地网关与账务证据

复用隔离项目 `moshu-reseller-acceptance-c`，不连接生产或收费模型。原始报告 `D:/projects/key-route-existing-e6f9d04e36ec/evidence.json`，摘要 `summary.md`。

主站 **7/7**：主组缺模型选备1、调整优先级选备2、无可调度主账号选备1 SSE、502 规则切组 HTTP、503 `response.failed` 输出前切组 SSE、输出后失败不重放、全部不可调度 503 零上游请求。

五次成功各产生一条 usage 和一条 dedup，目标分组 0.5/0.75 倍率、余额、Key 配额均核对准确；未生成自定义全局暂停。部分输出场景只调用主账号，不重放；无 usage 的合成失败无正扣费。非 owner 更新 403、重复备用/重复状态码 400，拒绝后存量配置不变。

本轮 10 个临时资源清理均 200，内网 mock 移除，原 9 个服务在矩阵过程中未重启。该网关测试在 c 镜像完成；本轮 d 镜像的后端生产源码未变化，复用成功证据，不重复整套网关测试。

L1 合成管理员仍需合规确认，故 **L1/COS CRUD/HTTP/SSE 功能矩阵未执行**；没有代用户确认、改确认记录或绕过保护。相同镜像启动/健康检查不能替代这一门槛。用户可自行完成本地确认后仅用 `-Sites l1,cos` 补测，避免重复主站。

## 最终本地构建坐标

- 镜像：`sub2api-custom:key-routing-20261020d`，不是 GHCR 发布。
- OCI index：`sha256:e0b592a5f4fb63ccc194678a87c315fffbdbe5f6ba3044dd73a0d3c1eeb5f50c`。
- Linux amd64 manifest：`sha256:3c5122cb1b66b9d74eeda295e03535bb9af8d6db68d9f1db33ead28051171639`。
- 版本显示仍为 `0.2.16-custom.11`，构建标识 `local-key-editor-20261020d`；不创建虚假发布标签。
- 本机入口：主站 `http://127.0.0.1:18180/keys`、L1 `18181`、COS `18182`。只替换三应用，保留合成 PostgreSQL、Redis、网络、卷。
- 验收环境保留；关闭用 `docker compose -p moshu-reseller-acceptance-c -f deploy/acceptance/docker-compose.isolation.yml stop`，不要加 `down -v`。执行时设置 `SUB2API_IMAGE=sub2api-custom:key-routing-20261020d` 供 Compose 插值。

### 最终产物复验

- 三个最终 d 应用均 `healthy`、restart 0，三个本机 `/health` 均 200/`status=ok`；最近启动日志 panic/fatal/迁移/数据库/Redis 关键错误匹配均 0。六个数据服务持续运行，未重建或删除卷。
- 仅通过 UI 创建专用合成 Key `codex-key-editor-acceptance-20261020`，额度上限 $1、所属合成管理员余额 $0、不调用模型；保留给用户本地验收。原测试 Key `1` 未持久化修改，未复制/输出完整密钥。
- 在最终 d 镜像通过 UI 更新主组为 `2`、备1为 `default`、备2为 `3`、连续次数 `5`、统一冷却 `240`，保存后重开回读完全一致，额度仍 $1。前一 dev UI 创建值亦在容器重建后保留，证明持久化而非页面假状态。
- 浏览器真实确认清空中间规则输入后第一次点击即删除该规则，其他规则不变；添加/搜索/主备交换、空搜索结果、关闭/再开草稿保持；嵌套 Escape 只关搜索选择器，保留 Key 草稿。
- 桌面 1280×960 与手机 390×844 检查弹窗；手机弹窗自身 scrollWidth/clientWidth 同为 390，按钮和规则可访问。底层官方 Key 表格仍有横向宽度，不把该检查表述为全页面零横向溢出。
- 最终产物浏览器 console error `[]`。临时 viewport 已恢复；前端开发服务器随后停止，用户入口回到实际 Docker 产物 `18180`。
- 截图：`D:/projects/key-editor-final-20261020.png`（默认宽度规则）、`D:/projects/key-editor-final-20261020-desktop.png`、`D:/projects/key-editor-final-20261020-mobile.png`。旧参考截图 `D:/projects/key-rules-wanmo-reference-20261020.png` 保留。

补测 L1/COS 时应传当前准确镜像：`-Sites l1,cos -ExpectedImageId sha256:e0b592a5f4fb63ccc194678a87c315fffbdbe5f6ba3044dd73a0d3c1eeb5f50c`，前提是用户本人先完成本地合规确认。不要用默认旧 c 镜像值跳过校验；不要无因重复主站已经通过的整套矩阵。
