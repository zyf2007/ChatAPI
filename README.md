# ChatAPI
[[Telegram](https://t.me/hutao_space)] |  [[LinuxDO](https://linux.do/u/hutao)] | [[BiliBili](https://www.bilibili.com/video/BV11PLg6LEbB)]  
本项目是一个让各类 AI 客户端用 OpenAI Responses 风格接口调用人类的项目，并带有一个 Web 控制台界面，可以帮你组装 Tool Calling 请求，或设置自动回复规则。  
通过这个项目，你可以让别人把你配置到 Agent 或 聊天机器人中，然后自己扮演 AI 助手被调用。
也可以在自己开发 Agent 的时候作为 Mock LLM 使用。

- 后端：Go
- 前端：React + Vite + Ant Design
- 数据存储：SQLite / PostgreSQL（规划中，当前首批已落 SQLite）

默认提供：

- Go 后端重构进行中，当前分支已切换到新的 Go 工程骨架（可能并不稳定）
- 支持 `/v1/chat/completions`、`/v1/responses`、`/messages` 三套接口
- Go 后端通过 `backend/cmd/server` 启动


## 1. 部署
### Docker Compose 一键部署

需要 Docker 与 Docker Compose。先准备部署密钥和管理员密码：

```bash
cp docker-compose.env.example .env
openssl rand -base64 48  # 生成 CHATAPI_MASTER_KEY
openssl rand -base64 48  # 生成 CHATAPI_SESSION_SECRET
```

把生成的随机值分别填入 `.env` 中的 `CHATAPI_MASTER_KEY` 和
`CHATAPI_SESSION_SECRET`，并修改 `CHATAPI_ADMIN_PASSWORD`。这些值必须长期备份；
更换 master key 会导致数据库中已经加密的模型 API Key 无法解密。

启动服务：

```bash
docker compose up -d --build
```

启动完成后访问 `http://localhost:5000`。SQLite 数据库和媒体文件保存在
`chatapi-data` 命名卷中，重新构建容器不会丢失。常用维护命令：

```bash
docker compose ps
docker compose logs -f chatapi
docker compose up -d --build  # 拉取代码后重新构建升级
docker compose down           # 停止服务，保留数据卷
```

镜像还包含 `migrate-db`，需要迁移到 PostgreSQL 时可通过
`docker compose exec chatapi migrate-db --help` 查看参数。

不要使用 `docker compose down -v`，除非确实要删除全部 ChatAPI 数据。
使用域名或反向代理时，还需要在 `.env` 中设置 `CHATAPI_BASE_URL` 和
`CHATAPI_CORS_ORIGINS`。镜像构建默认通过 `goproxy.cn` 下载 Go 模块；无法访问时可在
`.env` 中修改 `GOPROXY`。

### 单二进制构建

在项目根目录执行：

```bash
make build EMBED_FRONTEND=1
```

该命令会先构建前端，再使用 Go `embed_frontend` build tag 将前端产物写入
后端可执行文件，输出为 `build/chatapi`。部署时只需要分发这个二进制文件，
不需要设置 `CHATAPI_WEB_DIST_DIR`。

使用 `make build` 可以只构建后端，并继续从 `CHATAPI_WEB_DIST_DIR` 提供前端文件。

### 无需 Nginx 一键部署
#### 构建前端

```bash
cd ./frontend
npm i
npm run build
```

首页默认显示当前访问来源作为 API 基址；如需在构建时指定其他基址，可在构建前设置 `VITE_HOMEPAGE_API_BASE_URL`。

#### 设置 `.env`

先从项目根目录复制配置模板：

```bash
cp backend/.env.example backend/.env
```

然后至少修改以下密钥和管理员密码。两个密钥均可通过
`openssl rand -base64 48` 生成，并应长期备份：

```env
CHATAPI_DATA_DIR=./data
CHATAPI_DB_DRIVER=sqlite
CHATAPI_DB_DSN=./data/chatapi.sqlite3
CHATAPI_HOST=0.0.0.0
CHATAPI_PORT=5000
CHATAPI_WEB_DIST_DIR=../frontend/dist
CHATAPI_CORS_ORIGINS=http://localhost:5173,http://127.0.0.1:5173
CHATAPI_MASTER_KEY=replace-with-a-long-random-master-key
CHATAPI_SESSION_SECRET=replace-with-another-long-random-session-secret
CHATAPI_ADMIN_USERNAME=superadmin
CHATAPI_ADMIN_PASSWORD=replace-with-a-strong-unique-password
```

将现有 SQLite 数据库导入空的 PostgreSQL 数据库。该命令是离线迁移工具：执行前必须停止会写入源库的 ChatAPI 实例，迁移完成并校验后再修改数据库配置；它不会持续复制迁移期间或迁移后的增量写入。

```bash
cd backend
go run ./cmd/migrate-db \
  --sqlite ./data/chatapi.sqlite3 \
  --postgres-dsn 'postgres://chatapi:password@localhost:5432/chatapi?sslmode=disable'
```

#### 启动 Go 后端

```bash
cd ./backend
go run ./cmd/server
```
### dev部署

#### 启动后端

```bash
cd ./backend
go run ./cmd/server
```

#### 启动前端

```bash
cd ./frontend
npm i
npm run dev
```

## 3. 配置环境变量

先复制配置模板：

```bash
cp backend/.env.example backend/.env
```

建议至少确认以下配置：

```env
CHATAPI_DATA_DIR=./data
CHATAPI_DB_DRIVER=sqlite
CHATAPI_DB_DSN=./data/chatapi.sqlite3
CHATAPI_HOST=0.0.0.0
CHATAPI_PORT=5000
CHATAPI_MASTER_KEY=replace-with-a-long-random-master-key
CHATAPI_SESSION_SECRET=replace-with-another-long-random-session-secret
CHATAPI_ADMIN_USERNAME=superadmin
CHATAPI_ADMIN_PASSWORD=replace-with-a-strong-unique-password
```

如果部署配置保存在项目目录之外，可以设置外部 env 文件路径：

```env
CHATAPI_ENV_FILE=/path/to/chatapi.env
```

外部 env 文件与项目内 `.env` 使用相同格式，已存在的进程环境变量不会被文件中的值覆盖。

建议同时确认以下配置：

```env
CHATAPI_CORS_ORIGINS=http://localhost:5173,http://127.0.0.1:5173
```

可选配置：

```env
# 直接让 Go 后端对外托管前端静态文件
# CHATAPI_WEB_DIST_DIR=../frontend/dist

```



调用示例：  

```bash
curl https://127.0.0.1:5173/v1/responses \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer sk-i-love-you-hutao' \
  -d '{
    "model": "胡桃酱",
    "input": [
      {
        "type": "message",
        "role": "user",
        "content": [
          {
            "type": "input_text",
            "text": "在这里打字就可以和胡桃酱本人对话！"
          }
        ]
      }
    ],
    "stream": true
  }'

```

Anthropic Messages 兼容接口使用 `/v1/messages`，例如：

```bash
curl https://127.0.0.1:5173/v1/messages \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer sk-i-love-you-hutao' \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "messages": [
      {
        "role": "user",
        "content": "你好"
      }
    ],
    "stream": true
  }'
```
