import { useMemo, useState } from 'react'

import type { SystemConfig } from '../../../types/chat'
import { DEFAULT_SYSTEM_CONFIG } from './config'

export function useSystemSettingsState() {
  const [config, setConfig] = useState<SystemConfig>(DEFAULT_SYSTEM_CONFIG)
  const [savedConfig, setSavedConfig] = useState<SystemConfig>(DEFAULT_SYSTEM_CONFIG)
  const [, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [testEmail, setTestEmail] = useState('')
  const [sendingTest, setSendingTest] = useState(false)
  const [registrationEmailDomainsError, setRegistrationEmailDomainsError] = useState('')

  const dirtyState = useMemo(
    () => ({
      public_statistics: config.public_statistics !== savedConfig.public_statistics,
      title: config.title_enabled !== savedConfig.title_enabled || config.title !== savedConfig.title,
      registration:
        config.external_registration_enabled !== savedConfig.external_registration_enabled
        || config.email_verification_enabled !== savedConfig.email_verification_enabled
        || config.email_provider !== savedConfig.email_provider
        || config.registration_email_domain_restriction_enabled !== savedConfig.registration_email_domain_restriction_enabled
        || config.registration_email_domains !== savedConfig.registration_email_domains
        || config.api_key_limit_per_user !== savedConfig.api_key_limit_per_user,
      ntfy: config.ntfy_private_url_policy !== savedConfig.ntfy_private_url_policy,
      realtime:
        config.realtime_max_connections !== savedConfig.realtime_max_connections
        || config.realtime_max_connections_per_user !== savedConfig.realtime_max_connections_per_user
        || config.realtime_queue_size !== savedConfig.realtime_queue_size,
      images:
        config.image_max_single_bytes !== savedConfig.image_max_single_bytes
        || config.image_max_request_bytes !== savedConfig.image_max_request_bytes
        || config.image_max_total_bytes !== savedConfig.image_max_total_bytes,
    }),
    [config, savedConfig],
  )

  function updateSection<K extends keyof SystemConfig>(key: K, value: SystemConfig[K]) {
    if (key === 'registration_email_domains') {
      setRegistrationEmailDomainsError('')
    }
    if (key === 'registration_email_domain_restriction_enabled' && !value) {
      setRegistrationEmailDomainsError('')
    }
    if (key === 'email_verification_enabled' && value && !config.email_provider) {
      const fallbackProvider = config.email_provider_options[0]?.value ?? ''
      if (fallbackProvider) {
        setConfig((current) => ({ ...current, [key]: value, email_provider: fallbackProvider }))
        return
      }
    }
    setConfig((current) => ({ ...current, [key]: value }))
  }

  return {
    config,
    dirtyState,
    hasUnsavedChanges: Object.values(dirtyState).some(Boolean),
    registrationEmailDomainsError,
    savedConfig,
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
  }
}
