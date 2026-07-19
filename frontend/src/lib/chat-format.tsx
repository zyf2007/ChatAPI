import type {
  JsonSchema,
  MessageItem,
  TimelineMessageContentPart,
  BuiltinToolOption,
  ToolFieldValue,
  ToolSchemaOption,
} from '../types/chat'

type RenderableContentPart =
  | { type: 'text'; text: string }
  | { type: 'thinking'; text: string }
  | { type: 'image'; src: string; detail?: string }

type RenderMessageContentOptions = {
  onImageClick?: (src: string, detail?: string, alt?: string) => void
}

export function normalizeChatText(value: string): string {
  return value
    .replace(/\r\n/g, '\n')
    .replace(/\\r\\n/g, '\n')
    .replace(/\\n/g, '\n')
}

// Legacy-only: historical messages stored thinking inside <think> tags in Content.
// New messages must arrive as typed content_parts and never pass through this path.
function splitLegacyThinkingBlocks(value: string): RenderableContentPart[] {
  const normalized = normalizeChatText(value)
  const pattern = /<think(?:\s[^>]*)?>\s*([\s\S]*?)\s*<\/think>/gi
  const parts: RenderableContentPart[] = []
  let lastIndex = 0
  let match: RegExpExecArray | null

  while ((match = pattern.exec(normalized)) !== null) {
    const before = normalized.slice(lastIndex, match.index).trim()
    if (before) {
      parts.push({ type: 'text', text: before })
    }

    const thinkingText = String(match[1] ?? '').trim()
    if (thinkingText) {
      parts.push({ type: 'thinking', text: thinkingText })
    }
    lastIndex = pattern.lastIndex
  }

  const after = normalized.slice(lastIndex).trim()
  if (after) {
    parts.push({ type: 'text', text: after })
  }

  return parts.length > 0 ? parts : [{ type: 'text', text: normalized }]
}


export function formatTime(value: string) {
  if (!value) return ''
  return new Intl.DateTimeFormat('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(value))
}

export function formatJson(value: unknown) {
  if (value == null) return ''
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

function toToolSchemaOption(schema: unknown): ToolSchemaOption | null {
  if (!schema || typeof schema !== 'object') return null

  const record = schema as Record<string, unknown>
  const functionRecord =
    record.type === 'function' &&
    record.function &&
    typeof record.function === 'object'
      ? (record.function as Record<string, unknown>)
      : record

  const name = functionRecord.name
  if (typeof name !== 'string' || !name.trim()) return null

  const description =
    typeof functionRecord.description === 'string' ? functionRecord.description : ''
  const rawParameters =
    functionRecord.parameters && typeof functionRecord.parameters === 'object'
      ? functionRecord.parameters
      : functionRecord.input_schema && typeof functionRecord.input_schema === 'object'
        ? functionRecord.input_schema
        : record.parameters && typeof record.parameters === 'object'
          ? record.parameters
          : record.input_schema && typeof record.input_schema === 'object'
            ? record.input_schema
        : null
  const parameters = normalizeToolParameters(rawParameters)

  return {
    name: name.trim(),
    description,
    parameters,
  }
}

function normalizeToolParameters(parameters: unknown): JsonSchema {
  if (!parameters || typeof parameters !== 'object') {
    return { type: 'object', properties: {} }
  }
  const schema = parameters as JsonSchema
  if (!schema.type && schema.properties) {
    return { ...schema, type: 'object' }
  }
  return schema
}

export function getSchemaType(schema?: JsonSchema): string {
  if (!schema?.type) return ''
  return Array.isArray(schema.type) ? String(schema.type[0] ?? '') : schema.type
}

export function getLastToolSchemas(items: MessageItem[]): ToolSchemaOption[] {
  for (let index = items.length - 1; index >= 0; index -= 1) {
    const candidate = items[index]?.metadata?.request_debug?.tool_schemas
    if (!Array.isArray(candidate) || candidate.length === 0) continue
    return candidate
      .map((item) => toToolSchemaOption(item))
      .filter((item): item is ToolSchemaOption => item !== null)
  }
  return []
}

export function getLastBuiltinTools(items: MessageItem[]): BuiltinToolOption[] {
  for (let index = items.length - 1; index >= 0; index -= 1) {
    const requestDebug = items[index]?.metadata?.request_debug
    if (!requestDebug) continue
    const candidate = requestDebug.builtin_tools
    if (!Array.isArray(candidate) || candidate.length === 0) return []
    return candidate
      .map((item) => normalizeBuiltinTool(item))
      .filter((item): item is BuiltinToolOption => item !== null)
  }
  return []
}

function normalizeBuiltinTool(item: unknown): BuiltinToolOption | null {
  if (!item || typeof item !== 'object') return null
  const record = item as Record<string, unknown>
  const kind = typeof record.kind === 'string' ? record.kind.trim() : ''
  if (!kind) return null
  return {
    kind,
    type: typeof record.type === 'string' ? record.type : undefined,
    label: typeof record.label === 'string' ? record.label : undefined,
    raw: record.raw && typeof record.raw === 'object' ? record.raw as Record<string, unknown> : undefined,
  }
}

export function buildInitialToolFormValues(schema?: JsonSchema) {
  const values: Record<string, ToolFieldValue> = {}
  const properties = schema?.properties ?? {}
  for (const [key, propertySchema] of Object.entries(properties)) {
    const type = getSchemaType(propertySchema)
    if (propertySchema.default == null) continue
    if (
      type === 'string' ||
      type === 'number' ||
      type === 'integer' ||
      type === 'boolean'
    ) {
      values[key] = propertySchema.default as ToolFieldValue
    } else {
      values[key] = formatJson(propertySchema.default)
    }
  }
  return values
}

export function normalizeToolFieldValue(value: unknown, schema?: JsonSchema) {
  const type = getSchemaType(schema)
  if (value == null || value === '') return undefined

  if (schema?.enum?.length) {
    return value
  }

  if (type === 'number' || type === 'integer') {
    return typeof value === 'number' ? value : Number(value)
  }
  if (type === 'boolean') {
    return Boolean(value)
  }
  if (type === 'array' || type === 'object') {
    if (typeof value !== 'string') return value
    return JSON.parse(value)
  }
  return typeof value === 'string' ? value : String(value)
}

function isHostedImageUrl(value: string): boolean {
  return /^(?:https?:\/\/[^/]+)?\/api\/media\/assets\/[A-Za-z0-9._-]+(?:\?.*)?$/i.test(
    value.trim(),
  )
}

function isRenderableImageUrl(value: string): boolean {
  return isHostedImageUrl(value)
}

function tryParseStructuredContent(rawContent: string): unknown {
  try {
    return JSON.parse(rawContent)
  } catch {
    // Some mock payloads use Python repr style:
    // [{'type': 'input_image', 'image_url': '/api/media/assets/...'}]
  }

  const trimmed = rawContent.trim()
  if (!trimmed || (!trimmed.startsWith('[') && !trimmed.startsWith('{'))) return null

  let normalized = ''
  let inSingleQuote = false
  let inDoubleQuote = false
  let escapeNext = false

  for (const char of trimmed) {
    if (escapeNext) {
      normalized += char
      escapeNext = false
      continue
    }
    if (char === '\\') {
      normalized += char
      escapeNext = true
      continue
    }
    if (char === "'" && !inDoubleQuote) {
      normalized += '"'
      inSingleQuote = !inSingleQuote
      continue
    }
    if (char === '"' && !inSingleQuote) {
      normalized += char
      inDoubleQuote = !inDoubleQuote
      continue
    }
    normalized += inSingleQuote && char === '"' ? '\\"' : char
  }

  normalized = normalized
    .replace(/\bNone\b/g, 'null')
    .replace(/\bTrue\b/g, 'true')
    .replace(/\bFalse\b/g, 'false')

  try {
    return JSON.parse(normalized)
  } catch {
    return null
  }
}

function parseRenderableContent(rawContent: string): RenderableContentPart[] {
  const fallback = rawContent.trim()
    ? isRenderableImageUrl(rawContent.trim())
      ? [{ type: 'image', src: rawContent.trim() } satisfies RenderableContentPart]
      : splitLegacyThinkingBlocks(rawContent)
    : []

  const parsed = tryParseStructuredContent(rawContent)

  const parts: RenderableContentPart[] = []

  const visit = (value: unknown): void => {
    if (value == null) return
    if (typeof value === 'string') {
      if (isRenderableImageUrl(value)) {
        parts.push({ type: 'image', src: value.trim() })
      } else if (value.trim()) {
        parts.push(...splitLegacyThinkingBlocks(value))
      }
      return
    }
    if (Array.isArray(value)) {
      for (const item of value) visit(item)
      return
    }
    if (typeof value !== 'object') return

    const record = value as Record<string, unknown>
    const itemType = String(record.type ?? '').trim().toLowerCase()
    const imageCandidate =
      typeof record.image_url === 'string'
        ? record.image_url
        : typeof record.url === 'string'
          ? record.url
          : typeof record.src === 'string'
            ? record.src
            : typeof record.data === 'string' && isRenderableImageUrl(record.data)
              ? record.data
              : ''

    if (imageCandidate && isRenderableImageUrl(imageCandidate)) {
      parts.push({
        type: 'image',
        src: imageCandidate.trim(),
        detail:
          typeof record.detail === 'string' && record.detail.trim()
            ? record.detail.trim()
            : undefined,
      })
      return
    }

    if (
      (itemType === 'input_image' ||
        itemType === 'output_image' ||
        itemType === 'image' ||
        (itemType === 'file' && typeof record.image_url === 'string')) &&
      typeof record.image_url === 'string' &&
      isRenderableImageUrl(record.image_url)
    ) {
      parts.push({
        type: 'image',
        src: record.image_url.trim(),
        detail:
          typeof record.detail === 'string' && record.detail.trim()
            ? record.detail.trim()
            : undefined,
      })
      return
    }

    if (
      typeof record.text === 'string' &&
      record.text.trim() &&
      (itemType === 'input_text' ||
        itemType === 'output_text' ||
        itemType === 'text' ||
        !itemType)
    ) {
      parts.push(...splitLegacyThinkingBlocks(record.text))
      return
    }

    if (typeof record.content === 'string' && record.content.trim()) {
      visit(record.content)
      return
    }

    for (const [childKey, childValue] of Object.entries(record)) {
      if (childKey === 'type') continue
      visit(childValue)
    }
  }

  if (parsed == null) return fallback
  visit(parsed)
  return parts.length > 0 ? parts : fallback
}

export function renderMessageContent(
  rawContent: string,
  options: RenderMessageContentOptions = {},
  contentParts?: TimelineMessageContentPart[],
) {
  const { onImageClick } = options
  const parts: RenderableContentPart[] = Array.isArray(contentParts) && contentParts.length > 0
    ? contentParts.flatMap((part): RenderableContentPart[] => {
        if (part.type === 'image' && part.src) {
          return [{ type: 'image', src: part.src, detail: part.media_type }]
        }
        // Typed segments are authoritative: never re-parse <think> out of answer text.
        if (part.type === 'thinking' && part.text) {
          return [{ type: 'thinking', text: part.text }]
        }
        if ((part.type === 'text' || !part.type) && part.text) {
          return [{ type: 'text', text: part.text }]
        }
        return []
      })
    : parseRenderableContent(rawContent)
  if (parts.length === 0) return null

  const nodes: React.ReactNode[] = []
  let pendingThinking: string[] = []

  const flushThinking = (key: string) => {
    if (pendingThinking.length === 0) return
    nodes.push(
      <details key={key} className="message-thinking-card">
        <summary>
          <span>思考内容</span>
          <span className="message-thinking-hint">点击展开</span>
        </summary>
        <div className="message-thinking-body">{pendingThinking.join('')}</div>
      </details>,
    )
    pendingThinking = []
  }

  parts.forEach((part, index) => {
    if (part.type === 'thinking') {
      pendingThinking.push(part.text)
      return
    }

    flushThinking(`thinking-${index}`)

    if (part.type === 'image') {
      nodes.push(
        <figure key={`${part.src.slice(0, 32)}-${index}`} className="message-image-card">
          <button
            type="button"
            className="message-image-button"
            onClick={() => onImageClick?.(part.src, part.detail, `message image ${index + 1}`)}
            aria-label={`放大查看图片 ${index + 1}`}
            title="点击在网页内全屏查看"
          >
            <img src={part.src} alt={`message image ${index + 1}`} className="message-image" />
          </button>
          {part.detail ? <figcaption>detail: {part.detail}</figcaption> : null}
        </figure>,
      )
      return
    }

    nodes.push(
      <div key={`text-${index}`} className="message-text-block">
        {part.text}
      </div>,
    )
  })

  flushThinking(`thinking-${parts.length}`)
  return nodes
}

export function buildCurlCommand(requestBody: unknown): string {
  if (requestBody == null) return ''
  const origin = window.location.origin
  const format = (requestBody as Record<string, unknown>)?.model != null
    && 'input' in (requestBody as Record<string, unknown>)
    && !('messages' in (requestBody as Record<string, unknown>))
    && !('max_tokens' in (requestBody as Record<string, unknown>))
    ? 'responses'
    : (requestBody as Record<string, unknown>)?.messages != null
      ? 'chat_completions'
      : 'anthropic'

  let endpoint = '/v1/responses'
  if (format === 'chat_completions') endpoint = '/v1/chat/completions'
  else if (format === 'anthropic') endpoint = '/v1/messages'

  const body = JSON.stringify(requestBody, null, 2)
  return `curl '${origin}${endpoint}' \\\n  -H 'Content-Type: application/json' \\\n  -H 'Authorization: Bearer YOUR_API_KEY' \\\n  -d '${body}'`
}
