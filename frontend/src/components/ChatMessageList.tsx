import { useEffect, useRef, useState, type ReactNode } from 'react'

import { CopyOutlined, WarningOutlined, UserOutlined } from '@ant-design/icons'
import { App, Avatar, Button, Empty, Spin } from 'antd'

import {
  buildCurlCommand,
  formatJson,
  formatTime,
  renderMessageContent,
} from '../lib/chat-format'
import type {
  ConversationEventItem,
  MessageItem,
  TimelineItem,
  TimelineMessageContentPart,
  VisibleTimelineDraftItem,
  VisibleTimelineItem,
  OutputPolicyChip,
} from '../types/chat'

type ChatMessageListProps = {
  messagesLoading: boolean
  sending: boolean
  isWaitingForUser: boolean
  visibleMessages: VisibleTimelineItem[]
}

const DISCLOSURE_ANIMATION_MS = 150

function AnimatedDisclosure({
  children,
  className = '',
  title,
}: {
  children: ReactNode
  className?: string
  title: ReactNode
}) {
  const [expanded, setExpanded] = useState(false)
  const [mounted, setMounted] = useState(false)
  const [closing, setClosing] = useState(false)
  const openFrameRef = useRef<number | null>(null)
  const closeTimerRef = useRef<number | null>(null)

  useEffect(() => {
    return () => {
      if (openFrameRef.current !== null) {
        window.cancelAnimationFrame(openFrameRef.current)
      }
      if (closeTimerRef.current !== null) {
        window.clearTimeout(closeTimerRef.current)
      }
    }
  }, [])

  const handleToggle = () => {
    if (expanded) {
      setClosing(true)
      setExpanded(false)
      if (openFrameRef.current !== null) {
        window.cancelAnimationFrame(openFrameRef.current)
        openFrameRef.current = null
      }
      if (closeTimerRef.current !== null) {
        window.clearTimeout(closeTimerRef.current)
      }
      closeTimerRef.current = window.setTimeout(() => {
        closeTimerRef.current = null
        setClosing(false)
        setMounted(false)
      }, DISCLOSURE_ANIMATION_MS)
      return
    }

    setClosing(false)
    setMounted(true)
    if (closeTimerRef.current !== null) {
      window.clearTimeout(closeTimerRef.current)
      closeTimerRef.current = null
    }
    if (openFrameRef.current !== null) {
      window.cancelAnimationFrame(openFrameRef.current)
    }
    openFrameRef.current = window.requestAnimationFrame(() => {
      openFrameRef.current = null
      setExpanded(true)
    })
  }

  return (
    <div
      className={`message-debug-card ${className} ${expanded ? 'is-open' : 'is-closed'} ${
        mounted ? 'is-mounted' : 'is-unmounted'
      } ${closing ? 'is-closing' : ''}`}
    >
      <button
        aria-expanded={expanded}
        className="message-debug-summary"
        type="button"
        onClick={handleToggle}
      >
        <span>{title}</span>
        <span className="message-debug-summary-state">{expanded ? '折叠' : '展开'}</span>
      </button>
      {mounted && (
        <div className="message-debug-body">
          <div className="message-debug-body-inner">{children}</div>
        </div>
      )}
    </div>
  )
}

function draftMessageFromItem(item: Extract<VisibleTimelineItem, { kind: 'draft' }>): MessageItem {
  return {
    id: item.id,
    role: 'draft',
    content: item.content,
    content_parts: item.content_parts,
    created_at: item.created_at,
  }
}

function isDraftItem(item: VisibleTimelineItem): item is VisibleTimelineDraftItem {
  return item.kind === 'draft'
}

function isMessageTimelineItem(item: VisibleTimelineItem): item is TimelineItem & { message: MessageItem } {
  return item.kind === 'message' && !!item.message
}

function eventLevelLabel(level?: string) {
  switch ((level || '').toLowerCase()) {
    case 'warn':
    case 'warning':
      return 'Warning'
    case 'error':
      return 'Error'
    default:
      return 'Info'
  }
}

function stringifyMetadataValue(value: unknown) {
  if (value == null) return ''
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return formatJson(value)
}

function SystemTimelineEvent({
  createdAt,
  event,
  contentParts,
  onImageClick,
}: {
  createdAt: string
  event: ConversationEventItem
  contentParts?: TimelineMessageContentPart[]
  onImageClick: (src: string, detail?: string, alt?: string) => void
}) {
  const metadataEntries = Object.entries(event.metadata ?? {}).filter(([, value]) => value != null && value !== '')
  const detailRows = [
    { label: '类型', value: event.type },
    { label: '级别', value: eventLevelLabel(event.level) },
    { label: '请求 ID', value: event.request_id || '' },
    { label: '时间', value: formatTime(createdAt) },
    { label: '详情', value: event.detail || '' },
  ].filter((row) => row.value)

  return (
    <div className="timeline-event-row">
      <AnimatedDisclosure
        className={`timeline-event-chip level-${event.level || 'info'}`}
        title={
          <span className="timeline-event-chip-title">
            <WarningOutlined />
            <span>{event.title}</span>
            <span className="timeline-event-chip-time">{formatTime(createdAt)}</span>
          </span>
        }
      >
        {contentParts?.length ? (
          <div className="message-content timeline-event-content">
            {renderMessageContent('', { onImageClick }, contentParts)}
          </div>
        ) : null}
        <div className="timeline-event-detail-grid">
          {detailRows.map((row) => (
            <div className="timeline-event-detail-row" key={row.label}>
              <span className="message-debug-label">{row.label}</span>
              <span className="message-debug-value">{row.value}</span>
            </div>
          ))}
        </div>
        {metadataEntries.length > 0 ? (
          <div className="timeline-event-metadata">
            <div className="message-debug-label">附加信息</div>
            <div className="timeline-event-detail-grid">
              {metadataEntries.map(([key, value]) => (
                <div className="timeline-event-detail-row" key={key}>
                  <span className="message-debug-label">{key}</span>
                  <span className="message-debug-value">{stringifyMetadataValue(value)}</span>
                </div>
              ))}
            </div>
          </div>
        ) : null}
      </AnimatedDisclosure>
    </div>
  )
}

function outputPolicyChipTitle(chip: OutputPolicyChip) {
  return `${chip.key} · ${chip.support_level}`
}

export function ChatMessageList({
  isWaitingForUser,
  messagesLoading,
  sending,
  visibleMessages,
}: ChatMessageListProps) {
  const { message: antMessage } = App.useApp()
  const [previewImage, setPreviewImage] = useState<null | {
    alt: string
    detail?: string
    src: string
  }>(null)

  useEffect(() => {
    if (!previewImage) return

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setPreviewImage(null)
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    document.body.classList.add('image-preview-open')

    return () => {
      window.removeEventListener('keydown', handleKeyDown)
      document.body.classList.remove('image-preview-open')
    }
  }, [previewImage])

  if (messagesLoading && visibleMessages.length === 0) {
    return (
      <div className="empty-stage">
        <Spin size="large" />
      </div>
    )
  }

  if (visibleMessages.length === 0) {
    return (
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
    )
  }

  return (
    <>
      {visibleMessages.map((item) => {
        if (item.kind === 'system_event' && item.event) {
          return (
            <SystemTimelineEvent
              key={item.id}
              createdAt={item.created_at}
              event={item.event}
              contentParts={item.content_parts}
              onImageClick={(src, detail, alt) =>
                setPreviewImage({ src, detail, alt: alt ?? 'generated image' })
              }
            />
          )
        }

        const message = isDraftItem(item)
          ? draftMessageFromItem(item)
          : isMessageTimelineItem(item)
            ? item.message
            : null
        if (!message) {
          return null
        }

        const isUser = message.role === 'user'
        const isToolInput = message.role === 'tool'
        const isDraft = message.role === 'draft'
        const isToolCall = message.metadata?.response_mode === 'tool_call'
        const isToolResult = message.metadata?.response_mode === 'tool_result'
        const requestDebug = message.metadata?.request_debug
        const optionChips = requestDebug?.option_chips ?? []
        const outputPolicy = message.metadata?.output_policy
        const outputPolicyChips = outputPolicy?.applied_chips ?? []
        const userRenderableContent = message.content
        const debugSections = [
          {
            label: '请求格式',
            value: requestDebug?.request_format || message.metadata?.request_format || '',
          },
          { label: 'api-key', value: requestDebug?.api_key_name || '-' },
          { label: '模型', value: requestDebug?.model || message.metadata?.model || '' },
          { label: '请求 ID', value: requestDebug?.request_id || '' },
          { label: '响应 ID', value: requestDebug?.response_id || message.response_id || '' },
          { label: '请求 Keys', value: requestDebug?.request_keys?.join(', ') || '' },
          { label: 'User-Agent', value: requestDebug?.request_headers?.user_agent || '' },
          { label: 'Content-Type', value: requestDebug?.request_headers?.content_type || '' },
        ].filter((section) => section.value)
        const hasDebugCard =
          isUser &&
          !isDraft &&
          !!(
            debugSections.length ||
            optionChips.length ||
            requestDebug?.tool_schemas?.length ||
            requestDebug?.builtin_tools?.length ||
            requestDebug?.request_body != null ||
            requestDebug?.request_options != null ||
            requestDebug?.raw_request_body != null
          )

        return (
          <div
            key={message.id}
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
              } ${hasDebugCard ? 'has-debug' : ''} ${isDraft ? 'draft' : ''}`}
            >
              {isToolCall && <div className="message-kind-badge">Tool Call</div>}
              {isToolResult && <div className="message-kind-badge tool-result">Tool Result</div>}
              <div className="message-content">
                {renderMessageContent(userRenderableContent, {
                  onImageClick: (src, detail, alt) => {
                    setPreviewImage({
                      alt: alt ?? 'message image',
                      detail,
                      src,
                    })
                  },
                }, message.content_parts)}
              </div>
              {!isUser && !isToolInput && outputPolicyChips.length > 0 ? (
                <div className="message-option-chip-row message-output-policy-chip-row">
                  {outputPolicyChips.map((chip, index) => (
                    <span
                      key={`${chip.key}-${index}`}
                      className={`message-option-chip output-policy ${chip.support_level}`}
                      title={outputPolicyChipTitle(chip)}
                    >
                      <span>{chip.label}</span>
                      {chip.value ? <span>{chip.value}</span> : null}
                    </span>
                  ))}
                </div>
              ) : null}
              {(isToolCall || isToolResult) && (
                <div className="message-tool-meta">
                  <div>
                    <span className="message-debug-label">Tool</span>
                    <span className="message-debug-value">{message.metadata?.tool_name || '-'}</span>
                  </div>
                  <div>
                    <span className="message-debug-label">Call ID</span>
                    <span className="message-debug-value">{message.metadata?.tool_call_id || '-'}</span>
                  </div>
                </div>
              )}
              {hasDebugCard && (
                <AnimatedDisclosure title="请求详情">
                  {optionChips.length > 0 ? (
                    <div className="message-option-chip-row">
                      {optionChips.map((chip, index) => (
                        <span
                          key={`${chip.key}-${index}`}
                          className={`message-option-chip ${chip.category} ${chip.support_level}`}
                          title={`${chip.key} · ${chip.support_level}`}
                        >
                          <span>{chip.label}</span>
                          {chip.value ? <span>{chip.value}</span> : null}
                        </span>
                      ))}
                    </div>
                  ) : null}
                  {debugSections.map((section) => (
                    <div key={section.label} className="message-debug-row">
                      <span className="message-debug-label">{section.label}</span>
                      <span className="message-debug-value">{section.value}</span>
                    </div>
                  ))}
                  {(requestDebug?.tool_schemas?.length ||
                    requestDebug?.builtin_tools?.length ||
                    requestDebug?.request_body != null ||
                    requestDebug?.request_options != null ||
                    requestDebug?.raw_request_body != null) && (
                    <AnimatedDisclosure className="message-debug-subcard" title="Debug信息">
                      {requestDebug?.request_options != null ? (
                        <div className="message-debug-block">
                          <div className="message-debug-label">Request Options</div>
                          <pre>{formatJson(requestDebug.request_options)}</pre>
                        </div>
                      ) : null}
                      {requestDebug?.tool_schemas?.length ? (
                        <div className="message-debug-block">
                          <div className="message-debug-label">Tool Schemas</div>
                          <pre>{formatJson(requestDebug.tool_schemas)}</pre>
                        </div>
                      ) : null}
                      {requestDebug?.builtin_tools?.length ? (
                        <div className="message-debug-block">
                          <div className="message-debug-label">Built-in Tools</div>
                          <pre>{formatJson(requestDebug.builtin_tools)}</pre>
                        </div>
                      ) : null}
                      {requestDebug?.request_body != null ? (
                        <div className="message-debug-block">
                          <div className="message-debug-label-row">
                            <span className="message-debug-label">Request Body（后端规范化重建）</span>
                            <Button
                              size="small"
                              type="link"
                              icon={<CopyOutlined />}
                              className="copy-curl-btn"
                              onClick={() => {
                                const curl = buildCurlCommand(requestDebug.request_body)
                                if (!curl) return
                                if (navigator.clipboard && window.isSecureContext) {
                                  navigator.clipboard.writeText(curl).then(() => {
                                    antMessage.success('已复制 curl')
                                  }).catch(() => {
                                    antMessage.error('复制失败')
                                  })
                                } else {
                                  const textarea = document.createElement('textarea')
                                  textarea.value = curl
                                  textarea.style.position = 'fixed'
                                  textarea.style.opacity = '0'
                                  document.body.appendChild(textarea)
                                  textarea.select()
                                  document.execCommand('copy')
                                  document.body.removeChild(textarea)
                                  antMessage.success('已复制 curl')
                                }
                              }}
                            >
                              复制 curl
                            </Button>
                          </div>
                          <pre>{formatJson(requestDebug.request_body)}</pre>
                        </div>
                      ) : null}
                      {requestDebug?.raw_request_body != null ? (
                        <div className="message-debug-block">
                          <div className="message-debug-label">Raw Request Body（Ingress 解析对象）</div>
                          <pre>{formatJson(requestDebug.raw_request_body)}</pre>
                        </div>
                      ) : null}
                    </AnimatedDisclosure>
                  )}
                </AnimatedDisclosure>
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
                          : message.role}
                </span>
                <span>{formatTime(message.created_at)}</span>
              </div>
            </div>
            {!isUser && !isToolInput && (
              <Avatar className="message-avatar assistant-avatar">AI</Avatar>
            )}
          </div>
        )
      })}
      {sending && (
        <div className="message-row assistant">
          <Avatar className="message-avatar assistant-avatar">AI</Avatar>
          <div className="message-bubble assistant typing">
            <Spin size="small" />
            <span>正在生成回复...</span>
          </div>
        </div>
      )}
      {previewImage && (
        <div
          className="image-preview-overlay"
          role="dialog"
          aria-modal="true"
          aria-label="图片预览"
          onClick={() => setPreviewImage(null)}
        >
          <button
            type="button"
            className="image-preview-close"
            onClick={() => setPreviewImage(null)}
            aria-label="关闭图片预览"
          >
            ×
          </button>
          <figure className="image-preview-frame" onClick={(event) => event.stopPropagation()}>
            <img src={previewImage.src} alt={previewImage.alt} className="image-preview-image" />
            {previewImage.detail ? (
              <figcaption className="image-preview-caption">detail: {previewImage.detail}</figcaption>
            ) : null}
          </figure>
        </div>
      )}
    </>
  )
}
