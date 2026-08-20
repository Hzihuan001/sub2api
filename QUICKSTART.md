# sub2api · Cursor 平台 开箱即用

本仓库在 sub2api 基础上新增了 **Cursor 账号平台**：把 Cursor 订阅账号当上游，对外提供 OpenAI 兼容 API（`/v1/chat/completions`、动态 `/v1/models`），对话走 Cursor 的 `api5 agent.v1.AgentService/Run`。

完整部署 + 端到端验证手册见 [`docs/CURSOR_DEPLOY_E2E_RUNBOOK_CN.md`](docs/CURSOR_DEPLOY_E2E_RUNBOOK_CN.md)。下面是最快路径。

## 一、Linux（Docker Compose，推荐）

```bash
git clone https://github.com/SJwen0/cursor--.git
cd cursor--/deploy
cp .env.example .env
# 编辑 .env：至少设 POSTGRES_PASSWORD / JWT_SECRET / TOTP_ENCRYPTION_KEY / ADMIN_EMAIL / ADMIN_PASSWORD
#（可选：RUN_MODE=simple 跳过计费，便于试用）
docker compose up -d --build   # 本地构建镜像（官方镜像不含 Cursor 代码，必须 --build）
curl -fsS http://127.0.0.1:8080/health   # {"status":"ok"}
```

> Windows 装了 Docker Desktop（WSL2 后端）用同样的命令；无 Docker 的原生方式见手册 §3.2。

## 二、接入 Cursor 账号（管理台或 API）

1. 管理台登录（`ADMIN_EMAIL`/`ADMIN_PASSWORD`）。
2. 新建**平台=cursor** 的分组。
3. 新建 Cursor 账号：平台选 Cursor，走**深链 OAuth**（生成链接 → 浏览器确认 → 自动取令牌），或粘贴 `WorkosCursorSessionToken` / `crsr_` API Key。
4. 在该分组下建一个 API Key。

（管理台是 `/api/v1/admin/*`，网关是 `/v1/*`；详细 curl 见手册 §4。）

## 三、调用

```bash
curl -sS http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer <你的 API Key>" \
  -H 'Content-Type: application/json' \
  -d '{"model":"auto","messages":[{"role":"user","content":"你好"}]}'
```

- **model 用 `auto`**（免费账号只有 Auto；`auto` 会映射到上游默认模型。传具体名要账号套餐支持）。
- `GET /v1/models` 返回该账号可用模型（首次是内置兜底表，成功对话后异步同步为上游完整清单）。

## 关键环境旋钮（Cursor 对话链路）

| 变量 | 默认 | 说明 |
|------|------|------|
| `SUB2API_CURSOR_AGENT_BASE_URL` | `https://agentn.global.api5.cursor.sh` | api5 对话 host，区域可换 `agentn.us.api5.cursor.sh` |
| `SUB2API_CURSOR_AGENT_CLIENT_VERSION` | `cli-2026.08.11-e8db854` | 遇 `permission_denied` 时从 `https://cursor.com/install` 取当前版本替换 |
| `SUB2API_CURSOR_AGENT_IDLE_TIMEOUT` | `30s` | 空闲兜底；长回答被截断可调大 |

> 服务器出站需能访问 `*.api5.cursor.sh`（HTTP/2）。
