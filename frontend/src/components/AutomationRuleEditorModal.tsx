import { useEffect, useState } from 'react'
import { Button, Card, Input, InputNumber, Modal, Select, Space, Spin, Typography } from 'antd'

import { buildInitialToolFormValues, getLastToolSchemas } from '../lib/chat-format'
import { requestJson } from '../lib/api'
import { ToolField } from './ToolField'
import type {
  AutomationRule,
  Conversation,
  MessageItem,
  ToolFieldValue,
  ToolSchemaOption,
} from '../types/chat'

type AutomationRuleEditorModalProps = {
  conversations: Conversation[]
  editingAutomationRule: AutomationRule | null
  open: boolean
  saving: boolean
  setEditingAutomationRule: (value: AutomationRule | null) => void
  onCancel: () => void
  onSave: (rule: AutomationRule) => void | Promise<void>
}

export function AutomationRuleEditorModal({
  conversations,
  editingAutomationRule,
  open,
  saving,
  setEditingAutomationRule,
  onCancel,
  onSave,
}: AutomationRuleEditorModalProps) {
  const [toolCallModalOpen, setToolCallModalOpen] = useState(false)
  const [toolCallSchemaConversationId, setToolCallSchemaConversationId] = useState('')
  const [toolCallSchemas, setToolCallSchemas] = useState<ToolSchemaOption[]>([])
  const [toolCallSchemasLoading, setToolCallSchemasLoading] = useState(false)
  const [toolCallToolName, setToolCallToolName] = useState('')
  const [toolCallFormValues, setToolCallFormValues] = useState<Record<string, ToolFieldValue>>({})
  const [toolCallId, setToolCallId] = useState('')

  useEffect(() => {
    if (!toolCallModalOpen || !toolCallSchemaConversationId) {
      setToolCallSchemas([])
      setToolCallToolName('')
      setToolCallFormValues({})
      return
    }
    let cancelled = false
    setToolCallSchemasLoading(true)
    requestJson<{ ok: boolean; items?: MessageItem[] }>(
      `/api/conversations/${toolCallSchemaConversationId}/messages`,
    )
      .then((data) => {
        if (cancelled) return
        const messages = Array.isArray(data.items) ? data.items : []
        setToolCallSchemas(getLastToolSchemas(messages))
      })
      .catch(() => {
        if (cancelled) return
        setToolCallSchemas([])
      })
      .finally(() => {
        if (!cancelled) setToolCallSchemasLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [toolCallModalOpen, toolCallSchemaConversationId])

  function openToolCallModal() {
    if (!editingAutomationRule) return
    const action = editingAutomationRule.action
    setToolCallSchemaConversationId('')
    setToolCallSchemas([])
    setToolCallToolName(action.tool_name ?? '')
    setToolCallFormValues(
      action.tool_arguments
        ? (() => {
            try {
              return JSON.parse(action.tool_arguments) as Record<string, ToolFieldValue>
            } catch {
              return {}
            }
          })()
        : {},
    )
    setToolCallId(action.tool_call_id ?? '')
    setToolCallModalOpen(true)
  }

  function handleToolCallModalOk() {
    if (!editingAutomationRule || !toolCallToolName) return
    let argumentsJson = '{}'
    try {
      const selectedSchema = toolCallSchemas.find((s) => s.name === toolCallToolName)
      if (selectedSchema) {
        const properties = selectedSchema.parameters?.properties ?? {}
        const required = new Set(selectedSchema.parameters?.required ?? [])
        const entries = Object.entries(properties).flatMap(([key]) => {
          const rawValue = toolCallFormValues[key]
          if (rawValue == null || rawValue === '') {
            if (required.has(key)) return []
            return []
          }
          return [[key, rawValue] as const]
        })
        argumentsJson = JSON.stringify(Object.fromEntries(entries))
      }
    } catch {
      // keep default
    }
    setEditingAutomationRule({
      ...editingAutomationRule,
      action: {
        ...editingAutomationRule.action,
        type: 'tool_call',
        tool_name: toolCallToolName,
        tool_arguments: argumentsJson,
        tool_call_id: toolCallId,
      },
    })
    setToolCallModalOpen(false)
  }

  function validateRegex(pattern: string): boolean {
    if (!pattern) return true
    try {
      // eslint-disable-next-line no-new
      new RegExp(pattern)
      return true
    } catch {
      return false
    }
  }

  return (
    <>
      <Modal
        title={editingAutomationRule ? `编辑规则 ${editingAutomationRule.id}` : '编辑规则'}
        width={980}
        open={open}
        onCancel={() => {
          if (saving) return
          onCancel()
        }}
        onOk={() => {
          if (!editingAutomationRule) return
          void onSave(editingAutomationRule)
        }}
        okText="保存规则"
        okButtonProps={{ loading: saving }}
        cancelButtonProps={{ disabled: saving }}
        destroyOnHidden
      >
        <Space direction="vertical" size={18} className="automation-editor-stack">
          <div className="automation-editor-section">
            <Typography.Title level={5} className="automation-editor-title">
              条件
              <Typography.Text
                type="secondary"
                style={{ fontSize: 12, fontWeight: 'normal', marginLeft: 8 }}
              >
                示例：weather|forecast、^帮我.*查天气$、(北京|上海).+天气
              </Typography.Text>
            </Typography.Title>
            <div>
              <Typography.Text className="prune-input-label">正则表达式</Typography.Text>
              <Input
                value={editingAutomationRule?.conditions.contains?.[0]?.pattern ?? ''}
                onChange={(event) => {
                  if (!editingAutomationRule) return
                  const pattern = event.target.value
                  const currentConditions = editingAutomationRule.conditions.contains ?? []
                  const firstCondition = currentConditions[0]
                  if (firstCondition) {
                    setEditingAutomationRule({
                      ...editingAutomationRule,
                      conditions: {
                        ...editingAutomationRule.conditions,
                        contains: [{ match_type: 'regex', pattern }, ...currentConditions.slice(1)],
                      },
                    })
                  } else {
                    setEditingAutomationRule({
                      ...editingAutomationRule,
                      conditions: {
                        ...editingAutomationRule.conditions,
                        contains: [{ match_type: 'regex', pattern }],
                      },
                    })
                  }
                }}
                placeholder="输入正则表达式，匹配的消息将触发规则"
                status={
                  editingAutomationRule?.conditions.contains?.[0]?.pattern &&
                  !validateRegex(editingAutomationRule.conditions.contains[0].pattern)
                    ? 'error'
                    : undefined
                }
                style={
                  editingAutomationRule?.conditions.contains?.[0]?.pattern
                    ? {
                        borderColor: validateRegex(editingAutomationRule.conditions.contains[0].pattern)
                          ? '#52c41a'
                          : '#ff4d4f',
                      }
                    : undefined
                }
              />
              {editingAutomationRule?.conditions.contains?.[0]?.pattern &&
                !validateRegex(editingAutomationRule.conditions.contains[0].pattern) && (
                  <Typography.Text type="danger" style={{ fontSize: 12 }}>
                    正则语法错误
                  </Typography.Text>
                )}
            </div>
          </div>
          <div className="automation-editor-section">
            <Typography.Title level={5} className="automation-editor-title">
              动作
            </Typography.Title>
            <div className="automation-action-type-group">
              <Button
                type={editingAutomationRule?.action.type === 'output_text' ? 'primary' : 'default'}
                onClick={() => {
                  if (!editingAutomationRule) return
                  setEditingAutomationRule({
                    ...editingAutomationRule,
                    action: {
                      ...editingAutomationRule.action,
                      type: 'output_text',
                    },
                  })
                }}
              >
                流式输出
              </Button>
              <Button
                type={editingAutomationRule?.action.type === 'complete' ? 'primary' : 'default'}
                onClick={() => {
                  if (!editingAutomationRule) return
                  setEditingAutomationRule({
                    ...editingAutomationRule,
                    action: {
                      ...editingAutomationRule.action,
                      type: 'complete',
                    },
                  })
                }}
              >
                结束输出
              </Button>
              <Button
                danger={editingAutomationRule?.action.type === 'error'}
                type={editingAutomationRule?.action.type === 'error' ? 'primary' : 'default'}
                onClick={() => {
                  if (!editingAutomationRule) return
                  setEditingAutomationRule({
                    ...editingAutomationRule,
                    action: {
                      ...editingAutomationRule.action,
                      type: 'error',
                    },
                  })
                }}
              >
                返回 error
              </Button>
              <Button
                type={editingAutomationRule?.action.type === 'tool_call' ? 'primary' : 'default'}
                onClick={() => {
                  if (!editingAutomationRule) return
                  setEditingAutomationRule({
                    ...editingAutomationRule,
                    action: {
                      ...editingAutomationRule.action,
                      type: 'tool_call',
                    },
                  })
                  openToolCallModal()
                }}
              >
                工具调用
              </Button>
            </div>
            <div className="automation-editor-grid" style={{ marginTop: 12 }}>
              <div className="automation-editor-inline-field">
                <Typography.Text className="prune-input-label">收到后延时（秒）</Typography.Text>
                <InputNumber
                  min={0}
                  value={editingAutomationRule?.timing.delay_seconds ?? 0}
                  onChange={(value) => {
                    if (!editingAutomationRule) return
                    setEditingAutomationRule({
                      ...editingAutomationRule,
                      timing: {
                        ...editingAutomationRule.timing,
                        delay_seconds: typeof value === 'number' ? value : 0,
                      },
                    })
                  }}
                  className="prune-input"
                />
              </div>
              {editingAutomationRule?.action.type === 'output_text' && (
                <div className="automation-editor-inline-field">
                  <Typography.Text className="prune-input-label">每隔时间重复（秒）</Typography.Text>
                  <InputNumber
                    min={0}
                    value={editingAutomationRule?.timing.repeat_interval_seconds ?? 0}
                    onChange={(value) => {
                      if (!editingAutomationRule) return
                      setEditingAutomationRule({
                        ...editingAutomationRule,
                        timing: {
                          ...editingAutomationRule.timing,
                          repeat_interval_seconds: typeof value === 'number' ? value : 0,
                        },
                      })
                    }}
                    className="prune-input"
                  />
                </div>
              )}
              {editingAutomationRule?.action.type === 'output_text' && (
                <div className="automation-editor-inline-field">
                  <Typography.Text className="prune-input-label">最多输出次数</Typography.Text>
                  <InputNumber
                    min={1}
                    precision={0}
                    value={editingAutomationRule?.timing.max_output_count ?? 120}
                    onChange={(value) => {
                      if (!editingAutomationRule) return
                      setEditingAutomationRule({
                        ...editingAutomationRule,
                        timing: {
                          ...editingAutomationRule.timing,
                          max_output_count: Math.max(1, Number(value ?? 120)),
                        },
                      })
                    }}
                    className="prune-input"
                  />
                </div>
              )}
            </div>
            {editingAutomationRule?.action.type === 'output_text' ? (
              <div className="automation-editor-action-field">
                <Typography.Text className="prune-input-label">输出文本</Typography.Text>
                <Input.TextArea
                  value={editingAutomationRule.action.text}
                  onChange={(event) => {
                    if (!editingAutomationRule) return
                    setEditingAutomationRule({
                      ...editingAutomationRule,
                      action: {
                        ...editingAutomationRule.action,
                        text: event.target.value,
                      },
                    })
                  }}
                  autoSize={{ minRows: 5, maxRows: 12 }}
                  placeholder="命中规则后输出的文本"
                />
              </div>
            ) : null}
            {editingAutomationRule?.action.type === 'complete' ? (
              <div className="automation-editor-action-field">
                <Typography.Text className="prune-input-label">结束时补充文本</Typography.Text>
                <Input.TextArea
                  value={editingAutomationRule.action.text}
                  onChange={(event) => {
                    if (!editingAutomationRule) return
                    setEditingAutomationRule({
                      ...editingAutomationRule,
                      action: {
                        ...editingAutomationRule.action,
                        text: event.target.value,
                      },
                    })
                  }}
                  autoSize={{ minRows: 4, maxRows: 10 }}
                  placeholder="可留空；留空时直接以当前草稿结束"
                />
              </div>
            ) : null}
            {editingAutomationRule?.action.type === 'error' ? (
              <div className="automation-editor-action-field">
                <Typography.Text className="prune-input-label">错误信息</Typography.Text>
                <Input.TextArea
                  value={editingAutomationRule.action.error_message}
                  onChange={(event) => {
                    if (!editingAutomationRule) return
                    setEditingAutomationRule({
                      ...editingAutomationRule,
                      action: {
                        ...editingAutomationRule.action,
                        error_message: event.target.value,
                      },
                    })
                  }}
                  autoSize={{ minRows: 4, maxRows: 10 }}
                  placeholder="命中规则后直接返回给请求方的错误信息"
                />
              </div>
            ) : null}
            {editingAutomationRule?.action.type === 'tool_call' ? (
              <div className="automation-editor-action-field">
                <Typography.Text className="prune-input-label">工具调用配置</Typography.Text>
                <Card
                  size="small"
                  style={{ marginTop: 8 }}
                  extra={
                    <Button size="small" onClick={openToolCallModal}>
                      编辑
                    </Button>
                  }
                >
                  <Typography.Text>
                    {editingAutomationRule.action.tool_name
                      ? `Tool: ${editingAutomationRule.action.tool_name}`
                      : '未配置工具调用'}
                  </Typography.Text>
                </Card>
              </div>
            ) : null}
          </div>
        </Space>
      </Modal>
      <Modal
        title="编辑工具调用"
        width={680}
        open={toolCallModalOpen}
        onCancel={() => setToolCallModalOpen(false)}
        onOk={handleToolCallModalOk}
        okText="确认"
        destroyOnHidden
      >
        <Space direction="vertical" size={16} style={{ width: '100%' }}>
          <div>
            <Typography.Text className="prune-input-label">选择历史会话（获取 Tool Schema）</Typography.Text>
            <Select
              value={toolCallSchemaConversationId || undefined}
              onChange={(value) => setToolCallSchemaConversationId(value)}
              placeholder="选择一个会话以加载其 tool schema"
              style={{ width: '100%' }}
              showSearch
              optionFilterProp="label"
              options={conversations.map((conversation) => ({
                label: conversation.title || conversation.id,
                value: conversation.id,
              }))}
            />
          </div>
          {toolCallSchemasLoading && <Spin size="small" />}
          {!toolCallSchemasLoading && toolCallSchemaConversationId && toolCallSchemas.length === 0 && (
            <Typography.Text type="secondary">该会话中没有可用的 tool schema</Typography.Text>
          )}
          {toolCallSchemas.length > 0 && (
            <>
              <div>
                <Typography.Text className="prune-input-label">选择 Tool</Typography.Text>
                <Select
                  value={toolCallToolName || undefined}
                  onChange={(value) => {
                    setToolCallToolName(value)
                    const schema = toolCallSchemas.find((item) => item.name === value)
                    setToolCallFormValues(
                      schema ? buildInitialToolFormValues(schema.parameters) : {},
                    )
                  }}
                  placeholder="选择一个 tool"
                  style={{ width: '100%' }}
                  options={toolCallSchemas.map((schema) => ({
                    label: schema.name,
                    value: schema.name,
                  }))}
                />
              </div>
              <div>
                <Typography.Text className="prune-input-label">Tool Call ID（可留空自动生成）</Typography.Text>
                <Input
                  value={toolCallId}
                  onChange={(event) => setToolCallId(event.target.value)}
                  placeholder="tool call id"
                />
              </div>
              {(() => {
                const selectedSchema = toolCallSchemas.find((item) => item.name === toolCallToolName)
                if (!selectedSchema) return null
                const properties = selectedSchema.parameters?.properties ?? {}
                const requiredFields = selectedSchema.parameters?.required ?? []
                const entries = Object.entries(properties)
                return (
                  <Card size="small" title={selectedSchema.name}>
                    {selectedSchema.description && (
                      <Typography.Text type="secondary" style={{ display: 'block', marginBottom: 12 }}>
                        {selectedSchema.description}
                      </Typography.Text>
                    )}
                    {entries.length > 0 ? (
                      <div className="tool-form-grid">
                        {entries.map(([fieldName, schema]) => (
                          <ToolField
                            key={fieldName}
                            disabled={false}
                            fieldName={fieldName}
                            onChange={(nextField, nextValue) =>
                              setToolCallFormValues((prev) => ({
                                ...prev,
                                [nextField]: nextValue,
                              }))
                            }
                            required={requiredFields.includes(fieldName)}
                            schema={schema}
                            value={toolCallFormValues[fieldName]}
                          />
                        ))}
                      </div>
                    ) : (
                      <Typography.Text type="secondary">该 tool 没有参数</Typography.Text>
                    )}
                  </Card>
                )
              })()}
            </>
          )}
        </Space>
      </Modal>
    </>
  )
}
