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
  Flex,
  Input,
  Select,
  Segmented,
  Space,
  Typography,
} from 'antd'
import { LogoutOutlined, MenuOutlined, SaveOutlined, SendOutlined } from '@ant-design/icons'

import { GithubButton } from './GithubButton'
import { ThemeToggle } from './ThemeToggle'
import { ToolField } from './ToolField'
import { ChatMessageList } from './ChatMessageList'
import type {
  ComposerMode,
  ToolFieldValue,
  ToolSchemaOption,
  VisibleMessage,
} from '../types/chat'

const { TextArea } = Input

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
  selectedConversationTitle: string
  selectedToolSchema: ToolSchemaOption | null
  sending: boolean
  setComposer: (value: string) => void
  setComposerMode: (value: ComposerMode) => void
  setThinkingText: (value: string) => void
  setToolCallId: (value: string) => void
  setToolFormValues: Dispatch<SetStateAction<Record<string, ToolFieldValue>>>
  setToolName: (value: string) => void
  thinkingText: string
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
    selectedConversationTitle,
    selectedToolSchema,
    sending,
    setComposer,
    setComposerMode,
    setThinkingText,
    setToolCallId,
    setToolFormValues,
    setToolName,
    thinkingText,
    toolCallId,
    toolFormValues,
    toolName,
    visibleMessages,
  } = props
  const composerCardRef = useRef<HTMLDivElement | null>(null)
  const [composerHeight, setComposerHeight] = useState(0)
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
  const composerStyle = {
    bottom: isMobile ? `${visualViewportRect.bottomInset}px` : 0,
    maxHeight: isMobile
      ? `calc(${visualViewportRect.height}px - env(safe-area-inset-top) - 8px)`
      : undefined,
  } as CSSProperties

  const toolFields = Object.entries(selectedToolSchema?.parameters.properties ?? {})

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
              <Segmented
                value={composerMode}
                onChange={(value) => {
                  const nextMode = value as ComposerMode
                  setComposerMode(nextMode)
                }}
                options={[
                  { label: 'Assistant Message', value: 'assistant_message' },
                  { label: '添加思考', value: 'thinking' },
                  { label: 'Tool Call', value: 'tool_call' },
                ]}
                disabled={sending || !isWaitingForUser}
              />
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
                    会自动以 &lt;think&gt;...&lt;/think&gt; 输出给调用方
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
                    ? '思考内容会包成 <think>...</think> 追加到当前回复草稿，不会结束这一轮。'
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
