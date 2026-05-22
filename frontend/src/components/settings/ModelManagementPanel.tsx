import { useEffect, useState } from 'react'
import { Button, Form, Input, Popconfirm, Table, Typography } from 'antd'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons'

import { appMessage } from '../../lib/antdApp'
import { requestJson } from '../../lib/api'

type ModelManagementPanelProps = {
  open: boolean
}

type ModelListResponse = {
  ok: boolean
  models: string[]
}

type ModelRow = {
  id: string
}

export function ModelManagementPanel({ open }: ModelManagementPanelProps) {
  const [models, setModels] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const [creating, setCreating] = useState(false)
  const [deletingId, setDeletingId] = useState('')
  const [form] = Form.useForm<{ id: string }>()

  useEffect(() => {
    if (!open) return
    let active = true
    async function loadModels() {
      setLoading(true)
      try {
        const data = await requestJson<ModelListResponse>('/api/config/models')
        if (!active) return
        setModels(Array.isArray(data.models) ? data.models : [])
      } catch (error) {
        if (!active) return
        appMessage.error(error instanceof Error ? error.message : '加载模型列表失败')
      } finally {
        if (active) setLoading(false)
      }
    }
    void loadModels()
    return () => { active = false }
  }, [open])

  async function handleCreate(values: { id: string }) {
    const modelId = values.id.trim()
    if (!modelId) {
      appMessage.warning('请输入模型标识符')
      return
    }
    if (models.includes(modelId)) {
      appMessage.info('该模型标识符已存在')
      form.resetFields()
      return
    }
    setCreating(true)
    try {
      const data = await requestJson<ModelListResponse>('/api/config/models', {
        method: 'POST',
        body: JSON.stringify({ id: modelId }),
      })
      setModels(Array.isArray(data.models) ? data.models : [])
      form.resetFields()
      appMessage.success('模型标识符已添加')
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '添加模型标识符失败')
    } finally {
      setCreating(false)
    }
  }

  async function handleDelete(modelId: string) {
    setDeletingId(modelId)
    try {
      const data = await requestJson<ModelListResponse>(`/api/config/models/${encodeURIComponent(modelId)}`, {
        method: 'DELETE',
      })
      setModels(Array.isArray(data.models) ? data.models : [])
      appMessage.success('模型标识符已删除')
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '删除模型标识符失败')
    } finally {
      setDeletingId('')
    }
  }

  const columns = [
    {
      title: '模型标识符',
      dataIndex: 'id',
      key: 'id',
      render: (value: string) => (
        <Typography.Text copyable={{ text: value }} style={{ fontFamily: 'monospace' }}>
          {value}
        </Typography.Text>
      ),
    },
    {
      title: '操作',
      key: 'action',
      render: (_: unknown, record: ModelRow) => (
        <Popconfirm
          title="确定删除该模型标识符？"
          onConfirm={() => handleDelete(record.id)}
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
        </Popconfirm>
      ),
    },
  ]

  return (
    <div className="api-key-management-panel">
      <div className="api-key-management-header">
        <Typography.Text className="api-key-management-subtitle">
          管理可供客户端选择的模型标识符。
        </Typography.Text>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => form.submit()}>
          添加模型
        </Button>
      </div>

      <Form form={form} layout="vertical" onFinish={handleCreate} className="api-key-management-form">
        <Form.Item
          name="id"
          label="模型标识符"
          rules={[{ required: true, message: '请输入模型标识符' }]}
        >
          <Input placeholder="例如 test、gpt-4o、claude-sonnet" allowClear />
        </Form.Item>
        <Form.Item>
          <Button type="primary" htmlType="submit" icon={<PlusOutlined />} loading={creating}>
            添加
          </Button>
        </Form.Item>
      </Form>

      <Table
        className="api-key-management-table"
        columns={columns}
        dataSource={models.map((id) => ({ id }))}
        rowKey="id"
        loading={loading}
        pagination={false}
        size="small"
        locale={{ emptyText: '暂未添加模型标识符' }}
      />
    </div>
  )
}
