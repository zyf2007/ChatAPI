import {
  Avatar,
  Badge,
  Button,
  Empty,
  Input,
  InputNumber,
  List,
  Modal,
  Popover,
  Space,
  Select,
  Tooltip,
  Typography,
} from 'antd'
import {
  DeleteOutlined,
  LeftOutlined,
  LogoutOutlined,
  RightOutlined,
  SettingOutlined,
  StopOutlined,
} from '@ant-design/icons'

import { formatTime } from '../lib/chat-format'
import { SettingsModal } from './settings/SettingsModal'
import type {
  AuthSession,
  AutomationRule,
  AutomationRuleCondition,
  Conversation,
} from '../types/chat'

type ConversationSidebarProps = {
  abortPopoverConversationId: string
  abortReason: string
  abortingConversationId: string
  auth: AuthSession
  automationRuleEditorOpen: boolean
  automationRules: AutomationRule[]
  automationRulesModalOpen: boolean
  collapsed: boolean
  conversations: Conversation[]
  deletingConversationId: string
  editingAutomationRule: AutomationRule | null
  onAbortConversation: (conversationId: string) => void | Promise<void>
  onCreateAutomationRule: () => void | Promise<void>
  onDeleteAutomationRule: (ruleId: string) => void | Promise<void>
  onDeleteConversation: (conversationId: string) => void | Promise<void>
  onEditAutomationRule: (ruleId: string) => void | Promise<void>
  onLogout: () => void | Promise<void>
  onPruneConversations: () => void | Promise<void>
  onSaveAutomationRule: (rule: AutomationRule) => void | Promise<void>
  onSelectConversation: (conversationId: string) => void | Promise<void>
  onToggleAutomationRule: (ruleId: string, enabled: boolean) => void | Promise<void>
  onToggleCollapsed: () => void
  pruneKeepCount: number
  pruneModalOpen: boolean
  pruningConversations: boolean
  savingAutomationRules: boolean
  selectedConversationId: string
  setAbortPopoverConversationId: (value: string) => void
  setAbortReason: (value: string) => void
  setAutomationRuleEditorOpen: (value: boolean) => void
  setAutomationRulesModalOpen: (value: boolean) => void
  setEditingAutomationRule: (value: AutomationRule | null) => void
  setPruneKeepCount: (value: number) => void
  setPruneModalOpen: (value: boolean) => void
}

export function ConversationSidebar({
  abortPopoverConversationId,
  abortReason,
  abortingConversationId,
  auth,
  automationRuleEditorOpen,
  automationRules,
  automationRulesModalOpen,
  collapsed,
  conversations,
  deletingConversationId,
  editingAutomationRule,
  onAbortConversation,
  onCreateAutomationRule,
  onDeleteAutomationRule,
  onDeleteConversation,
  onEditAutomationRule,
  onLogout,
  onPruneConversations,
  onSaveAutomationRule,
  onSelectConversation,
  onToggleAutomationRule,
  onToggleCollapsed,
  pruneKeepCount,
  pruneModalOpen,
  pruningConversations,
  savingAutomationRules,
  selectedConversationId,
  setAbortPopoverConversationId,
  setAbortReason,
  setAutomationRuleEditorOpen,
  setAutomationRulesModalOpen,
  setEditingAutomationRule,
  setPruneKeepCount,
  setPruneModalOpen,
}: ConversationSidebarProps) {
  function updateConditionList(
    group: 'contains' | 'excludes',
    updater: (items: AutomationRuleCondition[]) => AutomationRuleCondition[],
  ) {
    if (!editingAutomationRule) return
    setEditingAutomationRule({
      ...editingAutomationRule,
      conditions: {
        ...editingAutomationRule.conditions,
        [group]: updater(editingAutomationRule.conditions[group]),
      },
    })
  }

  return (
    <div className={`sidebar-inner ${collapsed ? 'collapsed' : ''}`}>
      <div className="sidebar-top">
        <div className="sidebar-top-copy">
          <Typography.Text className="eyebrow">ChatAPI</Typography.Text>
          {!collapsed ? (
            <Typography.Title level={4} className="sidebar-title">
              会话
            </Typography.Title>
          ) : null}
        </div>
        <Space size={4}>
          <Tooltip title={collapsed ? '展开侧边栏' : '收起侧边栏'}>
            <Button
              type="text"
              size="small"
              icon={collapsed ? <RightOutlined /> : <LeftOutlined />}
              className="sidebar-action-button"
              onClick={onToggleCollapsed}
            />
          </Tooltip>
          {!collapsed ? (
            <Tooltip title="删除最近 N 个会话以外的会话">
              <Button
                type="text"
                danger
                size="small"
                icon={<DeleteOutlined />}
                className="sidebar-action-button"
                onClick={() => setPruneModalOpen(true)}
              />
            </Tooltip>
          ) : null}
        </Space>
      </div>
      <List
        className="conversation-list"
        dataSource={conversations}
        locale={{ emptyText: <Empty description="暂无会话" /> }}
        renderItem={(item) => {
          const active = item.id === selectedConversationId
          const realtimeStatus = item.metadata?.realtime_status
          const isWaiting = realtimeStatus === 'waiting'
          const statusColor =
            realtimeStatus === 'waiting'
              ? '#22c55e'
              : realtimeStatus === 'closed' || realtimeStatus === 'aborted'
                ? '#ef4444'
                : ''

          return (
            <List.Item
              className={`conversation-item ${active ? 'active' : ''}`}
              onClick={() => void onSelectConversation(item.id)}
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
                  {!collapsed ? (
                    <div className="conversation-meta">
                      <Typography.Text className="conversation-title">
                        {item.title || '新会话'}
                      </Typography.Text>
                      <Typography.Paragraph
                        className="conversation-preview"
                        ellipsis={{ rows: 2 }}
                      >
                        {item.last_message_preview || item.last_user_text || '尚无消息'}
                      </Typography.Paragraph>
                      <Typography.Text className="conversation-time">
                        {item.message_count > 0
                          ? `${item.message_count} 条消息 · ${formatTime(item.last_message_at)}`
                          : '空会话'}
                      </Typography.Text>
                    </div>
                  ) : null}
                </Space>
                {isWaiting ? (
                  <Popover
                    trigger="click"
                    open={abortPopoverConversationId === item.id}
                    onOpenChange={(open) => {
                      if (!open) {
                        setAbortPopoverConversationId('')
                        setAbortReason('')
                        return
                      }
                      setAbortPopoverConversationId(item.id)
                      setAbortReason('')
                    }}
                    placement="leftTop"
                    content={
                      <div
                        className="abort-popover"
                        onClick={(event) => event.stopPropagation()}
                      >
                        <Input
                          value={abortReason}
                          onChange={(event) => setAbortReason(event.target.value)}
                          placeholder="输入返回给请求方的错误信息"
                          onPressEnter={() => void onAbortConversation(item.id)}
                        />
                        <Button
                          danger
                          type="primary"
                          loading={abortingConversationId === item.id}
                          onClick={() => void onAbortConversation(item.id)}
                        >
                          abort
                        </Button>
                      </div>
                    }
                  >
                    <Button
                      type="text"
                      danger
                      size="small"
                      icon={<StopOutlined />}
                      className="conversation-delete-button"
                      loading={abortingConversationId === item.id}
                      onClick={(event) => {
                        event.stopPropagation()
                      }}
                    />
                  </Popover>
                ) : (
                  <Tooltip title="删除会话">
                    <Button
                      type="text"
                      danger
                      size="small"
                      icon={<DeleteOutlined />}
                      className="conversation-delete-button"
                      loading={deletingConversationId === item.id}
                      onClick={(event) => {
                        event.stopPropagation()
                        void onDeleteConversation(item.id)
                      }}
                    />
                  </Tooltip>
                )}
              </div>
            </List.Item>
          )
        }}
      />
      <div className="sidebar-footer">
        {!collapsed ? (
          <>
            <div className="footer-head">
              <Typography.Text className="footer-name">{auth.user?.username}</Typography.Text>
              <Tooltip title="自动化规则">
                <Button
                  type="text"
                  icon={<SettingOutlined />}
                  className="footer-settings-button"
                  onClick={() => setAutomationRulesModalOpen(true)}
                />
              </Tooltip>
            </div>
            <Button icon={<LogoutOutlined />} onClick={() => void onLogout()} block>
              退出登录
            </Button>
          </>
        ) : (
          <div className="sidebar-footer-collapsed">
            <Tooltip title="自动化规则">
              <Button
                type="text"
                icon={<SettingOutlined />}
                className="footer-settings-button"
                onClick={() => setAutomationRulesModalOpen(true)}
              />
            </Tooltip>
            <Tooltip title="退出登录">
              <Button type="text" icon={<LogoutOutlined />} onClick={() => void onLogout()} />
            </Tooltip>
          </div>
        )}
      </div>
      <Modal
        title="批量删除旧会话"
        open={pruneModalOpen}
        onCancel={() => {
          if (pruningConversations) return
          setPruneModalOpen(false)
        }}
        onOk={() => void onPruneConversations()}
        okText="删除"
        okButtonProps={{ danger: true, loading: pruningConversations }}
        cancelButtonProps={{ disabled: pruningConversations }}
        destroyOnHidden
      >
        <Space direction="vertical" size={12} className="prune-modal-stack">
          <Typography.Text>
            保留最近 n 个会话，其余更早的会话将被删除。等待中的会话会自动跳过。
          </Typography.Text>
          <div>
            <Typography.Text className="prune-input-label">保留数量</Typography.Text>
            <InputNumber
              min={0}
              precision={0}
              value={pruneKeepCount}
              onChange={(value) => setPruneKeepCount(typeof value === 'number' ? value : 0)}
              className="prune-input"
              placeholder="输入 n"
            />
          </div>
        </Space>
      </Modal>
      <SettingsModal
        automationRuleEditorOpen={automationRuleEditorOpen}
        automationRules={automationRules}
        onCreateAutomationRule={onCreateAutomationRule}
        onDeleteAutomationRule={onDeleteAutomationRule}
        onEditAutomationRule={onEditAutomationRule}
        onToggleAutomationRule={onToggleAutomationRule}
        open={automationRulesModalOpen}
        onClose={() => setAutomationRulesModalOpen(false)}
        savingAutomationRules={savingAutomationRules}
      />
      <Modal
        title={editingAutomationRule ? `编辑规则 ${editingAutomationRule.id}` : '编辑规则'}
        width={980}
        open={automationRuleEditorOpen}
        onCancel={() => {
          if (savingAutomationRules) return
          setAutomationRuleEditorOpen(false)
          setEditingAutomationRule(null)
        }}
        onOk={() => {
          if (!editingAutomationRule) return
          void onSaveAutomationRule(editingAutomationRule)
        }}
        okText="保存规则"
        okButtonProps={{ loading: savingAutomationRules }}
        cancelButtonProps={{ disabled: savingAutomationRules }}
        destroyOnHidden
      >
        <Space direction="vertical" size={18} className="automation-editor-stack">
          <div className="automation-editor-section">
            <Typography.Title level={5} className="automation-editor-title">
              条件
            </Typography.Title>
            <div className="automation-editor-grid">
              <div>
                <Typography.Text className="prune-input-label">请求中包含字符串</Typography.Text>
                <div className="automation-condition-list">
                  {(editingAutomationRule?.conditions.contains ?? []).map((item, index) => (
                    <div className="automation-condition-row" key={`contains-${index}`}>
                      <Select
                        value={item.match_type}
                        options={[
                          { value: 'substring', label: '普通文本' },
                          { value: 'regex', label: '正则' },
                        ]}
                        onChange={(value) =>
                          updateConditionList('contains', (items) =>
                            items.map((entry, entryIndex) =>
                              entryIndex === index
                                ? {
                                    ...entry,
                                    match_type: value as AutomationRuleCondition['match_type'],
                                  }
                                : entry,
                            ),
                          )
                        }
                        className="automation-condition-type"
                      />
                      <Input
                        value={item.pattern}
                        onChange={(event) =>
                          updateConditionList('contains', (items) =>
                            items.map((entry, entryIndex) =>
                              entryIndex === index
                                ? {
                                    ...entry,
                                    pattern: event.target.value,
                                  }
                                : entry,
                            ),
                          )
                        }
                        placeholder={
                          item.match_type === 'regex'
                            ? '示例：weather|forecast 或 ^帮我.*查天气$'
                            : '示例：天气 或 帮我查一下'
                        }
                      />
                      <Button
                        danger
                        onClick={() =>
                          updateConditionList('contains', (items) =>
                            items.filter((_, entryIndex) => entryIndex !== index),
                          )
                        }
                      >
                        删除
                      </Button>
                    </div>
                  ))}
                  <Button
                    onClick={() =>
                      updateConditionList('contains', (items) => [
                        ...items,
                        { match_type: 'substring', pattern: '' },
                      ])
                    }
                  >
                    添加包含条件
                  </Button>
                </div>
              </div>
              <div>
                <Typography.Text className="prune-input-label">请求中不包含字符串</Typography.Text>
                <div className="automation-condition-list">
                  {(editingAutomationRule?.conditions.excludes ?? []).map((item, index) => (
                    <div className="automation-condition-row" key={`excludes-${index}`}>
                      <Select
                        value={item.match_type}
                        options={[
                          { value: 'substring', label: '普通文本' },
                          { value: 'regex', label: '正则' },
                        ]}
                        onChange={(value) =>
                          updateConditionList('excludes', (items) =>
                            items.map((entry, entryIndex) =>
                              entryIndex === index
                                ? {
                                    ...entry,
                                    match_type: value as AutomationRuleCondition['match_type'],
                                  }
                                : entry,
                            ),
                          )
                        }
                        className="automation-condition-type"
                      />
                      <Input
                        value={item.pattern}
                        onChange={(event) =>
                          updateConditionList('excludes', (items) =>
                            items.map((entry, entryIndex) =>
                              entryIndex === index
                                ? {
                                    ...entry,
                                    pattern: event.target.value,
                                  }
                                : entry,
                            ),
                          )
                        }
                        placeholder={
                          item.match_type === 'regex'
                            ? '示例：上海|北京 或 ^debug:'
                            : '示例：上海 或 不要联网'
                        }
                      />
                      <Button
                        danger
                        onClick={() =>
                          updateConditionList('excludes', (items) =>
                            items.filter((_, entryIndex) => entryIndex !== index),
                          )
                        }
                      >
                        删除
                      </Button>
                    </div>
                  ))}
                  <Button
                    onClick={() =>
                      updateConditionList('excludes', (items) => [
                        ...items,
                        { match_type: 'substring', pattern: '' },
                      ])
                    }
                  >
                    添加排除条件
                  </Button>
                </div>
              </div>
            </div>
          </div>
          <div className="automation-editor-section">
            <Typography.Title level={5} className="automation-editor-title">
              时间
            </Typography.Title>
            <div className="automation-editor-grid">
              <div>
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
              <div>
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
                输出指定文本
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
          </div>
        </Space>
      </Modal>
    </div>
  )
}
