import { Button, Empty, List, Space, Switch, Typography } from 'antd'
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'

import type { AutomationRule } from '../../types/chat'

export type AutomationRulesPanelProps = {
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
          在等待中的请求上开始录制，完成后再设置匹配正则和播放节奏。
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
                {rule.name}
              </Typography.Text>
              <Typography.Paragraph className="automation-rule-summary">
        {`${rule.steps.length} 个步骤 · ${rule.playback.mode === 'recorded' ? '真实节奏' : `${rule.playback.fixed_interval_ms}ms 固定间隔`}${rule.playback.loop ? ` · ${rule.playback.loop_interval_ms}ms 循环` : ''} · ${rule.match.pattern || '尚未设置匹配条件'}`}
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
