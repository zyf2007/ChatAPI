# Lab 工作区页面与 API 边界

Lab 是共享的单工作区，owner 固定为 `lab_default`；Serve 使用账户会话。
`GET /api/auth/session` 的 `mode` 是前端判断运行模式的权威输入。
Lab session 的连接限制和会话保留数读取当前策略，会话数读取工作区存储。
Lab 全局口令/token 门禁保护 HTTP、WebSocket 和管理 SSE；不恢复 Serve 用户 cookie，
避免遗留账户会话改变 Lab 的资源归属。接口未注册本身不能作为删除功能的理由。

## 页面审计

| 页面或入口 | Lab 行为与依据 | 数据来源 |
| --- | --- | --- |
| 首页 `/` | 展示/复制实际支持的三个推理协议；隐藏未实现的统计入口 | `/v1/messages`、`/v1/responses`、`/v1/chat/completions` |
| 工作区 `/app` | 保留列表、分页、时间线、回复、思考、工具输出和中止 | `/api/ws`；`/api/lab/workspace` 提供 HTTP 快照 |
| 删除会话、批量清理 | 保留；禁止删除等待中的请求，清理跳过等待中的会话 | `DELETE /api/conversations/{conversationID}`、`POST /api/conversations/prune` |
| 生图上传与图片读取 | 保留；校验 owner、当前请求和工具许可 | `POST /api/conversations/{conversationID}/output-images`、`GET /api/media/assets/{fileID}` |
| 设置 → 模型列表 | **保留并接通**；模型名称和鉴权 Key 是独立资源，不应一起隐藏 | `GET/POST /api/user/virtual-models`、`DELETE /api/user/virtual-models/{modelID}` |
| 协议模型发现 | **接通**；直接读取 Lab owner 的持久化模型，不伪造 Serve 模型凭证 | `GET /v1/models` |
| 设置 → 辅助模型 | 保留；浏览器直接请求上游 | 按 `lab_default` 隔离的 localStorage |
| 设置 → 工作区设置 | **恢复 ntfy 配置**；新请求按 owner 读取配置并进入通知队列 | `GET/POST /api/user/config`；共享通知服务 `NotifyWaiting` |
| 我的设置中的消息限流 | Lab 隐藏；当前字段仅存储，没有消息限流执行者，不呈现为可用策略 | 实际支持的请求限流在实例设置中配置 |
| 密码、TOTP、应用 Key、模型 Key | 隐藏；Lab 使用共享入口门禁，不使用 Serve 账户及 Key | 不注册相应账户 API |
| 设置 → 媒体设置 | **恢复**；使用共享媒体策略服务，影响协议输入预处理及图片输出 | `GET/PATCH /api/admin/settings/media` |
| 设置 → 实例设置 | **恢复**入口；打开下列 4 个可用分区 | `/admin/settings/overview` |
| 管理概览 `/admin/settings/overview` | **恢复**运行环境、连接/内存/CPU等指标；配置领域只含 access/media | `/api/admin/settings/overview`、`/api/admin/settings/runtime`、`/api/admin/monitor/stream` |
| 系统 `/admin/settings/system` | **恢复**只读启动参数及 SQLite 维护记录 | runtime 与 `/api/admin/audit/logs?event_type=system.storage.vacuum` |
| 访问限流 `/admin/settings/access` | **恢复**匿名访问限流、会话保留数、请求有效期、输出事件上限及 WebSocket 连接策略；隐藏无 Lab 执行主体的用户/会话/App Key/模型 Key 限流字段 | `GET/PATCH /api/admin/settings/access`，共享 access/chat/realtime 策略 |
| 媒体 `/admin/settings/media` | **恢复**媒体策略页，与设置弹窗使用同一服务 | `/api/admin/settings/media` |
| 用户管理、账户认证、自动化 3 个管理分区 | 隐藏并跳回 `/app`；Lab 不提供账户管理，自动化匹配未支持 Lab 身份 | 不挂载其 API 消费者，后端不开放对应路由或配置领域 |
| 自动化规则与录制 | 暂时隐藏；v3 启用规则必须绑定真实模型 Key，Lab 请求没有该身份。仅开放 CRUD 无法让规则执行，不能作为“已接通” | 未请求 `/api/automation/rules`，未发送录制初始化命令 |
| `/login`、`/register`、`/forgot-password` | 跳回 `/app` | Lab session 已认证 |
| `/stat`、`/statistics` | 跳回 `/app`；当前后端未实现汇总接口 | 不挂载 `/api/statistics/summary` 消费者 |
| 退出登录 | 隐藏；Lab 门禁不等同于 Serve 账户会话 | 不请求 `/api/auth/logout` |

管理页共审计 **7 个分区，恢复 4 个、隐藏 3 个**。设置弹窗提供模型列表、辅助模型、
工作区设置、媒体设置、实例设置 5 个入口。模型配置与上游辅助模型端点配置各自管理。
Serve 的模型/账户/管理接口仍要求原有会话或 Key 权限；Lab 工作区能力只在全局门禁内显式注册。

## 验收

- Go 路由集成测试：Lab 门禁覆盖新增 CRUD、配置、管理 HTTP/SSE；验证模型持久化、
  `/v1/models` 发现与删除、跨 owner 列表/删除隔离、配置归属及 ntfy URL 校验、媒体/请求策略更新和生效后的 session 配额投影。
  同时保留图片上传/读取、等待中删除冲突和清理回归，检查 Serve 匿名请求仍被拒绝。
- Playwright 在桌面及移动 Chromium 走真实模型新增 → 发现 → 整页重载 → 删除流程，
  工作区通知草稿及媒体设置保存 → 重载，并访问 4 个实例管理分区及不适用分区的重定向。
  配置测试不触发外部推送，最后恢复原配置；新增模型和会话只清理自身资源。
- 浏览器同时检查页面异常、失败 API 响应及不适用账户 API 请求；网络审计明确允许已接通的工作区 API。
- 共享策略修改及批量清理只在 `CHATAPI_TEST_LAB_MUTATIONS=1` 的隔离数据库运行。
