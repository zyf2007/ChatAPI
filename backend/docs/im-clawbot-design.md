# IM Provider 与微信 ClawBot 接入设计

## 目标

ChatAPI 将 IM 作为可替换的操作员通道。首个 Provider 是腾讯微信 ClawBot（iLink HTTP/JSON API）：

1. ChatAPI 收到 `turn.waiting`。
2. 已绑定用户的微信收到请求摘要。
3. 扫码者本人发送文本，ChatAPI 以该文本完成选中的 pending turn。

该通道不是新的模型请求入口；微信联系人不会因此创建 ChatAPI turn。

协议依据：

- [微信开放文档：ClawBot 相关接口](https://developers.weixin.qq.com/doc/aispeech/knowledge/openapi/Clawbotrelated.html)
- [Tencent/openclaw-weixin](https://github.com/Tencent/openclaw-weixin)

## 首版范围

| 微信输入 | 行为 |
| --- | --- |
| 普通文本 | `stream_complete` 当前请求 |
| `/list` | 从 `PendingRegistry.ListByOwnerID` 读取实时列表 |
| `/use <编号>` | 按 conversation/request ID 前缀选择请求 |
| `/abort [原因]` | 中止当前请求 |
| `/bind` | 刷新 context token 并返回绑定说明 |
| `/help` | 返回帮助 |

首版不支持流式 delta、思考、工具调用、媒体或群聊。原因是 iLink cursor checkpoint 与非幂等 delta 无法在现有存储边界内实现原子提交；普通完成和中止会再次由 pending request identity 校验，重放不会重复完成 turn。

## Provider 契约

`internal/service/im.Provider` 负责：

- 登录挑战：`StartLogin` / `PollLogin`（仅向 iLink 提交当前 owner 已保存的 local token，不跨用户汇总）
- 账号长轮询：`Run`
- 出站文本：`Send`
- Provider 状态判断：`Ready`；`ReadinessVersion` 只在获得新回复上下文时改变，使 Coordinator 区分 cursor checkpoint 与真实 readiness 恢复

Coordinator 不解析 Provider 私有 credentials/state。Provider 通过 checkpoint 回调提交 opaque state；Coordinator 负责加密、账号 generation、worker 生命周期、owner 鉴权、pending 选择和 turn control。

后续 Provider 可以实现相同契约，而无需进入 `chat/turn` 或复制 workspace handler。

## 账号与秘密

每个 ChatAPI 用户最多保存一个 `im.account.clawbot` user config。公开 envelope 只包含：Provider、外部 bot/user ID、受信 endpoint、连接时间。以下 JSON 合并后由 `secretbox` 使用 `CHATAPI_MASTER_KEY` 加密：

- `bot_token`
- `context_token`
- `get_updates_buf`
- processed message ID window

HTTP 状态、日志和前端响应均不返回 ciphertext 或明文秘密。删除连接会先 invalidate generation、取消 worker、等待 in-flight callback barrier，再删除 config；旧 checkpoint 不能把账号写回。

## 微信身份边界

Provider 只接受同时满足下列条件的消息：

- `message_type == USER`
- `message_state == FINISH`
- `group_id` 为空
- `from_user_id ==` 扫码确认返回的 `ilink_user_id`
- `to_user_id` 为空（部分响应省略该字段）或等于 `ilink_bot_id`
- 恰好一个完整文本 item
- message ID 未在去重窗口中

Coordinator 在每个控制命令前重新读取 ChatAPI user，停用或删除的 owner 不能控制 turn。账号服务的停用/删除成功路径还会同步调用 `RevokeOwner`，取消 login/runtime、等待 callback barrier 并删除 IM config。`chat/control.Execute` 仍校验 owner、conversation、response 与 request identity。

## Cursor 与重复消息

Provider 先处理 batch，再 checkpoint `get_updates_buf`、最新 context token 和最近 128 个 message ID。崩溃可能重放已经执行但尚未 checkpoint 的消息，因此首版只开放终态操作和只读/幂等命令：

- 已完成/中止的 request 会从 PendingRegistry 消失，重放无法再次控制。
- `/list`、`/use`、`/bind`、`/help` 重放最多产生重复说明。
- 出站 waiting notification 的 `client_id` 由 request ID 稳定派生，有限重试不会产生不同消息身份。

## 通知与并发

`HandleChatEvent` 不执行网络请求。它只把每个 owner 的最新 waiting snapshot 写入 dirty map，并向容量为 1 的 wake channel 发信号。两个 worker 从 dirty map 取不同 owner；同 owner 在途时继续合并新 snapshot，完成后重新唤醒。

发送前再次确认 PendingRegistry 中存在同一个 conversation/request。waiting event 入队时不会改变当前选择；只有通知成功送达且 request 仍 pending，才在同一 runtime barrier 内把该 conversation 设为普通回复目标，避免用户回复上一条可见通知却误结束尚未送达的新请求。账号尚未收到 `/bind` 时保留最新 waiting snapshot；首次 context checkpoint 后重新排队。

每个 account runtime 带单调 generation 和 callback barrier：

1. disconnect/replace 先增加 generation 并从 active map 移除 runtime；
2. cancel 长轮询；
3. 等待正在执行的 inbound/checkpoint/send；checkpoint 持有 barrier 完成存储写入，旧写入只能发生在删除之前；
4. 删除或替换持久化账号；
5. 旧 callback 因 generation 不匹配返回，不会发送、控制或持久化。

## 网络安全

- QR 入口固定为 `https://ilinkai.weixin.qq.com`；生产 client 使用 `urlsafety.SafeDialer` 在连接时重新解析并拒绝私网、回环、链路本地和混合 DNS 结果，且不使用代理。
- 动态 `baseurl`/`redirect_host` 必须为 HTTPS、默认端口、无 userinfo/query/fragment/path，且 hostname 等于 `weixin.qq.com` 或以 `.weixin.qq.com` 结尾。
- `evilweixin.qq.com` 不满足点边界。
- HTTP 响应上限 1 MiB；二维码 token/URL、context/cursor、入站/出站文本均有限制。
- 长轮询上限 40 秒，普通请求 15 秒，start/stop notify 5 秒。
- `ret/errcode == -14` 标记 `reauth_required`，不无限重试旧 token；`sendmessage -2` 在仍持有该 runtime barrier 时记录本次发送使用的 context generation 为失效，避免随后到达的新 context 被旧失败覆盖。Provider 为每个有效本人入站 context 递增持久化 generation；cursor-only checkpoint 不改变 generation，只有新的 context generation 才恢复 Ready 并重排一次最新 waiting 通知。
- 敏感请求不跟随 HTTP 3xx；iLink 的合法 endpoint 切换只接受 JSON `redirect_host` 并重新执行白名单校验。

## 用户 API

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| GET | `/api/user/im/clawbot` | 安全状态 |
| POST | `/api/user/im/clawbot/login` | 创建二维码 |
| POST | `/api/user/im/clawbot/login/{session_id}/poll` | owner-scoped 状态查询/验证码 |
| DELETE | `/api/user/im/clawbot` | 断开并删除连接 |

路由沿用 session authentication、principal access 和现有 mutation CSRF 约束。login session 仅保存在内存，绑定 owner，5 分钟过期，同一 session 只允许一个在途 poll。

## 恢复与限制

服务启动时枚举 active users 并恢复可解密的 IM account。ChatAPI 的 pending turn 不跨进程恢复为 waiting；旧微信回复因此只会得到“当前没有等待中的请求”。账号连接恢复不意味着旧 turn 恢复。

当前限制：

- 每用户一个 ClawBot；
- 仅扫码者本人私聊文本；
- 无跨节点 worker lease；多副本部署必须保证同一账号只由一个 ChatAPI 实例运行；
- 不保证 iLink 服务端对 client ID 的去重行为，ChatAPI 自身仍以 pending identity 防止重复终态控制。
