export type AuthUser = {
  id: string
  username: string
  role: 'admin' | 'user'
}

export type AuthSession = {
  authenticated: boolean
  user: AuthUser | null
  totp_enabled: boolean
  registration_enabled: boolean
  geetest_enabled: boolean
  geetest_captcha_id: string
  current_connection_count: number
  realtime_max_connections_per_user: number
}

export type User = {
  id: string
  username: string
  role: 'admin' | 'user'
  created_at: string
  last_login_at?: string
  api_key_count?: number
  current_connection_count?: number
}

export type AdminUserHistoryMessage = {
  id: string
  conversation_id: string
  conversation_title: string
  role: 'user' | 'assistant' | 'system' | string
  content: string
  status?: string
  response_id?: string | null
  metadata?: Record<string, unknown>
  created_at: string
}

export type AdminUserHistoryResponse = {
  ok: boolean
  user: User
  recent_messages: AdminUserHistoryMessage[]
}

export type ApiKeyInfo = {
  id: string
  name: string
  api_key: string
  created_at: string
}

export type ApiKeyListResponse = {
  ok: boolean
  api_keys: ApiKeyInfo[]
  api_key_limit_per_user: number
}

export type UserConfig = {
  ntfy_url_enabled: boolean
  ntfy_url: string
  messages_per_minute_limit_enabled: boolean
  messages_per_minute_limit: number
}

export type TotpSetup = {
  secret: string
  uri: string
  qr_base64: string
}

export type Conversation = {
  id: string
  title: string
  last_user_text: string
  created_at: string
  updated_at: string
  last_message_at: string
  message_count: number
  last_message_preview: string
  metadata?: {
    realtime_status?: 'waiting' | 'closed' | 'aborted' | string
    realtime_draft_text?: string
  }
}

export type MessageItem = {
  id: string
  role: 'user' | 'assistant' | 'system' | string
  content: string
  created_at: string
  status?: string
  response_id?: string | null
  metadata?: {
    provider?: string
    model?: string
    request_format?: 'responses' | 'chat_completions' | 'anthropic_messages' | string
    response_mode?: 'assistant_message' | 'tool_call' | 'tool_result' | string
    tool_name?: string
    tool_call_id?: string
    arguments?: string
    output?: string
    request_debug?: {
      request_id?: string
      response_id?: string
      model?: string
      request_format?: 'responses' | 'chat_completions' | 'anthropic_messages' | string
      api_key_name?: string
      request_keys?: string[]
      input_text?: string
      input_payload?: unknown
      tool_schemas?: unknown[]
      request_body?: unknown
      headers?: {
        user_agent?: string
        content_type?: string
        origin?: string
        referer?: string
      }
    }
    [key: string]: unknown
  }
}

export type ResponsesPayload = {
  conversation: Conversation
  output_text?: string
  output?: Array<{
    content?: Array<{ text?: string }>
  }>
}

export type JsonSchema = {
  type?: string | string[]
  title?: string
  description?: string
  enum?: Array<string | number | boolean | null>
  default?: unknown
  properties?: Record<string, JsonSchema>
  required?: string[]
  items?: JsonSchema
}

export type ToolSchemaOption = {
  name: string
  description: string
  parameters: JsonSchema
}

export type ToolFieldValue = string | number | boolean
export type ComposerMode = 'assistant_message' | 'thinking' | 'tool_call'
export type VisibleMessage = MessageItem & { draft?: boolean }
export type GeetestValidationResult = {
  lot_number: string
  captcha_output: string
  pass_token: string
  gen_time: string
}
export type LoginFormValues = {
  username: string
  password: string
  totp?: string
  geetest_params?: GeetestValidationResult
}

export type AutomationRuleCondition = {
  match_type: 'substring' | 'regex'
  pattern: string
}

export type AutomationRule = {
  id: string
  enabled: boolean
  conditions: {
    contains: AutomationRuleCondition[]
    excludes: AutomationRuleCondition[]
  }
  timing: {
    delay_seconds: number
    repeat_interval_seconds: number
  }
  action: {
    type: 'output_text' | 'complete' | 'error' | 'tool_call'
    text: string
    error_message: string
    tool_name?: string
    tool_arguments?: string
    tool_call_id?: string
  }
}

export type StatisticsSummary = {
  total_requests: number
  average_request_time_seconds: number
  average_tpm: number
  total_tokens: number
  input_tokens: number
  output_tokens: number
  start_at?: string | null
  end_at?: string | null
}

export type SystemConfig = {
  public_statistics: boolean
  title_enabled: boolean
  title: string
  external_registration_enabled: boolean
  email_verification_enabled: boolean
  email_provider: string
  email_provider_options: Array<{
    value: string
    label: string
  }>
  registration_email_domain_restriction_enabled: boolean
  registration_email_domains: string
  api_key_limit_per_user: number
  realtime_max_connections: number
  realtime_max_connections_per_user: number
  realtime_queue_size: number
  image_max_single_bytes: number
  image_max_request_bytes: number
  image_max_total_bytes: number
  image_usage?: {
    total_bytes: number
    file_count: number
    orphan_bytes: number
    orphan_count: number
  }
}

export type RegisterConfig = {
  registration_enabled: boolean
  email_verification_enabled: boolean
  registration_email_domain_restriction_enabled: boolean
  registration_email_domains: string
  geetest_enabled: boolean
  geetest_captcha_id: string
}

export type PasswordResetConfig = {
  password_reset_enabled: boolean
  geetest_enabled: boolean
  geetest_captcha_id: string
}

export type WorkspaceSnapshotEvent = {
  type: 'snapshot'
  conversations: Conversation[]
}

export type WorkspaceConversationUpsertEvent = {
  type: 'conversation_upsert'
  conversation: Conversation
  messages?: MessageItem[]
}

export type WorkspaceConversationDeleteEvent = {
  type: 'conversation_delete'
  conversation_id: string
}

export type WorkspaceConnectionCountEvent = {
  type: 'connection_count'
  current_connection_count: number
}
