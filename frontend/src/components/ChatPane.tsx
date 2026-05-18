import type { KeyboardEvent, UIEvent } from 'react'

import {
  Avatar,
  Button,
  Card,
  Empty,
  Flex,
  Input,
  Select,
  Segmented,
  Space,
  Spin,
  Switch,
  Typography,
} from 'antd'
import { LogoutOutlined, MenuOutlined, SaveOutlined, SendOutlined, UserOutlined } from '@ant-design/icons'

import { formatJson, formatTime, getSchemaType, renderMessageContent } from '../lib/chat-format'
import type {
  ComposerMode,
  JsonSchema,
  ToolFieldValue,
  ToolSchemaOption,
  VisibleMessage,
} from '../types/chat'

const { TextArea } = Input

type ChatPaneProps = {
  availableToolSchemas: ToolSchemaOption[]
  bottomRef: React.RefObject<HTMLDivElement | null>
  chatScrollRef: React.RefObject<HTMLDivElement | null>
  composer: string
  composerMode: ComposerMode
  draftBuffer: string
  handleChatScroll: (event: UIEvent<HTMLDivElement>) => void
  handleComposerKeyDown: (event: KeyboardEvent<HTMLTextAreaElement>) => void
  isMobile: boolean
  isWaitingForUser: boolean
  keyboardOffset: number
  onDraft: () => void | Promise<void>
  onLogout: () => void | Promise<void>
  onOpenDrawer: () => void
  onSend: () => void | Promise<void>
  selectedConversationTitle: string
  selectedToolSchema: ToolSchemaOption | null
  sending: boolean
  setComposer: (value: string) => void
  setComposerMode: (value: ComposerMode) => void
  setToolCallId: (value: string) => void
  setToolFormValues: React.Dispatch<React.SetStateAction<Record<string, ToolFieldValue>>>
  setToolName: (value: string) => void
  toolCallId: string
  toolFormValues: Record<string, ToolFieldValue>
  toolName: string
  visibleMessages: VisibleMessage[]
}

function ToolField({
  disabled,
  fieldName,
  onChange,
  required,
  schema,
  value,
}: {
  disabled: boolean
  fieldName: string
  onChange: (fieldName: string, value: ToolFieldValue | string) => void
  required: boolean
  schema: JsonSchema
  value: ToolFieldValue | undefined
}) {
  const type = getSchemaType(schema)
  const label = schema.title || fieldName
  const description = schema.description || ''

  if (schema.enum?.length) {
    return (
      <div key={fieldName} className="tool-form-item">
        <div className="tool-form-label-row">
          <span className="tool-form-label">
            {label}
            {required && <span className="tool-form-required">*</span>}
          </span>
          <span className="tool-form-type">enum</span>
        </div>
        {description ? <div className="tool-form-description">{description}</div> : null}
        <Select
          value={value}
          allowClear={!required}
          placeholder={`选择 ${label}`}
          options={schema.enum.map((option) => ({
            label: String(option),
            value: option,
          }))}
          onChange={(nextValue) => onChange(fieldName, nextValue as ToolFieldValue)}
          disabled={disabled}
        />
      </div>
    )
  }

  if (type === 'boolean') {
    return (
      <div key={fieldName} className="tool-form-item">
        <div className="tool-form-label-row">
          <span className="tool-form-label">
            {label}
            {required && <span className="tool-form-required">*</span>}
          </span>
          <span className="tool-form-type">boolean</span>
        </div>
        {description ? <div className="tool-form-description">{description}</div> : null}
        <Switch
          checked={Boolean(value)}
          onChange={(checked) => onChange(fieldName, checked)}
          disabled={disabled}
        />
      </div>
    )
  }

  const isComplex = type === 'array' || type === 'object'
  const placeholder = isComplex ? `请输入 ${label} 的 JSON` : description || `请输入 ${label}`

  return (
    <div key={fieldName} className="tool-form-item">
      <div className="tool-form-label-row">
        <span className="tool-form-label">
          {label}
          {required && <span className="tool-form-required">*</span>}
        </span>
        <span className="tool-form-type">{type || 'string'}</span>
      </div>
      {description ? <div className="tool-form-description">{description}</div> : null}
      {isComplex ? (
        <TextArea
          value={typeof value === 'string' ? value : ''}
          onChange={(event) => onChange(fieldName, event.target.value)}
          placeholder={placeholder}
          autoSize={{ minRows: 3, maxRows: 8 }}
          disabled={disabled}
        />
      ) : (
        <Input
          value={value == null ? '' : String(value)}
          type={type === 'number' || type === 'integer' ? 'number' : 'text'}
          onChange={(event) => onChange(fieldName, event.target.value)}
          placeholder={placeholder}
          disabled={disabled}
        />
      )}
    </div>
  )
}

export function ChatPane(props: ChatPaneProps) {
  const {
    availableToolSchemas,
    bottomRef,
    chatScrollRef,
    composer,
    composerMode,
    draftBuffer,
    handleChatScroll,
    handleComposerKeyDown,
    isMobile,
    isWaitingForUser,
    keyboardOffset,
    onDraft,
    onLogout,
    onOpenDrawer,
    onSend,
    selectedConversationTitle,
    selectedToolSchema,
    sending,
    setComposer,
    setComposerMode,
    setToolCallId,
    setToolFormValues,
    setToolName,
    toolCallId,
    toolFormValues,
    toolName,
    visibleMessages,
  } = props

  const toolFields = Object.entries(selectedToolSchema?.parameters.properties ?? {})

  const providerLabel = (() => {
    const provider = visibleMessages.find(m => m.role === 'user')?.metadata?.provider
    if (provider === 'chat_completions') return 'OpenAI Chat Completions'
    if (provider === 'anthropic') return 'Anthropic Messages'
    return 'OpenAI Responses'
  })()

  return (
    <div className="chat-pane">
      <div className="chat-topbar">
        <Space align="center" size={12}>
          {isMobile && (
            <Button icon={<MenuOutlined />} onClick={onOpenDrawer} className="menu-button" />
          )}
          <div>
            <Typography.Text className="eyebrow">{providerLabel}</Typography.Text>
            <Typography.Title level={3} className="chat-title">
              {selectedConversationTitle || '选择一个会话'}
            </Typography.Title>
          </div>
        </Space>
        <Space>
          {!isMobile && (
            <Button icon={<LogoutOutlined />} onClick={() => void onLogout()}>
              退出
            </Button>
          )}
        </Space>
      </div>

      <div ref={chatScrollRef} className="chat-scroll" onScroll={handleChatScroll}>
        {visibleMessages.length === 0 ? (
          <div className="empty-stage">
            <Empty
              description={
                isWaitingForUser
                  ? '可以开始流式输出，再点击结束输出完成这一轮'
                  : '等待左侧会话出现绿色状态后再回复'
              }
              image={Empty.PRESENTED_IMAGE_SIMPLE}
            />
          </div>
        ) : (
          visibleMessages.map((item) => {
            const isUser = item.role === 'user'
            const isToolInput = item.role === 'tool'
            const isDraft = item.role === 'draft'
            const isToolCall = item.metadata?.response_mode === 'tool_call'
            const isToolResult = item.metadata?.response_mode === 'tool_result'
            const requestDebug = item.metadata?.request_debug
            const debugSections = [
              { label: '模型', value: requestDebug?.model || item.metadata?.model || '' },
              { label: '请求 ID', value: requestDebug?.request_id || '' },
              { label: '响应 ID', value: requestDebug?.response_id || item.response_id || '' },
              { label: '请求 Keys', value: requestDebug?.request_keys?.join(', ') || '' },
              { label: 'User-Agent', value: requestDebug?.headers?.user_agent || '' },
              { label: 'Content-Type', value: requestDebug?.headers?.content_type || '' },
            ].filter((section) => section.value)
            const hasDebugCard =
              isUser &&
              !isDraft &&
              !!(
                debugSections.length ||
                requestDebug?.tool_schemas?.length ||
                requestDebug?.input_payload != null ||
                requestDebug?.request_body != null
              )

            return (
              <div
                key={item.id}
                className={`message-row ${
                  isUser
                    ? 'user'
                    : isToolInput
                      ? 'tool-input'
                      : isToolCall
                        ? 'tool-call'
                        : isToolResult
                          ? 'tool-result'
                          : 'assistant'
                } ${isDraft ? 'draft' : ''}`}
              >
                {(isUser || isToolInput) && (
                  <Avatar className="message-avatar user-avatar" icon={<UserOutlined />} />
                )}
                <div
                  className={`message-bubble ${
                    isUser
                      ? 'user'
                      : isToolInput
                        ? 'tool-input'
                        : isToolCall
                          ? 'tool-call'
                          : isToolResult
                            ? 'tool-result'
                            : 'assistant'
                  } ${isDraft ? 'draft' : ''}`}
                >
                  {isToolCall && <div className="message-kind-badge">Tool Call</div>}
                  {isToolResult && <div className="message-kind-badge tool-result">Tool Result</div>}
                  <div className="message-content">{renderMessageContent(item.content)}</div>
                  {(isToolCall || isToolResult) && (
                    <div className="message-tool-meta">
                      <div>
                        <span className="message-debug-label">Tool</span>
                        <span className="message-debug-value">{item.metadata?.tool_name || '-'}</span>
                      </div>
                      <div>
                        <span className="message-debug-label">Call ID</span>
                        <span className="message-debug-value">{item.metadata?.tool_call_id || '-'}</span>
                      </div>
                    </div>
                  )}
                  {hasDebugCard && (
                    <details className="message-debug-card">
                      <summary>请求详情</summary>
                      <div className="message-debug-body">
                        {debugSections.map((section) => (
                          <div key={section.label} className="message-debug-row">
                            <span className="message-debug-label">{section.label}</span>
                            <span className="message-debug-value">{section.value}</span>
                          </div>
                        ))}
                        {requestDebug?.tool_schemas?.length ? (
                          <div className="message-debug-block">
                            <div className="message-debug-label">Tool Schemas</div>
                            <pre>{formatJson(requestDebug.tool_schemas)}</pre>
                          </div>
                        ) : null}
                        {requestDebug?.input_payload != null ? (
                          <div className="message-debug-block">
                            <div className="message-debug-label">Input Payload</div>
                            <pre>{formatJson(requestDebug.input_payload)}</pre>
                          </div>
                        ) : null}
                        {requestDebug?.request_body != null ? (
                          <div className="message-debug-block">
                            <div className="message-debug-label">Request Body</div>
                            <pre>{formatJson(requestDebug.request_body)}</pre>
                          </div>
                        ) : null}
                      </div>
                    </details>
                  )}
                  <div className="message-meta">
                    <span>
                      {isDraft
                        ? '流式输出中'
                        : isToolInput
                          ? 'tool'
                          : isToolCall
                            ? 'tool_call'
                            : isToolResult
                              ? 'tool_result'
                              : item.role}
                    </span>
                    <span>{formatTime(item.created_at)}</span>
                  </div>
                </div>
                {!isUser && !isToolInput && (
                  <Avatar className="message-avatar assistant-avatar">AI</Avatar>
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
                      <div className="tool-form-empty">当前 tool 没有参数，直接点击结束输出即可。</div>
                    )}
                  </div>
                ) : (
                  <div className="tool-form-empty">当前消息里没有可解析的 tool schema。</div>
                )}
              </div>
            )}
            {composerMode === 'assistant_message' && (
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
            )}
          </Space>
          <Flex justify="space-between" align="center" gap={12} wrap className="composer-actions">
            <Typography.Text className="composer-hint">
              {isWaitingForUser
                ? composerMode === 'assistant_message'
                  ? '流式输出的片段会保留在本轮回复里，结束输出之后这一轮结束。'
                  : 'Tool Call 模式会根据 schema 组装参数 JSON，结束输出后会返回一个 function_call item。'
                : '没有新的 user 请求时不能输出回复。'}
            </Typography.Text>
            <Space>
              <Button
                icon={<SaveOutlined />}
                onClick={() => void onDraft()}
                disabled={
                  !isWaitingForUser ||
                  !composer.trim() ||
                  sending ||
                  composerMode !== 'assistant_message'
                }
              >
                流式输出
              </Button>
              <Button
                type="primary"
                icon={<SendOutlined />}
                onClick={() => void onSend()}
                loading={sending}
                disabled={
                  sending ||
                  !isWaitingForUser ||
                  (composerMode === 'assistant_message'
                    ? !composer.trim() && !draftBuffer.trim()
                    : !toolName.trim())
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
