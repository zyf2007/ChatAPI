import { useEffect } from 'react'
import { Button, Input, InputNumber, Select, Switch, Typography } from 'antd'

import { appMessage } from '../../lib/antdApp'
import { requestJson } from '../../lib/api'
import type { SystemConfig } from '../../types/chat'
import {
  formatBytes,
  isRegistrationEmailDomainError,
  normalizeSystemConfig,
} from './systemSettings/config'
import { useSystemSettingsState } from './systemSettings/useSystemSettingsState'

type SystemSettingsPanelProps = {
  open: boolean
  onClose: () => void
}

export function SystemSettingsPanel({ open, onClose }: SystemSettingsPanelProps) {
  const {
    config,
    hasUnsavedChanges,
    registrationEmailDomainsError,
    saving,
    sendingTest,
    setConfig,
    setLoading,
    setRegistrationEmailDomainsError,
    setSavedConfig,
    setSaving,
    setSendingTest,
    setTestEmail,
    testEmail,
    updateSection,
  } = useSystemSettingsState()

  useEffect(() => {
    if (!open) return
    let active = true
    async function loadConfig() {
      setLoading(true)
      try {
        const data = await requestJson<{ ok: boolean } & SystemConfig>('/api/config/system')
        if (!active) return
        const nextConfig = normalizeSystemConfig(data)
        setConfig(nextConfig)
        setSavedConfig(nextConfig)
        setRegistrationEmailDomainsError('')
      } catch (error) {
        if (!active) return
        appMessage.error(error instanceof Error ? error.message : '系统设置加载失败')
      } finally {
        if (active) setLoading(false)
      }
    }
    void loadConfig()
    return () => { active = false }
  }, [open, setConfig, setLoading, setRegistrationEmailDomainsError, setSavedConfig])

  async function handleSave() {
    setSaving(true)
    try {
      const data = await requestJson<{ ok: boolean } & SystemConfig>('/api/config/system', {
        method: 'POST',
        body: JSON.stringify(config),
      })
      const nextConfig = normalizeSystemConfig(data)
      setConfig(nextConfig)
      setSavedConfig(nextConfig)
      setRegistrationEmailDomainsError('')
      appMessage.success('系统设置已保存')
    } catch (error) {
      const message = error instanceof Error ? error.message : '系统设置保存失败'
      if (isRegistrationEmailDomainError(message)) {
        setRegistrationEmailDomainsError(message)
      }
      appMessage.error(message)
    } finally {
      setSaving(false)
    }
  }

  async function handleSendTestEmail() {
    if (!testEmail || !testEmail.includes('@')) {
      appMessage.warning('请输入有效的邮箱地址')
      return
    }
    setSendingTest(true)
    try {
      await requestJson<{ ok: boolean; message?: string; error?: string }>('/api/admin/send-test-email', {
        method: 'POST',
        body: JSON.stringify({ email: testEmail }),
      })
      appMessage.success('测试邮件已发送')
      setTestEmail('')
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '发送测试邮件失败')
    } finally {
      setSendingTest(false)
    }
  }

  return (
    <div className="system-settings-panel">
      <div className="system-settings-rows">
        <div className="system-settings-row">
          <Typography.Text className="system-settings-row-title">公开统计</Typography.Text>
          <div className="system-settings-row-body">
            <Typography.Text className="system-settings-row-help">
              开启后，未登录用户也可以访问独立统计页和统计接口。
            </Typography.Text>
          </div>
          <Switch
            checked={config.public_statistics}
            checkedChildren="公开"
            unCheckedChildren="关闭"
            onChange={(checked) => updateSection('public_statistics', checked)}
          />
        </div>

        <div className="system-settings-row">
          <Typography.Text className="system-settings-row-title">站点标题</Typography.Text>
          <div className="system-settings-row-body">
            <Typography.Text
              className={`system-settings-row-help ${
                config.title_enabled ? 'system-settings-row-help-hidden' : 'system-settings-row-help-visible'
              }`}
            >
              开启后，页面和通知里使用这里的标题。
            </Typography.Text>
            <div
              className={`system-settings-row-field ${
                config.title_enabled ? 'system-settings-row-field-visible' : 'system-settings-row-field-hidden'
              }`}
            >
              <Input
                value={config.title}
                placeholder="站点标题"
                allowClear
                onChange={(event) => updateSection('title', event.target.value)}
              />
            </div>
          </div>
          <Switch
            checked={config.title_enabled}
            checkedChildren="启用"
            unCheckedChildren="关闭"
            onChange={(enabled) => updateSection('title_enabled', enabled)}
          />
        </div>

        <div className="system-settings-row">
          <Typography.Text className="system-settings-row-title">消息推送地址</Typography.Text>
          <div className="system-settings-row-body">
            <Typography.Text className="system-settings-row-help system-settings-row-help-visible">
              一般保持关闭；仅在自建 ntfy 位于本机或内网时开启。
            </Typography.Text>
          </div>
          <Select
            value={config.ntfy_private_url_policy}
            options={[
              { value: 'disabled', label: '关闭' },
              { value: 'admin', label: '仅管理员' },
              { value: 'all', label: '所有用户' },
            ]}
            style={{ width: 120 }}
            onChange={(value) => updateSection('ntfy_private_url_policy', value)}
          />
        </div>

        <div className="system-settings-row">
          <Typography.Text className="system-settings-row-title">测试邮件</Typography.Text>
          <div className="system-settings-row-body">
            <Typography.Text className="system-settings-row-help">
              填写邮箱地址并点击发送，用于验证当前配置的邮箱发送方式是否正确。
            </Typography.Text>
            <div className="system-settings-row-field system-settings-row-field-visible">
              <Input
                value={testEmail}
                placeholder="接收测试邮件的邮箱"
                onChange={(event) => setTestEmail(event.target.value)}
                onPressEnter={() => void handleSendTestEmail()}
              />
            </div>
          </div>
          <Button
            type="primary"
            loading={sendingTest}
            disabled={!testEmail}
            onClick={() => void handleSendTestEmail()}
          >
            发送
          </Button>
        </div>

        <div className="system-settings-row">
          <Typography.Text className="system-settings-row-title">外部注册</Typography.Text>
          <div className="system-settings-row-body">
            <Typography.Text className="system-settings-row-help">
              开启后，未注册用户可以通过邮箱注册新账号。
            </Typography.Text>
          </div>
          <Switch
            checked={config.external_registration_enabled}
            checkedChildren="开启"
            unCheckedChildren="关闭"
            onChange={(checked) => updateSection('external_registration_enabled', checked)}
          />
        </div>

        <div className="system-settings-row">
          <Typography.Text className="system-settings-row-title">限制注册邮箱域名</Typography.Text>
          <div className="system-settings-row-body">
            <Typography.Text
              className={`system-settings-row-help ${
                config.registration_email_domain_restriction_enabled
                  ? 'system-settings-row-help-hidden'
                  : 'system-settings-row-help-visible'
              }`}
            >
              开启后，只允许指定域名的邮箱注册，多个域名用英文逗号分隔，例如 example.com,example.org。
            </Typography.Text>
            <div
              className={`system-settings-row-field ${
                config.registration_email_domain_restriction_enabled
                  ? 'system-settings-row-field-visible'
                  : 'system-settings-row-field-hidden'
              }`}
            >
              <Input
                value={config.registration_email_domains}
                placeholder="example.com,example.org"
                allowClear
                status={registrationEmailDomainsError ? 'error' : undefined}
                onChange={(event) => updateSection('registration_email_domains', event.target.value)}
              />
              {registrationEmailDomainsError ? (
                <Typography.Text type="danger">{registrationEmailDomainsError}</Typography.Text>
              ) : null}
            </div>
          </div>
          <Switch
            checked={config.registration_email_domain_restriction_enabled}
            checkedChildren="开启"
            unCheckedChildren="关闭"
            onChange={(checked) => updateSection('registration_email_domain_restriction_enabled', checked)}
          />
        </div>

        <div className="system-settings-row">
          <Typography.Text className="system-settings-row-title">API Key 数量上限</Typography.Text>
          <div className="system-settings-row-body">
            <Typography.Text className="system-settings-row-help">
              限制每个账号最多可创建的 API Key 数量。填 0 表示不限制。
            </Typography.Text>
            <div className="system-settings-row-field system-settings-row-field-visible">
              <InputNumber
                value={config.api_key_limit_per_user}
                min={0}
                precision={0}
                controls
                className="system-settings-number-input"
                placeholder="0"
                onChange={(value) => updateSection('api_key_limit_per_user', Number(value ?? 0))}
              />
            </div>
          </div>
        </div>

        <div className="system-settings-row system-settings-row-stacked">
          <Typography.Text className="system-settings-row-title">实时连接限制</Typography.Text>
          <div className="system-settings-row-body system-settings-row-body-stacked">
            <Typography.Text className="system-settings-row-help-static">
              限制 WebSocket 总连接、单用户连接和每条连接的事件队列。填 0 表示连接数不限制，队列上限必须大于 0。
            </Typography.Text>
            <div className="system-settings-compact">
              <InputNumber
                addonBefore="全局最大连接数"
                value={config.realtime_max_connections}
                min={0}
                precision={0}
                onChange={(value) => updateSection('realtime_max_connections', Number(value ?? 0))}
              />
              <InputNumber
                addonBefore="单用户最大连接数"
                value={config.realtime_max_connections_per_user}
                min={0}
                precision={0}
                onChange={(value) => updateSection('realtime_max_connections_per_user', Number(value ?? 0))}
              />
              <InputNumber
                addonBefore="事件队列上限"
                value={config.realtime_queue_size}
                min={1}
                precision={0}
                onChange={(value) => updateSection('realtime_queue_size', Math.max(1, Number(value ?? 1)))}
              />
            </div>
          </div>
        </div>

        <div className="system-settings-row system-settings-row-stacked">
          <Typography.Text className="system-settings-row-title">图片存储限制</Typography.Text>
          <div className="system-settings-row-body system-settings-row-body-stacked">
            <Typography.Text className="system-settings-row-help-static">
              超过限制的图片不会落盘，历史消息中会显示“图片已过期”。大小单位为字节，填 0 表示不限制。
            </Typography.Text>
            <div className="system-settings-compact">
              <InputNumber
                addonBefore="单张上限"
                value={config.image_max_single_bytes}
                min={0}
                precision={0}
                onChange={(value) => updateSection('image_max_single_bytes', Number(value ?? 0))}
              />
              <InputNumber
                addonBefore="单请求上限"
                value={config.image_max_request_bytes}
                min={0}
                precision={0}
                onChange={(value) => updateSection('image_max_request_bytes', Number(value ?? 0))}
              />
              <InputNumber
                addonBefore="总容量上限"
                value={config.image_max_total_bytes}
                min={0}
                precision={0}
                onChange={(value) => updateSection('image_max_total_bytes', Number(value ?? 0))}
              />
            </div>
            <Typography.Text type="secondary">
              当前占用 {formatBytes(config.image_usage?.total_bytes)} / {config.image_usage?.file_count ?? 0} 个文件；
              可清理孤儿文件 {formatBytes(config.image_usage?.orphan_bytes)} / {config.image_usage?.orphan_count ?? 0} 个。
            </Typography.Text>
          </div>
        </div>

        <div className="system-settings-row">
          <Typography.Text className="system-settings-row-title">邮箱验证</Typography.Text>
          <div className="system-settings-row-body">
            <Typography.Text
              className={`system-settings-row-help ${
                config.email_verification_enabled ? 'system-settings-row-help-hidden' : 'system-settings-row-help-visible'
              }`}
            >
              开启后，注册时需要输入邮箱收到的验证码。请选择可用的邮箱提供商。
            </Typography.Text>
            <div
              className={`system-settings-row-field ${
                config.email_verification_enabled ? 'system-settings-row-field-visible' : 'system-settings-row-field-hidden'
              }`}
            >
              {config.email_provider_options.length > 0 ? (
                <Select
                  value={config.email_provider || undefined}
                  placeholder="选择邮箱提供商"
                  options={config.email_provider_options}
                  style={{ width: '100%' }}
                  onChange={(value) => updateSection('email_provider', value)}
                />
              ) : (
                <Typography.Text type="secondary">
                  当前未检测到可用的邮箱提供商，请先配置 SMTP、Resend、Brevo 或腾讯云 SES 凭证和模板 ID。
                </Typography.Text>
              )}
            </div>
          </div>
          <Switch
            checked={config.email_verification_enabled}
            checkedChildren="开启"
            unCheckedChildren="关闭"
            onChange={(checked) => updateSection('email_verification_enabled', checked)}
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
    </div>
  )
}
