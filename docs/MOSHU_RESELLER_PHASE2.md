# Moshu 代理站第二阶段改造规格

本文档用于在新的会话中继续改造 Moshu 主站与 L1 代理站。第二阶段开始前，第一阶段不修改 Moshu 主站，只使用 Moshu 已有的“每分组独立 API 密钥”。

## 1. 第一阶段现状与边界

L1 是独立部署的标准版 Sub2API，运行在以下容器中：

- `api-L1`
- `postgres-L1`
- `redis-L1`

L1 的数据库、Redis 和数据目录均不与 Moshu 共用。`api-L1` 同时加入 `1panel-network`，通过 Docker DNS 访问 `http://1Panel-sub2api-bjGj:8080`。

启用 `MOSHU_ONLY_MODE=true` 后：

- L1 管理员可以管理自己的用户、密钥、充值、订阅、用量和销售倍率。
- L1 管理员可以读取 Moshu 上游连接状态并执行连接测试。
- L1 管理员只能调整现有销售分组的 `rate_multiplier`、说明和用户倍率覆盖。
- L1 管理员不能新增、导入、修改、复制或删除上游账号。
- L1 管理员不能配置代理 IP、执行 OAuth 接入、安装/修改插件或通过整库恢复引入其他上游。
- L1 内置更新与回滚接口只读，防止官方升级覆盖“仅 Moshu”约束。
- 当前 Moshu 分组密钥仍由站点所有者配置；L1 管理员不能看到密钥明文。

当前单一产品示例：Moshu 分组 `GPT-企业` 的成本倍率为 `0.35`，L1 同名销售分组的销售倍率为 `0.40`。L1 用户消费记入 L1，真实上游成本记入 Moshu。

## 2. 第二阶段目标

把手工配置“每分组一把密钥”升级为正式的 Moshu 代理商协议，使一个 L1 站点可以：

1. 使用一次性授权码注册为 Moshu 的下游代理站；
2. 一键读取 Moshu 授权给它的产品/分组；
3. 自动获得或轮换每个产品的内部凭证；
4. 只读取 Moshu 成本规则，在 L1 独立设置销售倍率；
5. 逐请求对账，明确标准价、Moshu 成本倍率、实际成本、L1 销售价和毛利；
6. 将 L1 搬到其他服务器时不改变协议和数据库边界。

第二阶段不应设计“一把超级密钥直接访问 Moshu 全部分组”。推荐由 Moshu 为每个代理商、每个授权产品生成独立内部凭证，再由注册接口把这些凭证作为一个产品包下发。这样可单独吊销、轮换和限制分组，不会扩大泄露影响面。

## 3. Moshu 主站需要新增的对象

建议新增以下表或等价领域对象，表名可按现有代码规范调整：

### `reseller_tenants`

- `id`
- `name`
- `status`
- `protocol_version`
- `allowed_cidrs`（可选）
- `created_at`、`updated_at`

### `reseller_enrollment_codes`

- 一次性、短有效期、只保存哈希；
- 使用后立即失效；
- 绑定目标代理商和允许的产品范围。

### `reseller_products`

- `reseller_id`
- `moshu_group_id`
- `product_code`（稳定、不因分组改名而变化）
- `display_name`
- `enabled`
- `cost_rate_multiplier`
- `price_catalog_version`
- 可用模型与能力快照

### `reseller_credentials`

- `reseller_id`
- `product_id`
- Moshu 内部 API Key ID；
- 凭证状态、过期时间、最近轮换时间；
- 不保存可逆明文副本，首次签发后只返回一次。

### `reseller_request_settlements`

至少保存：

- `request_id`、`upstream_request_id`
- `reseller_id`、`product_id`、`moshu_group_id`
- 请求模型、实际上游模型、service tier
- 输入、输出、缓存读写、reasoning、图片、音频等计费量
- `standard_cost`
- `cost_rate_multiplier`
- `actual_cost`
- `price_catalog_version`
- 状态、错误类型、开始与完成时间

此记录是 Moshu 与 L1 对账的权威成本凭据，不能让 L1 自行推算后覆盖。

## 4. Moshu 代理商 API

建议建立独立版本前缀，例如 `/api/reseller/v1`：

- `POST /enrollments/exchange`：使用一次性授权码完成 L1 注册；返回代理站身份、短期访问令牌和产品清单。
- `GET /catalog`：读取授权产品、成本倍率、模型范围和价格目录版本，支持 ETag/版本增量。
- `POST /credentials/rotate`：按产品轮换内部凭证；旧凭证保留短暂重叠期后自动失效。
- `GET /settlements`：按时间和游标分页拉取代理商自己的请求成本记录。
- `GET /settlements/{request_id}`：查询单次请求的最终成本。
- `GET /balance`：读取代理商账户余额、冻结额和预警状态（若采用预付模式）。
- `POST /webhook-test`：验证回调地址和签名配置（可选）。

模型调用仍使用现有 OpenAI/Anthropic 兼容入口，不另造推理协议。产品凭证应天然限定到对应 Moshu 分组。

## 5. 认证与安全要求

- 管理协议使用代理商专用身份，绝不复用 Moshu 超级管理员 Token。
- 一次性授权码只保存哈希，短时有效，兑换时绑定代理站实例 ID。
- 管理 API 使用短期 JWT 或签名令牌；刷新凭证需可吊销。
- 每次请求携带 `X-Reseller-Request-ID`，Moshu 做幂等去重。
- 可增加 HMAC 请求签名、时间戳与 nonce，防止重放。
- 跨服务器后建议使用 HTTPS；条件允许时增加 mTLS。
- Moshu 只返回该代理商被授权的数据，所有查询必须强制 tenant scope。
- 任何响应、日志和审计记录都不得输出 API Key 明文。

## 6. L1 第二阶段需要新增的功能

### Moshu 接入向导

- 输入一次性授权码；
- 显示 Moshu 站点身份和协议版本；
- 选择 Moshu 已授权产品；
- 一键创建或更新 L1 对应销售分组；
- 只允许设置销售倍率、销售名称、用户覆盖和限额。

### 本地数据对象

建议新增：

- `moshu_reseller_connection`
- `moshu_products`
- `moshu_catalog_sync_runs`
- `moshu_settlement_cursors`
- `moshu_request_profit_records`

成本倍率、产品代码、Moshu 分组 ID 和价格目录版本为只读字段。销售倍率属于 L1，可由朋友自行调整。

### 请求与结算

1. L1 接收客户请求并生成全局唯一 `reseller_request_id`；
2. 根据 L1 销售分组选择对应 Moshu 产品凭证；
3. 请求发给 Moshu，并传递幂等请求 ID；
4. L1 按自己的销售倍率向客户预扣；
5. 请求完成后读取 Moshu 返回的成本元数据或异步结算记录；
6. L1 完成退款/补扣并记录毛利。

流式请求必须采用“预冻结 + 最终结算”，不能只依赖连接断开时的本地 token 估算。

### 毛利与防倒挂

每条记录至少展示：

`标准价 × Moshu 成本倍率 = Moshu 实际成本`

`标准价 × L1 销售倍率 = L1 客户扣费`

`L1 客户扣费 - Moshu 实际成本 = 毛利`

当销售倍率低于成本倍率加安全缓冲时，默认禁止保存；只有站点所有者可显式放行免费或亏损产品。

## 7. 同步规则

- 产品目录按版本同步，L1 保存上次成功版本和 ETag。
- Moshu 删除授权时，L1 将产品标记为不可售，不物理删除历史记录。
- 成本倍率变化时，从 Moshu 指定的生效时间开始应用，不回写历史结算。
- L1 销售倍率绝不反向同步到 Moshu。
- 凭证轮换与目录同步分开，避免普通价格更新导致调用中断。
- 同步失败保留上次有效目录并报警，不自动开放未知模型。

## 8. 迁移到其他服务器

迁移 L1 时仅迁移：

- `api-L1` 镜像或源码版本；
- `postgres-L1` 数据；
- `redis-L1` 仅在确需保留短期队列时迁移，通常可重建；
- L1 数据目录和环境变量；
- 新服务器域名、HTTPS 和 Moshu 允许的出口 IP。

迁移后在 Moshu 控制台重新登记实例并轮换代理商管理令牌/产品凭证。不要复制 Moshu 数据库或 Moshu 管理员凭证。

## 9. 回滚策略

- 协议按版本发布，第一版固定为 `v1`；
- 第二阶段启用前保留现有每分组密钥；
- 若新协议异常，可临时切回旧密钥，不改变 L1 用户、余额和销售分组；
- Moshu 新表采用增量迁移，不修改或删除现有用量、用户、账号、分组表；
- 所有新功能用独立开关控制，关闭后不影响 Moshu 原生产调用链。

## 10. 验收标准

- L1 管理员无法通过 UI 或 API 接入非 Moshu 上游；
- Moshu 可单独授权、停用和轮换每个代理商产品；
- Moshu 与 L1 对同一 `request_id` 的成本金额一致；
- L1 能独立管理客户、余额、密钥、销售倍率和销售报表；
- 成本倍率和价格目录只能来自 Moshu；
- 流式中断、重试、上游错误和重复请求均不会重复扣费；
- L1 可在不迁移 Moshu 数据的情况下搬到另一台服务器；
- 回滚不会丢失 L1 用户、余额、用量和历史利润记录。

