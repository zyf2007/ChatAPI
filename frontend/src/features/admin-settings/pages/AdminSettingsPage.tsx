import { useEffect, useState } from 'react'
import {
  ArrowLeftOutlined,
  DashboardOutlined,
  TeamOutlined,
  LockOutlined,
  PictureOutlined,
  RobotOutlined,
  SettingOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { Button, Collapse, Descriptions, Layout, List, Menu, Spin, Statistic, Tag, Typography } from 'antd'
import { Navigate, useNavigate, useParams } from 'react-router-dom'

import { useAuthSession } from '../../../hooks/useAuthSession'
import { getAuditLogs, getOverview, getRuntime, type AuditLogItem } from '../api/settings'
import { DomainSettingsSection } from '../sections/DomainSettingsSection'
import { UserManagementPanel } from '../../../components/settings/UserManagementPanel'
import { useAdminMonitoring } from '../hooks/useAdminMonitoring'
import './admin-settings.css'

const domains = [
  { key: 'auth', label: '访问与认证', icon: <LockOutlined /> },
  { key: 'access', label: '访问限流', icon: <ThunderboltOutlined /> },
  { key: 'media', label: '媒体', icon: <PictureOutlined /> },
  { key: 'automation', label: '自动化', icon: <RobotOutlined /> },
]

const items = [
  { key: 'overview', label: '概览', icon: <DashboardOutlined /> },
  { key: 'users', label: '用户管理', icon: <TeamOutlined /> },
  { key: 'system', label: '系统设置', icon: <SettingOutlined /> },
  ...domains,
]

const labSectionKeys = new Set(['overview', 'system', 'access', 'media'])
const sectionKeys = new Set(items.map((item) => item.key))
const noMonitoredUsers: string[] = []

export function AdminSettingsPage() {
  const auth = useAuthSession()
  const navigate = useNavigate()
  const params = useParams()
  const selected = params['*'] ?? ''
  const [overview, setOverview] = useState<Record<string, unknown> | null>(null)
  const [runtime, setRuntime] = useState<Record<string, unknown> | null>(null)

  useEffect(() => {
    if (auth.session.mode === 'lab' && !labSectionKeys.has(selected)) return
    if (!auth.session.authenticated || !isAdminRole(auth.session.user?.role)) return
    void getOverview().then(setOverview)
    void getRuntime().then(setRuntime)
  }, [auth.session.authenticated, auth.session.mode, auth.session.user?.role, selected])

  if (auth.loading) return <div className="boot-screen"><Spin /></div>
  if (auth.session.mode === 'lab' && !labSectionKeys.has(selected)) return <Navigate to="/app" replace />
  if (!auth.session.authenticated) return <Navigate to="/login" replace />
  if (!isAdminRole(auth.session.user?.role)) return <Navigate to="/app" replace />
  if (!sectionKeys.has(selected)) return <Navigate to="/admin/settings/overview" replace />

  return (
    <Layout className="admin-settings-shell">
      <aside className="admin-settings-nav">
        <div className="admin-settings-brand">
          <Button type="text" icon={<ArrowLeftOutlined />} onClick={() => navigate('/app')} />
          <div>
            <Typography.Text strong>系统设置</Typography.Text>
            <Typography.Text type="secondary">{auth.session.mode === 'lab' ? 'Lab 实例管理' : '管理员控制面'}</Typography.Text>
          </div>
        </div>
        <Menu
          mode="inline"
          selectedKeys={[selected]}
          items={auth.session.mode === 'lab' ? items.filter((item) => labSectionKeys.has(item.key)) : items}
          onSelect={({ key }) => navigate(`/admin/settings/${key}`)}
        />
      </aside>
      <main className="admin-settings-main">
        {selected === 'overview' ? (
          <Overview overview={overview} runtime={runtime} />
        ) : selected === 'users' ? (
          <UserManagementPanel open currentRole={auth.session.user.role} />
        ) : selected === 'system' ? (
          <SystemSettings runtime={runtime} />
        ) : (
          <DomainSettingsSection key={selected} domain={selected} isLab={auth.session.mode === 'lab'} />
        )}
      </main>
    </Layout>
  )
}

function isAdminRole(role?: string) {
  return role === 'admin' || role === 'superadmin'
}

function SystemSettings({ runtime }: { runtime: Record<string, unknown> | null }) {
  const [logs, setLogs] = useState<AuditLogItem[]>([])
  const [loading, setLoading] = useState(true)
  const config = (runtime?.config ?? {}) as Record<string, unknown>

  useEffect(() => {
    let active = true
    getAuditLogs('system.storage.vacuum')
      .then((items) => { if (active) setLogs(items) })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [])

  return (
    <section className="admin-domain-section">
      <div className="admin-domain-heading"><Typography.Title level={3}>系统设置</Typography.Title></div>
      <Typography.Paragraph type="secondary">以下参数来自服务启动环境，修改后需重启 ChatAPI 才会生效。</Typography.Paragraph>
      <Descriptions bordered size="small" column={1} title="SQLite 存储维护">
        <Descriptions.Item label="定时 VACUUM">{config.database_driver === 'sqlite' && config.storage_cleanup_enabled && config.storage_vacuum_enabled ? '已启用' : '未启用'}</Descriptions.Item>
        <Descriptions.Item label="执行时间">{String(config.storage_cleanup_time ?? '-')}</Descriptions.Item>
        <Descriptions.Item label="数据库驱动">{String(config.database_driver ?? '-')}</Descriptions.Item>
      </Descriptions>
      <Typography.Title level={4} style={{ marginTop: 24 }}>最近维护日志</Typography.Title>
      <List
        loading={loading}
        dataSource={logs}
        locale={{ emptyText: '暂无已执行的存储维护任务' }}
        renderItem={(item) => (
          <List.Item>
            <List.Item.Meta
              title={<><Tag color={item.outcome === 'success' ? 'green' : 'red'}>{item.outcome === 'success' ? '成功' : '失败'}</Tag> SQLite VACUUM</>}
              description={`${formatDateTime(item.created_at)}${item.metadata?.duration_ms != null ? ` · ${item.metadata.duration_ms} ms` : ''}${item.metadata?.error ? ` · ${String(item.metadata.error)}` : ''}`}
            />
          </List.Item>
        )}
      />
    </section>
  )
}

function Overview({ overview, runtime: runtimeDocument }: { overview: Record<string, unknown> | null; runtime: Record<string, unknown> | null }) {
  const runtimeSummary = (overview?.runtime ?? {}) as Record<string, unknown>
  const configuredDomains = (overview?.domains ?? {}) as Record<string, unknown>
  const monitoring = useAdminMonitoring(true, noMonitoredUsers)
  const metrics = monitoring.metrics
  return (
    <section className="admin-overview">
      <Typography.Title level={2}>系统概览</Typography.Title>
      <div className="admin-overview-status">
        <Typography.Text type="secondary">实例运行状态</Typography.Text>
        <Tag color={monitoring.connected ? 'green' : 'default'}>{monitoring.connected ? '实时' : '连接中'}</Tag>
      </div>
      <div className="admin-overview-stats admin-overview-metrics">
        <Statistic title="实例连接" value={monitoring.totalConnections} />
        <Statistic title="Goroutine" value={metrics?.goroutines ?? 0} />
        <Statistic title="Go 堆内存" value={formatBytes(metrics?.heap_alloc_bytes ?? 0)} />
        <Statistic title="运行时间" value={formatUptime(metrics?.uptime_seconds ?? 0)} />
        <Statistic title="CPU 使用率" value={metrics?.cpu_usage_percent ?? 0} precision={1} suffix="%" />
        <Statistic title="CPU 逻辑核心" value={metrics?.cpu_count ?? 0} />
        <Statistic title="主机内存" value={formatMemoryUsage(metrics?.memory_total_bytes ?? 0, metrics?.memory_available_bytes ?? 0)} />
        <Statistic title="Swap" value={formatUsage(metrics?.swap_used_bytes ?? 0, metrics?.swap_total_bytes ?? 0)} />
      </div>
      <div className="admin-overview-stats">
        <Statistic title="配置领域" value={Object.keys(configuredDomains).length} />
        <Statistic title="运行模式" value={String(runtimeSummary.mode ?? '-')} />
        <Statistic title="数据库" value={String(runtimeSummary.database_driver ?? '-')} />
        <Statistic title="邮件服务" value={runtimeSummary.smtp_configured ? '已配置' : '未配置'} />
      </div>
      <Collapse ghost items={[{ key: 'runtime', label: '运行环境', children: <Runtime runtime={runtimeDocument} /> }]} />
    </section>
  )
}

function Runtime({ runtime }: { runtime: Record<string, unknown> | null }) {
  const config = (runtime?.config ?? {}) as Record<string, unknown>
  return (
    <section>
      <Typography.Paragraph type="secondary">
        这些值来自进程启动配置，只读且敏感内容已经脱敏。修改环境变量后需要重启服务。
      </Typography.Paragraph>
      <div className="admin-runtime-grid">
        {Object.entries(config).map(([key, value]) => (
          <div key={key}>
            <Typography.Text type="secondary">{key}</Typography.Text>
            <Typography.Text>
              {Array.isArray(value) ? value.join(', ') : String(value ?? '')}
            </Typography.Text>
          </div>
        ))}
      </div>
    </section>
  )
}

function formatBytes(value: number) {
  if (value < 1024 * 1024) return `${Math.round(value / 1024)} KiB`
  if (value < 1024 * 1024 * 1024) return `${(value / 1024 / 1024).toFixed(1)} MiB`
  return `${(value / 1024 / 1024 / 1024).toFixed(1)} GiB`
}

function formatUsage(used: number, total: number) {
  if (total <= 0) return '未启用'
  return `${formatBytes(used)} / ${formatBytes(total)}`
}

function formatMemoryUsage(total: number, available: number) {
  return formatUsage(Math.max(0, total - available), total)
}

function formatUptime(seconds: number) {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
}

function formatDateTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}
