import { useEffect, useState } from 'react'
import { Button, Form, Input, Popconfirm, Table, Tabs, Typography } from 'antd'
import { CopyOutlined, DeleteOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'

import { VirtualModelsPanel } from './VirtualModelsPanel'
import { appMessage } from '../../lib/antdMessage'
import { requestJson } from '../../lib/api'
import type { ApiKeyInfo, ApiKeyListResponse, ModelKeyInfo } from '../../types/chat'

type ApiKeyManagementPanelProps = {
  open: boolean
}

export function ApiKeyManagementPanel({ open }: ApiKeyManagementPanelProps) {
  const [appKeys, setAppKeys] = useState<ApiKeyInfo[]>([])
  const [modelKeys, setModelKeys] = useState<ModelKeyInfo[]>([])
  const [appLoading, setAppLoading] = useState(false)
  const [modelLoading, setModelLoading] = useState(false)
  const [creatingAppKey, setCreatingAppKey] = useState(false)
  const [creatingModelKey, setCreatingModelKey] = useState(false)
  const [deletingId, setDeletingId] = useState('')
  const [apiKeyLimit, setApiKeyLimit] = useState(0)
  const [appForm] = Form.useForm()
  const [modelForm] = Form.useForm()

  useEffect(() => {
    if (!open) return
    let active = true
    async function loadAppKeys() {
      setAppLoading(true)
      try {
        const data = await requestJson<ApiKeyListResponse>('/api/user/api-keys')
        if (!active) return
        setAppKeys(Array.isArray(data.api_keys) ? data.api_keys : [])
        setApiKeyLimit(Number(data.api_key_limit_per_user ?? 0))
      } catch (error) {
        if (!active) return
        appMessage.error(error instanceof Error ? error.message : '加载应用 API Key 列表失败')
      } finally {
        if (active) setAppLoading(false)
      }
    }

    async function loadModelKeys() {
      setModelLoading(true)
      try {
        const data = await requestJson<{ ok: boolean; items: ModelKeyInfo[] }>('/api/user/model-keys')
        if (!active) return
        setModelKeys(Array.isArray(data.items) ? data.items : [])
      } catch (error) {
        if (!active) return
        appMessage.error(error instanceof Error ? error.message : '加载虚拟模型 Key 列表失败')
      } finally {
        if (active) setModelLoading(false)
      }
    }

    void loadAppKeys()
    void loadModelKeys()
    return () => { active = false }
  }, [open])

  async function handleCreateAppKey(values: { name: string }) {
    if (apiKeyLimit > 0 && appKeys.length >= apiKeyLimit) {
      appMessage.warning(`当前账号最多只能创建 ${apiKeyLimit} 个 API Key`)
      return
    }
    setCreatingAppKey(true)
    try {
      const data = await requestJson<{ ok: boolean; api_key: ApiKeyInfo & { api_key?: string } }>('/api/user/api-keys', {
        method: 'POST',
        body: JSON.stringify({
          name: values.name,
        }),
      })
      setAppKeys((prev) => [...prev, data.api_key])
      appForm.resetFields()
      if (data.api_key.api_key) {
        void navigator.clipboard?.writeText(data.api_key.api_key).catch(() => {})
        appMessage.success('应用 API Key 已创建，明文已复制到剪贴板')
      } else {
        appMessage.success('应用 API Key 已创建')
      }
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '创建应用 API Key 失败')
    } finally {
      setCreatingAppKey(false)
    }
  }

  async function handleCreateModelKey(values: { name: string; key?: string }) {
    setCreatingModelKey(true)
    try {
      const data = await requestJson<{ ok: boolean; model_key: ModelKeyInfo & { api_key?: string } }>('/api/user/model-keys', {
        method: 'POST',
        body: JSON.stringify({
          name: values.name,
          key: values.key,
        }),
      })
      setModelKeys((prev) => [...prev, data.model_key])
      modelForm.resetFields()
      if (data.model_key.api_key) {
        void navigator.clipboard?.writeText(data.model_key.api_key).catch(() => {})
        appMessage.success('虚拟模型 Key 已创建，明文已复制到剪贴板')
      } else {
        appMessage.success('虚拟模型 Key 已创建')
      }
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '创建虚拟模型 Key 失败')
    } finally {
      setCreatingModelKey(false)
    }
  }

  async function handleDeleteAppKey(keyId: string) {
    setDeletingId(keyId)
    try {
      await requestJson(`/api/user/api-keys/${keyId}`, { method: 'DELETE' })
      setAppKeys((prev) => prev.filter((k) => k.id !== keyId))
      appMessage.success('应用 API Key 已删除')
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '删除应用 API Key 失败')
    } finally {
      setDeletingId('')
    }
  }

  async function handleDeleteModelKey(keyId: string) {
    setDeletingId(keyId)
    try {
      await requestJson(`/api/user/model-keys/${keyId}`, { method: 'DELETE' })
      setModelKeys((prev) => prev.filter((k) => k.id !== keyId))
      appMessage.success('虚拟模型 Key 已删除')
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '删除虚拟模型 Key 失败')
    } finally {
      setDeletingId('')
    }
  }

  async function copyKey(kind: 'api-keys' | 'model-keys', keyId: string) {
    try {
      const data = await requestJson<{ api_key: string }>(`/api/user/${kind}/${keyId}/secret`)
      await navigator.clipboard.writeText(data.api_key)
      appMessage.success('API Key 已复制')
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '复制 API Key 失败')
    }
  }

  const appKeyColumns = [
    { title: '名称', dataIndex: 'name', key: 'name', render: (v: string) => v || '-' },
    {
      title: 'Key 前缀',
      dataIndex: 'key_prefix',
      key: 'key_prefix',
      render: (v: string) => (
        <Typography.Text style={{ fontFamily: 'monospace' }}>{v || '-'}</Typography.Text>
      ),
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      key: 'created_at',
      render: (v: string) => new Date(v).toLocaleString(),
    },
    {
      title: '操作',
      key: 'action',
      render: (_: unknown, record: ApiKeyInfo) => (
        <><Button type="text" icon={<CopyOutlined />} onClick={() => copyKey('api-keys', record.id)}>复制</Button><Popconfirm
          title="确定删除该应用 API Key？"
          onConfirm={() => handleDeleteAppKey(record.id)}
          okText="删除"
          cancelText="取消"
          okButtonProps={{ danger: true }}
        >
          <Button
            type="link"
            danger
            icon={<DeleteOutlined />}
            loading={deletingId === record.id}
          >
            删除
          </Button>
        </Popconfirm></>
      ),
    },
  ]

  const modelKeyColumns = [
    { title: '名称', dataIndex: 'name', key: 'name', render: (v: string) => v || '-' },
    {
      title: 'Key 前缀',
      dataIndex: 'key_prefix',
      key: 'key_prefix',
      render: (v: string) => <Typography.Text style={{ fontFamily: 'monospace' }}>{v || '-'}</Typography.Text>,
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      key: 'created_at',
      render: (v: string) => new Date(v).toLocaleString(),
    },
    {
      title: '操作',
      key: 'action',
      render: (_: unknown, record: ModelKeyInfo) => (
        <><Button type="text" icon={<CopyOutlined />} onClick={() => copyKey('model-keys', record.id)}>复制</Button><Popconfirm
          title="确定删除该虚拟模型 Key？"
          onConfirm={() => handleDeleteModelKey(record.id)}
          okText="删除"
          cancelText="取消"
          okButtonProps={{ danger: true }}
        >
          <Button
            type="link"
            danger
            icon={<DeleteOutlined />}
            loading={deletingId === record.id}
          >
            删除
          </Button>
        </Popconfirm></>
      ),
    },
  ]

  return (
    <Tabs
      items={[
        {
          key: 'virtual-models', label: '虚拟模型管理', children: <VirtualModelsPanel open={open} />,
        },
        {
          key: 'model-keys',
          label: '虚拟模型 Key',
          children: (
            <div className="api-key-management-panel">
              <div className="api-key-management-header"><Typography.Text className="api-key-management-subtitle">任一虚拟模型 Key 均可访问当前用户全部虚拟模型。</Typography.Text><Button type="primary" icon={<PlusOutlined />} onClick={() => modelForm.submit()}>新建虚拟模型 Key</Button></div>
              <Form form={modelForm} layout="vertical" onFinish={handleCreateModelKey} className="api-key-management-form">
                <Form.Item name="name" label="名称"><Input placeholder="例如 default, workspace, agent" allowClear /></Form.Item>
                <Form.Item name="key" label="Key"><Input placeholder="留空自动生成，或手动填写" addonAfter={<Button type="text" icon={<ReloadOutlined />} aria-label="生成随机 Key" onClick={() => modelForm.setFieldValue('key', `sk-${crypto.randomUUID()}`)} />} /></Form.Item>
                <Form.Item><Button type="primary" htmlType="submit" icon={<PlusOutlined />} loading={creatingModelKey}>创建</Button></Form.Item>
              </Form>
              <Table className="api-key-management-table" columns={modelKeyColumns} dataSource={modelKeys} rowKey="id" loading={modelLoading} pagination={false} size="small" />
            </div>
          ),
        },
        {
          key: 'app-keys',
          label: '应用 API Key',
          children: (
            <div className="api-key-management-panel">
              <div className="api-key-management-header">
                <Typography.Text className="api-key-management-subtitle">
                  管理应用 API Key，用于读写自己的 ChatAPI、规则和请求数据。
                  {apiKeyLimit > 0 ? ` 已使用 ${appKeys.length} / ${apiKeyLimit}。` : ' 当前不限制数量。'}
                </Typography.Text>
                <Button
                  type="primary"
                  icon={<PlusOutlined />}
                  onClick={() => appForm.submit()}
                  disabled={apiKeyLimit > 0 && appKeys.length >= apiKeyLimit}
                >
                  新建应用 Key
                </Button>
              </div>

              <Form form={appForm} layout="vertical" onFinish={handleCreateAppKey} className="api-key-management-form">
                <Form.Item name="name" label="名称">
                  <Input placeholder="例如 n8n、本地调试、CI" allowClear />
                </Form.Item>
                <Form.Item>
                  <Button type="primary" htmlType="submit" icon={<PlusOutlined />} loading={creatingAppKey}>
                    创建
                  </Button>
                </Form.Item>
              </Form>

              <Table
                className="api-key-management-table"
                columns={appKeyColumns}
                dataSource={appKeys}
                rowKey="id"
                loading={appLoading}
                pagination={false}
                size="small"
              />
            </div>
          ),
        },
      ]}
    />
  )
}
