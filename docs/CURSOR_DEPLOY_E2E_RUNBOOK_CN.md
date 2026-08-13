# sub2api Cursor 平台部署 + 整链 e2e Runbook（Linux / Windows 双环境）

> 适用提交：`9a8788f3a`（分支 `main`，含 Cursor 平台适配一期 + api5 `agent.v1.AgentService/Run` 对话链路二期）。
> 本文所有命令可直接照抄。凡出现 `<...>` 的都是占位符，替换后再执行。
> **不要把真实 Cursor 令牌写进任何提交、日志或截图。**

---

## 0. 先读这一节：三个必须知道的前置事实

### 0.1 官方镜像里没有 Cursor 代码，必须本地构建

`deploy/docker-compose.yml` 里写死的是 `image: weishaw/sub2api:latest`（上游 Docker Hub 镜像）。
Cursor 适配只存在于你本地的 `9a8788f3a`，**没有推到 GitHub**，所以那个镜像里不含本功能。
Linux 走 Docker 时必须**自己 build 镜像**（第 2 节给了完整步骤 + 一个 override 文件）。

### 0.2 建 `platform=cursor` 分组会被 400 拦住 —— 必须先打一行补丁

`backend/internal/handler/admin/group_handler.go` 的 gin 绑定标签还没加 `cursor`：

```go
// 第 101 行（CreateGroupRequest）与第 169 行（UpdateGroupRequest）
Platform string `json:"platform" binding:"omitempty,oneof=anthropic openai gemini antigravity grok composite"`
```

service 层（`isValidPlatform` / `NormalizeGroupPlatform` / `defaultModelsListCandidateIDs`）和前端下拉框都已经支持 `cursor`，
唯独这个 handler 绑定标签漏了。结果：**不管走 Web UI 还是 curl，`POST /api/v1/admin/groups` 传 `platform=cursor` 都会返回
`400 Invalid request: ... 'oneof' tag`**。

两条路，任选其一（推荐 A）：

**A. 打补丁后重新构建**（一行 sed，见 2.3 / 3.3）
**B. 不改代码，用 SQL 兜底**：先用 `platform=grok` 建分组，再 `UPDATE groups SET platform='cursor' WHERE id=<GID>;`，然后重启服务让调度快照重建（见 4.2 备选）。

`groups.platform` 是普通 `VARCHAR(50)`，没有 CHECK 约束，所以 B 是安全的。

### 0.3 跨平台是真的跨平台

| 事项 | 说明 |
|------|------|
| Go 后端 | `CGO_ENABLED=0` 纯 Go，Windows / Linux 都原生编译运行，无需 CGO 工具链 |
| `internal/pkg/cursor` | 纯 Go，无系统调用依赖 |
| api5 对话链路（`AgentService/Run`） | `BuildAgentHeaders` 只发 10 个 header，**完全不发** `x-cursor-client-os` / `-arch` / `-device-type` / `x-cursor-checksum`，所以宿主 OS 与上游无关 |
| api2 链路（`AvailableModels`） | 会发 `x-cursor-client-os`，默认取宿主：Windows → `win32`，Linux → `linux`。这只是一个身份标识值，两个值上游都接受 |
| 数据库迁移 | `222_add_cursor_platform.sql` 在**每次启动**都会自动执行（见 0.4），Windows / Linux 一致 |

### 0.4 迁移是每次启动自动跑的

两处都会跑，不需要手动执行任何 SQL：

- 首次启动（`AUTO_SETUP=true`）：`setup.AutoSetupFromEnv()` → `repository.ApplyMigrations()`
- **每次**启动：`repository` 初始化 ent client 时调用 `applyMigrationsFS()`（`backend/internal/repository/ent.go:72`）

机制：PostgreSQL advisory lock 串行化 + `schema_migrations` 表按文件名去重 + SHA256 校验和防篡改。
所以升级只要「换二进制/镜像 → 重启」即可，`222_add_cursor_platform.sql` 会自动补上。

---

## 1. 把代码送上服务器（不经过 GitHub）

origin 指向 `https://github.com/Wei-Shaw/sub2api.git`（别人的仓库），**不要 push**。用下面两种离线搬运方式。

### 1.1 方式一：git bundle（推荐，保留完整历史）

**本机（Windows PowerShell，仓库根目录 `D:\sjwen\sub2api`）：**

```powershell
cd D:\sjwen\sub2api
git bundle create D:\sub2api.bundle --all HEAD
git bundle verify D:\sub2api.bundle
# 确认 HEAD 是 9a8788f3a
git log --oneline -1
scp D:\sub2api.bundle <user>@<server>:/opt/
```

> `--all HEAD` 两个都写：`--all` 带上所有分支/标签，`HEAD` 让 clone 出来能直接检出到正确分支，
> 否则会看到 `remote HEAD refers to nonexistent ref` 警告。

**服务器（Linux）：**

```bash
cd /opt
git clone /opt/sub2api.bundle sub2api
cd sub2api
git checkout main
git log --oneline -1        # 必须看到 9a8788f3a
```

**如果目标机也是 Windows（PowerShell）：**

```powershell
git clone C:\sub2api.bundle C:\srv\sub2api
cd C:\srv\sub2api
git checkout main
git log --oneline -1
```

### 1.2 方式二：整树打包（不要历史，最快）

**Windows → Linux**（Win10/11 自带 `tar.exe`）：

```powershell
cd D:\sjwen
tar --exclude=node_modules --exclude=.git --exclude=dist --exclude=bin `
    -czf D:\sub2api-src.tar.gz sub2api
scp D:\sub2api-src.tar.gz <user>@<server>:/opt/
```

```bash
cd /opt && tar xzf sub2api-src.tar.gz && cd sub2api
```

**Linux → Linux**（rsync，可增量重传）：

```bash
rsync -az --delete \
  --exclude node_modules --exclude .git --exclude dist --exclude bin \
  /opt/sub2api/ <user>@<server>:/opt/sub2api/
```

**Windows → Windows**（局域网复制）：

```powershell
robocopy D:\sjwen\sub2api C:\srv\sub2api /E /XD node_modules .git dist bin
```

> 注意：方式二没有 `.git`，`backend/scripts/resolve-version.sh` 取不到 tag，
> 构建时请显式传版本号（下文命令里已经带了）。

---

## 2. Linux 部署（Docker Compose，主推）

### 2.1 组件与端口

`deploy/docker-compose.yml` 起三个服务，同处 bridge 网络 `sub2api-network`：

| 服务 | 镜像 | 端口 | 数据卷 |
|------|------|------|--------|
| `sub2api` | `weishaw/sub2api:latest`（本文改成本地构建） | 宿主 `${BIND_HOST:-0.0.0.0}:${SERVER_PORT:-8080}` → 容器 `8080` | `sub2api_data` → `/app/data` |
| `postgres` | `postgres:18-alpine` | **不暴露到宿主** | `postgres_data` → `/var/lib/postgresql/data`（`PGDATA` 已显式指定） |
| `redis` | `redis:8-alpine` | **不暴露到宿主** | `redis_data` → `/data` |

`deploy/docker-compose.local.yml` 是同一套，但用 `./data`、`./postgres_data`、`./redis_data` 三个本地目录代替命名卷，整目录 `tar` 走就能迁移。**下文用默认的 `docker-compose.yml`**；用 local 版只需把命令里的文件名换掉，并记得 `mkdir -p data postgres_data redis_data`。

### 2.2 准备 `.env`

```bash
cd /opt/sub2api/deploy
cp .env.example .env
chmod 600 .env
```

**必填 / 强烈建议填的项**（其余保持 `.env.example` 默认即可）：

```bash
# 生成密钥
POSTGRES_PASSWORD="$(openssl rand -hex 24)"
JWT_SECRET="$(openssl rand -hex 32)"
TOTP_ENCRYPTION_KEY="$(openssl rand -hex 32)"
ADMIN_PASSWORD="$(openssl rand -base64 18)"

# 写回 .env（先删掉模板里的同名行，避免重复键）
sed -i '/^POSTGRES_PASSWORD=/d;/^JWT_SECRET=/d;/^TOTP_ENCRYPTION_KEY=/d;/^ADMIN_PASSWORD=/d;/^ADMIN_EMAIL=/d' .env
cat >> .env <<EOF
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
JWT_SECRET=${JWT_SECRET}
TOTP_ENCRYPTION_KEY=${TOTP_ENCRYPTION_KEY}
ADMIN_EMAIL=admin@sub2api.local
ADMIN_PASSWORD=${ADMIN_PASSWORD}
EOF

echo "记下来：ADMIN_PASSWORD=${ADMIN_PASSWORD}"
```

| 变量 | 是否必需 | 说明 |
|------|---------|------|
| `POSTGRES_PASSWORD` | **必需** | compose 里写了 `${POSTGRES_PASSWORD:?...}`，不设直接启动失败 |
| `JWT_SECRET` | 强烈建议 | 留空则每次启动随机生成→所有人被登出。`openssl rand -hex 32` |
| `TOTP_ENCRYPTION_KEY` | 强烈建议 | 留空则每次启动随机→已配置的 2FA 全部失效 |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD` | 建议 | 首启自动建管理员。密码留空会随机生成并**只在首启日志里打印一次** |
| `SERVER_PORT` | 可选 | 宿主监听端口，默认 8080 |
| `BIND_HOST` | 可选 | 默认 `0.0.0.0`；只想本机访问就设 `127.0.0.1` |
| `TZ` | 可选 | 默认 `Asia/Shanghai`，影响所有时间统计口径 |
| `RUN_MODE` | 可选 | `standard`（默认，走计费/余额校验）或 `simple`（跳过计费，**e2e 最省事**） |
| `POSTGRES_USER` / `POSTGRES_DB` | 可选 | 默认都是 `sub2api` |

> **e2e 提速建议**：把 `RUN_MODE=simple` 写进 `.env`。standard 模式下管理员初始余额为 0，
> 网关会因余额不足直接拒绝请求，你得先手动充值（第 4.4 节给了充值命令）。

### 2.3 打补丁（0.2 节的必要步骤）

```bash
cd /opt/sub2api

# 分组创建 / 编辑接口放行 cursor
sed -i 's/oneof=anthropic openai gemini antigravity grok composite/oneof=anthropic openai gemini antigravity grok cursor composite/g' \
  backend/internal/handler/admin/group_handler.go

# 可选：让 composite 分组也能路由到 cursor
sed -i 's/binding:"required,oneof=anthropic openai gemini antigravity grok"/binding:"required,oneof=anthropic openai gemini antigravity grok cursor"/' \
  backend/internal/handler/admin/group_handler.go

# 确认改到了（应看到 3 行都含 cursor）
grep -n 'oneof=anthropic openai gemini' backend/internal/handler/admin/group_handler.go
```

### 2.4 构建镜像

```bash
cd /opt/sub2api
docker build -t sub2api:local \
  --build-arg VERSION="$(cat backend/cmd/server/VERSION)" \
  --build-arg COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo 9a8788f3a)" \
  --build-arg GOPROXY=https://goproxy.cn,direct \
  --build-arg GOSUMDB=sum.golang.google.cn \
  --build-arg NPM_CONFIG_REGISTRY=https://registry.npmmirror.com \
  -f Dockerfile .
```

- 三段构建：`node:24-alpine`（pnpm 9 构建前端）→ `golang:1.26.5-alpine`（`CGO_ENABLED=0 -tags embed` 交叉编译）→ `alpine:3.21`（运行时，附带版本匹配的 `pg_dump`/`psql`）。
- 海外机器把 `GOPROXY` / `GOSUMDB` / `NPM_CONFIG_REGISTRY` 三个 build-arg 删掉即可。
- 构建需要 `docs/legal/` 目录（前端合规页 build 期 import），第 1 节两种搬运方式都会带上。
- 首次构建约 5–15 分钟，之后有 BuildKit cache mount 会快很多。

### 2.5 写一个 override 文件（换镜像 + 注入 Cursor env）

`deploy/docker-compose.yml` 的 `environment:` 是**显式白名单**，`.env` 里写 `SUB2API_CURSOR_AGENT_*` **不会**传进容器。用 override 一次解决「换成本地镜像」和「注入 Cursor 旋钮」两件事：

```bash
cat > /opt/sub2api/deploy/docker-compose.override.yml <<'EOF'
services:
  sub2api:
    image: sub2api:local
    pull_policy: never
    environment:
      # 留空则用代码内默认值；这里显式写出来便于排障时改
      - SUB2API_CURSOR_AGENT_BASE_URL=${SUB2API_CURSOR_AGENT_BASE_URL:-https://agentn.global.api5.cursor.sh}
      - SUB2API_CURSOR_AGENT_CLIENT_VERSION=${SUB2API_CURSOR_AGENT_CLIENT_VERSION:-cli-2026.08.11-e8db854}
      - SUB2API_CURSOR_AGENT_GHOST_MODE=${SUB2API_CURSOR_AGENT_GHOST_MODE:-true}
      - SUB2API_CURSOR_AGENT_FIRST_BYTE_TIMEOUT=${SUB2API_CURSOR_AGENT_FIRST_BYTE_TIMEOUT:-60s}
      - SUB2API_CURSOR_AGENT_IDLE_TIMEOUT=${SUB2API_CURSOR_AGENT_IDLE_TIMEOUT:-4s}
EOF
```

> `docker-compose.override.yml` 只在**不带 `-f`** 执行 `docker compose` 时自动叠加。
> 如果你坚持用 `docker-compose.local.yml`，必须显式写两个 `-f`：
> `docker compose -f docker-compose.local.yml -f docker-compose.override.yml up -d`

### 2.6 启动 + 健康检查

```bash
cd /opt/sub2api/deploy
docker compose up -d
docker compose ps
docker compose logs -f sub2api      # Ctrl-C 退出
```

首启日志里应依次出现：

```
Auto setup mode enabled...
Testing database connection... / Database connection successful
Testing Redis connection...    / Redis connection successful
Initializing database...       / Database initialized successfully
Creating admin user...         / Admin user created: admin@sub2api.local
Configuration file created / Installation lock created
Auto setup completed successfully!
Server started on 0.0.0.0:8080
```

如果 `ADMIN_PASSWORD` 留空了，捞一次随机密码（只打印这一次）：

```bash
docker compose logs sub2api | grep -i "Generated admin password"
```

健康检查与迁移确认：

```bash
curl -fsS http://127.0.0.1:8080/health && echo
# → {"status":"ok"}

# 确认 Cursor 迁移已落库
docker compose exec -T postgres psql -U sub2api -d sub2api \
  -c "SELECT filename, applied_at FROM schema_migrations WHERE filename LIKE '222%';"
```

### 2.7 常用运维命令

```bash
cd /opt/sub2api/deploy
docker compose logs -f sub2api          # 跟日志
docker compose restart sub2api          # 改了 env 后重启（Cursor 旋钮是进程级单次读取，必须重启才生效）
docker compose down                     # 停止（保留数据卷）
docker compose down -v                  # 停止并删数据（慎用）
docker compose exec -T postgres psql -U sub2api -d sub2api   # 进数据库
```

升级流程（改代码之后）：

```bash
cd /opt/sub2api && docker build -t sub2api:local -f Dockerfile . \
  && cd deploy && docker compose up -d
```

迁移在新容器启动时自动应用，无需额外动作。

---

## 3. Windows 部署

### 3.1 方式 A：Docker Desktop（与 Linux 命令完全一致）

前置：Docker Desktop 已启用 **WSL 2 backend**（Settings → General → Use the WSL 2 based engine）。

```powershell
cd C:\srv\sub2api\deploy
Copy-Item .env.example .env
```

生成密钥并写入 `.env`（PowerShell 没有 `openssl`，用 .NET 生成）：

```powershell
function New-Hex32 {
  $b = New-Object byte[] 32
  [Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($b)
  ($b | ForEach-Object { $_.ToString('x2') }) -join ''
}

$pg    = New-Hex32
$jwt   = New-Hex32
$totp  = New-Hex32
$admin = New-Hex32

$path = "C:\srv\sub2api\deploy\.env"
$keep = Get-Content $path | Where-Object {
  $_ -notmatch '^(POSTGRES_PASSWORD|JWT_SECRET|TOTP_ENCRYPTION_KEY|ADMIN_EMAIL|ADMIN_PASSWORD)='
}
$keep += @(
  "POSTGRES_PASSWORD=$pg"
  "JWT_SECRET=$jwt"
  "TOTP_ENCRYPTION_KEY=$totp"
  "ADMIN_EMAIL=admin@sub2api.local"
  "ADMIN_PASSWORD=$admin"
)
# 必须用 LF：容器内的 sh 读到 CRLF 会把 \r 当成值的一部分
[IO.File]::WriteAllText($path, ($keep -join "`n") + "`n")

Write-Host "ADMIN_PASSWORD=$admin"
```

打补丁（等价于 2.3）：

```powershell
$p = "C:\srv\sub2api\backend\internal\handler\admin\group_handler.go"
$s = [IO.File]::ReadAllText($p)
$s = $s.Replace(
  'oneof=anthropic openai gemini antigravity grok composite',
  'oneof=anthropic openai gemini antigravity grok cursor composite')
$s = $s.Replace(
  'binding:"required,oneof=anthropic openai gemini antigravity grok"',
  'binding:"required,oneof=anthropic openai gemini antigravity grok cursor"')
[IO.File]::WriteAllText($p, $s)
Select-String -Path $p -Pattern 'oneof=anthropic openai gemini'
```

构建 + override + 启动（命令与 Linux 相同，只有路径写法不同）：

```powershell
cd C:\srv\sub2api
docker build -t sub2api:local `
  --build-arg VERSION=(Get-Content backend\cmd\server\VERSION) `
  --build-arg COMMIT=(git rev-parse --short HEAD) `
  --build-arg GOPROXY=https://goproxy.cn,direct `
  --build-arg GOSUMDB=sum.golang.google.cn `
  --build-arg NPM_CONFIG_REGISTRY=https://registry.npmmirror.com `
  -f Dockerfile .

$override = @'
services:
  sub2api:
    image: sub2api:local
    pull_policy: never
    environment:
      - SUB2API_CURSOR_AGENT_BASE_URL=https://agentn.global.api5.cursor.sh
      - SUB2API_CURSOR_AGENT_CLIENT_VERSION=cli-2026.08.11-e8db854
      - SUB2API_CURSOR_AGENT_GHOST_MODE=true
      - SUB2API_CURSOR_AGENT_FIRST_BYTE_TIMEOUT=60s
      - SUB2API_CURSOR_AGENT_IDLE_TIMEOUT=4s
'@
[IO.File]::WriteAllText("C:\srv\sub2api\deploy\docker-compose.override.yml", $override)

cd C:\srv\sub2api\deploy
docker compose up -d
docker compose ps
docker compose logs -f sub2api
```

**Windows 上的差异点：**

| 差异 | 说明 |
|------|------|
| 换行符 | `.env` 和 `.sh` 必须是 LF。CRLF 会让 `POSTGRES_PASSWORD` 末尾多个 `\r`，表现为 PG 认证失败。上面的写法已强制 LF。仓库根建议加 `git config core.autocrlf false` |
| 数据卷路径 | 用**命名卷**（默认 `docker-compose.yml`）比 `docker-compose.local.yml` 的 bind mount 稳：NTFS bind mount 到 `postgres:18-alpine` 会有 owner/权限问题，PG 可能拒绝启动 |
| WSL2 后端 | 必须开启；Hyper-V/Windows containers 模式跑不了这些 Linux 镜像 |
| 行连接符 | PowerShell 用反引号 `` ` ``，不是 `\` |
| 资源 | WSL2 默认内存可能不够 Node 构建前端。`%USERPROFILE%\.wslconfig` 里设 `memory=8GB` |

### 3.2 方式 B：原生无 Docker

#### 3.2.1 装 PostgreSQL

选任一：

- **安装版**：<https://www.enterprisedb.com/downloads/postgres-postgresql-downloads>，一路默认，端口 5432，记住 `postgres` 超级用户密码。
- **便携版（zip）**：解压后

```powershell
cd C:\pgsql\bin
.\initdb.exe -D C:\pgsql\data -U postgres --encoding=UTF8 --locale=C
.\pg_ctl.exe -D C:\pgsql\data -l C:\pgsql\logfile.txt start
```

建库建用户（`sub2api` 需要 CREATEDB 权限：AUTO_SETUP 会先连 `postgres` 维护库、检测目标库不存在时自动 `CREATE DATABASE`）：

```powershell
# 用 127.0.0.1 而不是 localhost：Windows 上 localhost 会先试 IPv6 (::1) 再回退
& "C:\Program Files\PostgreSQL\18\bin\psql.exe" -U postgres -h 127.0.0.1 -c "CREATE USER sub2api WITH PASSWORD 'sub2api' CREATEDB;"
& "C:\Program Files\PostgreSQL\18\bin\psql.exe" -U postgres -h 127.0.0.1 -c "CREATE DATABASE sub2api OWNER sub2api;"
```

#### 3.2.2 装 Redis

选任一：

- **tporadowski/redis**（免费，Redis 5 兼容，够用）：
  <https://github.com/tporadowski/redis/releases> 下载 `Redis-x64-*.zip` 或 `.msi`

```powershell
# zip 版前台跑
cd C:\redis
.\redis-server.exe --port 6379 --save 60 1 --appendonly yes

# msi 版注册成服务
Start-Service Redis
```

- **Memurai**（商业，Redis 7 兼容，有 Developer 免费档）：<https://www.memurai.com/get-memurai>

验证：

```powershell
.\redis-cli.exe -h 127.0.0.1 -p 6379 ping   # → PONG
```

#### 3.2.3 构建 exe

需要 Go **1.26.5**（`backend/go.mod` 里硬性要求）和 pnpm。

```powershell
cd C:\srv\sub2api

# 先打 0.2 的补丁（见 3.1 的 PowerShell 片段）

# 1) 构建前端 —— vite 输出到 backend\internal\web\dist
cd frontend
pnpm install --frozen-lockfile
pnpm run build
cd ..

# 2) 构建后端（-tags embed 把前端打进 exe）
cd backend
$env:CGO_ENABLED = "0"
go build -tags embed `
  -ldflags "-s -w -X main.Version=$(Get-Content cmd\server\VERSION) -X main.Commit=$(git rev-parse --short HEAD) -X main.BuildType=release" `
  -trimpath -o ..\bin\sub2api.exe .\cmd\server
cd ..
.\bin\sub2api.exe -version
```

> 只想跑 API、不要 Web 管理界面？跳过前端构建，去掉 `-tags embed` 即可。
> 后端所有 API（含本文的全部 e2e 步骤）照常工作，只有访问 `/` 会返回
> `Frontend not embedded. Build with -tags embed to include frontend.`
>
> **不要**用 `make build`：`backend/Makefile` 依赖 `scripts/resolve-version.sh`（bash），Windows 上跑不了。

#### 3.2.4 设置环境变量并启动

`DATA_DIR` 决定 `config.yaml` 和 `.installed` 落在哪；不设的话会落到当前工作目录。

```powershell
$env:DATA_DIR   = "C:\srv\sub2api-data"
New-Item -ItemType Directory -Force -Path $env:DATA_DIR | Out-Null

# 首启自动初始化（不弹 Web 安装向导）
$env:AUTO_SETUP        = "true"
$env:SERVER_HOST       = "0.0.0.0"
$env:SERVER_PORT       = "8080"
$env:SERVER_MODE       = "release"
$env:RUN_MODE          = "simple"     # e2e 省事；正式环境改 standard

$env:DATABASE_HOST     = "127.0.0.1"
$env:DATABASE_PORT     = "5432"
$env:DATABASE_USER     = "sub2api"
$env:DATABASE_PASSWORD = "sub2api"
$env:DATABASE_DBNAME   = "sub2api"
$env:DATABASE_SSLMODE  = "disable"

$env:REDIS_HOST        = "127.0.0.1"
$env:REDIS_PORT        = "6379"
$env:REDIS_PASSWORD    = ""
$env:REDIS_DB          = "0"

$env:ADMIN_EMAIL       = "admin@sub2api.local"
$env:ADMIN_PASSWORD    = "Admin#12345678"
$env:JWT_SECRET        = (New-Hex32)      # 复用 3.1 里定义的函数
$env:TOTP_ENCRYPTION_KEY = (New-Hex32)
$env:JWT_EXPIRE_HOUR   = "24"
$env:TZ                = "Asia/Shanghai"

# Cursor 专属旋钮（进程级默认；留空则用代码内默认值）
$env:SUB2API_CURSOR_AGENT_BASE_URL          = "https://agentn.global.api5.cursor.sh"
$env:SUB2API_CURSOR_AGENT_CLIENT_VERSION    = "cli-2026.08.11-e8db854"
$env:SUB2API_CURSOR_AGENT_GHOST_MODE        = "true"
$env:SUB2API_CURSOR_AGENT_FIRST_BYTE_TIMEOUT = "60s"
$env:SUB2API_CURSOR_AGENT_IDLE_TIMEOUT      = "4s"

C:\srv\sub2api\bin\sub2api.exe
```

另开一个 PowerShell 窗口验证：

```powershell
curl.exe -fsS http://127.0.0.1:8080/health
# → {"status":"ok"}
```

> **注意 `$env:` 只在当前 PowerShell 会话有效。** 要固化：
> `[Environment]::SetEnvironmentVariable("SUB2API_CURSOR_AGENT_BASE_URL","https://agentn.global.api5.cursor.sh","Machine")`
> 或用 NSSM / `sc.exe` 注册成 Windows 服务时在服务配置里写死。

#### 3.2.5 Linux 原生（无 Docker）补充

同样可行，命令换成 bash：

```bash
cd /opt/sub2api/frontend && pnpm install --frozen-lockfile && pnpm run build
cd /opt/sub2api/backend
CGO_ENABLED=0 go build -tags embed \
  -ldflags "-s -w -X main.Version=$(cat cmd/server/VERSION) -X main.Commit=$(git rev-parse --short HEAD) -X main.BuildType=release" \
  -trimpath -o ../bin/sub2api ./cmd/server

export DATA_DIR=/var/lib/sub2api AUTO_SETUP=true RUN_MODE=simple
export DATABASE_HOST=127.0.0.1 DATABASE_USER=sub2api DATABASE_PASSWORD=sub2api DATABASE_DBNAME=sub2api DATABASE_SSLMODE=disable
export REDIS_HOST=127.0.0.1 REDIS_PORT=6379
export ADMIN_EMAIL=admin@sub2api.local ADMIN_PASSWORD='Admin#12345678'
export JWT_SECRET=$(openssl rand -hex 32) TOTP_ENCRYPTION_KEY=$(openssl rand -hex 32)
mkdir -p "$DATA_DIR"
/opt/sub2api/bin/sub2api
```

`deploy/sub2api.service` 是现成的 systemd unit，可以拿来改。

---

## 4. 建数据（Linux / Windows 通用，走本机 admin API）

以下所有请求打 `http://127.0.0.1:8080`。管理面 API 前缀是 `/api/v1`，网关前缀是 `/v1`。
统一响应封装：`{"code":0,"message":"success","data":{...}}`。

### 4.0 先拿到那个 Cursor session JWT

`<CURSOR_SESSION_JWT>` 指账号凭据里的 `access_token`。三种来源：

**（1）前端深链 OAuth（推荐，全自动）**
Web 管理台 → 账号 → 新建 → 平台选 Cursor → 走 OAuth 授权。底层调的是：

```
POST /api/v1/admin/cursor/oauth/auth-url    → {auth_url, session_id, state}
   浏览器打开 auth_url（https://cursor.com/loginDeepControl?challenge=..&uuid=..&mode=login）并确认
POST /api/v1/admin/cursor/oauth/poll        → {access_token, refresh_token, expires_at, sub}
   未确认时返回 200 + {"status":"pending"}，前端会轮询
```

也可以直接建号一步到位：`POST /api/v1/admin/cursor/sso-to-oauth`，body 里 `sso_tokens` 传粘贴来的凭据数组，返回 `created[].account`。

**（2）已实现的 web → client 兑换**
把浏览器 `cursor.com` 的 `WorkosCursorSessionToken` cookie（`userId::JWT` 形式）粘进去：

```
POST /api/v1/admin/cursor/oauth/sso-token   body: {"sso_token":"<cookie 原文>"}
```

后端 `ImportFromCookie` 本地解析（不发网络请求），把 cookie 同时存为 `web_session_token`。
**注意**：纯 web cookie 单独驱动对话会被上游判 `ERROR_NOT_LOGGED_IN`；账号第一次被调度时，
token provider 会用 `web_session_token` 换一个 client 凭据（`upgradeWebSession`）。
如果粘的是 `crsr_` 开头的 User API Key，走的是 `ImportFromAPIKey`（上游 `/auth/exchange_user_api_key`），可反复兑换。

**（3）离线探针（不起服务，验证令牌本身能不能用）**

```bash
cd /opt/sub2api/backend
export SUB2API_CURSOR_TOKEN='<CURSOR_SESSION_JWT>'
go run ./cmd/cursor_e2e -mode models                       # 走 api2 AvailableModels
go run ./cmd/cursor_e2e -mode agent -prompt "say hi" -model default   # 走 api5 AgentService/Run
go run ./cmd/cursor_e2e -mode exchange                     # web cookie → client token
```

```powershell
cd C:\srv\sub2api\backend
$env:SUB2API_CURSOR_TOKEN = '<CURSOR_SESSION_JWT>'
go run .\cmd\cursor_e2e -mode agent -prompt "say hi" -model default
```

这个探针复用的就是网关同一份 `internal/pkg/cursor`，它能通 = 网关也能通。令牌只从环境变量读，不落盘、不完整打印。

### 4.1 管理员登录

**Linux / macOS：**

```bash
BASE=http://127.0.0.1:8080
ADMIN_TOKEN=$(curl -fsS -X POST "$BASE/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@sub2api.local","password":"<ADMIN_PASSWORD>"}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["access_token"])')
echo "${ADMIN_TOKEN:0:20}..."

ADMIN_UID=$(curl -fsS -X POST "$BASE/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@sub2api.local","password":"<ADMIN_PASSWORD>"}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["user"]["id"])')
echo "admin uid = $ADMIN_UID"
```

**Windows PowerShell：**

```powershell
$Base = "http://127.0.0.1:8080"
$login = Invoke-RestMethod -Method Post -Uri "$Base/api/v1/auth/login" `
  -ContentType 'application/json' `
  -Body (@{ email='admin@sub2api.local'; password='<ADMIN_PASSWORD>' } | ConvertTo-Json)
$AdminToken = $login.data.access_token
$AdminUid   = $login.data.user.id
$H = @{ Authorization = "Bearer $AdminToken" }
"admin uid = $AdminUid"
```

### 4.2 建 `platform=cursor` 分组

> 前提：0.2 的补丁已生效。没打补丁会返回 400。

**Linux：**

```bash
GID=$(curl -fsS -X POST "$BASE/api/v1/admin/groups" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
        "name": "cursor-default",
        "description": "Cursor 平台默认分组",
        "platform": "cursor",
        "rate_multiplier": 1.0,
        "is_exclusive": false,
        "subscription_type": "standard"
      }' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["id"])')
echo "group id = $GID"
```

**Windows：**

```powershell
$group = Invoke-RestMethod -Method Post -Uri "$Base/api/v1/admin/groups" -Headers $H `
  -ContentType 'application/json' -Body (@{
    name              = 'cursor-default'
    description       = 'Cursor 平台默认分组'
    platform          = 'cursor'
    rate_multiplier   = 1.0
    is_exclusive      = $false
    subscription_type = 'standard'
  } | ConvertTo-Json)
$Gid = $group.data.id
"group id = $Gid"
```

- `is_exclusive: false` 很重要：专属分组只有被显式授权的用户能绑 API Key。
- 分组名叫 `cursor-default` 还有个好处：建账号时不传 `group_ids` 会自动绑定 `<platform>-default` 同名分组。

**备选（不打补丁的 SQL 兜底）：**

```bash
# 1) 先用 grok 建（binding 放行）
GID=$(curl -fsS -X POST "$BASE/api/v1/admin/groups" \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"cursor-default","platform":"grok","rate_multiplier":1.0,"is_exclusive":false,"subscription_type":"standard"}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["id"])')

# 2) 改平台（groups.platform 是普通 VARCHAR(50)，无 CHECK 约束）
cd /opt/sub2api/deploy
docker compose exec -T postgres psql -U sub2api -d sub2api \
  -c "UPDATE groups SET platform='cursor' WHERE id=${GID};"

# 3) 重启让调度快照重建（否则最多要等 300s 的全量重建周期）
docker compose restart sub2api
```

### 4.3 建 Cursor 账号

**Linux：**

```bash
CURSOR_JWT='<CURSOR_SESSION_JWT>'

AID=$(curl -fsS -X POST "$BASE/api/v1/admin/accounts" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{
        \"name\": \"cursor-acc-1\",
        \"platform\": \"cursor\",
        \"type\": \"oauth\",
        \"credentials\": {
          \"access_token\": \"${CURSOR_JWT}\",
          \"base_url\": \"https://api2.cursor.sh\"
        },
        \"group_ids\": [${GID}],
        \"concurrency\": 3,
        \"priority\": 50
      }" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["id"])')
echo "account id = $AID"
```

**Windows：**

```powershell
$CursorJwt = '<CURSOR_SESSION_JWT>'

$acc = Invoke-RestMethod -Method Post -Uri "$Base/api/v1/admin/accounts" -Headers $H `
  -ContentType 'application/json' -Body (@{
    name        = 'cursor-acc-1'
    platform    = 'cursor'
    type        = 'oauth'
    credentials = @{
      access_token = $CursorJwt
      base_url     = 'https://api2.cursor.sh'
    }
    group_ids   = @($Gid)
    concurrency = 3
    priority    = 50
  } | ConvertTo-Json -Depth 5)
$Aid = $acc.data.id
"account id = $Aid"
```

凭据键说明：

| 键 | 用途 |
|----|------|
| `access_token` | **必需**。bare JWT 或 `userId::JWT` 两种形式都行，传输层 `ParseToken` 会归一化 |
| `base_url` | 可选，默认 `https://api2.cursor.sh`。**这是 api2**（`AvailableModels` 用），不是 api5 对话 host |
| `refresh_token` | 可选。深链登录才有；API Key 兑换出来的 refresh token 上游不认，改用重新兑换 `api_key` |
| `api_key` | 可选，`crsr_` 开头的 User API Key，可反复兑换新 access token |
| `web_session_token` | 可选，`WorkosCursorSessionToken` cookie。首次调度时用来升级成 client 凭据 |
| `agent_base_url` / `agent_client_version` / `agent_ghost_mode` | 可选，**单账号覆盖** api5 参数，优先级高于进程级 env |

> 也可以把 api5 覆盖放在 `extra` 里：`cursor_agent_base_url` / `cursor_agent_client_version` / `cursor_agent_ghost_mode`。
> 优先级：`credentials` > `extra` > 进程 env > 代码常量。

### 4.4 （仅 `RUN_MODE=standard` 需要）给管理员充值

standard 模式下余额为 0 会被 `checkBalanceEligibility` 直接拒绝：

```bash
curl -fsS -X POST "$BASE/api/v1/admin/users/$ADMIN_UID/balance" \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"balance": 100, "operation": "set", "notes": "e2e"}' | head -c 200; echo
```

```powershell
Invoke-RestMethod -Method Post -Uri "$Base/api/v1/admin/users/$AdminUid/balance" -Headers $H `
  -ContentType 'application/json' `
  -Body (@{ balance=100; operation='set'; notes='e2e' } | ConvertTo-Json) | Out-Null
```

`operation` 取值：`set` / `add` / `subtract`；`balance` 必须 `> 0`。

### 4.5 建 API Key（绑定到 cursor 分组）

**Linux：**

```bash
SK=$(curl -fsS -X POST "$BASE/api/v1/keys" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"cursor-e2e\",\"group_id\":${GID}}" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["key"])')
echo "api key = $SK"
```

**Windows：**

```powershell
$key = Invoke-RestMethod -Method Post -Uri "$Base/api/v1/keys" -Headers $H `
  -ContentType 'application/json' -Body (@{ name='cursor-e2e'; group_id=$Gid } | ConvertTo-Json)
$Sk = $key.data.key
"api key = $Sk"
```

Key 默认 `sk-` 前缀。**必须绑分组**：不带 group 的 Key 会被 `RequireGroupAssignment` 中间件在网关层拦下。

---

## 5. 验证整链

### 5.1 `GET /v1/models`

**Linux：**

```bash
curl -fsS "$BASE/v1/models" -H "Authorization: Bearer $SK" | python3 -m json.tool | head -40
```

**Windows（curl.exe）：**

```powershell
curl.exe -fsS "$Base/v1/models" -H "Authorization: Bearer $Sk"
```

**Windows（原生 PowerShell）：**

```powershell
$KH = @{ Authorization = "Bearer $Sk" }
(Invoke-RestMethod -Uri "$Base/v1/models" -Headers $KH).data | Select-Object -First 20 id
```

预期：`{"object":"list","data":[{"id":"...","object":"model",...}]}`。

- **第一次调用**（账号还没被调度过）返回内置兜底清单 `cursorpkg.DefaultModelIDs()`，13 个 id：
  `auto`、`cursor-small`、`composer-2.5`、`composer-2.5-fast`、`claude-4.5-sonnet`、`claude-4.6-sonnet`、
  `claude-opus-4.8`、`gpt-5`、`gpt-5.6-sol`、`gemini-3-pro`、`gemini-3.5-flash`、`deepseek-v3.1`、`grok-4.6`。
- **跑过一次成功对话之后**，后台会异步拉一次 api2 `AvailableModels`（204 个模型）并写进
  `accounts.extra.cursor_observed_models`（TTL 6 小时），下次 `/v1/models` 就是完整清单。
  想立刻同步也可以：`POST /api/v1/admin/accounts/<AID>/models/sync-upstream`（带 admin token）。

### 5.2 `POST /v1/chat/completions` 非流式

**Linux：**

```bash
curl -sS -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $SK" \
  -H 'Content-Type: application/json' \
  -d '{
        "model": "default",
        "messages": [
          {"role": "user", "content": "用一句中文确认你在线。"}
        ],
        "stream": false
      }' | python3 -m json.tool
```

**Windows（curl.exe，注意 JSON 引号）：**

```powershell
$body = '{"model":"default","messages":[{"role":"user","content":"say hi in one short sentence"}],"stream":false}'
$body | Out-File -Encoding utf8NoBOM C:\temp\chat.json
curl.exe -sS -X POST "$Base/v1/chat/completions" `
  -H "Authorization: Bearer $Sk" `
  -H "Content-Type: application/json" `
  --data-binary "@C:\temp\chat.json"
```

> 走文件是为了绕开 PowerShell 对 `"` 的吞噬。想内联的话用 `--data-binary '{...}'` 配单引号，
> 但含中文时更容易踩编码坑。

**Windows（原生 PowerShell，最省事）：**

```powershell
$resp = Invoke-RestMethod -Method Post -Uri "$Base/v1/chat/completions" -Headers $KH `
  -ContentType 'application/json; charset=utf-8' -Body ([Text.Encoding]::UTF8.GetBytes((@{
    model    = 'default'
    messages = @(@{ role='user'; content='用一句中文确认你在线。' })
    stream   = $false
  } | ConvertTo-Json -Depth 5)))
$resp.choices[0].message.content
```

预期：标准 OpenAI `chat.completion` 结构，`choices[0].message.content` 是真实回复。

### 5.3 `POST /v1/chat/completions` 流式

**Linux：**

```bash
curl -N -sS -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $SK" \
  -H 'Content-Type: application/json' \
  -d '{
        "model": "default",
        "messages": [{"role":"user","content":"数到五，每个数字一行。"}],
        "stream": true
      }'
```

**Windows：**

```powershell
$body = '{"model":"default","messages":[{"role":"user","content":"count to five"}],"stream":true}'
$body | Out-File -Encoding utf8NoBOM C:\temp\chat-stream.json
curl.exe -N -sS -X POST "$Base/v1/chat/completions" `
  -H "Authorization: Bearer $Sk" `
  -H "Content-Type: application/json" `
  --data-binary "@C:\temp\chat-stream.json"
```

预期：一串 `data: {...}` SSE 帧，最后 `data: [DONE]`。`-N` / `--no-buffer` 必须加，否则 curl 会缓冲到结束才吐。

### 5.4 端到端自检清单

```
[ ] curl /health                     → {"status":"ok"}
[ ] schema_migrations 里有 222_add_cursor_platform.sql
[ ] 登录拿到 access_token
[ ] 分组创建成功且 platform == "cursor"
[ ] 账号创建成功且 platform == "cursor" / type == "oauth"
[ ] API Key 创建成功且 group_id == 分组 id
[ ] GET /v1/models 返回 Cursor 模型（先 13 个，跑过对话后 204 个）
[ ] POST /v1/chat/completions 非流式拿到真实回复
[ ] POST /v1/chat/completions stream:true 拿到 SSE + [DONE]
```

---

## 6. Cursor 专属环境变量旋钮

代码位置：`backend/internal/service/openai_gateway_cursor_transport.go`。

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `SUB2API_CURSOR_AGENT_BASE_URL` | `https://agentn.global.api5.cursor.sh` | api5 对话 host。备选：`https://agent.api5.cursor.sh`（direct）、`https://agentn.us.api5.cursor.sh`（US 区）。区域性失败时逐个试 |
| `SUB2API_CURSOR_AGENT_CLIENT_VERSION` | `cli-2026.08.11-e8db854` | `x-cursor-client-version`。Cursor 会抬高最低版本门槛，被拒时表现为 Connect `permission_denied` |
| `SUB2API_CURSOR_AGENT_GHOST_MODE` | `true` | `x-ghost-mode`（隐私模式）。只有 `0` / `false` / `no` / `off`（忽略大小写）会关掉，其它任何值都是 true |
| `SUB2API_CURSOR_AGENT_FIRST_BYTE_TIMEOUT` | `60s` | 等首帧的预算。Go duration 格式（`90s`、`2m`）。非法或 ≤0 回落默认 |
| `SUB2API_CURSOR_AGENT_IDLE_TIMEOUT` | `4s` | 输出静默多久判定一轮结束。上游在等 tool exec-result 时不会主动关流，所以靠这个收尾。回复被截断就调大到 `8s`/`15s` |

**三条硬性注意：**

1. **进程级、只读一次**。`sync.Once` 缓存，改完必须重启（`docker compose restart sub2api` 或重启 exe）。
2. **Docker 下必须走 override**。`deploy/docker-compose.yml` 的 `environment:` 是显式白名单，只写进 `.env` 传不进容器（见 2.5）。
3. **可以按账号覆盖**，优先级 `credentials` > `extra` > 进程 env > 代码常量：
   - credentials 键：`agent_base_url` / `agent_client_version` / `agent_ghost_mode`
   - extra 键：`cursor_agent_base_url` / `cursor_agent_client_version` / `cursor_agent_ghost_mode`

**客户端版本过期时怎么更新：**

```bash
curl -fsS https://cursor.com/install | grep -oE 'cli-20[0-9]{2}\.[0-9]{2}\.[0-9]{2}-[0-9a-f]{7,40}' | head -1
```

```powershell
(curl.exe -fsS https://cursor.com/install) -join "`n" |
  Select-String -Pattern 'cli-20\d{2}\.\d{2}\.\d{2}-[0-9a-f]{7,40}' -AllMatches |
  ForEach-Object { $_.Matches[0].Value }
```

拿到的字符串塞进 `SUB2API_CURSOR_AGENT_CLIENT_VERSION` 然后重启。
（代码里有 `cursorpkg.ParseCLIVersionFromInstallScript()` 做同样的解析，但它是纯函数——不会自己联网抓，所以更新版本永远是运维动作。）

---

## 7. 排错手册

### 7.1 按症状定位

| 症状 | 判断 | 处理 |
|------|------|------|
| `400 ... 'oneof' tag` 建分组失败 | 0.2 的补丁没打 | 打补丁重新构建，或走 4.2 的 SQL 兜底 |
| `/v1/models` 通，但 `/v1/chat/completions` 失败 | **鉴权没问题**（models 走 api2，用同一个 token 过了），是 api5 对话链路的问题 | 看 7.2 |
| Connect `permission_denied` | 客户端版本被上游判过期 | 按第 6 节更新 `SUB2API_CURSOR_AGENT_CLIENT_VERSION` |
| `ERROR_NOT_LOGGED_IN` | 用的是纯 web cookie，还没升级成 client 凭据 | 确认 `credentials.web_session_token` 存在；或 `POST /api/v1/admin/cursor/accounts/<AID>/refresh` 主动刷；或改用 `crsr_` API Key |
| 免费账号点名模型就报错 | Cursor 免费档只服务 `default` | 请求 `"model":"default"`（`auto` / `AUTO` / 空串也会被映射成 `default`） |
| 回复被截断 | 空闲超时太短 | `SUB2API_CURSOR_AGENT_IDLE_TIMEOUT=15s` |
| 首帧一直等不到 | 首字节超时太短或网络慢 | `SUB2API_CURSOR_AGENT_FIRST_BYTE_TIMEOUT=120s` |
| `Service temporarily unavailable` / 无可用账号 | 分组里没有可调度的 cursor 账号 | `GET /api/v1/admin/accounts?platform=cursor` 检查 status/schedulable/group 绑定 |
| 网关 401 / 提示未分组 | API Key 没绑分组 | `PUT /api/v1/admin/api-keys/<KID>` 或重建 Key 带 `group_id` |
| standard 模式下余额报错 | 管理员余额 0 | 4.4 充值，或改 `RUN_MODE=simple` 重启 |

### 7.2 出站网络自检

对话链路要能直连 `*.api5.cursor.sh`，而且**必须是 HTTP/2**（`AgentService/Run` 是双向流，HTTP/1.1 下客户端会直接拒绝开流）。

```bash
# 应看到 HTTP/2
curl -sSI --http2 https://agentn.global.api5.cursor.sh/ | head -3
# 顺带确认 api2 也通
curl -sSI https://api2.cursor.sh/ | head -3
```

```powershell
curl.exe -sSI --http2 https://agentn.global.api5.cursor.sh/ | Select-Object -First 3
```

需要走代理的话，在账号上绑一个 Proxy（`/api/v1/admin/proxies` 建，账号里填 `proxy_id`）。
代码会给每个 proxy URL 各建一个 HTTP/2 客户端并缓存（最多 64 个，30 分钟无使用后回收）。

### 7.3 日志

```bash
# Docker
cd /opt/sub2api/deploy && docker compose logs -f --tail=200 sub2api

# 原生（容器内/DATA_DIR 下都会写文件）
tail -f /var/lib/sub2api/logs/sub2api.log
```

```powershell
Get-Content C:\srv\sub2api-data\logs\sub2api.log -Wait -Tail 200
```

调高日志级别：`LOG_LEVEL=debug`（会打印 `cursor_observed_models_sync_failed` 之类的 debug 记录）。

### 7.4 迁移校验和不匹配

如果启动时报 `migration XXX checksum mismatch`，说明某个已应用的迁移文件在应用后被改过。
不要手改数据库，按报错提示 `git checkout <commit> -- backend/migrations/XXX.sql` 恢复原文件；
真要改 schema 就新加一个迁移文件。

### 7.5 跨平台一致性说明

同一个 `9a8788f3a` 在 Windows 和 Linux 上：
数据库 schema 一致（同一套 `backend/migrations/*.sql`）、api5 对话链路发出的 header 完全一致（不含 OS 标识）、
只有 api2 的 `x-cursor-client-os` 会显示 `win32` / `linux` 之差，上游两者都接受。
所以 Windows 上验证通过的行为，Linux 上会重现，反之亦然。
Docker 镜像本身是 `linux/amd64`（或 buildx 指定的目标），在 Windows Docker Desktop 上跑的也是这个 Linux 镜像，
和 Linux 服务器上跑的是同一份产物。
