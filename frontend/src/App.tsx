import { useEffect, useRef, useState } from 'react'
import { Spin } from 'antd'
import { Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom'

import './App.css'
import { GithubButton } from './components/GithubButton'
import { HomepageScreen } from './components/HomepageScreen'
import { LoginScreen } from './components/LoginScreen'
import { ForgotPasswordScreen } from './components/ForgotPasswordScreen'
import { RegistrationScreen } from './components/RegistrationScreen'
import { type GeetestCaptcha } from './components/GeetestCaptchaField'
import { StatisticsPage } from './components/StatisticsPage'
import { ThemeToggle } from './components/ThemeToggle'
import { WorkspaceRoute } from './components/WorkspaceRoute'
import { useAuthSession } from './hooks/useAuthSession'
import { appMessage } from './lib/antdMessage'
import type { LoginFormValues } from './types/chat'
import { AdminSettingsPage } from './features/admin-settings/pages/AdminSettingsPage'

type LoginError = Error & {
  responseBody?: {
    totp_required?: boolean
  }
}

function RouteLoading() {
  return (
    <div className="boot-screen">
      <Spin size="large" />
    </div>
  )
}

function LoginRoute() {
  const navigate = useNavigate()
  const location = useLocation()
  const auth = useAuthSession()
  const [loading, setLoading] = useState(false)
  const [totpRequired, setTotpRequired] = useState(false)
  const captchaRef = useRef<GeetestCaptcha | null>(null)
  const localLoginRequested = new URLSearchParams(location.search).get('local') === '1'

  useEffect(() => {
    if (!auth.loading && !auth.session.authenticated && auth.session.oidc_enabled && !localLoginRequested) {
      window.location.assign('/api/auth/oidc/login')
    }
  }, [auth.loading, auth.session.authenticated, auth.session.oidc_enabled, localLoginRequested])

  async function handleSubmit(values: LoginFormValues) {
    setLoading(true)
    try {
      await auth.login(values)
      navigate('/app', { replace: true })
    } catch (error) {
      captchaRef.current?.reset()
      const loginError = error instanceof Error ? (error as LoginError) : null
      if (loginError?.responseBody?.totp_required) {
        setTotpRequired(true)
        appMessage.error(loginError.message || '请输入验证码')
      } else {
        appMessage.error(error instanceof Error ? error.message : '登录失败')
      }
    } finally {
      setLoading(false)
    }
  }

  if (auth.loading) {
    return <RouteLoading />
  }

  if (auth.session.authenticated) {
    return <Navigate to="/app" replace />
  }

  if (auth.session.oidc_enabled && !localLoginRequested) {
    return <RouteLoading />
  }

  return (
    <LoginScreen
      loading={loading}
      totpEnabled={auth.session.totp_enabled || totpRequired}
      registrationEnabled={auth.session.registration_enabled}
      geetestEnabled={auth.session.geetest_enabled}
      geetestCaptchaId={auth.session.geetest_captcha_id}
      geetestCaptchaRef={captchaRef}
      onSubmit={(values) => void handleSubmit(values)}
      onNavigateToRegister={() => navigate('/register')}
      onNavigateToForgotPassword={() => navigate('/forgot-password')}
    />
  )
}

function RegisterRoute() {
  const navigate = useNavigate()
  const auth = useAuthSession()

  if (auth.loading) {
    return <RouteLoading />
  }

  if (auth.session.authenticated) {
    return <Navigate to="/app" replace />
  }

  return (
    <RegistrationScreen
      onRegistered={() => navigate('/login')}
      onBackToLogin={() => navigate('/login')}
    />
  )
}

function StatisticsRoute() {
  const auth = useAuthSession()
  if (auth.loading) return <RouteLoading />
  if (auth.session.mode === 'lab') return <Navigate to="/app" replace />
  return <StatisticsPage />
}

function HomepageRoute() {
  const auth = useAuthSession()
  if (auth.loading) return <RouteLoading />
  return <HomepageScreen isLab={auth.session.mode === 'lab'} />
}

function ForgotPasswordRoute() {
  const navigate = useNavigate()
  const auth = useAuthSession()

  if (auth.loading) {
    return <RouteLoading />
  }

  if (auth.session.authenticated) {
    return <Navigate to="/app" replace />
  }

  return (
    <ForgotPasswordScreen
      onReset={() => navigate('/login')}
      onBackToLogin={() => navigate('/login')}
    />
  )
}

function App() {
  const location = useLocation()

  useEffect(() => {
    document.body.dataset.route = location.pathname
    return () => {
      delete document.body.dataset.route
    }
  }, [location.pathname])

  return (
    <>
	  {!location.pathname.startsWith('/app') && !location.pathname.startsWith('/admin') ? (
        <div className="global-theme-toggle-wrap">
          <div className="global-theme-toggle-group">
            <GithubButton />
            <ThemeToggle />
          </div>
        </div>
      ) : null}
      <Routes>
        <Route path="/" element={<HomepageRoute />} />
        <Route path="/login" element={<LoginRoute />} />
        <Route path="/register" element={<RegisterRoute />} />
        <Route path="/forgot-password" element={<ForgotPasswordRoute />} />
        <Route path="/stat" element={<StatisticsRoute />} />
        <Route path="/statistics" element={<StatisticsRoute />} />
        <Route path="/app/*" element={<WorkspaceRoute />} />
		<Route path="/admin/settings/*" element={<AdminSettingsPage />} />
        <Route path="*" element={<HomepageRoute />} />
      </Routes>
    </>
  )
}

export default App
