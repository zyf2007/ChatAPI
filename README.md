# ChatAPI
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
#### 构建前端

```bash
cd ./frontend
npm i
npm run build
```

首页默认显示当前访问来源作为 API 基址；如需在构建时指定其他基址，可在构建前设置 `VITE_HOMEPAGE_API_BASE_URL`。

#### 设置.env
```env
CHATAPI_USERNAME=用户名
CHATAPI_PASSWORD=密码
# 可选；如果不填，后端会在首次启动时自动生成并写入数据库配置表
# CHATAPI_SESSION_SECRET=随机字符串

CHATAPI_DB_PATH=./data/chatapi.sqlite3
CHATAPI_DATA_DIR=./data

CHATAPI_HOST=0.0.0.0
CHATAPI_PORT=443
CHATAPI_WEB_DIST_DIR=../frontend/dist
CHATAPI_TLS_CERT_FILE=../certs/server.crt
CHATAPI_TLS_KEY_FILE=../certs/server.key
```

#### 启动Flask

```bash
cd ./backend
uv sync
uv run main.py
```
### dev部署

#### 后动后端

```bash
cd ./backend
uv sync
uv run main.py
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

# 基于手机端Termux的部署方案
## 1.注意事项
注意:请确保您的系统正常,并且使用了在Github或F-Droid下载的[[Termux](https://github.com/termux/termux-app/releases)]或者国内打包版本[[ZeroTermux](https://github.com/hanxinhao000/ZeroTermux/releases)],请确保系统应当为64位(Arm64或aarch64)
以保证能正常运行和部署,通过以下命令可以查询自己设备的架构,为了更好更方便的管理,可以使用文件管理器,如[[MT管理器](https://mt2.cn/download/)],遇到问题可以先将项目更新到最新版本
或者询问AI,询问他人,为了避免豆包的疑惑行为,可以使用其他ai(如:DeepSeek,Qwen[千问]),如果遇到实在的bug请报告给创作者
```bash
uname -m
```
输出数据根据以下查看,应当输出aarch64才满足条件
###### aarch64 说明:64位 ARM（ARMv8-A 或更高）
###### armv7l 或 armv8l 说明:32位 ARM（ARMv7-A 或兼容）
###### x86_64 说明:64位 x86 架构（Intel/AMD）
###### i686 或 i386 说明:32位 x86 架构
###### riscv64 说明:RISC-V 64位架构
至少Android 5以上,最好Android 12以上

注意:本项目本身还是基于电脑端的没有手机端的优化支持,但目前还是可以在手机端上运行的,还有请确保您已更新termux的相关文件已更新最新
现在先来准备工作,输入以下指令，安装一些依赖等等
## 2.准备工作
```bash
termux-setup-storage #给文件权限
termux-change-repo #更换镜像源,步骤:ok,↓↓,ok,等待跑完,反正就选中国(Chinese)
pkg update && pkg upgrade -y #更新一下
pkg install -y git wget curl vim binutils clang make pkg-config #安装一些必备依赖
pkg install -y python python-pip build-essential
pkg install -y libjpeg-turbo libpng freetype harfbuzz libtiff libwebp openjpeg
pip install uv
uname -m #再看一眼架构
pkg install -y nodejs #安装不可或缺的东西
node --version && npm --version #验证一下安装
```

## 3.正式部署
### 无需 Nginx 一键部署
#### 下载项目+构建前端

```bash
cd ~/
git clone https://github.com/zyf2007/ChatAPI.git #Github无法正确访问下载,请看下[其他所需]
cd ChatAPI
cd frontend
npm i
npm run build
```

首页默认显示当前访问来源作为 API 基址；如需在构建时指定其他基址，可在构建前设置 `VITE_HOMEPAGE_API_BASE_URL`。

## 4. 配置环境变量

先复制配置模板：

```bash
cd ~/ChatAPI/backend
cp .env.example .env
```

至少需要修改以下配置：

```env
CHATAPI_USERNAME=admin #用户名(管理员名称)
CHATAPI_PASSWORD=change-me #用户密码(管理员密码)
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
## 5.启动服务(两个都要)
#### 启动后端

```bash
cd ~/ChatAPI/backend
uv sync
uv run main.py
```

#### 启动前端(需要后端)
由于需要同时启动,所以需要新建一个会话,两个会话要分别运行后端和前端,前端主要提供了用于管理的网页
```bash
cd ~/ChatAPI/frontend
npm i
npm run dev
```
## 6.Termux 专用服务管理方案

#### 方案一：使用 tmux 管理多会话（推荐）

##### 安装 tmux
```bash
pkg install tmux -y
```

##### 创建后端会话
```bash
tmux new -s chatapi-backend
cd ~/ChatAPI/backend
uv run main.py
```
Ctrl+B, D 分离会话

##### 创建前端会话
```bash
tmux new -s chatapi-frontend
cd ~/ChatAPI/frontend
npm run dev -- --host 0.0.0.0
```
Ctrl+B, D 分离会话

##### 查看运行中会话
```bash
tmux ls
```

##### 重新附着会话
```bash
tmux attach -t chatapi-backend
```

#### 方案二：使用 nohup 后台运行

##### 后端
```bash
cd ~/ChatAPI/backend
nohup uv run main.py > backend.log 2>&1 &
```

##### 前端
```bash
cd ~/ChatAPI/frontend
nohup npm run dev -- --host 0.0.0.0 > frontend.log 2>&1 &
```

#### 开机自启（需安装 Termux:Boot 插件）

1. 安装 Termux:Boot 插件/[(ZT)Termux:Boot]
2. 创建自启脚本 ~/.termux/boot/start-chatapi.sh：

```bash
#!/data/data/com.termux/files/usr/bin/bash
cd ~/ChatAPI/backend
nohup uv run main.py > backend.log 2>&1 &
sleep 3
cd ~/ChatAPI/frontend
nohup npm run dev -- --host 0.0.0.0 > frontend.log 2>&1 &
```

赋予执行权限：
```bash
chmod +x ~/.termux/boot/start-chatapi.sh
```

## 7.公网访问方案

Termux 中的服务通常运行在局域网中，如需公网访问，可使用以下内网穿透工具：

##### 使用 cloudflared（Cloudflare Tunnel）
```bash
pkg install cloudflared -y
cloudflared tunnel --url http://localhost:5000
```

##### 或使用 ngrok（需要注册获取 token）
```bash
pkg install wget -y
wget https://bin.equinox.io/c/bNyj1mQVY4c/ngrok-v3-stable-linux-arm64.tgz
tar xzf ngrok-v3-stable-linux-arm64.tgz
./ngrok config add-authtoken YOUR_TOKEN
./ngrok http 5000
```

## 8.安全增强建议

#### 8.1账户安全策略

1.修改默认管理员密码：部署后立即修改 CHATAPI_PASSWORD，避免使用弱密码。

2.启用 TOTP 两步验证：在 Web 控制台的「系统设置」中启用 TOTP，配合 Google Authenticator 等认证器使用。TOTP 密钥应安全存储在数据库中。

3.启用 API Key 认证：为 API 调用启用 Bearer Token 认证，调用时需携带 Authorization: Bearer <api_key> 头。

4.启用消息限流：在系统设置中配置请求限流策略，防止滥用。

#### 8.2网络安全配置

1.生产环境使用 HTTPS：强烈建议使用 Nginx 反向代理 + Let's Encrypt 免费证书

2.限制 CORS 来源：仅允许必要的域名，避免使用通配符

3.配置防火墙：Termux 中可使用 iptables 或 nftables 限制访问来源 IP

#### 8.3敏感信息管理

1.登录后在 Web 控制台启用并保存 API Key、站点标题、ntfy 地址和 TOTP，这些配置不应放在 .env 文件中

2.SESSION_SECRET 若不填写，后端会在首次启动时自动生成并写入数据库配置表

## 消息推送地址安全设置

ChatAPI 支持通过 ntfy 发送消息通知。用户可以在「我的设置」中填写 ntfy 推送地址。

### Q：什么时候需要修改「消息推送地址」？

大多数情况下保持默认「关闭」即可。只有自建 ntfy 和 ChatAPI 在同一台机器、同一个内网或私有网络里时，才需要开启，例如 `http://127.0.0.1:8080/topic` 或 `http://192.168.1.10:8080/topic`。

如果使用官方 `https://ntfy.sh/your-topic`，或自建 ntfy 使用公网域名，例如 `https://ntfy.example.com/topic`，都不需要修改。

三个选项含义：

- 关闭：所有用户都不能填写本机或内网推送地址。
- 仅管理员：只有管理员可以填写本机或内网推送地址。
- 所有用户：所有登录用户都可以填写本机或内网推送地址。

推荐优先使用「仅管理员」，只在完全信任所有用户时选择「所有用户」。默认关闭是为了防止用户通过推送地址让服务器访问 `127.0.0.1`、`localhost`、内网 IP 或云 metadata 地址，造成 SSRF 风险。

## Termux常见问题排查

###### Q1：数据库锁错误（database is locked）

原因：SQLite 在高并发写入时可能发生锁冲突。

解决方案：

  1.启用 WAL 模式（?journal_mode=WAL）
  
  2.使用连接池限制并发连接数
  
  3.将繁忙写入操作包装在事务中

###### Q2：Session Secret 相关错误

原因：未在 .env 中配置 CHATAPI_SESSION_SECRET。

解决方案：留空即可，后端会在首次启动时自动生成并写入数据库配置表。若需手动指定，建议使用高熵随机字符串。

###### Q3：邮件发送失败

检查项：

  1.SMTP 服务器地址和端口是否正确
  
  2.是否启用了 SMTP 认证（部分服务商需要）
  
  3.发件人邮箱是否经过验证
  
  4.使用第三方 API（如 Resend、腾讯云 SES）时，API Key 是否有效且权限正确。Resend 建议权限设置为“仅发送”，以降低泄露风险。

###### Q4：Termux 中 Node.js 构建前端失败

解决方案：
  1.增加 swap 空间：dd if=/dev/zero of=$PREFIX/swapfile bs=1M count=1024 && chmod 600 $PREFIX/swapfile && mkswap 
  $PREFIX/swapfile && swapon $PREFIX/swapfile
  
  2.使用 npm run build 前确保内存充足（建议 1GB 以上可用 RAM）

## 通过Github下载所需
默认为最新版本,如果您的设备不支持请自行寻找适配版本下载
[[ZeroTermux‖0.118.3.58‖全架构](https://github.com/hanxinhao000/ZeroTermux/releases/download/ZeroTermux-0.118.3.58/ZeroTermux-ZeroTermux-0.118.3.58_release_universal.apk)]

[[Termux‖V0.119.0-beta3‖全架构](https://github.com/termux/termux-app/releases/download/v0.119.0-beta.3/termux-app_v0.119.0-beta.3+apt-android-7-github-debug_universal.apk)]

## 通过下载站下载所需
默认为最新版本,如果您的设备不支持请自行寻找适配版本下载
使用下载站:[[gh-proxy](https://gh-proxy.com/)],Cloudflare
### Termux
[[Termux‖V0.119.0-beta3‖全架构](https://gh-proxy.org/https://github.com/termux/termux-app/releases/download/v0.119.0-beta.3/termux-app_v0.119.0-beta.3+apt-android-7-github-debug_universal.apk)]主站加速，全球高速分发

[[Termux‖V0.119.0-beta3‖全架构](https://v4.gh-proxy.org/https://github.com/termux/termux-app/releases/download/v0.119.0-beta.3/termux-app_v0.119.0-beta.3+apt-android-7-github-debug_universal.apk)]优选加速服务器，仅支持IPv4 网络智能解析

[[Termux‖V0.119.0-beta3‖全架构](https://v6.gh-proxy.org/https://github.com/termux/termux-app/releases/download/v0.119.0-beta.3/termux-app_v0.119.0-beta.3+apt-android-7-github-debug_universal.apk)]优选加速服务器，支持 IPv6/IPv4 网络智能解析

### ZeroTermux
[[ZeroTermux‖0.118.3.58‖全架构](https://gh-proxy.org/https://github.com/hanxinhao000/ZeroTermux/releases/download/ZeroTermux-0.118.3.58/ZeroTermux-ZeroTermux-0.118.3.58_release_universal.apk)]主站加速，全球高速分发

[[ZeroTermux‖0.118.3.58‖全架构](https://v4.gh-proxy.org/https://github.com/hanxinhao000/ZeroTermux/releases/download/ZeroTermux-0.118.3.58/ZeroTermux-ZeroTermux-0.118.3.58_release_universal.apk)]优选加速服务器，仅支持IPv4 网络智能解析

[[ZeroTermux‖0.118.3.58‖全架构](https://v6.gh-proxy.org/https://github.com/hanxinhao000/ZeroTermux/releases/download/ZeroTermux-0.118.3.58/ZeroTermux-ZeroTermux-0.118.3.58_release_universal.apk)]优选加速服务器，支持 IPv6/IPv4 网络智能解析

## 其他所需
国内软件使用国内通道,Github相关使用[[下载站](https://gh-proxy.com/)],默认为最新版本,如果您的设备不支持请自行寻找适配版本下载
### 克隆本项目(下载站)
##### 主站加速，全球高速分发
```bash
git clone https://gh-proxy.org/https://github.com/zyf2007/ChatAPI.git
```
##### 优选加速服务器，仅支持IPv4 网络智能解析
```bash
git clone https://v4.gh-proxy.org/https://github.com/zyf2007/ChatAPI.git
```
##### 优选加速服务器，支持 IPv6/IPv4 网络智能解析
```bash
git clone https://v6.gh-proxy.org/https://github.com/zyf2007/ChatAPI.git
```

### MT管理器
[[MT管理器](https://pan.mt2.cn/apk/26040964)]

### MT管理器代替版[NP管理器]
[[NP管理器](http://normalplayer.top:8991/member/view/fileDownload/NP.apk)]

## Nginx 反向代理示例

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



## 调用示例：  

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
