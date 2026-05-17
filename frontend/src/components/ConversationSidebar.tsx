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
import { DeleteOutlined, LogoutOutlined, SettingOutlined, StopOutlined } from '@ant-design/icons'

import { formatTime } from '../lib/chat-format'
import type { AuthSession, Conversation } from '../types/chat'

type ConversationSidebarProps = {
  abortPopoverConversationId: string
  abortReason: string
  abortingConversationId: string
  auth: AuthSession
  conversations: Conversation[]
  deletingConversationId: string
  onAbortConversation: (conversationId: string) => void | Promise<void>
  onDeleteConversation: (conversationId: string) => void | Promise<void>
  onLogout: () => void | Promise<void>
  onSelectConversation: (conversationId: string) => void | Promise<void>
  pruneKeepCount: number
  pruneModalOpen: boolean
  pruningConversations: boolean
  savingStreamHeartbeatConfig: boolean
  selectedConversationId: string
  setAbortPopoverConversationId: (value: string) => void
  setAbortReason: (value: string) => void
  setPruneKeepCount: (value: number) => void
  setPruneModalOpen: (value: boolean) => void
  setStreamHeartbeatIntervalSeconds: (value: number) => void
  setStreamHeartbeatModalOpen: (value: boolean) => void
  setStreamHeartbeatText: (value: string) => void
  streamHeartbeatIntervalSeconds: number
  streamHeartbeatModalOpen: boolean
  streamHeartbeatText: string
  onPruneConversations: () => void | Promise<void>
  onSaveStreamHeartbeatConfig: () => void | Promise<void>
}

export function ConversationSidebar({
  abortPopoverConversationId,
  abortReason,
  abortingConversationId,
  auth,
  conversations,
  deletingConversationId,
  onAbortConversation,
  onDeleteConversation,
  onLogout,
  onPruneConversations,
  onSaveStreamHeartbeatConfig,
  onSelectConversation,
  pruneKeepCount,
  pruneModalOpen,
  pruningConversations,
  savingStreamHeartbeatConfig,
  selectedConversationId,
  setAbortPopoverConversationId,
  setAbortReason,
  setPruneKeepCount,
  setPruneModalOpen,
  setStreamHeartbeatIntervalSeconds,
  setStreamHeartbeatModalOpen,
  setStreamHeartbeatText,
  streamHeartbeatIntervalSeconds,
  streamHeartbeatModalOpen,
  streamHeartbeatText,
}: ConversationSidebarProps) {
  return (
    <div className="sidebar-inner">
      <div className="sidebar-top">
        <div>
          <Typography.Text className="eyebrow">ChatAPI</Typography.Text>
          <Typography.Title level={4} className="sidebar-title">
            会话
          </Typography.Title>
        </div>
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
                        ? `${item.message_count} 条消息 · ${formatTime(item.last_message_at)}`
                        : '空会话'}
                    </Typography.Text>
                  </div>
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
        <div className="footer-head">
          <Typography.Text className="footer-name">{auth.user?.username}</Typography.Text>
          <Tooltip title="流式保活设置">
            <Button
              type="text"
              icon={<SettingOutlined />}
              className="footer-settings-button"
              onClick={() => setStreamHeartbeatModalOpen(true)}
            />
          </Tooltip>
        </div>
        <Button icon={<LogoutOutlined />} onClick={() => void onLogout()} block>
          退出登录
        </Button>
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
      <Modal
        title="流式保活设置"
        open={streamHeartbeatModalOpen}
        onCancel={() => {
          if (savingStreamHeartbeatConfig) return
          setStreamHeartbeatModalOpen(false)
        }}
        onOk={() => void onSaveStreamHeartbeatConfig()}
        okText="保存"
        okButtonProps={{ loading: savingStreamHeartbeatConfig }}
        cancelButtonProps={{ disabled: savingStreamHeartbeatConfig }}
        destroyOnHidden
      >
        <Space direction="vertical" size={12} className="prune-modal-stack">
          <div>
            <Typography.Text className="prune-input-label">初始回复字段</Typography.Text>
            <Input.TextArea
              value={streamHeartbeatText}
              onChange={(event) => setStreamHeartbeatText(event.target.value)}
              autoSize={{ minRows: 4, maxRows: 10 }}
              placeholder="可输入空格、换行或零宽度空格"
            />
            <div className="stream-heartbeat-actions">
              <Button
                size="small"
                onClick={() => setStreamHeartbeatText('\u200B')}
              >
                填入零宽度空格
              </Button>
            </div>
          </div>
          <div>
            <Typography.Text className="prune-input-label">间隔时间（秒）</Typography.Text>
            <InputNumber
              min={0}
              value={streamHeartbeatIntervalSeconds}
              onChange={(value) =>
                setStreamHeartbeatIntervalSeconds(typeof value === 'number' ? value : 0)
              }
              className="prune-input"
              placeholder="输入秒数，0 表示关闭"
            />
          </div>
        </Space>
      </Modal>
    </div>
  )
}
