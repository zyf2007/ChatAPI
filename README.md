# ChatAPI 部署说明

本项目是一个让别人用 OpenAI Responses 风格接口调用人类的项目，并带有一个 Web 控制台界面，可以帮你组装 Tool Calling 请求。
你可以让别人把你配置到 Agent 或 聊天机器人中，然后自己扮演 AI 助手 被调用。

- 后端：Flask
- 前端：React + Vite + Ant Design
- 数据存储：SQLite

默认提供：

- 基于 `.env` 的用户名密码登录
- Responses 风格接口 `POST /v1/responses`（暂不支持/chat/completions）
- 会话列表与消息持久化能力，便于调试和查看上下文
- 可选 ntfy 消息推送

## 1. 部署

### 后动后端

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
cp .env.example .env
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
CHATAPI_PORT=5000
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
- 后端地址：`http://127.0.0.1:5000`
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
        proxy_pass http://127.0.0.1:5000/api/;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location /v1/ {
        proxy_pass http://127.0.0.1:5000/v1/;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

如果要启用 HTTPS，建议由 Nginx 处理证书，而不是直接使用 Flask 内置服务。


## 5. 可用接口

后端默认提供以下接口：

- `POST /api/auth/login`
- `POST /api/auth/logout`
- `GET /api/auth/session`
- `GET /api/health`
- `GET /api/conversations`
- `GET /api/conversations/<id>/messages`
- `POST /api/conversations`
- `POST /api/conversations/<id>/rename`
- `POST /v1/responses`

核心接口是 `/v1/responses`，接受 OpenAI Responses 风格请求，例如：

```json
{
  "model": "mock-gpt-4.1-mini",
  "input": "请返回一条 mock 响应"
}
```

调用示例：

```bash
curl https://127.0.0.1:5000/v1/responses \
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
