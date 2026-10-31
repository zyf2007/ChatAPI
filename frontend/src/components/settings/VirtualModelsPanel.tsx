import { useEffect, useState } from 'react'
import { Button, Form, Input, Popconfirm, Table, Typography } from 'antd'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons'

import { appMessage } from '../../lib/antdMessage'
import { requestJson } from '../../lib/api'
import type { VirtualModelInfo } from '../../types/chat'

/** Model names are workspace resources; API credentials are managed separately. */
export function VirtualModelsPanel({ open }: { open: boolean }) {
  const [models, setModels] = useState<VirtualModelInfo[]>([])
  const [loading, setLoading] = useState(false)
  const [creating, setCreating] = useState(false)
  const [deletingId, setDeletingId] = useState('')
  const [form] = Form.useForm<{ name: string }>()

  useEffect(() => {
    if (!open) return
    let active = true
    async function load() {
      setLoading(true)
      return requestJson<{ items: VirtualModelInfo[] }>('/api/user/virtual-models')
        .then((data) => { if (active) setModels(data.items) })
        .catch((error: unknown) => { if (active) appMessage.error(error instanceof Error ? error.message : '加载虚拟模型列表失败') })
        .finally(() => { if (active) setLoading(false) })
    }
    void load()
    return () => { active = false }
  }, [open])

  async function createModel({ name }: { name: string }) {
    setCreating(true)
    try {
      const data = await requestJson<{ item: VirtualModelInfo }>('/api/user/virtual-models', {
        method: 'POST', body: JSON.stringify({ name: name.trim() }),
      })
      setModels((current) => [data.item, ...current])
      form.resetFields()
      appMessage.success('虚拟模型已添加')
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '添加虚拟模型失败')
    } finally { setCreating(false) }
  }

  async function deleteModel(id: string) {
    setDeletingId(id)
    try {
      await requestJson(`/api/user/virtual-models/${encodeURIComponent(id)}`, { method: 'DELETE' })
      setModels((current) => current.filter((model) => model.id !== id))
      appMessage.success('虚拟模型已删除')
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '删除虚拟模型失败')
    } finally { setDeletingId('') }
  }

  return (
    <div className="api-key-management-panel">
      <Typography.Paragraph type="secondary">配置向客户端公布的模型名称，可通过 /v1/models 获取模型列表。</Typography.Paragraph>
      <Form form={form} layout="vertical" onFinish={createModel} className="api-key-management-form">
        <Form.Item name="name" label="虚拟模型名称" rules={[{ required: true, whitespace: true, message: '请输入虚拟模型名称' }]}>
          <Input placeholder="例如 kirari-chat, test-model" allowClear />
        </Form.Item>
        <Form.Item><Button type="primary" htmlType="submit" icon={<PlusOutlined />} loading={creating}>添加虚拟模型</Button></Form.Item>
      </Form>
      <Table<VirtualModelInfo> className="api-key-management-table" dataSource={models} rowKey="id" loading={loading} pagination={false} size="small" columns={[
        { title: '名称', dataIndex: 'name', key: 'name' },
        { title: '创建时间', dataIndex: 'created_at', key: 'created_at', render: (value: string) => new Date(value).toLocaleString() },
        { title: '操作', key: 'action', render: (_, model) => (
          <Popconfirm title="确定删除该虚拟模型？" onConfirm={() => deleteModel(model.id)} okText="删除" cancelText="取消" okButtonProps={{ danger: true }}>
            <Button type="link" danger icon={<DeleteOutlined />} loading={deletingId === model.id}>删除</Button>
          </Popconfirm>
        ) },
      ]} />
    </div>
  )
}
