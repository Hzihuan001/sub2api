# 主备分组模型目录合并：本地候选与验收（2026-10-20）

## 状态与范围

- **仅本地开发/验收完成，未提交、推送或部署生产**。生产仍为 `0.2.16-custom.12` / `custom-0.2.16.12`，digest `e6a53ec6…`，正式发布记录不改写。
- 活动树：`C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom`；分支 `codex/group-failover-rules-v0.2.16`；基线 HEAD `554f8b8cc1d1aef125745da8d4edb58ce830737a`。新增源码和测试尚在工作区，不能只提交 tracked 文件。
- 多分组 Key 的普通 `GET /v1/models`、`GET /models` 及其单模型详情合并主/备目录，按配置顺序去重，同名模型保留优先组的原生 metadata。单组 Key 不走新增逻辑。
- 每次读取实际分组权限、启用状态及可调度账号来源；分组与 Key 模型白名单只做减法，不把任意白名单文字伪造成上游能力。账号为空/不可调度时，不凭平台名称生成默认目录；DeepSeek/Kimi 等无内置目录的平台必须有真实模型映射，不回退为 Claude。
- 目录读取不选择路由、不获取调度槽、不改故障计数、账号状态、售价或账目。实际请求仍走原生路由、计费和 Key 故障规则。

## 解耦与安全边界

- 独立 service `key_route_catalogue.go` 负责候选来源；middleware 同名文件只为普通元数据路径注入只读来源；handler 同名文件合并原生表示。宿主只增加 middleware 小接线和 `GatewayHandler.Models` 三行入口，不复制调度器、计费器或 Ent schema，无新增迁移/API 字段。
- 原生目录在复制的 Gin context 和断开的 2 MiB 有界 writer 中渲染；任何源失败均拒绝半份目录，不把源错误详情或私有映射目标外泄。源 `If-None-Match` 被移除，最终 ETag 基于合并/过滤后的结果；详情仍按原生格式和 404 行为返回。
- pinned OpenAI 的真实目录可以包含静态默认表之外的新模型，但必须有新鲜健康 unmapped/passthrough 账号依据，并继续双重白名单过滤。验收发现并修复了“仅动态白名单导致提前跳过”的问题；取消期间不制造伪 502。
- 只把 `*` 当作原生映射通配，合法 `[1m]` 后缀和字面 `?` 保留。备用目录排除已知图片/视频模型，不扩大现有无状态文本备用路由范围。
- Codex `client_version` manifest、强制平台、原生 Gemini/Antigravity 和 WebSocket 路径保持现状；本轮不是所有平台元数据的大一统。
- **原生鉴权边界保留**：主组本身 inactive/删除/失权时，现有鉴权仍可能在目录 handler 前返回 403；不等同于主组账号池为空/暂停（后者可显示有效备用模型）。没有为元数据绕过原有授权门禁。
- **可达前缀一致性**：中间组撤权、变为订阅或 Claude Code 专用时，真实选组会终止；目录也停止向后枚举，仅保留前面已验证的模型。普通 inactive/missing 仍可跳过；inactive 同时撤权时先按权限终止，不扩大现有路由权限。
- Composite 仅合并真实账号支持的映射/默认目录，不从 route-only 别名配置推断健康能力。UI 高级模型选择候选仍沿用现有公开白名单来源，未改成远程自动枚举。

## 最终本地产物

- 镜像：`sub2api-custom:key-catalogue-20261020c`。
- Engine image ID / OCI index：`sha256:2454007241e32bc3c66f8e3d0d7375d7be407fc8de08c2ab8c7dd6ebde773a96`；linux/amd64 manifest：`sha256:9b4bf8683bbc96d4c3a805af9eee2d6756607c9bc3d6fd53c2f5a8d179311fe3`。
- 保持版本 `0.2.16-custom.12`，但构建 commit 明确为 `local-key-catalogue-20261020c`，构建时间 `2026-10-20T02:36:02.3094079Z`，不是新的生产标签。a/b 为较早阶段，不作当前交付。
- 项目 `moshu-reseller-acceptance-c` 仅重建 main/l1/cos 三个应用；六个 PostgreSQL/Redis 容器 ID 未变，未重建网络/卷。三应用同一 c 镜像、healthy/restart 0，三个 `/health` 均 200。
- 用户入口：主站 `http://127.0.0.1:18180/keys`、L1 `18181/keys`、COS `18182/keys`。本地主站管理/网关验收已执行；L1/COS 仍只验启动健康，完整管理矩阵待本人合规前置，不代用户接受确认。

## 自动化与 Docker 实测

- 受影响 service/middleware/路由定向通过；handler 的 `TestKeyRouteCatalogue|TestKeyModelListing` 定向通过（0.241 秒），含 pinned 动态模型、双重白名单、最终 304、取消及容量边界。c 的目录权限前缀修复后，service 定向 11 顶层/32 含子项通过（0.118 秒），增加四种阻断/两种正常跳过及不读取后续数据断言；最后 service Go vet 通过，其余未改包复用先前通过结果。
- 架构契约 `scripts/custom_contracts.py` 通过（7 模块、10 测试组）；新文件和测试已登记 `custom/contracts.json` / `custom/manifest.yml`。PowerShell 解析、mock Node 语法、`git diff --check` 通过。
- 本地 Docker 最终 Linux embed 编译通过；前端未改，复用既有 `.12` 前端构建缓存，不声称本轮重跑整套前端。完整 isolation wrapper 被 dirty-worktree guard 阻止，不把独立契约通过写成完整 clean-tree/生成校验通过。
- 新增互斥 `-CatalogueCasesOnly`，最多两次合成网关调用；叠加 `-CatalogueMetadataOnly` 后固定预算为零，只验目录/权限/缓存。只访问隔离网络内 mock，不调用生产或付费模型。创建三种平台组 OpenAI/DeepSeek/Kimi，独有模型明确设置合成逐模型价（输入 `0.000001`/token、输出 `0.000002`/token）。
- 验证合并去重、备用模型详情、最终 ETag/304、旧 ETag 下停用/权限变更不泄露、账号空池不造默认、Key 白名单与详情 404、元数据零模型调用/零扣费。
- 独有 `backup-model` HTTP 和 `third-model` SSE 各成功一次，真实网关转换为 `/v1/chat/completions`；各一条 usage/dedup，输入 10/输出 2，按实际备组 0.5/0.75 倍率分别扣 `0.000007` / `0.0000105`，合计 `0.0000175` 合成金额，余额/Key 配额精确一致，无全局账号暂停。
- b 的 HTTP/SSE 报告：`D:/projects/key-catalogue-local-chat-fixture-20261020/evidence.json`，run `760222176bb1`，**passed、2/2、0 findings**，当地时间 10:23:44–10:24:24，镜像 ID `sha256:10b2f1fede9269876a390657e39ebbd09c63af7643e9cc8ee3d18bd469e99230`。8 个临时资源均按 ID 删除、200；mock 删除，验收期间原九服务 ID/启动时间/restart 未变，已有页面验收 Key 保留。
- c 的最终目录补验：`D:/projects/key-catalogue-boundary-local-20261020/evidence.json`，run `04de08429bf4`，**passed、0 模型请求、0 findings**，于当地 10:39:34 完成。新增真实中间组撤权时保留主组、隐藏后续组的断言；其余目录/详情/ETag/白名单/空池检查通过，零 usage/dedup/余额及配额变化，8 资源清理均 200，mock 删除、原九服务未重启。不把沿用 b 的 HTTP/SSE 证据说成 c 再跑一遍。
- 浏览器只读复核：重新载入页面，既有两把 Key 保留；专用 Key 的连续 5、限流最近 20 中 3、TTFT 8/20 中 3、SLA 20 低于 95%、冷却 240、精确模型 `gpt-4o-mini` 与 $1 上限回显正确。没有保存/修改/删除 Key，最后取消退出，console error `[]`。截图 `D:/projects/key-catalogue-editor-review-20261020.jpg`；computer-use 技能要求的真实页面核对未引入应用代码变化。

### 失败证据保留

第一次 Docker 报告 `D:/projects/key-catalogue-local-20261020/evidence.json` / run `d4fe79ab44d4` 保持 `failed`：目录/权限等检查已执行，首个备用模型 HTTP 为 200，但 mock 对 Chat 路径返回 Responses 格式，结果缺少预期内容。该次 1 次合成调用产生 usage 输入 10/输出 2、金额 `0.000007`；不能称零扣款。8 个资源清理均 200、原九服务未重启。

仅修复 mock 以按请求协议返回 Chat HTTP/SSE、统计 stream 为 boolean，应用 b 镜像不变；随后限定再跑上述两调用矩阵，不重复旧 13 场景/SLA 全套。连同第一次，累计 3 次本地合成模型调用，无真实费用。不能删除失败记录或把补验写成首次单轮全绿。

## 复验与停止

证据目录必须不存在；不传自动合规确认开关：

```powershell
pwsh -NoProfile -File deploy/tests/key-routing-acceptance-existing.ps1 -Execute -Sites main -CatalogueCasesOnly -CatalogueMetadataOnly -ExpectedImageId sha256:2454007241e32bc3c66f8e3d0d7375d7be407fc8de08c2ab8c7dd6ebde773a96 -OutputDirectory D:/projects/key-catalogue-new-run
```

停止但保留合成数据：

```powershell
$env:SUB2API_IMAGE = 'sub2api-custom:key-catalogue-20261020c'
docker compose -p moshu-reseller-acceptance-c -f deploy/acceptance/docker-compose.isolation.yml stop
```

不要执行 `down -v`。等用户本地验收后，另行确认提交/版本、clean-tree 门禁、正式镜像及三站发布；本轮无新的生产部署。
