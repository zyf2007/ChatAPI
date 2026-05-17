export type AuthUser = {
  username: string
}

export type AuthSession = {
  authenticated: boolean
  user: AuthUser | null
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
    response_mode?: 'assistant_message' | 'tool_call' | 'tool_result' | string
    tool_name?: string
    tool_call_id?: string
    arguments?: string
    output?: string
    request_debug?: {
      request_id?: string
      response_id?: string
      model?: string
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
export type ComposerMode = 'assistant_message' | 'tool_call'
export type VisibleMessage = MessageItem & { draft?: boolean }
export type LoginFormValues = {
  username: string
  password: string
}

export type StreamHeartbeatConfig = {
  heartbeat_text: string
  heartbeat_interval_seconds: number
}
