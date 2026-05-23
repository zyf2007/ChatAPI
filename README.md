# ChatAPI
适用于手机Termux
[[Telegram](https://t.me/hutao_space)] |  [[LinuxDO](https://linux.do/u/hutao)] | [[BiliBili](https://www.bilibili.com/video/BV11PLg6LEbB)]  
本项目是一个让 各类 AI 客户端用 OpenAI Responses 风格接口调用人类的项目，并带有一个 Web 控制台界面，可以帮你组装 Tool Calling 请求，或设置自动回复规则。  
通过这个项目，你可以让别人把你配置到 Agent 或 聊天机器人中，然后自己扮演 AI 助手被调用。
也可以在自己开发 Agent 的时候作为 Mock LLM 使用。

- 后端：Flask
- 前端：React + Vite + Ant Design
- 数据存储：SQLite

默认提供：

- 基于 `.env` 的用户名密码登录，支持可选 TOTP
- 支持 `/v1/chat/completions`、`/v1/responses`、`/messages` 三套接口
- 会话列表与消息持久化能力，便于调试和查看上下文
- 自动化回复输出能力，支持定时流式发送、循环输出，条件判断自动回复等场景
- 可选 ntfy 消息推送

## 1. 部署
### 无需 Nginx 一键部署
#### 准备
```bash
pkg update
pkg install -y libjpeg-turbo libpng freetype harfbuzz libtiff libwebp openjpeg
pkg install nodejs
npm install -g typescript
#还有问题就去问AI先(如:DeepSeek)
```
#### 构建前端

```bash
cd ~/
git clone https://github.com/zyf2007/ChatAPI.git
cd ~/ChatAPI/frontend
npm i
npm run build
```

首页默认显示当前访问来源作为 API 基址；如需在构建时指定其他基址，可在构建前设置 `VITE_HOMEPAGE_API_BASE_URL`。

## 2. 配置环境变量

先复制配置模板：

```bash
cd ~/ChatAPI/backend
cp .env.example .env
```

至少需要修改以下配置：

```env
CHATAPI_USERNAME=admin
CHATAPI_PASSWORD=change-me
# 可选；如果不填，后端会在首次启动时自动生成并写入数据库配置表
# CHATAPI_SESSION_SECRET=change-this-session-secret
```

如果部署配置保存在项目目录之外，可以设置外部 env 文件路径：

```env
CHATAPI_ENV_FILE=/path/to/chatapi.env
```

外部 env 文件与项目内 `.env` 使用相同格式，已存在的进程环境变量不会被文件中的值覆盖。

建议同时确认以下配置：

```env
CHATAPI_DB_PATH=./data/chatapi.sqlite3
CHATAPI_DATA_DIR=./data
CHATAPI_HOST=0.0.0.0
CHATAPI_PORT=5000
CHATAPI_CORS_ORIGINS=http://localhost:5173,http://127.0.0.1:5173
```

登录后可以在「系统设置」里启用并保存 `API Key`、站点标题、ntfy 地址、消息限流和 TOTP，这些不再需要放在 `.env` 里。

可选配置：

```env
# 直接让 Flask 对外托管前端静态文件（例如 Vite build 后的 dist）
# CHATAPI_WEB_DIST_DIR=./frontend/dist

# 直接由 Flask 提供 HTTPS 时使用
# CHATAPI_TLS_CERT_FILE=./certs/server.crt
# CHATAPI_TLS_KEY_FILE=./certs/server.key

# 邮件发送可选配置：
# CHATAPI_EMAIL_FROM=noreply@kirari.fun
# CHATAPI_SMTP_HOST=smtp.example.com
# CHATAPI_RESEND_API_KEY=re_xxxxxxxxx
# CHATAPI_BREVO_API_KEY=YOUR_BREVO_API_KEY
# CHATAPI_TENCENTCLOUD_SECRET_ID=AKIDxxxxxxxxxxxxxxxx
# CHATAPI_TENCENTCLOUD_SECRET_KEY=xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
# CHATAPI_TENCENTCLOUD_SES_REGION=ap-guangzhou
# 普通 SES 账号还需要模板 ID；模板数据会由程序动态生成。
# CHATAPI_TENCENTCLOUD_TEMPLATE_ID=100091
```

#### 启动后端

```bash
cd ~/ChatAPI/backend
pkg install uv #如果没有的话
uv sync
uv run main.py
```

#### 启动前端

```bash
cd ~/ChatAPI/frontend
npm i
npm run dev
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

如果不想额外部署 Nginx，也可以直接让 Flask 对外同时提供 API 和前端静态文件：

```env
CHATAPI_WEB_DIST_DIR=./frontend/dist
```

设置后：

- `/api/*` 和 `/v1/*` 继续走后端接口
- 其他路径会从该目录下直接返回静态文件
- 当请求路径不存在且目录中包含 `index.html` 时，会自动回退到 `index.html`，可用于前端单页应用路由



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

Anthropic Messages 兼容接口使用 `/messages`，例如：

```bash
curl https://127.0.0.1:5000/messages \
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
