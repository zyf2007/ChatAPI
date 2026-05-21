import { useEffect, useState } from 'react'

import { appMessage } from '../lib/antdApp'
import { requestJson } from '../lib/api'
import type { AuthSession, AuthUser, LoginFormValues } from '../types/chat'

const DEFAULT_SESSION: AuthSession = {
  authenticated: false,
  user: null,
  totp_enabled: false,
  registration_enabled: false,
  geetest_enabled: false,
  geetest_captcha_id: '',
  current_connection_count: 0,
  realtime_max_connections_per_user: 0,
}

export function useAuthSession() {
  const [session, setSession] = useState<AuthSession>(DEFAULT_SESSION)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let active = true

    async function loadSession() {
      try {
        const nextSession = await requestJson<AuthSession>('/api/auth/session')
        if (!active) return
        setSession(nextSession)
      } catch (error) {
        if (!active) return
        setSession(DEFAULT_SESSION)
        appMessage.error(error instanceof Error ? error.message : '无法初始化登录状态')
      } finally {
        if (active) {
          setLoading(false)
        }
      }
    }

    void loadSession()

    return () => {
      active = false
    }
  }, [])

  async function refreshSession() {
    const nextSession = await requestJson<AuthSession>('/api/auth/session')
    setSession(nextSession)
    return nextSession
  }

  async function login(values: LoginFormValues) {
    await requestJson<{ ok: boolean; user: AuthUser }>('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify(values),
    })
    return refreshSession()
  }

  async function logout() {
    await requestJson('/api/auth/logout', { method: 'POST' })
    setSession(DEFAULT_SESSION)
  }

  return {
    loading,
    login,
    logout,
    refreshSession,
    session,
  }
}
