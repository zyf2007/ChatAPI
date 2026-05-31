import {
  useEffect,
  useRef,
  useState,
  type CSSProperties,
  type Dispatch,
  type KeyboardEvent,
  type RefObject,
  type SetStateAction,
} from 'react'

import {
  Button,
  Card,
  Empty,
  Flex,
  Input,
  Modal,
  Select,
  Tag,
  Segmented,
  Space,
  Typography,
} from 'antd'
import { EyeOutlined, LogoutOutlined, MenuOutlined, SaveOutlined, SendOutlined } from '@ant-design/icons'

import { GithubButton } from './GithubButton'
import { ThemeToggle } from './ThemeToggle'
import { ToolField } from './ToolField'
import { ChatMessageList } from './ChatMessageList'
import { appMessage } from '../lib/antdApp'
import type {
  ComposerMode,
  ReasoningStreamMode,
  ToolFieldValue,
  MessageItem,
  ToolSchemaOption,
  VisibleMessage,
} from '../types/chat'

const { TextArea } = Input

type RequestContextRecord = {
  id: string
  created_at: string
  request_id: string
  request_format: string
  model: string
  request_keys: string[]
  input_payload: unknown
  headers: {
    user_agent?: string
    content_type?: string
    origin?: string
    referer?: string
  }
  message_roles: string[]
}

function extractRequestContextFromMessages(messages: MessageItem[]): RequestContextRecord[] {
  const items: RequestContextRecord[] = []
  const seen = new Set<string>()

  for (let index = messages.length - 1; index >= 0; index -= 1) {
    const message = messages[index]
    const debug = message.metadata?.request_debug
    if (!debug || typeof debug !== 'object') continue

    const requestId = String(debug.request_id || '').trim()
    if (!requestId || seen.has(requestId)) continue
    seen.add(requestId)

    const inputPayload = debug.input_payload
    const payloadItems = Array.isArray(inputPayload) ? inputPayload : [inputPayload]
    const messageRoles = payloadItems
      .map((item) => (item && typeof item === 'object' && 'role' in item ? String((item as { role?: unknown }).role || '').trim() : ''))
      .filter(Boolean)

    items.push({
      id: message.id,
      created_at: message.created_at,
      request_id: requestId,
      request_format: String(debug.request_format || ''),
      model: String(debug.model || ''),
      request_keys: Array.isArray(debug.request_keys) ? debug.request_keys.map((item) => String(item)) : [],
      input_payload: inputPayload,
      headers: debug.headers && typeof debug.headers === 'object' ? {
        user_agent: String((debug.headers as { user_agent?: unknown }).user_agent || ''),
        content_type: String((debug.headers as { content_type?: unknown }).content_type || ''),
        origin: String((debug.headers as { origin?: unknown }).origin || ''),
        referer: String((debug.headers as { referer?: unknown }).referer || ''),
      } : {},
      message_roles: messageRoles,
    })
  }

  return items
}

function formatSnapshotValue(value: unknown): string {
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

function renderSnapshotPayload(payload: unknown) {
  const items = Array.isArray(payload) ? payload : [payload]
  return items.map((item, index) => {
    const role = item && typeof item === 'object' && 'role' in item
      ? String((item as { role?: unknown }).role || 'item')
      : 'item'
    const content = item && typeof item === 'object' && 'content' in item
      ? (item as { content?: unknown }).content
      : item
    const isHiddenRole = role === 'system' || role === 'developer'
    return (
      <div className={`request-context-item ${isHiddenRole ? 'request-context-item-hidden' : ''}`} key={`${role}-${index}`}>
        <div className="request-context-item-header">
          <Tag color={isHiddenRole ? 'gold' : role === 'assistant' ? 'blue' : role === 'tool' ? 'purple' : 'green'}>{role}</Tag>
          {isHiddenRole ? <Typography.Text type="secondary">默认不显示在普通聊天流中</Typography.Text> : null}
        </div>
        <pre className="request-context-pre">{formatSnapshotValue(content)}</pre>
      </div>
    )
  })
}

type ChatPaneProps = {
  availableToolSchemas: ToolSchemaOption[]
  chatScrollRef: RefObject<HTMLDivElement | null>
  composer: string
  composerMode: ComposerMode
  draftBuffer: string
  handleComposerKeyDown: (event: KeyboardEvent<HTMLTextAreaElement>) => void
  isMobile: boolean
  isWaitingForUser: boolean
  keyboardOffset: number
  messagesLoading: boolean
  onDraft: () => void | Promise<void>
  onLogout: () => void | Promise<void>
  onOpenDrawer: () => void
  onSend: () => void | Promise<void>
  selectedConversationId: string
  selectedConversationTitle: string
  selectedRequestFormat: string
  selectedToolSchema: ToolSchemaOption | null
  sending: boolean
  setComposer: (value: string) => void
  setComposerMode: (value: ComposerMode) => void
  setThinkingText: (value: string) => void
  setReasoningStreamMode: (value: ReasoningStreamMode) => void
  setToolCallId: (value: string) => void
  setToolFormValues: Dispatch<SetStateAction<Record<string, ToolFieldValue>>>
  setToolName: (value: string) => void
  thinkingText: string
  reasoningStreamMode: ReasoningStreamMode
  toolCallId: string
  toolFormValues: Record<string, ToolFieldValue>
  toolName: string
  visibleMessages: VisibleMessage[]
}

export function ChatPane(props: ChatPaneProps) {
  const {
    availableToolSchemas,
    chatScrollRef,
    composer,
    composerMode,
    draftBuffer,
    handleComposerKeyDown,
    isMobile,
    isWaitingForUser,
    keyboardOffset,
    messagesLoading,
    onDraft,
    onLogout,
    onOpenDrawer,
    onSend,
    selectedConversationId,
    selectedConversationTitle,
    selectedRequestFormat,
    selectedToolSchema,
    sending,
    setComposer,
    setComposerMode,
    setThinkingText,
    setReasoningStreamMode,
    setToolCallId,
    setToolFormValues,
    setToolName,
    thinkingText,
    reasoningStreamMode,
    toolCallId,
    toolFormValues,
    toolName,
    visibleMessages,
  } = props
  const composerCardRef = useRef<HTMLDivElement | null>(null)
  const [composerHeight, setComposerHeight] = useState(0)
  const [requestContextOpen, setRequestContextOpen] = useState(false)
  const [requestSnapshots, setRequestSnapshots] = useState<RequestContextRecord[]>([])
  const [visualViewportRect, setVisualViewportRect] = useState(() => ({
    bottomInset: 0,
    height: typeof window === 'undefined' ? 0 : window.innerHeight,
    offsetTop: 0,
  }))

  useEffect(() => {
    const element = composerCardRef.current
    if (!element) return

    const updateComposerHeight = () => {
      setComposerHeight(Math.ceil(element.getBoundingClientRect().height))
    }

    updateComposerHeight()

    if (typeof ResizeObserver === 'undefined') {
      window.addEventListener('resize', updateComposerHeight)
      return () => window.removeEventListener('resize', updateComposerHeight)
    }

    const observer = new ResizeObserver(updateComposerHeight)
    observer.observe(element)
    return () => observer.disconnect()
  }, [])

  useEffect(() => {
    if (typeof window === 'undefined') return

    const updateVisualViewportRect = () => {
      const viewport = window.visualViewport
      const height = Math.round(viewport?.height ?? window.innerHeight)
      const offsetTop = Math.round(viewport?.offsetTop ?? 0)
      const bottomInset = Math.max(0, Math.round(window.innerHeight - height - offsetTop))
      setVisualViewportRect({
        bottomInset,
        height,
        offsetTop,
      })
    }

    updateVisualViewportRect()

    const viewport = window.visualViewport
    window.addEventListener('resize', updateVisualViewportRect)
    viewport?.addEventListener('resize', updateVisualViewportRect)
    viewport?.addEventListener('scroll', updateVisualViewportRect)

    return () => {
      window.removeEventListener('resize', updateVisualViewportRect)
      viewport?.removeEventListener('resize', updateVisualViewportRect)
      viewport?.removeEventListener('scroll', updateVisualViewportRect)
    }
  }, [])

  const paneStyle = {
    '--composer-height': `${composerHeight}px`,
    '--keyboard-offset': `${keyboardOffset}px`,
    '--app-viewport-height': `${visualViewportRect.height}px`,
    '--visual-keyboard-offset': `${visualViewportRect.bottomInset}px`,
    '--visual-viewport-height': `${visualViewportRect.height}px`,
  } as CSSProperties
  function openRequestContext() {
    if (!selectedConversationId) {
      appMessage.warning('请先选择一个会话')
      return
    }
    const snapshots = extractRequestContextFromMessages(visibleMessages)
    setRequestSnapshots(snapshots)
    setRequestContextOpen(true)
  }

  const composerStyle = {
    bottom: isMobile ? `${visualViewportRect.bottomInset}px` : 0,
    maxHeight: isMobile
      ? `calc(${visualViewportRect.height}px - env(safe-area-inset-top) - 8px)`
      : undefined,
  } as CSSProperties

  const toolFields = Object.entries(selectedToolSchema?.parameters.properties ?? {})
  const isResponsesConversation = selectedRequestFormat === 'responses'
  const reasoningModeOptions = [
    { label: 'summery 模式', value: 'summery' },
    { label: 'reasoning 模式', value: 'reasoning' },
  ]

  return (
    <div className="chat-pane" style={paneStyle}>
      <div className="chat-topbar">
        <Space align="center" size={12}>
          {isMobile && (
            <Button icon={<MenuOutlined />} onClick={onOpenDrawer} className="menu-button" />
          )}
          <div>
            <Typography.Text className="eyebrow">OpenAI Responses</Typography.Text>
            <Typography.Title level={3} className="chat-title">
              {selectedConversationTitle || '选择一个会话'}
            </Typography.Title>
          </div>
        </Space>
        <Space size={10}>
          <Button
            icon={<EyeOutlined />}
            disabled={!selectedConversationId}
            onClick={openRequestContext}
          >
            查看完整上下文
          </Button>
          <GithubButton className="workspace-github-button" />
          <ThemeToggle className="workspace-theme-toggle" />
          {!isMobile && (
            <Button icon={<LogoutOutlined />} onClick={() => void onLogout()}>
              退出
            </Button>
          )}
        </Space>
      </div>

      <div ref={chatScrollRef} className="chat-scroll">
        <ChatMessageList
          isWaitingForUser={isWaitingForUser}
          messagesLoading={messagesLoading}
          sending={sending}
          visibleMessages={visibleMessages}
        />
      </div>

      <Modal
        open={requestContextOpen}
        title="完整请求上下文"
        width={920}
        footer={null}
        onCancel={() => setRequestContextOpen(false)}
      >
        <Typography.Paragraph type="secondary">
          这里展示该会话最近请求的脱敏原始上下文。system/developer 默认不会出现在普通聊天流，但会在这里用于调试。
        </Typography.Paragraph>
        {requestSnapshots.length === 0 ? (
          <Empty description="当前会话暂无可用的请求上下文" />
        ) : (
          <Space direction="vertical" size={12} className="request-context-stack">
            {requestSnapshots.map((snapshot) => (
              <Card key={snapshot.id} size="small" className="request-context-card">
                <Space direction="vertical" size={10} className="request-context-stack">
                  <Space wrap size={8}>
                    <Tag color="geekblue">{snapshot.request_format}</Tag>
                    <Tag>{snapshot.model}</Tag>
                    <Typography.Text type="secondary">{snapshot.created_at}</Typography.Text>
                  </Space>
                  <div className="request-summary-grid">
                    <div className="request-summary-item"><span className="request-summary-label">请求格式</span><span className="request-summary-value">{snapshot.request_format || '-'}</span></div>
                    <div className="request-summary-item"><span className="request-summary-label">模型</span><span className="request-summary-value">{snapshot.model || '-'}</span></div>
                    <div className="request-summary-item"><span className="request-summary-label">请求 ID</span><span className="request-summary-value">{snapshot.request_id || '-'}</span></div>
                    <div className="request-summary-item request-summary-item-wide"><span className="request-summary-label">请求 Keys</span><span className="request-summary-value">{(snapshot.request_keys ?? []).join(', ') || '-'}</span></div>
                    <div className="request-summary-item request-summary-item-wide"><span className="request-summary-label">User-Agent</span><span className="request-summary-value">{snapshot.headers?.user_agent || '-'}</span></div>
                    <div className="request-summary-item request-summary-item-wide"><span className="request-summary-label">Content-Type</span><span className="request-summary-value">{snapshot.headers?.content_type || '-'}</span></div>
                  </div>
                  <Space wrap size={6}>
                    {snapshot.message_roles.map((role, index) => (
                      <Tag key={`${snapshot.id}-${role}-${index}`} color={role === 'system' || role === 'developer' ? 'gold' : undefined}>
                        {role}
                      </Tag>
                    ))}
                  </Space>
                  <div className="request-context-list">
                    {renderSnapshotPayload(snapshot.input_payload)}
                  </div>
                </Space>
              </Card>
            ))}
          </Space>
        )}
      </Modal>

      <Card ref={composerCardRef} className="composer-card" style={composerStyle}>
        <div className="composer-shell">
          <Space direction="vertical" size={12} className="composer-stack">
            {draftBuffer && (
              <div className="draft-banner">
                <span>已流式输出 {draftBuffer.length} 字</span>
                <Button
                  size="small"
                  disabled={composerMode !== 'assistant_message'}
                  onClick={() => {
                    setComposer(`${draftBuffer}${composer}`)
                  }}
                >
                  继续编辑
                </Button>
              </div>
            )}
            <div className="composer-mode-row">
              <Space wrap align="center" size={10}>
                <Segmented
                  value={composerMode}
                  onChange={(value) => {
                    const nextMode = value as ComposerMode
                    setComposerMode(nextMode)
                  }}
                  options={[
                    { label: 'Assistant Message', value: 'assistant_message' },
                    { label: '添加思考内容', value: 'thinking' },
                    { label: 'Tool Call', value: 'tool_call' },
                  ]}
                  disabled={sending || !isWaitingForUser}
                />
                {composerMode === 'thinking' && isResponsesConversation ? (
                  <div className="reasoning-mode-selector">
                    <Select
                      value={reasoningStreamMode}
                      onChange={(value) => setReasoningStreamMode(value as ReasoningStreamMode)}
                      options={reasoningModeOptions}
                      disabled={sending || !isWaitingForUser}
                      className="reasoning-mode-select"
                      dropdownMatchSelectWidth={false}
                    />
                  </div>
                ) : null}
              </Space>
            </div>
            {composerMode === 'tool_call' && (
              <div className="tool-call-panel">
                <div className="tool-call-fields">
                  <Select
                    value={toolName || undefined}
                    onChange={(value) => setToolName(value)}
                    placeholder={availableToolSchemas.length ? '选择一个 tool' : '当前请求没有可用 schema'}
                    options={availableToolSchemas.map((schema) => ({
                      label: schema.name,
                      value: schema.name,
                      title: schema.description,
                    }))}
                    disabled={sending || !isWaitingForUser || availableToolSchemas.length === 0}
                  />
                  <Input
                    value={toolCallId}
                    onChange={(event) => setToolCallId(event.target.value)}
                    placeholder="tool call id，可留空自动生成"
                    disabled={sending || !isWaitingForUser}
                  />
                </div>
                {selectedToolSchema && (
                  <div className="tool-schema-summary">
                    <div className="tool-schema-header">
                      <span className="tool-schema-name">{selectedToolSchema.name}</span>
                      <span className="tool-schema-badge">{toolFields.length} fields</span>
                    </div>
                    {selectedToolSchema.description ? (
                      <Typography.Text className="tool-schema-description">
                        {selectedToolSchema.description}
                      </Typography.Text>
                    ) : null}
                  </div>
                )}
                {selectedToolSchema ? (
                  <div className="tool-form-grid">
                    {toolFields.length ? (
                      toolFields.map(([fieldName, schema]) => (
                        <ToolField
                          key={fieldName}
                          disabled={sending || !isWaitingForUser}
                          fieldName={fieldName}
                          onChange={(nextField, nextValue) =>
                            setToolFormValues((prev) => ({
                              ...prev,
                              [nextField]: nextValue,
                            }))
                          }
                          required={(selectedToolSchema.parameters.required ?? []).includes(fieldName)}
                          schema={schema}
                          value={toolFormValues[fieldName]}
                        />
                      ))
                    ) : (
                      <div className="tool-form-empty">当前 tool 没有参数，直接点击左侧按钮输出即可。</div>
                    )}
                  </div>
                ) : (
                  <div className="tool-form-empty">当前消息里没有可解析的 tool schema。</div>
                )}
              </div>
            )}
            {composerMode === 'thinking' && (
              <div className="thinking-panel">
                <div className="thinking-panel-header">
                  <Typography.Text className="thinking-panel-title">公开思考内容</Typography.Text>
                  <Typography.Text className="thinking-panel-hint">
                    当前会以{' '}
                    {reasoningStreamMode === 'reasoning' ? 'reasoning' : 'summery'}
                    输出给调用方
                  </Typography.Text>
                </div>
                <TextArea
                  value={thinkingText}
                  onChange={(event) => setThinkingText(event.target.value)}
                  placeholder={
                    isWaitingForUser
                      ? '输入要展示给调用方看的思考过程，点击“输出思考”会追加到当前回复草稿里。'
                      : '当前没有等待中的 user 请求。'
                  }
                  autoSize={{ minRows: 4, maxRows: 10 }}
                  className="composer-textarea thinking-textarea"
                  disabled={sending || !isWaitingForUser}
                />
              </div>
            )}
            {composerMode === 'assistant_message' && (
              <div className="answer-panel">
                <TextArea
                  value={composer}
                  onChange={(event) => setComposer(event.target.value)}
                  onKeyDown={handleComposerKeyDown}
                  placeholder={
                    isWaitingForUser
                      ? '输入你作为 assistant 的回复。点“流式输出”会把当前内容追加到这轮回复里，点“结束输出”会结束这一轮。'
                      : '当前没有等待中的 user 请求。'
                  }
                  autoSize={{ minRows: 4, maxRows: 10 }}
                  className="composer-textarea"
                  disabled={sending || !isWaitingForUser}
                />
              </div>
            )}
          </Space>
          <Flex justify="space-between" align="center" gap={12} wrap className="composer-actions">
            <Typography.Text className="composer-hint">
              {isWaitingForUser
                ? composerMode === 'assistant_message'
                  ? '流式输出的片段会保留在本轮回复里，结束输出之后这一轮结束。'
                : composerMode === 'thinking'
                    ? `思考内容会以 ${
                        isResponsesConversation && reasoningStreamMode === 'reasoning'
                          ? 'reasoning'
                          : 'summery'
                      } 追加到当前回复草稿，不会结束这一轮。`
                    : 'Tool Call 模式会根据 schema 组装参数 JSON，点击左侧按钮会直接输出一个 function_call item。'
                : '没有新的 user 请求时不能输出回复。'}
            </Typography.Text>
            <Space>
              <Button
                type={composerMode === 'assistant_message' ? 'default' : 'primary'}
                icon={<SaveOutlined />}
                onClick={() => void onDraft()}
                disabled={
                  !isWaitingForUser ||
                  sending ||
                  (composerMode === 'assistant_message'
                    ? !composer.trim()
                    : composerMode === 'thinking'
                      ? !thinkingText.trim()
                      : !toolName.trim())
                }
              >
                {composerMode === 'assistant_message'
                  ? '流式输出'
                  : composerMode === 'thinking'
                    ? '输出思考'
                    : '输出 Tool Call'}
              </Button>
              <Button
                type={composerMode === 'assistant_message' ? 'primary' : 'default'}
                icon={<SendOutlined />}
                onClick={() => void onSend()}
                loading={sending}
                disabled={
                  sending ||
                  !isWaitingForUser ||
                  composerMode !== 'assistant_message' ||
                  (!composer.trim() && !draftBuffer.trim())
                }
              >
                结束输出
              </Button>
            </Space>
          </Flex>
        </div>
      </Card>
    </div>
  )
}
