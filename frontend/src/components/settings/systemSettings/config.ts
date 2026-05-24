import type { SystemConfig } from '../../../types/chat'

export const DEFAULT_SYSTEM_CONFIG: SystemConfig = {
  public_statistics: false,
  title_enabled: false,
  title: '',
  external_registration_enabled: false,
  email_verification_enabled: false,
  email_provider: '',
  email_provider_options: [],
  registration_email_domain_restriction_enabled: false,
  registration_email_domains: '',
  ntfy_private_url_policy: 'disabled',
  api_key_limit_per_user: 0,
  realtime_max_connections: 0,
  realtime_max_connections_per_user: 0,
  realtime_queue_size: 100,
  image_max_single_bytes: 0,
  image_max_request_bytes: 0,
  image_max_total_bytes: 0,
  image_usage: {
    total_bytes: 0,
    file_count: 0,
    orphan_bytes: 0,
    orphan_count: 0,
  },
}

export function normalizeSystemConfig(data: Partial<SystemConfig> & { ok?: boolean }): SystemConfig {
  const nextConfig: SystemConfig = {
    public_statistics: Boolean(data.public_statistics),
    title_enabled: Boolean(data.title_enabled),
    title: String(data.title ?? ''),
    external_registration_enabled: Boolean(data.external_registration_enabled),
    email_verification_enabled: Boolean(data.email_verification_enabled),
    email_provider: String(data.email_provider ?? ''),
    email_provider_options: Array.isArray(data.email_provider_options)
      ? data.email_provider_options
          .filter((option): option is { value: string; label: string } => Boolean(option?.value))
          .map((option) => ({
            value: String(option.value),
            label: String(option.label ?? option.value),
          }))
      : [],
    registration_email_domain_restriction_enabled: Boolean(data.registration_email_domain_restriction_enabled),
    registration_email_domains: String(data.registration_email_domains ?? ''),
    ntfy_private_url_policy: normalizeNtfyPrivateUrlPolicy(data.ntfy_private_url_policy),
    api_key_limit_per_user: Number(data.api_key_limit_per_user ?? 0),
    realtime_max_connections: Number(data.realtime_max_connections ?? 0),
    realtime_max_connections_per_user: Number(data.realtime_max_connections_per_user ?? 0),
    realtime_queue_size: Math.max(1, Number(data.realtime_queue_size ?? 100)),
    image_max_single_bytes: Number(data.image_max_single_bytes ?? 0),
    image_max_request_bytes: Number(data.image_max_request_bytes ?? 0),
    image_max_total_bytes: Number(data.image_max_total_bytes ?? 0),
    image_usage: data.image_usage ?? DEFAULT_SYSTEM_CONFIG.image_usage,
  }

  if (nextConfig.email_verification_enabled && !nextConfig.email_provider && nextConfig.email_provider_options.length > 0) {
    nextConfig.email_provider = nextConfig.email_provider_options[0].value
  }
  if (
    nextConfig.email_provider &&
    !nextConfig.email_provider_options.some((option) => option.value === nextConfig.email_provider)
  ) {
    nextConfig.email_provider = nextConfig.email_provider_options[0]?.value ?? ''
  }

  return nextConfig
}

export function normalizeNtfyPrivateUrlPolicy(value: unknown): SystemConfig['ntfy_private_url_policy'] {
  if (value === 'admin' || value === 'all') return value
  return 'disabled'
}

export function isRegistrationEmailDomainError(message: string) {
  return message.includes('邮箱域名') || message.includes('允许的域名')
}

export function formatBytes(value: number | undefined) {
  const bytes = Number(value ?? 0)
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
}
