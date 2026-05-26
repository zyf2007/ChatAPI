import { useState } from 'react'

import { requestJson } from '../lib/api'
import { appMessage } from '../lib/antdApp'
import type { AutomationRule, AutomationRuleCondition } from '../types/chat'

function normalizeRuleConditions(items: AutomationRuleCondition[]): AutomationRuleCondition[] {
  return items
    .map((item) => ({
      match_type: item.match_type === 'regex' ? ('regex' as const) : ('substring' as const),
      pattern: item.pattern.trim(),
    }))
    .filter((item) => item.pattern)
}

function buildEmptyAutomationRule(): AutomationRule {
  return {
    id: `rule_${Math.random().toString(36).slice(2, 10)}`,
    enabled: true,
    conditions: {
      contains: [],
      excludes: [],
    },
    timing: {
      delay_seconds: 0,
      repeat_interval_seconds: 0,
      max_output_count: 120,
    },
    action: {
      type: 'output_text',
      text: '',
      error_message: '',
      tool_name: '',
      tool_arguments: '',
      tool_call_id: '',
    },
  }
}

export function useAutomationRules() {
  const [automationRulesModalOpen, setAutomationRulesModalOpen] = useState(false)
  const [automationRuleEditorOpen, setAutomationRuleEditorOpen] = useState(false)
  const [automationRules, setAutomationRules] = useState<AutomationRule[]>([])
  const [editingAutomationRule, setEditingAutomationRule] = useState<AutomationRule | null>(null)
  const [savingAutomationRules, setSavingAutomationRules] = useState(false)

  async function loadAutomationRules() {
    const data = await requestJson<{ ok?: boolean; rules?: AutomationRule[] }>(
      '/api/config/automation-rules',
    )
    setAutomationRules(Array.isArray(data.rules) ? data.rules : [])
  }

  async function persistAutomationRules(nextRules: AutomationRule[], successText = '规则已保存') {
    setSavingAutomationRules(true)
    try {
      const response = await requestJson<{ ok: boolean; rules: AutomationRule[] }>(
        '/api/config/automation-rules',
        {
          method: 'POST',
          body: JSON.stringify({
            rules: nextRules,
          }),
        },
      )
      setAutomationRules(response.rules)
      appMessage.success(successText)
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '规则保存失败')
      throw error
    } finally {
      setSavingAutomationRules(false)
    }
  }

  async function handleSaveAutomationRule(rule: AutomationRule) {
    const normalized: AutomationRule = {
      ...rule,
      conditions: {
        contains: normalizeRuleConditions(rule.conditions.contains),
        excludes: normalizeRuleConditions(rule.conditions.excludes),
      },
      timing: {
        delay_seconds: Number(rule.timing.delay_seconds) || 0,
        repeat_interval_seconds: Number(rule.timing.repeat_interval_seconds) || 0,
        max_output_count: Math.max(1, Number(rule.timing.max_output_count) || 120),
      },
      action: {
        ...rule.action,
        text: rule.action.text ?? '',
        error_message: rule.action.error_message ?? '',
        tool_name: rule.action.tool_name ?? '',
        tool_arguments: rule.action.tool_arguments ?? '',
        tool_call_id: rule.action.tool_call_id ?? '',
      },
    }

    if (
      normalized.timing.delay_seconds < 0
      || normalized.timing.repeat_interval_seconds < 0
      || normalized.timing.max_output_count < 1
    ) {
      appMessage.warning('时间和次数配置不合法')
      return
    }
    if (normalized.action.type === 'output_text' && !normalized.action.text.trim()) {
      appMessage.warning('输出指定文本时必须填写文本')
      return
    }
    if (normalized.action.type === 'error' && !normalized.action.error_message.trim()) {
      appMessage.warning('返回 error 时必须填写错误信息')
      return
    }
    if (normalized.action.type === 'tool_call' && !normalized.action.tool_name?.trim()) {
      appMessage.warning('工具调用时必须选择一个 tool')
      return
    }

    const nextRules = automationRules.some((item) => item.id === normalized.id)
      ? automationRules.map((item) => (item.id === normalized.id ? normalized : item))
      : [...automationRules, normalized]
    await persistAutomationRules(nextRules)
    setAutomationRuleEditorOpen(false)
    setEditingAutomationRule(null)
  }

  async function handleDeleteAutomationRule(ruleId: string) {
    const nextRules = automationRules.filter((item) => item.id !== ruleId)
    await persistAutomationRules(nextRules, '规则已删除')
  }

  async function handleToggleAutomationRule(ruleId: string, enabled: boolean) {
    const nextRules = automationRules.map((item) =>
      item.id === ruleId ? { ...item, enabled } : item,
    )
    await persistAutomationRules(nextRules, enabled ? '规则已启用' : '规则已停用')
  }

  function handleCreateAutomationRule() {
    setEditingAutomationRule(buildEmptyAutomationRule())
    setAutomationRuleEditorOpen(true)
  }

  function handleEditAutomationRule(ruleId: string) {
    const rule = automationRules.find((item) => item.id === ruleId)
    if (!rule) return
    setEditingAutomationRule({
      ...rule,
      conditions: {
        contains: [...rule.conditions.contains],
        excludes: [...rule.conditions.excludes],
      },
      timing: { ...rule.timing },
      action: { ...rule.action },
    })
    setAutomationRuleEditorOpen(true)
  }

  function resetAutomationRuleUi() {
    setAutomationRulesModalOpen(false)
    setAutomationRuleEditorOpen(false)
    setEditingAutomationRule(null)
  }

  return {
    automationRuleEditorOpen,
    automationRules,
    automationRulesModalOpen,
    editingAutomationRule,
    handleCreateAutomationRule,
    handleDeleteAutomationRule,
    handleEditAutomationRule,
    handleSaveAutomationRule,
    handleToggleAutomationRule,
    loadAutomationRules,
    resetAutomationRuleUi,
    savingAutomationRules,
    setAutomationRuleEditorOpen,
    setAutomationRules,
    setAutomationRulesModalOpen,
    setEditingAutomationRule,
  }
}
