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
import { AutomationRuleEditorModal } from './AutomationRuleEditorModal'
import { SettingsModal } from './settings/SettingsModal'
import type {
  AuthSession,
  AutomationRule,
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
  onTotpRefresh: () => void
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
  totpEnabled: boolean
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
  onTotpRefresh,
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
  totpEnabled,
}: ConversationSidebarProps) {
  return (
    <div className={`sidebar-inner ${collapsed ? 'collapsed' : ''}`}>
      <div className="sidebar-top">
        <div className="sidebar-top-copy">
          <a
            className="eyebrow sidebar-brand-link"
            href="https://github.com/zyf2007/ChatAPI"
            target="_blank"
            rel="noreferrer noopener"
            aria-label="打开 ChatAPI GitHub 仓库"
            title="打开 ChatAPI GitHub 仓库"
          >
            ChatAPI
          </a>
          {!collapsed ? (
            <Typography.Title level={4} className="sidebar-title">
              会话
            </Typography.Title>
          ) : null}
        </div>
        <div className="sidebar-top-actions">
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
          {!collapsed ? (
            <Typography.Text className="sidebar-connection-count" type="secondary">
              当前连接数 {auth.current_connection_count}/
              {auth.realtime_max_connections_per_user === 0
                ? '∞'
                : auth.realtime_max_connections_per_user}
            </Typography.Text>
          ) : null}
        </div>
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
        user={auth.user}
        totpEnabled={totpEnabled}
        onTotpRefresh={onTotpRefresh}
      />
      <AutomationRuleEditorModal
        conversations={conversations}
        editingAutomationRule={editingAutomationRule}
        open={automationRuleEditorOpen}
        saving={savingAutomationRules}
        setEditingAutomationRule={setEditingAutomationRule}
        onCancel={() => {
          setAutomationRuleEditorOpen(false)
          setEditingAutomationRule(null)
        }}
        onSave={onSaveAutomationRule}
      />
    </div>
  )
}
