import { Button, Empty, List, Space, Switch, Typography } from 'antd'
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'

import type { AutomationRule } from '../../types/chat'

type AutomationRulesPanelProps = {
  automationRules: AutomationRule[]
  onCreateAutomationRule: () => void | Promise<void>
  onDeleteAutomationRule: (ruleId: string) => void | Promise<void>
  onEditAutomationRule: (ruleId: string) => void | Promise<void>
  onToggleAutomationRule: (ruleId: string, enabled: boolean) => void | Promise<void>
  savingAutomationRules: boolean
}

export function AutomationRulesPanel({
  automationRules,
  onCreateAutomationRule,
  onDeleteAutomationRule,
  onEditAutomationRule,
  onToggleAutomationRule,
  savingAutomationRules,
}: AutomationRulesPanelProps) {
  return (
    <div className="automation-rules-panel">
      <div className="automation-rules-header">
        <Typography.Text className="automation-rules-subtitle">
          规则由条件、时间、动作三段组成。命中后会自动介入当前流式输出。
        </Typography.Text>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => void onCreateAutomationRule()}>
          添加规则
        </Button>
      </div>
      <List
        className="automation-rule-list"
        dataSource={automationRules}
        locale={{ emptyText: <Empty description="还没有规则" /> }}
        renderItem={(rule) => (
          <List.Item className="automation-rule-item">
            <div className="automation-rule-copy">
              <Typography.Text className="automation-rule-title">
                {rule.id}
              </Typography.Text>
              <Typography.Paragraph className="automation-rule-summary">
                {`包含 ${rule.conditions.contains.length} 项，不包含 ${rule.conditions.excludes.length} 项，延时 ${rule.timing.delay_seconds} 秒，重复 ${rule.timing.repeat_interval_seconds} 秒，动作 ${rule.action.type}`}
              </Typography.Paragraph>
            </div>
            <Space size={10}>
              <Switch
                checked={rule.enabled}
                checkedChildren="启用"
                unCheckedChildren="停用"
                loading={savingAutomationRules}
                onChange={(checked) => void onToggleAutomationRule(rule.id, checked)}
              />
              <Button icon={<EditOutlined />} onClick={() => void onEditAutomationRule(rule.id)}>
                编辑
              </Button>
              <Button
                danger
                icon={<DeleteOutlined />}
                loading={savingAutomationRules}
                onClick={() => void onDeleteAutomationRule(rule.id)}
              >
                删除
              </Button>
            </Space>
          </List.Item>
        )}
      />
    </div>
  )
}
