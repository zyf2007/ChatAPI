export type AuthUser = {
  id: string
  username: string
  role: 'superadmin' | 'admin' | 'user'
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
  oidc_enabled?: boolean
  oidc_provider_name?: string
  local_password_login_enabled?: boolean
  email_verification_enabled?: boolean
}

export type User = {
  id: string
  username: string
  email: string
  role: 'superadmin' | 'admin' | 'user'
  is_active: boolean
  local_admin?: boolean
  created_at: string
  updated_at?: string
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
  key_prefix?: string
  scopes?: string[]
  created_at: string
  revoked_at?: string | null
  expires_at?: string | null
  last_used_at?: string | null
  api_key?: string
}

export type ModelKeyInfo = {
  id: string
  name: string
  model?: string
  key_prefix?: string
  created_at: string
  revoked_at?: string | null
  last_used_at?: string | null
  api_key?: string
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
  request_format?: 'responses' | 'chat_completions' | 'anthropic_messages' | string
  request_id?: string
  status?: 'waiting' | 'streaming' | 'closed' | 'aborted' | 'disconnected' | 'expired' | string
  draft_text?: string
  draft_output_segments?: TimelineMessageContentPart[]
}

export type TimelineMessageContentPart = {
  type: 'text' | 'image' | 'thinking' | string
  text?: string
  src?: string
  media_type?: string
  reasoning_stream_mode?: string
}

export type MessageItem = {
  id: string
  role: 'user' | 'assistant' | 'system' | string
  content: string
  content_parts?: TimelineMessageContentPart[]
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
    output_policy?: OutputPolicy
    request_debug?: {
      request_id?: string
      response_id?: string
      model?: string
      request_format?: 'responses' | 'chat_completions' | 'anthropic_messages' | string
      api_key_name?: string
      request_keys?: string[]
      input_text?: string
      tool_schemas?: unknown[]
      builtin_tools?: BuiltinToolOption[]
      option_chips?: OptionChip[]
      request_body?: unknown
      raw_request_body?: unknown
      request_options?: Record<string, unknown>
      request_headers?: {
        user_agent?: string
        content_type?: string
        origin?: string
        referer?: string
      }
    }
    [key: string]: unknown
  }
}

export type OptionChip = {
  key: string
  label: string
  value?: string
  protocol?: string
  category: 'request' | 'applied' | 'provider_specific' | 'unsupported' | string
  support_level:
    | 'applied'
    | 'normalized'
    | 'stored_only'
    | 'provider_specific'
    | 'unsupported'
    | 'partially_applied'
    | string
  detail?: unknown
}

export type OutputPolicyChip = {
  key: string
  label: string
  value?: string
  support_level: 'applied' | 'partially_applied' | string
}

export type OutputPolicy = {
  stop_sequence?: string
  applied_chips?: OutputPolicyChip[]
  finish_reason?: 'length' | 'stop_sequence' | string
  output_tokens?: number
  token_limit?: number
  token_counter?: string
  token_count_accuracy?: 'exact' | 'estimated' | string
}

export type ConversationEventItem = {
  id: string
  conversation_id: string
  owner_id: string
  type: string
  level: string
  title: string
  detail?: string
  request_id?: string
  metadata?: Record<string, unknown>
  created_at: string
}

export type TimelineItem = {
  id: string
  kind: 'message' | 'system_event' | string
  created_at: string
  message?: MessageItem
  event?: ConversationEventItem
  content_parts?: TimelineMessageContentPart[]
}

export type OutputImageAsset = {
  asset_id: string
  file_id: string
  conversation_id: string
  request_id: string
  url: string
  media_type: string
  bytes: number
  width: number
  height: number
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

export type BuiltinToolOption = {
  kind: 'web_search' | 'image_generation' | string
  type?: string
  label?: string
  raw?: Record<string, unknown>
}

export type ToolFieldValue = string | number | boolean
export type ComposerMode = 'assistant_message' | 'thinking' | 'tool_call' | 'builtin_tool'
export type ReasoningStreamMode = 'summery' | 'reasoning'
export type VisibleMessage = MessageItem & { draft?: boolean }
export type VisibleTimelineDraftItem = {
  id: string
  kind: 'draft'
  created_at: string
  draft: true
  content: string
  content_parts?: TimelineMessageContentPart[]
}
export type VisibleTimelineItem = TimelineItem | VisibleTimelineDraftItem
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

export type AutomationAction = {
  kind: 'stream_delta' | 'stream_complete' | 'respond' | 'abort' | 'builtin_tool' | string
  text?: string
  mode?: string
  tool_name?: string
  tool_call_id?: string
  output?: string
  builtin_tool_kind?: string
  builtin_tool_query?: string
  builtin_tool_asset_id?: string
  reasoning_stream_mode?: string
  error?: string
}

export type AutomationStep = {
  id: string
  delay_before_ms: number
  action: AutomationAction
}

export type AutomationRule = {
  schema_version: number
  id: string
  name: string
  enabled: boolean
  priority: number
  match: {
    target: 'last_user_text'
    pattern: string
  }
  playback: {
    mode: 'recorded' | 'fixed'
    initial_delay_ms: number
    fixed_interval_ms: number
    loop: boolean
    loop_interval_ms: number
  }
  steps: AutomationStep[]
  created_at?: string
  updated_at?: string
}

export type AutomationRecordingState = {
  revision: number
  active: boolean
  conversation_id?: string
  request_id?: string
  started_at?: string
  steps: AutomationStep[]
  draft_rule?: AutomationRule
  warning?: string
}

export type AutomationExecutionState = {
  revision: number
  rule_id: string
  conversation_id: string
  request_id: string
  status: 'running' | 'completed' | 'cancelled' | 'failed' | 'removed'
  step_index: number
  step_count: number
  cycle: number
  reason?: string
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
  type: 'workspace.snapshot'
  conversations: Conversation[]
  has_more: boolean
  next_cursor?: string
}

export type WorkspaceConversationPageEvent = {
  type: 'conversation.page'
  command_id: string
  conversations: Conversation[]
  has_more: boolean
  next_cursor?: string
}

export type WorkspaceConversationPageErrorEvent = {
  type: 'conversation.page.error'
  command_id: string
  code: string
  message: string
}

export type WorkspaceConversationUpsertEvent = {
  type: 'conversation.upsert'
  conversation: Conversation
}

export type WorkspaceConversationDeleteEvent = {
  type: 'conversation.remove'
  conversation_id: string
}

export type WorkspaceTimelineResetEvent = {
  type: 'timeline.reset'
  conversation_id: string
  items: TimelineItem[]
}

export type WorkspaceTimelineItemAppendEvent = {
  type: 'timeline.append'
  conversation_id: string
  item: TimelineItem
}

export type WorkspaceConnectionCountEvent = {
  type: 'workspace.connections'
  current_connection_count: number
}

export type WorkspaceCommand = {
  command_id: string
  kind: 'stream_delta' | 'stream_complete' | 'abort' | 'builtin_tool' | string
    conversation_id: string
    request_id: string
  text?: string
  mode?: string
  tool_name?: string
  tool_call_id?: string
  output?: string
  builtin_tool_kind?: string
  builtin_tool_query?: string
  builtin_tool_asset_id?: string
  reasoning_stream_mode?: string
  error?: string
}

export type WorkspaceCommandAckEvent = {
  type: 'workspace.command_ack'
  command_id: string
    conversation_id: string
    request_id: string
  auto_completed?: boolean
}

export type WorkspaceCommandErrorEvent = {
  type: 'workspace.command_error'
  command_id: string
    conversation_id?: string
    request_id?: string
  code: string
  message: string
}

export type AutomationRecordAckEvent = {
  type: 'automation.record.ack'
  command_id: string
  revision: number
  state: AutomationRecordingState
  executions?: AutomationExecutionState[]
}

export type AutomationRecordErrorEvent = {
  type: 'automation.record.error'
  command_id: string
  code: string
  message: string
}

export type AutomationRecordStateEvent = {
  type: 'automation.record.state'
  state: AutomationRecordingState
}

export type AutomationExecutionStateEvent = {
  type: 'automation.execution.state'
  execution: AutomationExecutionState
}
