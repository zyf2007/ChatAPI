import { useEffect, useRef, useState } from 'react'
import {
  App as AntApp,
  Avatar,
  Badge,
  Button,
  Card,
  Drawer,
  Empty,
  Flex,
  Form,
  Grid,
  Input,
  Layout,
  List,
  Space,
  Spin,
  Tooltip,
  Typography,
  message,
} from 'antd'
import {
  DeleteOutlined,
  LogoutOutlined,
  MenuOutlined,
  SaveOutlined,
  SendOutlined,
  UserOutlined,
} from '@ant-design/icons'
import './App.css'

type AuthUser = {
  username: string
}

type AuthSession = {
  authenticated: boolean
  user: AuthUser | null
}

type Conversation = {
  id: string
  title: string
  summary: string
  created_at: string
  updated_at: string
  last_message_at: string
  message_count: number
  last_message_preview: string
  metadata?: {
    realtime_status?: 'waiting' | 'closed' | 'aborted' | string
  }
}

type MessageItem = {
  id: string
  role: 'user' | 'assistant' | 'system' | string
  content: string
  created_at: string
  status?: string
}

type ResponsesPayload = {
  conversation: Conversation
  output_text?: string
  output?: Array<{
    content?: Array<{ text?: string }>
  }>
}

const { Header, Sider, Content } = Layout
const { TextArea } = Input

async function requestJson<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, {
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers ?? {}),
    },
    ...init,
  })

  const body = await response.json().catch(() => null)
  if (!response.ok) {
    const fallback =
      body?.error?.message ?? body?.error ?? body?.message ?? '请求失败'
    throw new Error(typeof fallback === 'string' ? fallback : '请求失败')
  }
  return body as T
}

function formatTime(value: string) {
  if (!value) return ''
  return new Intl.DateTimeFormat('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(value))
}

function App() {
  const screens = Grid.useBreakpoint()
  const isMobile = !screens.md
  const [booting, setBooting] = useState(true)
  const [auth, setAuth] = useState<AuthSession>({
    authenticated: false,
    user: null,
  })
  const [loginLoading, setLoginLoading] = useState(false)
  const [form] = Form.useForm()
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [selectedConversationId, setSelectedConversationId] = useState('')
  const [messages, setMessages] = useState<MessageItem[]>([])
  const [composer, setComposer] = useState('')
  const [draftBuffer, setDraftBuffer] = useState('')
  const [sending, setSending] = useState(false)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [keyboardOffset, setKeyboardOffset] = useState(0)
  const [deletingConversationId, setDeletingConversationId] = useState('')
  const chatScrollRef = useRef<HTMLDivElement | null>(null)
  const bottomRef = useRef<HTMLDivElement | null>(null)
  const shouldStickToBottomRef = useRef(true)
  const previousConversationIdRef = useRef('')

  const selectedConversation = conversations.find(
    (item) => item.id === selectedConversationId,
  )
  const isWaitingForUser =
    selectedConversation?.metadata?.realtime_status === 'waiting'

  useEffect(() => {
    let active = true

    async function bootstrapPage() {
      setBooting(true)
      try {
        const session = await requestJson<AuthSession>('/api/auth/session')
        if (!active) return
        setAuth(session)
        if (session.authenticated) {
          await loadConversations()
        }
      } catch (error) {
        if (active) {
          message.error(error instanceof Error ? error.message : '初始化失败')
        }
      } finally {
        if (active) {
          setBooting(false)
        }
      }
    }

    void bootstrapPage()

    return () => {
      active = false
    }
    // loadConversations is stable within this render path and only used here after mount.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    const conversationChanged =
      previousConversationIdRef.current !== selectedConversationId

    if (conversationChanged) {
      previousConversationIdRef.current = selectedConversationId
      shouldStickToBottomRef.current = true
    }

    if (!shouldStickToBottomRef.current) return

    const frame = window.requestAnimationFrame(() => {
      bottomRef.current?.scrollIntoView({
        behavior: conversationChanged ? 'auto' : 'smooth',
        block: 'end',
      })
    })

    return () => window.cancelAnimationFrame(frame)
  }, [selectedConversationId, messages, draftBuffer, sending])

  useEffect(() => {
    if (!auth.authenticated) return
    if (!selectedConversationId) return
    void loadMessages(selectedConversationId)
  }, [auth.authenticated, selectedConversationId])

  useEffect(() => {
    if (!auth.authenticated) return
    const timer = window.setInterval(() => {
      void loadConversations()
      if (selectedConversationId) {
        void loadMessages(selectedConversationId)
      }
    }, 1500)
    return () => window.clearInterval(timer)
    // loadConversations and loadMessages are recreated each render; this effect only needs
    // to react to authentication state and the selected conversation identity.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [auth.authenticated, selectedConversationId])

  useEffect(() => {
    if (typeof window === 'undefined' || !window.visualViewport) {
      setKeyboardOffset(0)
      return
    }

    const viewport = window.visualViewport
    const updateKeyboardOffset = () => {
      const rawOffset = Math.max(
        0,
        window.innerHeight - viewport.height - viewport.offsetTop,
      )
      setKeyboardOffset(rawOffset > 80 ? Math.round(rawOffset) : 0)
    }

    updateKeyboardOffset()
    viewport.addEventListener('resize', updateKeyboardOffset)
    viewport.addEventListener('scroll', updateKeyboardOffset)

    return () => {
      viewport.removeEventListener('resize', updateKeyboardOffset)
      viewport.removeEventListener('scroll', updateKeyboardOffset)
    }
  }, [])

  async function loadConversations(nextSelectedId?: string) {
    const data = await requestJson<{ items: Conversation[] }>('/api/conversations')
    setConversations(data.items)
    const requested =
      nextSelectedId ??
      selectedConversationId ??
      localStorage.getItem('chatapi.conversationId') ??
      ''
    const preferred = data.items.some((item) => item.id === requested)
      ? requested
      : data.items[0]?.id ?? ''

    if (!preferred) {
      if (selectedConversationId) {
        setSelectedConversationId('')
      }
      setMessages([])
      localStorage.removeItem('chatapi.conversationId')
      return
    }

    if (preferred !== selectedConversationId) {
      setSelectedConversationId(preferred)
      localStorage.setItem('chatapi.conversationId', preferred)
    }
  }

  async function loadMessages(conversationId: string) {
    try {
      const data = await requestJson<{ items: MessageItem[] }>(
        `/api/conversations/${conversationId}/messages`,
      )
      setMessages(data.items)
    } catch (error) {
      if (error instanceof Error && error.message.includes('not found')) {
        setMessages([])
        return
      }
      message.error(error instanceof Error ? error.message : '加载消息失败')
    }
  }

  async function handleLogin(values: { username: string; password: string }) {
    setLoginLoading(true)
    try {
      await requestJson<{ ok: boolean; user: AuthUser }>('/api/auth/login', {
        method: 'POST',
        body: JSON.stringify(values),
      })
      const session = await requestJson<AuthSession>('/api/auth/session')
      setAuth(session)
      await loadConversations()
      message.success('登录成功')
    } catch (error) {
      message.error(error instanceof Error ? error.message : '登录失败')
    } finally {
      setLoginLoading(false)
    }
  }

  async function handleLogout() {
    try {
      await requestJson('/api/auth/logout', { method: 'POST' })
    } finally {
      setAuth({ authenticated: false, user: null })
      setConversations([])
      setSelectedConversationId('')
      setMessages([])
      setComposer('')
      setDraftBuffer('')
      localStorage.removeItem('chatapi.conversationId')
      message.info('已退出登录')
    }
  }

  async function handleSelectConversation(conversationId: string) {
    setSelectedConversationId(conversationId)
    localStorage.setItem('chatapi.conversationId', conversationId)
    await loadMessages(conversationId)
    if (isMobile) setDrawerOpen(false)
  }

  async function handleDeleteConversation(conversationId: string) {
    const targetConversation = conversations.find((item) => item.id === conversationId)
    if (targetConversation?.metadata?.realtime_status === 'waiting') {
      message.warning('等待中的会话不允许删除')
      return
    }

    setDeletingConversationId(conversationId)
    try {
      await requestJson(`/api/conversations/${conversationId}`, {
        method: 'DELETE',
      })

      const remaining = conversations.filter((item) => item.id !== conversationId)
      const nextConversationId =
        conversationId === selectedConversationId ? remaining[0]?.id ?? '' : selectedConversationId

      if (!nextConversationId) {
        setSelectedConversationId('')
        setMessages([])
        localStorage.removeItem('chatapi.conversationId')
      } else if (nextConversationId !== selectedConversationId) {
        setSelectedConversationId(nextConversationId)
        localStorage.setItem('chatapi.conversationId', nextConversationId)
        await loadMessages(nextConversationId)
      }

      await loadConversations(nextConversationId)
      message.success('会话已删除')
    } catch (error) {
      message.error(error instanceof Error ? error.message : '删除会话失败')
    } finally {
      setDeletingConversationId('')
    }
  }

  async function handleDraft() {
    if (!isWaitingForUser) return
    const chunk = composer.trim()
    if (!chunk) return
    try {
      await requestJson('/api/chat/draft', {
        method: 'POST',
        body: JSON.stringify({
          text: chunk,
          conversation_id: selectedConversationId || undefined,
        }),
      })
      setDraftBuffer((prev) => `${prev}${chunk}`)
      setComposer('')
      message.success('已暂存')
    } catch (error) {
      message.error(error instanceof Error ? error.message : '暂存失败')
    }
  }

  function handleComposerKeyDown(event: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key !== 'Enter' || event.shiftKey) return
    event.preventDefault()
    if (sending || !isWaitingForUser || !composer.trim()) return
    void handleDraft()
  }

  function handleChatScroll(event: React.UIEvent<HTMLDivElement>) {
    const container = event.currentTarget
    const distanceToBottom =
      container.scrollHeight - container.scrollTop - container.clientHeight
    shouldStickToBottomRef.current = distanceToBottom <= 80
  }

  async function handleSend() {
    if (!isWaitingForUser) return
    const finalText = `${draftBuffer}${composer}`.trim()
    if (!finalText) return

    setSending(true)
    try {
      const payload = {
        text: finalText,
        conversation_id: selectedConversationId || undefined,
      }
      const response = await requestJson<ResponsesPayload>('/api/chat/send', {
        method: 'POST',
        body: JSON.stringify(payload),
      })
      const nextConversationId = response.conversation?.id ?? selectedConversationId
      if (nextConversationId) {
        setSelectedConversationId(nextConversationId)
        localStorage.setItem('chatapi.conversationId', nextConversationId)
      }
      setComposer('')
      setDraftBuffer('')
      await loadConversations(nextConversationId)
      if (nextConversationId) {
        await loadMessages(nextConversationId)
      }
      message.success('已发送')
    } catch (error) {
      message.error(error instanceof Error ? error.message : '发送失败')
    } finally {
      setSending(false)
    }
  }

  const visibleMessages: Array<MessageItem & { draft?: boolean }> = [
    ...messages,
    ...(draftBuffer
      ? [
          {
            id: 'draft-buffer',
            role: 'draft',
            content: draftBuffer,
            created_at: new Date().toISOString(),
            draft: true,
          },
        ]
      : []),
  ]

  const sidebar = (
    <div className="sidebar-inner">
      <div className="sidebar-top">
        <div>
          <Typography.Text className="eyebrow">ChatAPI</Typography.Text>
          <Typography.Title level={4} className="sidebar-title">
            会话
          </Typography.Title>
        </div>
      </div>
      <List
        className="conversation-list"
        dataSource={conversations}
        locale={{ emptyText: <Empty description="暂无会话" /> }}
        renderItem={(item) => {
          const active = item.id === selectedConversationId
          const realtimeStatus = item.metadata?.realtime_status
          const deleteDisabled = realtimeStatus === 'waiting'
          const statusColor =
            realtimeStatus === 'waiting'
              ? '#22c55e'
              : realtimeStatus === 'closed' || realtimeStatus === 'aborted'
                ? '#ef4444'
                : ''
          return (
            <List.Item
              className={`conversation-item ${active ? 'active' : ''}`}
              onClick={() => void handleSelectConversation(item.id)}
            >
              <div className="conversation-row">
                <Space align="start" className="conversation-main">
                  {statusColor ? (
                    <Badge dot color={statusColor} offset={[-4, 4]}>
                      <Avatar shape="square" className="conversation-avatar">
                        {item.title?.slice(0, 1) || '会'}
                      </Avatar>
                    </Badge>
                  ) : (
                    <Avatar shape="square" className="conversation-avatar">
                      {item.title?.slice(0, 1) || '会'}
                    </Avatar>
                  )}
                  <div className="conversation-meta">
                    <Typography.Text className="conversation-title">
                      {item.title || '新会话'}
                    </Typography.Text>
                    <Typography.Paragraph
                      className="conversation-preview"
                      ellipsis={{ rows: 2 }}
                    >
                      {item.last_message_preview || item.summary || '尚无消息'}
                    </Typography.Paragraph>
                    <Typography.Text className="conversation-time">
                      {item.message_count > 0
                        ? `${item.message_count} 条消息 · ${formatTime(
                            item.last_message_at,
                          )}`
                        : '空会话'}
                    </Typography.Text>
                  </div>
                </Space>
                <Tooltip
                  title={deleteDisabled ? '等待中的会话不允许删除' : '删除会话'}
                >
                  <Button
                    type="text"
                    danger
                    size="small"
                    icon={<DeleteOutlined />}
                    className="conversation-delete-button"
                    loading={deletingConversationId === item.id}
                    disabled={deleteDisabled}
                    onClick={(event) => {
                      event.stopPropagation()
                      void handleDeleteConversation(item.id)
                    }}
                  />
                </Tooltip>
              </div>
            </List.Item>
          )
        }}
      />
      <div className="sidebar-footer">
        <Space direction="vertical" size={8} className="footer-stack">
          <Typography.Text className="footer-name">
            {auth.user?.username}
          </Typography.Text>
          <Button icon={<LogoutOutlined />} onClick={() => void handleLogout()}>
            退出登录
          </Button>
        </Space>
      </div>
    </div>
  )

  const chatPane = (
    <div className="chat-pane">
      <div className="chat-topbar">
        <Space align="center" size={12}>
          {isMobile && (
            <Button
              icon={<MenuOutlined />}
              onClick={() => setDrawerOpen(true)}
              className="menu-button"
            />
          )}
          <div>
            <Typography.Text className="eyebrow">OpenAI Responses</Typography.Text>
            <Typography.Title level={3} className="chat-title">
              {selectedConversation?.title || '选择一个会话'}
            </Typography.Title>
          </div>
        </Space>
        <Space>
          {!isMobile && (
            <Button icon={<LogoutOutlined />} onClick={() => void handleLogout()}>
              退出
            </Button>
          )}
        </Space>
      </div>

      <div
        ref={chatScrollRef}
        className="chat-scroll"
        onScroll={handleChatScroll}
      >
        {visibleMessages.length === 0 ? (
          <div className="empty-stage">
            <Empty
              description={
                isWaitingForUser
                  ? '可以开始分段暂存回复，再点击发送结束这一轮'
                  : '等待左侧会话出现绿色状态后再回复'
              }
              image={Empty.PRESENTED_IMAGE_SIMPLE}
            />
          </div>
        ) : (
          visibleMessages.map((item) => {
            const isUser = item.role === 'user'
            const isDraft = item.role === 'draft'
            return (
              <div
                key={item.id}
                className={`message-row ${
                  isUser ? 'user' : 'assistant'
                } ${isDraft ? 'draft' : ''}`}
              >
                {isUser && (
                  <Avatar className="message-avatar user-avatar" icon={<UserOutlined />} />
                )}
                <div className={`message-bubble ${isUser ? 'user' : 'assistant'} ${isDraft ? 'draft' : ''}`}>
                  <div className="message-content">{item.content}</div>
                  <div className="message-meta">
                    <span>{isDraft ? '暂存草稿' : item.role}</span>
                    <span>{formatTime(item.created_at)}</span>
                  </div>
                </div>
                {!isUser && (
                  <Avatar className="message-avatar assistant-avatar">
                    AI
                  </Avatar>
                )}
              </div>
            )
          })
        )}
        {sending && (
          <div className="message-row assistant">
            <Avatar className="message-avatar assistant-avatar">AI</Avatar>
            <div className="message-bubble assistant typing">
              <Spin size="small" />
              <span>正在生成回复...</span>
            </div>
          </div>
        )}
        <div ref={bottomRef} />
      </div>

      <Card className="composer-card" style={{ bottom: keyboardOffset }}>
        <Space direction="vertical" size={12} className="composer-stack">
          {draftBuffer && (
            <div className="draft-banner">
              <span>已暂存 {draftBuffer.length} 字</span>
              <Button
                size="small"
                onClick={() => {
                  setComposer((prev) => `${draftBuffer}${prev}`)
                  setDraftBuffer('')
                }}
              >
                继续编辑
              </Button>
            </div>
          )}
          <TextArea
            value={composer}
            onChange={(event) => setComposer(event.target.value)}
            onKeyDown={handleComposerKeyDown}
            placeholder={
              isWaitingForUser
                ? '输入你作为 assistant 的回复。点“暂存”会把当前内容累积到这轮回复里，点“发送”会结束这一轮。'
                : '当前没有等待中的 user 请求。'
            }
            autoSize={{ minRows: 4, maxRows: 10 }}
            className="composer-textarea"
            disabled={sending || !isWaitingForUser}
          />
          <Flex justify="space-between" align="center" gap={12} wrap>
            <Typography.Text className="composer-hint">
              {isWaitingForUser
                ? '暂存的片段会保留在本轮回复里，发送之后这一轮结束。'
                : '没有新的 user 请求时不能发送回复。'}
            </Typography.Text>
            <Space>
              <Button
                icon={<SaveOutlined />}
                onClick={() => void handleDraft()}
                disabled={!isWaitingForUser || !composer.trim() || sending}
              >
                暂存
              </Button>
              <Button
                type="primary"
                icon={<SendOutlined />}
                onClick={() => void handleSend()}
                loading={sending}
                disabled={
                  sending ||
                  !isWaitingForUser ||
                  (!composer.trim() && !draftBuffer.trim())
                }
              >
                发送
              </Button>
            </Space>
          </Flex>
        </Space>
      </Card>
    </div>
  )

  if (booting) {
    return (
      <div className="boot-screen">
        <Spin size="large" />
      </div>
    )
  }

  if (!auth.authenticated) {
    return (
      <div className="login-screen">
        <div className="login-backdrop" />
        <Card className="login-card">
          <div className="login-copy">
            <Typography.Text className="eyebrow">ChatAPI</Typography.Text>
            <Typography.Title level={2} className="login-title">
              登录后进入聊天工作台
            </Typography.Title>
            <Typography.Paragraph className="login-desc">
              后端提供 OpenAI Responses 风格接口，前端负责会话列表、移动端侧栏和分段暂存。
            </Typography.Paragraph>
          </div>
          <Form
            form={form}
            layout="vertical"
            onFinish={(values) => void handleLogin(values)}
            autoComplete="off"
            className="login-form"
            initialValues={{ username: '', password: '' }}
          >
            <Form.Item
              label="账号"
              name="username"
              rules={[{ required: true, message: '请输入账号' }]}
            >
              <Input placeholder="账号" size="large" />
            </Form.Item>
            <Form.Item
              label="密码"
              name="password"
              rules={[{ required: true, message: '请输入密码' }]}
            >
              <Input.Password placeholder="密码" size="large" />
            </Form.Item>
            <Button
              type="primary"
              htmlType="submit"
              size="large"
              block
              loading={loginLoading}
            >
              登录
            </Button>
          </Form>
        </Card>
      </div>
    )
  }

  return (
    <AntApp>
      <Layout className="app-shell">
        {!isMobile && <Sider className="sidebar">{sidebar}</Sider>}
        <Layout className="main-layout">
          <Header className="header-shell">
            <div className="header-glow" />
          </Header>
          <Content className="content-shell">{chatPane}</Content>
        </Layout>
        <Drawer
          open={drawerOpen}
          onClose={() => setDrawerOpen(false)}
          placement="left"
          width={320}
          className="mobile-drawer"
          bodyStyle={{ padding: 0 }}
        >
          {sidebar}
        </Drawer>
      </Layout>
    </AntApp>
  )
}

export default App
