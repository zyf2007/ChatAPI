import { useMemo, useState, type KeyboardEvent } from 'react'
import { Alert, Button, Space, Typography } from 'antd'
import { EditOutlined, SaveOutlined, UndoOutlined } from '@ant-design/icons'

import { appMessage } from '../../lib/antdMessage'
import {
  KEYBOARD_SHORTCUT_ACTIONS,
  createDefaultKeyboardShortcutConfig,
  formatKeyboardShortcutBinding,
  getKeyboardShortcutValidationIssues,
  isAltGraphModifierActive,
  isKeyboardShortcutBindingSafe,
  keyboardShortcutBindingsEqual,
  keyboardShortcutConfigsEqual,
  normalizeKeyboardShortcutEvent,
  type KeyboardShortcutAction,
  type KeyboardShortcutConfig,
} from '../../features/keyboard-shortcuts/shortcuts'
import {
  saveKeyboardShortcutConfig,
  setKeyboardShortcutBinding,
} from '../../features/keyboard-shortcuts/storage'
import { useKeyboardShortcuts } from '../../features/keyboard-shortcuts/useKeyboardShortcuts'

const ACTION_COPY: Record<KeyboardShortcutAction, { label: string; description: string }> = {
  stream: {
    label: '流式输出',
    description: '发送当前助手消息或思考片段，但不结束本轮输出。',
  },
  complete: {
    label: '结束输出',
    description: '发送尚未输出的助手消息，并结束当前请求。',
  },
  newline: {
    label: '编辑器换行',
    description: '在助手消息或思考内容编辑器的光标处插入换行。',
  },
  restore_draft: {
    label: '继续编辑已输出内容',
    description: '把本轮已经流式输出的正文放回助手消息编辑器。',
  },
  cycle_mode: {
    label: '切换输出类型',
    description: '依次切换助手消息、思考内容和工具调用；默认不绑定。',
  },
}

function validationMessage(config: KeyboardShortcutConfig): string {
  const issue = getKeyboardShortcutValidationIssues(config)[0]
  if (!issue) return ''
  const labels = issue.actions.map((action) => ACTION_COPY[action].label).join('、')
  return issue.type === 'duplicate'
    ? `${labels} 使用了相同的组合键，请重新设置。`
    : `${labels} 的组合键可能覆盖打字、浏览器或系统操作，请重新设置。`
}

type DraftState = {
  base: KeyboardShortcutConfig
  config: KeyboardShortcutConfig
}

export function KeyboardShortcutsSettingsPanel({ userID }: { userID: string }) {
  const externalConfig = useKeyboardShortcuts(userID)
  const [draftState, setDraftState] = useState<DraftState | null>(null)
  const [recordingAction, setRecordingAction] = useState<KeyboardShortcutAction | null>(null)
  const [captureError, setCaptureError] = useState('')
  const [saveError, setSaveError] = useState('')
  const [announcement, setAnnouncement] = useState('')

  const hasLocalChanges = Boolean(
    draftState && !keyboardShortcutConfigsEqual(draftState.base, draftState.config),
  )
  const draftConfig = hasLocalChanges && draftState ? draftState.config : externalConfig
  const configError = useMemo(() => validationMessage(draftConfig), [draftConfig])
  const dirty = hasLocalChanges && !keyboardShortcutConfigsEqual(externalConfig, draftConfig)
  const externalChanged = Boolean(
    hasLocalChanges
    && draftState
    && !keyboardShortcutConfigsEqual(draftState.base, externalConfig)
    && !keyboardShortcutConfigsEqual(draftState.config, externalConfig),
  )

  function updateDraft(update: (current: KeyboardShortcutConfig) => KeyboardShortcutConfig) {
    setDraftState((current) => {
      const currentHasChanges = Boolean(
        current && !keyboardShortcutConfigsEqual(current.base, current.config),
      )
      const base = currentHasChanges && current ? current.base : externalConfig
      const config = currentHasChanges && current ? current.config : externalConfig
      return { base, config: update(config) }
    })
  }

  function startRecording(action: KeyboardShortcutAction) {
    const nextAction = recordingAction === action ? null : action
    setRecordingAction(nextAction)
    setCaptureError('')
    setSaveError('')
    setAnnouncement(nextAction ? `正在录制：${ACTION_COPY[action].label}` : '已取消录制')
  }

  function captureShortcut(action: KeyboardShortcutAction, event: KeyboardEvent<HTMLElement>) {
    if (recordingAction !== action) return
    event.preventDefault()
    event.stopPropagation()

    if (event.key === 'Escape') {
      setRecordingAction(null)
      setCaptureError('')
      setAnnouncement(`已取消录制：${ACTION_COPY[action].label}`)
      return
    }
    if (isAltGraphModifierActive(event)) {
      setCaptureError('AltGraph 用于国际键盘输入，不能绑定为发送快捷键。')
      return
    }

    const binding = normalizeKeyboardShortcutEvent(event)
    if (!binding) return
    if (!isKeyboardShortcutBindingSafe(binding)) {
      setCaptureError('为避免误触，仅支持 Enter、Tab 及其 Shift 组合；Ctrl/⌘ 或 Alt 只能与 Enter 组合，且不支持 Ctrl+Alt。')
      return
    }

    const conflict = KEYBOARD_SHORTCUT_ACTIONS.find((candidate) =>
      candidate !== action
      && keyboardShortcutBindingsEqual(draftConfig.bindings[candidate], binding),
    )
    if (conflict) {
      setCaptureError(`该组合键已用于“${ACTION_COPY[conflict].label}”，请先调整或清除原绑定。`)
      return
    }

    updateDraft((current) => setKeyboardShortcutBinding(current, action, binding))
    setRecordingAction(null)
    setCaptureError('')
    setSaveError('')
    setAnnouncement(`已将“${ACTION_COPY[action].label}”设为 ${formatKeyboardShortcutBinding(binding)}`)
  }

  function clearShortcut(action: KeyboardShortcutAction) {
    updateDraft((current) => setKeyboardShortcutBinding(current, action, null))
    setRecordingAction(null)
    setCaptureError('')
    setSaveError('')
    setAnnouncement(`已清除：${ACTION_COPY[action].label}`)
  }

  function restoreDefaults() {
    updateDraft(() => createDefaultKeyboardShortcutConfig())
    setRecordingAction(null)
    setCaptureError('')
    setSaveError('')
    setAnnouncement('已恢复默认快捷键，保存后生效')
  }

  function reloadExternalConfig() {
    setDraftState(null)
    setRecordingAction(null)
    setCaptureError('')
    setSaveError('')
    setAnnouncement('已载入其他标签页保存的快捷键')
  }

  function save() {
    if (externalChanged) {
      setSaveError('另一标签页已更新快捷键，请先载入新配置。')
      return
    }
    try {
      saveKeyboardShortcutConfig(userID, draftConfig)
      setDraftState(null)
      setSaveError('')
      setAnnouncement('快捷键已保存并立即生效')
      appMessage.success('快捷键已保存并立即生效')
    } catch (error) {
      setSaveError(error instanceof Error ? error.message : '快捷键保存失败，请检查浏览器存储权限。')
    }
  }

  if (!userID.trim()) {
    return <Alert type="warning" showIcon message="登录后才能配置快捷键" />
  }

  return (
    <div className="keyboard-shortcuts-settings">
      <Alert
        type="info"
        showIcon
        message="仅作用于回复编辑区"
        description="配置按当前 ChatAPI 用户隔离并保存在此浏览器。现有默认键位保持不变；未被动作占用的按键保留浏览器原生行为，例如未绑定 Shift+Tab 时仍会反向切换焦点。"
      />
      <div className="keyboard-shortcuts-heading">
        <Typography.Title level={4}>快捷键</Typography.Title>
        <Typography.Paragraph type="secondary">
          点击“录制”，再按下组合键。只按修饰键会继续等待，按 Esc 可取消录制。为防止误发送，编辑、浏览器和系统保留键不会被接受。
        </Typography.Paragraph>
      </div>

      <div className="keyboard-shortcut-list">
        {KEYBOARD_SHORTCUT_ACTIONS.map((action) => {
          const binding = draftConfig.bindings[action]
          const recording = recordingAction === action
          return (
            <div className="keyboard-shortcut-row" key={action}>
              <div className="keyboard-shortcut-copy">
                <Typography.Text strong>{ACTION_COPY[action].label}</Typography.Text>
                <Typography.Text type="secondary">{ACTION_COPY[action].description}</Typography.Text>
              </div>
              <kbd className={binding ? 'keyboard-shortcut-key' : 'keyboard-shortcut-key is-empty'}>
                {formatKeyboardShortcutBinding(binding)}
              </kbd>
              <Space size={8} className="keyboard-shortcut-actions">
                <Button
                  type={recording ? 'primary' : 'default'}
                  icon={<EditOutlined />}
                  aria-label={`${recording ? '正在录制' : '录制'}：${ACTION_COPY[action].label}`}
                  aria-pressed={recording}
                  onClick={() => startRecording(action)}
                  onKeyDown={(event) => captureShortcut(action, event)}
                  onBlur={() => {
                    if (recording) setRecordingAction(null)
                  }}
                >
                  {recording ? '请按组合键…' : '录制'}
                </Button>
                <Button
                  aria-label={`清除：${ACTION_COPY[action].label}`}
                  disabled={!binding}
                  onClick={() => clearShortcut(action)}
                >
                  清除
                </Button>
              </Space>
            </div>
          )
        })}
      </div>

      {externalChanged ? (
        <Alert
          className="keyboard-shortcuts-error"
          type="warning"
          showIcon
          message="另一标签页已更新快捷键"
          description="为避免覆盖较新的配置，请先载入后再重新修改。"
          action={<Button size="small" onClick={reloadExternalConfig}>载入新配置</Button>}
        />
      ) : null}

      {captureError || configError || saveError ? (
        <Alert
          className="keyboard-shortcuts-error"
          type="error"
          showIcon
          message={captureError || configError || saveError}
        />
      ) : null}

      <Space wrap className="keyboard-shortcuts-footer">
        <Button icon={<UndoOutlined />} onClick={restoreDefaults}>恢复默认</Button>
        <Button
          type="primary"
          icon={<SaveOutlined />}
          disabled={!dirty || Boolean(configError) || Boolean(recordingAction) || externalChanged}
          onClick={save}
        >
          保存到当前浏览器
        </Button>
      </Space>
      <span className="keyboard-shortcut-live" aria-live="polite">{announcement}</span>
    </div>
  )
}
