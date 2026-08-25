import { useState } from 'react'
import { Button, Modal, Tabs, Typography } from 'antd'
import { ControlOutlined } from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'

import type { AuthUser } from '../../types/chat'
import { ApiKeyManagementPanel } from './ApiKeyManagementPanel'
import { AutomationRulesPanel, type AutomationRulesPanelProps } from './AutomationRulesPanel'
import { BrowserAssistSettingsPanel } from './BrowserAssistSettingsPanel'
import { KeyboardShortcutsSettingsPanel } from './KeyboardShortcutsSettingsPanel'
import { UserSettingsPanel } from './UserSettingsPanel'

type SettingsModalProps = AutomationRulesPanelProps & {
  automationRuleEditorOpen: boolean
  open: boolean
  onClose: () => void
  user: AuthUser | null
  totpEnabled: boolean
  onTotpRefresh: () => void
}

type TabKey = 'user-settings' | 'api-keys' | 'automation' | 'browser-assist' | 'keyboard-shortcuts' | 'system'

export function SettingsModal({
  automationRuleEditorOpen,
  automationRules,
  onCreateAutomationRule,
  onDeleteAutomationRule,
  onEditAutomationRule,
  onToggleAutomationRule,
  open,
  onClose,
  savingAutomationRules,
  user,
  totpEnabled,
  onTotpRefresh,
}: SettingsModalProps) {
	const navigate = useNavigate()
  const isAdmin = user?.role === 'admin' || user?.role === 'superadmin'
  const [activeTab, setActiveTab] = useState<TabKey>('user-settings')

  const handleTabChange = (key: string) => {
    setActiveTab(key as TabKey)
  }

  const commonTabs = [
    {
      key: 'automation',
      label: '自动化规则',
      children: (
        <AutomationRulesPanel
          automationRules={automationRules}
          onCreateAutomationRule={onCreateAutomationRule}
          onDeleteAutomationRule={onDeleteAutomationRule}
          onEditAutomationRule={onEditAutomationRule}
          onToggleAutomationRule={onToggleAutomationRule}
          savingAutomationRules={savingAutomationRules}
        />
      ),
    },
    {
      key: 'api-keys',
      label: 'API Keys',
      children: <ApiKeyManagementPanel open={open && activeTab === 'api-keys'} />,
    },
    {
      key: 'browser-assist',
      label: '辅助模型',
      children: <BrowserAssistSettingsPanel open={open && activeTab === 'browser-assist'} userID={user?.id ?? ''} />,
    },
    {
      key: 'keyboard-shortcuts',
      label: '快捷键',
      children: (
        <KeyboardShortcutsSettingsPanel
          key={user?.id ?? 'anonymous'}
          userID={user?.id ?? ''}
        />
      ),
    },
  ]

  const userSettingsTab = {
    key: 'user-settings',
    label: isAdmin ? <span style={{ color: '#13c2c2' }}>我的设置</span> : '我的设置',
    children: (
      <UserSettingsPanel
        open={open && activeTab === 'user-settings'}
        onClose={onClose}
        totpEnabled={totpEnabled}
        onTotpRefresh={onTotpRefresh}
      />
    ),
  }

  const adminTabs = [
	{
	  key: 'system',
	  label: <span style={{ color: '#13c2c2' }}>系统设置</span>,
	  children: <div className="admin-settings-entry"><ControlOutlined/><Typography.Title level={4}>系统管理设置</Typography.Title><Typography.Paragraph type="secondary">管理认证、访问限制、聊天、媒体、自动化和实时通信策略。</Typography.Paragraph><Button type="primary" onClick={()=>{onClose();navigate('/admin/settings/overview')}}>打开管理控制面</Button></div>,
	},
  ]

  const tabs = isAdmin ? [...commonTabs, userSettingsTab, ...adminTabs] : [...commonTabs, userSettingsTab]

  return (
    <Modal
      title="设置"
      width={1120}
      open={open}
      onCancel={() => {
        if (savingAutomationRules || automationRuleEditorOpen) return
        onClose()
      }}
      footer={null}
      destroyOnHidden
      className="settings-modal"
    >
      <Tabs
        activeKey={activeTab}
        onChange={handleTabChange}
        items={tabs}
      />
    </Modal>
  )
}
