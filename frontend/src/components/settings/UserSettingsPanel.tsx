import { useEffect, useMemo, useState } from 'react'
import { Button, Divider, Form, Input, InputNumber, Switch, Typography } from 'antd'

import { appMessage } from '../../lib/antdMessage'
import { requestJson } from '../../lib/api'
import type { UserConfig } from '../../types/chat'
import { ClawBotSettingsCard } from './ClawBotSettingsCard'
import { TotpSetupPanel } from './TotpSetupPanel'

type UserSettingsPanelProps = {
  open: boolean
  onClose: () => void
  totpEnabled: boolean
  onTotpRefresh: () => void
}

const DEFAULT_CONFIG: UserConfig = {
  ntfy_url_enabled: false,
  ntfy_url: '',
  messages_per_minute_limit_enabled: false,
  messages_per_minute_limit: 0,
}

export function UserSettingsPanel({ open, onClose, totpEnabled, onTotpRefresh }: UserSettingsPanelProps) {
  const [config, setConfig] = useState<UserConfig>(DEFAULT_CONFIG)
  const [savedConfig, setSavedConfig] = useState<UserConfig>(DEFAULT_CONFIG)
  const [, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [resettingPassword, setResettingPassword] = useState(false)
  const [passwordForm] = Form.useForm<{ password: string; confirmPassword: string }>()

  useEffect(() => {
    if (!open) return
    let active = true
    async function loadConfig() {
      setLoading(true)
      try {
        const data = await requestJson<{ ok: boolean } & UserConfig>('/api/user/config')
        if (!active) return
        const nextConfig: UserConfig = {
          ntfy_url_enabled: Boolean(data.ntfy_url_enabled),
          ntfy_url: String(data.ntfy_url ?? ''),
          messages_per_minute_limit_enabled: Boolean(data.messages_per_minute_limit_enabled),
          messages_per_minute_limit: Number(data.messages_per_minute_limit ?? 0),
        }
        setConfig(nextConfig)
        setSavedConfig(nextConfig)
      } catch (error) {
        if (!active) return
        appMessage.error(error instanceof Error ? error.message : '用户设置加载失败')
      } finally {
        if (active) setLoading(false)
      }
    }
    void loadConfig()
    return () => { active = false }
  }, [open])

  const dirtyState = useMemo(() => ({
    ntfy_url: config.ntfy_url_enabled !== savedConfig.ntfy_url_enabled || config.ntfy_url !== savedConfig.ntfy_url,
    messages_per_minute_limit:
      config.messages_per_minute_limit_enabled !== savedConfig.messages_per_minute_limit_enabled ||
      config.messages_per_minute_limit !== savedConfig.messages_per_minute_limit,
  }), [config, savedConfig])

  const hasUnsavedChanges = Object.values(dirtyState).some(Boolean)

  function updateSection<K extends keyof UserConfig>(key: K, value: UserConfig[K]) {
    setConfig((current) => ({ ...current, [key]: value }))
  }

  async function handleSave() {
    setSaving(true)
    try {
      const data = await requestJson<{ ok: boolean } & UserConfig>('/api/user/config', {
        method: 'POST',
        body: JSON.stringify(config),
      })
      const nextConfig: UserConfig = {
        ntfy_url_enabled: Boolean(data.ntfy_url_enabled),
        ntfy_url: String(data.ntfy_url ?? ''),
        messages_per_minute_limit_enabled: Boolean(data.messages_per_minute_limit_enabled),
        messages_per_minute_limit: Number(data.messages_per_minute_limit ?? 0),
      }
      setConfig(nextConfig)
      setSavedConfig(nextConfig)
      appMessage.success('用户设置已保存')
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '用户设置保存失败')
    } finally {
      setSaving(false)
    }
  }

  async function handleResetPassword(values: { password: string; confirmPassword: string }) {
    if (values.password !== values.confirmPassword) {
      appMessage.error('两次输入的密码不一致')
      return
    }
    setResettingPassword(true)
    try {
      await requestJson<{ ok: boolean }>('/api/user/password', {
        method: 'POST',
        body: JSON.stringify({ password: values.password }),
      })
      passwordForm.resetFields()
      appMessage.success('密码已重置')
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '密码重置失败')
    } finally {
      setResettingPassword(false)
    }
  }

  return (
    <div className="user-settings-panel">
      <div className="system-settings-rows">
        <div className="system-settings-row">
          <Typography.Text className="system-settings-row-title">ntfy 推送地址</Typography.Text>
          <div className="system-settings-row-body">
            <Typography.Text
              className={`system-settings-row-help ${
                config.ntfy_url_enabled ? 'system-settings-row-help-hidden' : 'system-settings-row-help-visible'
              }`}
            >
              开启后，收到用户消息时会向这里发送推送。
            </Typography.Text>
            <div
              className={`system-settings-row-field ${
                config.ntfy_url_enabled ? 'system-settings-row-field-visible' : 'system-settings-row-field-hidden'
              }`}
            >
              <Input
                value={config.ntfy_url}
                placeholder="https://ntfy.sh/your-topic"
                allowClear
                onChange={(event) => updateSection('ntfy_url', event.target.value)}
              />
            </div>
          </div>
          <Switch
            checked={config.ntfy_url_enabled}
            checkedChildren="启用"
            unCheckedChildren="关闭"
            onChange={(enabled) => updateSection('ntfy_url_enabled', enabled)}
          />
        </div>

        <div className="system-settings-row">
          <Typography.Text className="system-settings-row-title">消息限流</Typography.Text>
          <div className="system-settings-row-body">
            <Typography.Text
              className={`system-settings-row-help ${
                config.messages_per_minute_limit_enabled
                  ? 'system-settings-row-help-hidden'
                  : 'system-settings-row-help-visible'
              }`}
            >
              开启后，按每分钟消息数限制输入请求。
            </Typography.Text>
            <div
              className={`system-settings-row-field ${
                config.messages_per_minute_limit_enabled
                  ? 'system-settings-row-field-visible'
                  : 'system-settings-row-field-hidden'
              }`}
            >
              <InputNumber
                value={config.messages_per_minute_limit}
                min={0}
                precision={0}
                controls
                className="system-settings-number-input"
                placeholder="0"
                onChange={(value) => updateSection('messages_per_minute_limit', Number(value ?? 0))}
              />
            </div>
          </div>
          <Switch
            checked={config.messages_per_minute_limit_enabled}
            checkedChildren="启用"
            unCheckedChildren="关闭"
            onChange={(enabled) => updateSection('messages_per_minute_limit_enabled', enabled)}
          />
        </div>
      </div>

      <div className="system-settings-footer">
        <Typography.Text className="system-settings-footer-hint">
          {hasUnsavedChanges ? '有未保存的更改。' : '当前状态已保存。'}
        </Typography.Text>
        <div className="system-settings-footer-actions">
          <Button onClick={onClose}>关闭</Button>
          <Button type="primary" disabled={!hasUnsavedChanges} loading={saving} onClick={() => void handleSave()}>
            保存
          </Button>
        </div>
      </div>

      <Divider />

      <div className="system-settings-rows">
        <div className="system-settings-row system-settings-row-stacked">
          <Typography.Text className="system-settings-row-title">重置密码</Typography.Text>
          <div className="system-settings-row-body system-settings-row-body-stacked">

            <div className="system-settings-row-field system-settings-row-field-visible system-settings-row-field-static">
              <Form
                form={passwordForm}
                layout="vertical"
                onFinish={(values) => void handleResetPassword(values)}
              >
                <Form.Item
                  label="新密码"
                  name="password"
                  rules={[{ required: true, message: '请输入新密码' }]}
                >
                  <Input.Password placeholder="至少 4 个字符" />
                </Form.Item>
                <Form.Item
                  label="确认新密码"
                  name="confirmPassword"
                  dependencies={['password']}
                  rules={[
                    { required: true, message: '请再次输入新密码' },
                    ({ getFieldValue }) => ({
                      validator(_, value) {
                        if (!value || getFieldValue('password') === value) {
                          return Promise.resolve()
                        }
                        return Promise.reject(new Error('两次输入的密码不一致'))
                      },
                    }),
                  ]}
                >
                  <Input.Password placeholder="再次输入新密码" />
                </Form.Item>
                <Form.Item style={{ marginBottom: 0 }}>
                  <Button type="primary" htmlType="submit" loading={resettingPassword}>
                    重置密码
                  </Button>
                </Form.Item>
              </Form>
            </div>
          </div>
        </div>
      </div>

      <Divider />

      <ClawBotSettingsCard open={open} />

      <Divider />

      <TotpSetupPanel totpEnabled={totpEnabled} onRefresh={onTotpRefresh} />
    </div>
  )
}
