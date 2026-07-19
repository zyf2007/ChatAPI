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
  Upload,
} from 'antd'
import {
  DeleteOutlined,
  LogoutOutlined,
  MenuOutlined,
  SaveOutlined,
  SendOutlined,
  UploadOutlined,
  VideoCameraOutlined,
  StopOutlined,
  CloseOutlined,
} from '@ant-design/icons'

import { GithubButton } from './GithubButton'
import { ThemeToggle } from './ThemeToggle'
import { ToolField } from './ToolField'
import { ChatMessageList } from './ChatMessageList'
import { ToolCallAssistPopover } from '../features/tool-call-assist/ToolCallAssistPopover'
import { normalizeChatText } from '../lib/chat-format'
import type {
  ComposerMode,
  BuiltinToolOption,
  ReasoningStreamMode,
  ToolFieldValue,
  ToolSchemaOption,
  VisibleTimelineItem,
  OutputImageAsset,
  AutomationExecutionState,
  AutomationRecordingState,
} from '../types/chat'

const { TextArea } = Input

type ChatPaneProps = {
  automationExecution: AutomationExecutionState | null
  automationRecording: AutomationRecordingState
  availableBuiltinTools: BuiltinToolOption[]
  availableToolSchemas: ToolSchemaOption[]
  builtinToolKind: string
  builtinToolQuery: string
  builtinToolAsset: OutputImageAsset | null
  uploadingOutputImage: boolean
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
  onAutomationRecording: (action: 'start' | 'stop' | 'cancel') => void | Promise<void>
  onOutputImageUpload: (file: File) => Promise<OutputImageAsset>
  onLogout: () => void | Promise<void>
  onOpenDrawer: () => void
  onSend: () => void | Promise<void>
  selectedConversationTitle: string
  selectedConversationId: string
  selectedRequestId: string
  selectedRequestFormat: string
  selectedToolSchema: ToolSchemaOption | null
  sending: boolean
  setComposer: (value: string) => void
  setComposerMode: (value: ComposerMode) => void
  setBuiltinToolKind: (value: string) => void
  setBuiltinToolQuery: (value: string) => void
  setBuiltinToolAsset: (value: OutputImageAsset | null) => void
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
  userID: string
  visibleMessages: VisibleTimelineItem[]
}

export function ChatPane(props: ChatPaneProps) {
  const {
    availableBuiltinTools,
    automationExecution,
    automationRecording,
    availableToolSchemas,
    builtinToolKind,
    builtinToolQuery,
    builtinToolAsset,
    uploadingOutputImage,
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
    onAutomationRecording,
    onOutputImageUpload,
    onLogout,
    onOpenDrawer,
    onSend,
    selectedConversationTitle,
    selectedConversationId,
    selectedRequestId,
    selectedRequestFormat,
    selectedToolSchema,
    sending,
    setComposer,
    setComposerMode,
    setBuiltinToolKind,
    setBuiltinToolQuery,
    setBuiltinToolAsset,
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
    userID,
    visibleMessages,
  } = props
  const [imageFileName, setImageFileName] = useState('')
  const [imageFileSize, setImageFileSize] = useState(0)
  const [imageFileError, setImageFileError] = useState('')

  async function selectImageFile(file: File) {
    if (!file.type.startsWith('image/')) {
      setImageFileError('请选择图片文件')
      return
    }

    try {
      const asset = await onOutputImageUpload(file)
      setImageFileName(file.name)
      setImageFileSize(asset.bytes)
      setImageFileError('')
    } catch (error) {
      setImageFileError(error instanceof Error ? error.message : '图片上传失败')
    }
  }

  function clearImageFile() {
    setBuiltinToolAsset(null)
    setImageFileName('')
    setImageFileSize(0)
    setImageFileError('')
  }

  function selectBuiltinToolKind(kind: string) {
    if (kind !== builtinToolKind) clearImageFile()
    setBuiltinToolKind(kind)
  }
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
  const isResponsesConversation = selectedRequestFormat === 'responses'
  const composerModeOptions = [
    { label: 'Assistant Message', value: 'assistant_message' },
    { label: '添加思考内容', value: 'thinking' },
    { label: 'Tool Call', value: 'tool_call' },
    ...(availableBuiltinTools.length ? [{ label: '内置工具', value: 'builtin_tool' }] : []),
  ]
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
            <div className="automation-record-bar">
              <Space size={8} wrap>
                {!automationRecording.active ? (
                  <Button
                    icon={<VideoCameraOutlined />}
                    disabled={!isWaitingForUser || sending}
                    onClick={() => void onAutomationRecording('start')}
                  >
                    录制自动化
                  </Button>
                ) : (
                  <>
                    <span className="automation-record-indicator">
                      <span className="automation-record-dot" />
                      {automationRecording.conversation_id === selectedConversationId ? '正在录制' : '正在录制另一会话'} · {automationRecording.steps.length} 步
                    </span>
                    <Button type="primary" icon={<StopOutlined />} onClick={() => void onAutomationRecording('stop')}>
                      停止并编辑
                    </Button>
                    <Button icon={<CloseOutlined />} onClick={() => void onAutomationRecording('cancel')}>
                      取消
                    </Button>
                  </>
                )}
                {automationExecution?.status === 'running' ? (
                  <Typography.Text type="secondary">
          自动播放第 {automationExecution.cycle || 1} 轮 · {automationExecution.step_index}/{automationExecution.step_count}
                  </Typography.Text>
                ) : null}
                {automationExecution?.status === 'failed' || automationExecution?.status === 'cancelled' ? (
                  <Typography.Text type="danger">
                    自动播放{automationExecution.status === 'failed' ? '失败' : '已取消'}{automationExecution.reason ? `：${automationExecution.reason}` : ''}
                  </Typography.Text>
                ) : null}
                {automationExecution?.status === 'completed' ? (
                  <Typography.Text type="secondary">自动播放已完成</Typography.Text>
                ) : null}
                {automationRecording.warning ? (
                  <Typography.Text type="danger">{automationRecording.warning}</Typography.Text>
                ) : null}
              </Space>
            </div>
            <div className="composer-mode-row">
              <Space wrap align="center" size={10}>
                <Segmented
                  value={composerMode}
                  onChange={(value) => {
                    const nextMode = value as ComposerMode
                    setComposerMode(nextMode)
                  }}
                  options={[
                    ...composerModeOptions,
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
                  <ToolCallAssistPopover
                    key={`${selectedConversationId}:${selectedRequestId}:${selectedToolSchema?.name ?? ''}`}
                    disabled={sending || !isWaitingForUser}
                    schema={selectedToolSchema}
                    userID={userID}
                    onApply={(values) => setToolFormValues((current) => ({ ...current, ...values }))}
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
            {composerMode === 'builtin_tool' && (
              <div className="tool-call-panel">
                <div className="tool-call-fields">
                  <Select
                    value={builtinToolKind || undefined}
                    onChange={selectBuiltinToolKind}
                    placeholder="选择内置工具"
                    options={availableBuiltinTools.map((tool) => ({
                      label: tool.label || tool.kind,
                      value: tool.kind,
                    }))}
                    disabled={sending || !isWaitingForUser || availableBuiltinTools.length === 0}
                  />
                </div>
                {builtinToolKind === 'web_search' ? (
                  <TextArea
                    value={builtinToolQuery}
                    onChange={(event) => setBuiltinToolQuery(event.target.value)}
                    placeholder="搜索词，会发送 Responses web_search_call 事件"
                    autoSize={{ minRows: 3, maxRows: 6 }}
                    className="composer-textarea"
                    disabled={sending || !isWaitingForUser}
                  />
                ) : null}
                {builtinToolKind === 'image_generation' ? (
                  <div className="image-result-upload">
                    {builtinToolAsset ? (
                      <div className="image-result-preview">
                        <img src={builtinToolAsset.url} alt="待输出的生图结果" />
                        <div className="image-result-file">
                          <Typography.Text strong ellipsis={{ tooltip: imageFileName }}>
                            {imageFileName || '已选择图片'}
                          </Typography.Text>
                          {imageFileSize > 0 ? (
                            <Typography.Text type="secondary">
                              {(imageFileSize / 1024).toFixed(imageFileSize >= 1024 * 1024 ? 0 : 1)} KB
                            </Typography.Text>
                          ) : null}
                        </div>
                        <Button
                          icon={<DeleteOutlined />}
                          onClick={clearImageFile}
                          disabled={sending || uploadingOutputImage || !isWaitingForUser}
                        >
                          移除
                        </Button>
                      </div>
                    ) : (
                      <Upload.Dragger
                        accept="image/*"
                        beforeUpload={(file) => {
                          void selectImageFile(file)
                          return Upload.LIST_IGNORE
                        }}
                        disabled={sending || uploadingOutputImage || !isWaitingForUser}
                        maxCount={1}
                        multiple={false}
                        showUploadList={false}
                      >
                        <UploadOutlined className="image-result-upload-icon" />
                        <Typography.Text strong>
                          {uploadingOutputImage ? '正在处理图片' : '点击或拖入图片'}
                        </Typography.Text>
                        <Typography.Text type="secondary">
                          服务端会转为 AVIF，确认输出时再生成协议 Base64
                        </Typography.Text>
                      </Upload.Dragger>
                    )}
                    {imageFileError ? (
                      <Typography.Text type="danger">{imageFileError}</Typography.Text>
                    ) : null}
                  </div>
                ) : null}
                {!builtinToolKind ? (
                  <div className="tool-form-empty">当前请求没有可用内置工具。</div>
                ) : null}
              </div>
            )}
            {composerMode === 'thinking' && (
              <div className="thinking-panel">
                <div className="thinking-panel-header">
                  <Typography.Text className="thinking-panel-title">公开思考内容</Typography.Text>
                  <Typography.Text className="thinking-panel-hint">
                    当前会以{' '}
                    {reasoningStreamMode === 'reasoning' ? 'reasoning' : 'summery'}
                    输出给调用方 · Enter 流式输出，Shift+Enter 换行
                  </Typography.Text>
                </div>
                <TextArea
                  value={thinkingText}
                  onChange={(event) => setThinkingText(event.target.value)}
                  onKeyDown={handleComposerKeyDown}
                  placeholder={
                    isWaitingForUser
                      ? '输入思考过程。按 Enter 会立刻流式输出给调用方（和正文一样），Shift+Enter 换行。'
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
                      ? '输入你作为 assistant 的回复。Enter 流式输出，Shift+Enter 换行，Ctrl/⌘+Enter 结束输出。'
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
              {sending
                ? '正在发送并等待服务端同步草稿…'
                : isWaitingForUser
                ? composerMode === 'assistant_message'
                  ? 'Enter 流式输出，Shift+Enter 换行，Ctrl/⌘+Enter 结束。片段会保留在本轮回复里。'
                : composerMode === 'thinking'
                    ? `Enter 流式输出思考（${
                        isResponsesConversation && reasoningStreamMode === 'reasoning'
                          ? 'reasoning'
                          : 'summery'
                      }），Shift+Enter 换行。思考不会结束这一轮。`
                    : composerMode === 'builtin_tool'
                      ? '内置工具会输出 Responses 官方内置工具事件，不会结束这一轮。'
                    : 'Tool Call 模式会根据 schema 组装参数 JSON，点击左侧按钮会直接输出一个 function_call item。'
                : '没有新的 user 请求时不能输出回复。'}
            </Typography.Text>
            <Space>
              <Button
                type={composerMode === 'assistant_message' ? 'default' : 'primary'}
                icon={<SaveOutlined />}
                onClick={() => void onDraft()}
                loading={sending}
                disabled={
                  !isWaitingForUser ||
                  sending ||
                  (composerMode === 'assistant_message'
                    ? !normalizeChatText(composer)
                    : composerMode === 'thinking'
                      ? !normalizeChatText(thinkingText)
                      : composerMode === 'builtin_tool'
                        ? !builtinToolKind.trim() ||
                          (builtinToolKind === 'web_search' && !builtinToolQuery.trim()) ||
                          (builtinToolKind === 'image_generation' && !builtinToolAsset)
                      : !toolName.trim())
                }
              >
                {composerMode === 'assistant_message'
                  ? '流式输出'
                  : composerMode === 'thinking'
                    ? '输出思考'
                    : composerMode === 'builtin_tool'
                      ? '输出内置工具'
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
                  (!normalizeChatText(composer) && !draftBuffer)
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
