import { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Collapse, Divider, Spin, Typography } from 'antd'
import { ReloadOutlined, SaveOutlined } from '@ant-design/icons'

import { appMessage } from '../../../lib/antdMessage'
import { getSettings, patchSettings } from '../api/settings'
import { SettingFieldControl } from '../components/SettingFieldControl'
import { RateLimitFieldRow } from '../components/RateLimitFieldRow'
import type { SettingField, SettingsDocument } from '../model/types'

export function DomainSettingsSection({ domain, isLab = false }: { domain: string; isLab?: boolean }) {
  const [document, setDocument] = useState<SettingsDocument | null>(null)
  const [changes, setChanges] = useState<Record<string, unknown>>({})
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)

  const draft = useMemo(
    () => ({ ...(document?.values ?? {}), ...changes }),
    [document, changes],
  )
  const dirty = Object.keys(changes).length > 0

  async function load() {
    setLoading(true)
    try {
      const next = await getSettings(domain)
      setDocument(next)
      setChanges({})
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '配置加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    let active = true
    getSettings(domain)
      .then((next) => {
        if (!active) return
        setDocument(next)
        setChanges({})
      })
      .catch((error) => {
        if (active) appMessage.error(error instanceof Error ? error.message : '配置加载失败')
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [domain])

  function changeField(key: string, value: unknown) {
    if (!document || saving) return
    setChanges((current) => {
      const next = { ...current }
      if (JSON.stringify(value) === JSON.stringify(document.values[key])) delete next[key]
      else next[key] = value
      return next
    })
  }

  async function save() {
    if (!document || document.stale || !dirty) return
    setSaving(true)
    try {
      const result = await patchSettings(domain, changes)
      setDocument(result.document)
      setChanges({})
      if (result.warnings?.length) {
        appMessage.warning(`配置已保存，但清理未完全成功：${result.warnings.join('；')}`)
      } else {
        appMessage.success(
          result.restart_required.length
            ? `已保存；${result.restart_required.length} 项重启后生效`
            : '配置已生效',
        )
      }
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  if (loading && !document) {
    return <div className="admin-settings-loading"><Spin /></div>
  }
  if (!document) return null

  const visibleFields = document.fields.filter((field) => !isLab || !/^(user|session|app_key|model_key)_rate_limit_/.test(field.key))
  const render = (fields: SettingField[]) => (
    <div className="admin-settings-fields">
      {fields.map((field) => (
        <SettingFieldControl
          key={field.key}
          field={field}
          source={document.sources[field.key] ?? 'default'}
          value={draft[field.key]}
          disabled={saving}
          onChange={(value) => changeField(field.key, value)}
        />
      ))}
    </div>
  )
  const renderAccessRateLimits = () => {
    const fieldsByKey = new Map(visibleFields.map((field) => [field.key, field]))
    const requestFields = visibleFields.filter((field) => field.key.endsWith('_rate_limit_requests'))
    const otherFields = visibleFields.filter((field) => !field.key.includes('_rate_limit_'))
    return (
      <>
      <div className="admin-settings-fields">
        {requestFields.map((requestsField) => {
          const prefix = requestsField.key.slice(0, -'_rate_limit_requests'.length)
          const windowField = fieldsByKey.get(`${prefix}_rate_limit_window`)
          if (!windowField) return null
          return (
            <RateLimitFieldRow
              key={prefix}
              requestsField={requestsField}
              windowField={windowField}
              requestsSource={document.sources[requestsField.key] ?? 'default'}
              windowSource={document.sources[windowField.key] ?? 'default'}
              requestsValue={draft[requestsField.key]}
              windowValue={draft[windowField.key]}
              disabled={saving}
              onRequestsChange={(value) => changeField(requestsField.key, value)}
              onWindowChange={(value) => changeField(windowField.key, value)}
            />
          )
        })}
      </div>
      {otherFields.length ? render(otherFields) : null}
      </>
    )
  }
  const common = visibleFields.filter((field) => field.level === 'common')
  const policy = visibleFields.filter((field) => field.level === 'policy')
  const advanced = visibleFields.filter((field) => field.level === 'advanced')

  return (
    <section className="admin-domain-section">
      <div className="admin-domain-heading">
        <Typography.Title level={3}>{document.title}</Typography.Title>
        <Button
          icon={<ReloadOutlined />}
          loading={loading}
          disabled={dirty || saving}
          onClick={() => void load()}
        >
          重新加载
        </Button>
      </div>
      {document.stale ? (
        <Alert
          type="warning"
          showIcon
          message="当前显示的是最近一次可用配置"
          description={document.refresh_error || '配置存储暂时不可用；恢复前不能保存更改'}
        />
      ) : null}
      {domain === 'access' ? renderAccessRateLimits() : render(common)}
      {domain !== 'access' && policy.length ? (
        <>
          <Divider orientation="left">更多策略</Divider>
          {render(policy)}
        </>
      ) : null}
      {advanced.length ? (
        <Collapse
          ghost
          items={[{ key: 'advanced', label: '高级设置', children: render(advanced) }]}
        />
      ) : null}
      <div className={`admin-settings-savebar ${dirty ? 'visible' : ''}`}>
        <Typography.Text>{dirty ? '有未保存的更改' : '配置已保存'}</Typography.Text>
        <div>
          <Button disabled={!dirty || saving} onClick={() => setChanges({})}>撤销</Button>
          <Button
            type="primary"
            icon={<SaveOutlined />}
            loading={saving}
            disabled={!dirty || document.stale}
            onClick={() => void save()}
          >
            保存
          </Button>
        </div>
      </div>
    </section>
  )
}
