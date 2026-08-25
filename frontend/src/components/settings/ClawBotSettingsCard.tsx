import { useEffect, useRef, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Flex,
  Input,
  Popconfirm,
  QRCode,
  Space,
  Spin,
  Tag,
  Typography,
} from 'antd'
import {
  DisconnectOutlined,
  LinkOutlined,
  ReloadOutlined,
  WechatOutlined,
} from '@ant-design/icons'

import { requestJson } from '../../lib/api'

const CLAWBOT_DOCS = 'https://developers.weixin.qq.com/doc/aispeech/knowledge/openapi/Clawbotrelated.html'

type ConnectionStatus = {
  provider: string
  connected: boolean
  ready: boolean
  worker_state: string
  reauth_required: boolean
  external_bot_id?: string
  connected_at?: string
  last_inbound_at?: string
  last_outbound_at?: string
  last_error?: string
  last_error_at?: string
}

type LoginState =
  | 'waiting'
  | 'scanned'
  | 'verify_required'
  | 'verify_blocked'
  | 'expired'
  | 'already_bound'
  | 'connected'

type LoginView = {
  session_id: string
  state: LoginState
  message: string
  qr_code_url?: string
  expires_at: string
  connection?: ConnectionStatus
}

const DISCONNECTED_STATUS: ConnectionStatus = {
  provider: 'clawbot',
  connected: false,
  ready: false,
  worker_state: 'disconnected',
  reauth_required: false,
}

function shouldAutoPoll(login: LoginView | null): boolean {
  return login?.state === 'waiting' || login?.state === 'scanned'
}

function formatDate(value?: string): string {
  if (!value) return '暂无'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '暂无' : date.toLocaleString()
}

export function ClawBotSettingsCard({ open }: { open: boolean }) {
  const [status, setStatus] = useState<ConnectionStatus | null>(null)
  const [login, setLogin] = useState<LoginView | null>(null)
  const [verifyCode, setVerifyCode] = useState('')
  const [loading, setLoading] = useState(false)
  const [connecting, setConnecting] = useState(false)
  const [disconnecting, setDisconnecting] = useState(false)
  const [error, setError] = useState('')
  const pollGeneration = useRef(0)
  const statusRequestVersion = useRef(0)

  useEffect(() => {
    if (!open) return
    const controller = new AbortController()
    let timer: number | undefined
    let initial = true
    async function loadStatus() {
      const requestVersion = ++statusRequestVersion.current
      if (initial) setLoading(true)
      try {
        const next = await requestJson<ConnectionStatus>('/api/user/im/clawbot', {
          signal: controller.signal,
        })
        if (!controller.signal.aborted && requestVersion === statusRequestVersion.current) {
          setStatus(next)
          setError('')
        }
      } catch (loadError) {
        if (!controller.signal.aborted && requestVersion === statusRequestVersion.current) {
          setError(loadError instanceof Error ? loadError.message : '微信连接状态加载失败')
        }
      } finally {
        if (!controller.signal.aborted) {
          if (initial) setLoading(false)
          initial = false
          timer = window.setTimeout(() => void loadStatus(), 15_000)
        }
      }
    }
    void loadStatus()
    return () => {
      controller.abort()
      if (timer !== undefined) window.clearTimeout(timer)
    }
  }, [open])

  const loginSessionID = login?.session_id
  const loginState = login?.state
  const loginExpiresAt = login?.expires_at

  useEffect(() => {
    if (!open || !loginSessionID || (loginState !== 'waiting' && loginState !== 'scanned')) return
    const sessionID = loginSessionID
    const generation = ++pollGeneration.current
    const controller = new AbortController()
    let timer: number | undefined

    async function poll() {
      if (loginExpiresAt && Date.parse(loginExpiresAt) <= Date.now()) {
        setLogin((current) => current ? { ...current, state: 'expired', message: '二维码已过期，请重新生成' } : current)
        return
      }
      try {
        const next = await requestJson<LoginView>(
          `/api/user/im/clawbot/login/${encodeURIComponent(sessionID)}/poll`,
          { method: 'POST', body: '{}', signal: controller.signal },
        )
        if (controller.signal.aborted || pollGeneration.current !== generation) return
        setLogin(next)
        setError('')
        if ((next.state === 'connected' || next.state === 'already_bound') && next.connection?.connected) {
          statusRequestVersion.current += 1
          setStatus(next.connection)
          setLogin(null)
          return
        }
        if (shouldAutoPoll(next)) timer = window.setTimeout(() => void poll(), 800)
      } catch (pollError) {
        if (controller.signal.aborted || pollGeneration.current !== generation) return
        setError(pollError instanceof Error ? pollError.message : '二维码状态查询失败')
        timer = window.setTimeout(() => void poll(), 2500)
      }
    }

    timer = window.setTimeout(() => void poll(), 300)
    return () => {
      controller.abort()
      if (timer !== undefined) window.clearTimeout(timer)
    }
  }, [open, loginExpiresAt, loginSessionID, loginState])

  async function startLogin() {
    setConnecting(true)
    setError('')
    setVerifyCode('')
    pollGeneration.current += 1
    try {
      const next = await requestJson<LoginView>('/api/user/im/clawbot/login', {
        method: 'POST',
        body: '{}',
      })
      setLogin(next)
    } catch (connectError) {
      setError(connectError instanceof Error ? connectError.message : '微信二维码创建失败')
    } finally {
      setConnecting(false)
    }
  }

  async function submitVerifyCode() {
    if (!login?.session_id || !verifyCode.trim()) return
    setConnecting(true)
    setError('')
    try {
      const next = await requestJson<LoginView>(
        `/api/user/im/clawbot/login/${encodeURIComponent(login.session_id)}/poll`,
        { method: 'POST', body: JSON.stringify({ verify_code: verifyCode.trim() }) },
      )
      setLogin(next)
      if ((next.state === 'connected' || next.state === 'already_bound') && next.connection?.connected) {
        statusRequestVersion.current += 1
        setStatus(next.connection)
        setLogin(null)
      }
    } catch (verifyError) {
      setError(verifyError instanceof Error ? verifyError.message : '验证码提交失败')
    } finally {
      setConnecting(false)
    }
  }

  async function disconnect() {
    setDisconnecting(true)
    setError('')
    pollGeneration.current += 1
    statusRequestVersion.current += 1
    try {
      await requestJson<void>('/api/user/im/clawbot', { method: 'DELETE' })
      setLogin(null)
      setStatus(DISCONNECTED_STATUS)
      setVerifyCode('')
    } catch (disconnectError) {
      setError(disconnectError instanceof Error ? disconnectError.message : '微信连接断开失败')
    } finally {
      setDisconnecting(false)
    }
  }

  const connected = Boolean(status?.connected)
  const needsReconnect = Boolean(
    status?.reauth_required
      || status?.worker_state === 'reauth_required'
      || status?.worker_state === 'error'
      || status?.worker_state === 'stopped',
  )
  const available = Boolean(connected && status?.ready && status.worker_state === 'running' && !needsReconnect)
  const terminalLogin = login && ['verify_blocked', 'expired', 'already_bound'].includes(login.state)
  const loginActive = Boolean(login && !terminalLogin && login.state !== 'connected')
  const disconnectControl = (
    <Popconfirm
      title="断开微信 ClawBot？"
      description="断开后将停止收取微信消息，已在处理的 ChatAPI 请求不会自动中止。"
      okText="断开"
      cancelText="取消"
      okButtonProps={{ danger: true, loading: disconnecting }}
      onConfirm={() => void disconnect()}
    >
      <Button danger icon={<DisconnectOutlined />}>断开连接</Button>
    </Popconfirm>
  )

  return (
    <section className="clawbot-settings" aria-labelledby="clawbot-settings-title">
      <Flex justify="space-between" align="flex-start" gap={16} wrap>
        <div>
          <Typography.Title level={4} id="clawbot-settings-title">
            <WechatOutlined /> 微信 ClawBot
          </Typography.Title>
          <Typography.Paragraph type="secondary">
            把新的待回复请求发送到扫码者微信。扫码者的普通文本回复会直接结束当前请求。
          </Typography.Paragraph>
        </div>
        <Button type="link" href={CLAWBOT_DOCS} target="_blank" rel="noreferrer" icon={<LinkOutlined />}>
          官方接口说明
        </Button>
      </Flex>

      {error ? <Alert className="clawbot-settings-alert" type="error" showIcon message={error} /> : null}
      {status?.last_error ? (
        <Alert
          className="clawbot-settings-alert"
          type={needsReconnect ? 'error' : 'warning'}
          showIcon
          message={status.last_error}
          description={status.last_error_at ? `发生时间：${formatDate(status.last_error_at)}` : undefined}
        />
      ) : null}

      <Spin spinning={loading}>
        <Card size="small" className="clawbot-connection-card">
          <Flex justify="space-between" align="center" gap={12} wrap>
            <Space wrap>
              <Typography.Text strong>连接状态</Typography.Text>
              {!connected ? <Tag>未连接</Tag> : null}
              {connected && !status?.ready && !needsReconnect ? <Tag color="gold">等待微信消息</Tag> : null}
              {connected && status?.ready && !available && !needsReconnect ? <Tag color="gold">正在启动</Tag> : null}
              {available ? <Tag color="green">可用</Tag> : null}
              {needsReconnect ? <Tag color="red">需要重新扫码</Tag> : null}
            </Space>
            {!connected ? (
              <Button
                type="primary"
                icon={<WechatOutlined />}
                loading={connecting}
                disabled={loginActive}
                onClick={() => void startLogin()}
              >
                {loginActive ? '等待扫码' : '连接微信'}
              </Button>
            ) : needsReconnect ? (
              <Space wrap>
                <Button
                  type="primary"
                  icon={<ReloadOutlined />}
                  loading={connecting}
                  disabled={loginActive}
                  onClick={() => void startLogin()}
                >
                  {loginActive ? '等待扫码' : '重新连接'}
                </Button>
                {disconnectControl}
              </Space>
            ) : disconnectControl}
          </Flex>

          {connected ? (
            <div className="clawbot-connection-meta">
              <Typography.Text type="secondary">连接时间：{formatDate(status?.connected_at)}</Typography.Text>
              <Typography.Text type="secondary">最近收到微信消息：{formatDate(status?.last_inbound_at)}</Typography.Text>
              <Typography.Text type="secondary">最近发送微信消息：{formatDate(status?.last_outbound_at)}</Typography.Text>
            </div>
          ) : null}

          {connected && !status?.ready ? (
            <Alert
              className="clawbot-settings-alert"
              type="info"
              showIcon
              message="请从扫码微信向 ClawBot 发送 /bind"
              description="收到第一条本人消息后，ChatAPI 才能取得回复上下文并主动推送新请求。"
            />
          ) : null}
        </Card>
      </Spin>

      {login ? (
        <Card size="small" className="clawbot-login-card" aria-live="polite">
          <Flex vertical align="center" gap={14}>
            {login.qr_code_url && !terminalLogin ? (
              <QRCode value={login.qr_code_url} size={196} bordered={false} />
            ) : null}
            <Typography.Text strong>{login.message}</Typography.Text>
            <Typography.Text type="secondary">
              二维码有效期至 {formatDate(login.expires_at)}。请只使用准备接收 ChatAPI 请求的微信扫码。
            </Typography.Text>
            {login.state === 'verify_required' ? (
              <Space.Compact className="clawbot-verify-code">
                <Input
                  value={verifyCode}
                  inputMode="numeric"
                  maxLength={12}
                  aria-label="微信验证码"
                  placeholder="输入微信显示的数字"
                  onChange={(event) => setVerifyCode(event.target.value.replace(/\D/g, ''))}
                  onPressEnter={() => void submitVerifyCode()}
                />
                <Button type="primary" loading={connecting} disabled={!verifyCode.trim()} onClick={() => void submitVerifyCode()}>
                  验证
                </Button>
              </Space.Compact>
            ) : null}
            {terminalLogin ? (
              <Button icon={<ReloadOutlined />} loading={connecting} onClick={() => void startLogin()}>
                重新生成二维码
              </Button>
            ) : null}
          </Flex>
        </Card>
      ) : null}

      <Alert
        className="clawbot-settings-alert"
        type="info"
        showIcon
        message="首版仅支持本人私聊文本"
        description={(
          <div className="clawbot-command-help">
            <Typography.Text>直接回复：结束当前请求</Typography.Text>
            <Typography.Text><code>/list</code> 查看等待请求；<code>/use &lt;编号&gt;</code> 切换请求</Typography.Text>
            <Typography.Text><code>/abort [原因]</code> 中止请求；<code>/help</code> 查看帮助</Typography.Text>
            <Typography.Text type="secondary">暂不支持流式片段、思考、工具调用、媒体或群聊。</Typography.Text>
          </div>
        )}
      />
    </section>
  )
}
