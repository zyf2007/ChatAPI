# ChatAPI 部署说明

本项目是一个让各类 AI 客户端用主流接口调用人类的项目，并带有一个 Web 控制台界面，可以帮你组装 Tool Calling 请求。  
你可以让别人把你配置到 Agent 或聊天机器人中，然后自己扮演 AI 助手被调用。

- 后端：Flask
- 前端：React + Vite + Ant Design
- 数据存储：SQLite

目前支持的 API 接口风格：

- **OpenAI Responses**：`POST /v1/responses`
- **OpenAI Chat Completions**：`POST /v1/chat/completions`
- **Anthropic Messages**：`POST /v1/messages`

## 1. 快速启动（推荐）

配置好 `.env` 后，直接在项目根目录运行：

```bash
./start.sh
```

脚本会同时启动后端（默认 `5001` 端口）和前端（默认 `5173` 端口），Ctrl+C 退出时两个进程会一起停止。

> **注意**：macOS 系统上 `5000` 端口默认被 AirPlay Receiver 占用，因此后端默认使用 `5001` 端口。

## 2. 手动启动

### 启动后端

```bash
cd ./backend
uv sync
uv run main.py
```

### 启动前端

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

至少需要修改以下配置：

```env
CHATAPI_USERNAME=admin
CHATAPI_PASSWORD=change-me
CHATAPI_SESSION_SECRET=change-this-session-secret
CHATAPI_API_KEY=sk-xxxxx
```

建议同时确认以下配置：

```env
CHATAPI_DB_PATH=./data/chatapi.sqlite3
CHATAPI_DATA_DIR=./data
CHATAPI_HOST=0.0.0.0
CHATAPI_PORT=5001
CHATAPI_CORS_ORIGINS=http://localhost:5173,http://127.0.0.1:5173
CHATAPI_MESSAGES_PER_MINUTE_LIMIT=0
```

可选配置：

```env
# ntfy 推送地址
# CHATAPI_NTFY_URL=https://ntfy.sh/your-topic

# 直接由 Flask 提供 HTTPS 时使用
# CHATAPI_TLS_CERT_FILE=./certs/server.crt
# CHATAPI_TLS_KEY_FILE=./certs/server.key
```

## 4. Nginx 反向代理示例

以下示例假设：

- 前端静态文件目录：`/path/to/ChatAPI/frontend/dist`
- 后端地址：`http://127.0.0.1:5001`
- 域名：`chat.example.com`

```nginx
server {
    listen 80;
    server_name chat.example.com;

    root /path/to/ChatAPI/frontend/dist;
    index index.html;

    location / {
        try_files $uri $uri/ /index.html;
    }

    location /api/ {
        proxy_pass http://127.0.0.1:5001/api/;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location /v1/ {
        proxy_pass http://127.0.0.1:5001/v1/;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

如果要启用 HTTPS，建议由 Nginx 处理证书，而不是直接使用 Flask 内置服务。

## 5. 可用接口

后端提供以下接口：

**认证**
- `POST /api/auth/login`
- `POST /api/auth/logout`
- `GET /api/auth/session`

**会话管理**
- `GET /api/health`
- `GET /api/conversations`
- `GET /api/conversations/<id>/messages`
- `POST /api/conversations`
- `POST /api/conversations/<id>/rename`

**AI 接口（对外暴露给 AI 客户端）**
- `POST /v1/responses` — OpenAI Responses 风格
- `POST /v1/chat/completions` — OpenAI Chat Completions 风格
- `POST /v1/messages` — Anthropic Messages 风格

### OpenAI Responses 调用示例

```bash
curl http://127.0.0.1:5001/v1/responses \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer sk-your-api-key' \
  -d '{
    "model": "your-name",
    "input": [{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "你好！"}]}],
    "stream": true
  }'
```

### OpenAI Chat Completions 调用示例

```bash
curl http://127.0.0.1:5001/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer sk-your-api-key' \
  -d '{
    "model": "your-name",
    "messages": [{"role": "user", "content": "你好！"}],
    "stream": true
  }'
```

### Anthropic Messages 调用示例

```bash
curl http://127.0.0.1:5001/v1/messages \
  -H 'Content-Type: application/json' \
  -H 'x-api-key: sk-your-api-key' \
  -d '{
    "model": "your-name",
    "messages": [{"role": "user", "content": "你好！"}],
    "max_tokens": 1024,
    "stream": true
  }'
```

## 6. 运行单元测试

```bash
cd backend
uv run pytest tests/ -v
```
