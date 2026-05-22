import { useState } from 'react'
import { Modal, Tabs } from 'antd'

import type { AutomationRule, AuthUser } from '../../types/chat'
import { ApiKeyManagementPanel } from './ApiKeyManagementPanel'
import { AutomationRulesPanel } from './AutomationRulesPanel'
import { ModelManagementPanel } from './ModelManagementPanel'
import { StatisticsPanel } from './StatisticsPanel'
import { SystemSettingsPanel } from './SystemSettingsPanel'
import { UserManagementPanel } from './UserManagementPanel'
import { UserSettingsPanel } from './UserSettingsPanel'

type SettingsModalProps = {
  automationRuleEditorOpen: boolean
  automationRules: AutomationRule[]
  onCreateAutomationRule: () => void | Promise<void>
  onDeleteAutomationRule: (ruleId: string) => void | Promise<void>
  onEditAutomationRule: (ruleId: string) => void | Promise<void>
  onToggleAutomationRule: (ruleId: string, enabled: boolean) => void | Promise<void>
  open: boolean
  onClose: () => void
  savingAutomationRules: boolean
  user: AuthUser | null
  totpEnabled: boolean
  onTotpRefresh: () => void
}

type TabKey = 'statistics' | 'user-settings' | 'api-keys' | 'models' | 'automation' | 'system' | 'users'

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
  const isAdmin = user?.role === 'admin'
  const [activeTab, setActiveTab] = useState<TabKey>('statistics')

  const handleTabChange = (key: string) => {
    setActiveTab(key as TabKey)
  }

  const commonTabs = [
    {
      key: 'statistics',
      label: '统计面板',
      children: <StatisticsPanel open={open && activeTab === 'statistics'} />,
    },
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
      key: 'models',
      label: '模型管理',
      children: <ModelManagementPanel open={open && activeTab === 'models'} />,
    },
    {
      key: 'system',
      label: <span style={{ color: '#13c2c2' }}>系统设置</span>,
      children: (
        <SystemSettingsPanel
          open={open && activeTab === 'system'}
          onClose={onClose}
        />
      ),
    },
    {
      key: 'users',
      label: <span style={{ color: '#13c2c2' }}>用户管理</span>,
      children: <UserManagementPanel open={open && activeTab === 'users'} />,
    },
  ]

  const tabs = isAdmin ? [...commonTabs, adminTabs[0], userSettingsTab, ...adminTabs.slice(1)] : [...commonTabs, userSettingsTab]

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
