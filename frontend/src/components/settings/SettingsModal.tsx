import { useEffect, useState } from 'react'
import { Modal, Tabs } from 'antd'

import type { AutomationRule } from '../../types/chat'
import { AutomationRulesPanel } from './AutomationRulesPanel'
import { StatisticsPanel } from './StatisticsPanel'

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
}

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
}: SettingsModalProps) {
  const [activeTab, setActiveTab] = useState<'statistics' | 'automation'>('statistics')

  useEffect(() => {
    if (open) {
      setActiveTab('statistics')
    }
  }, [open])

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
        onChange={(key) => setActiveTab(key as 'statistics' | 'automation')}
        items={[
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
        ]}
      />
    </Modal>
  )
}
