import type {
  JsonSchema,
  MessageItem,
  ToolFieldValue,
  ToolSchemaOption,
} from '../types/chat'

type RenderableContentPart =
  | { type: 'text'; text: string }
  | { type: 'image'; src: string; detail?: string }

function normalizeDisplayText(value: string): string {
  return value
    .replace(/\r\n/g, '\n')
    .replace(/\\r\\n/g, '\n')
    .replace(/\\n/g, '\n')
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
  const rawParameters = functionRecord.parameters ?? functionRecord.input_schema
  const parameters =
    rawParameters && typeof rawParameters === 'object'
      ? (rawParameters as JsonSchema)
      : { type: 'object', properties: {} }

  return {
    name: name.trim(),
    description,
    parameters,
  }
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

function isBase64DataImageUrl(value: string): boolean {
  return /^data:image\/[a-zA-Z0-9.+-]+;base64,/i.test(value.trim())
}

function tryParseStructuredContent(rawContent: string): unknown {
  try {
    return JSON.parse(rawContent)
  } catch {
    // Some mock payloads use Python repr style:
    // [{'type': 'input_image', 'image_url': 'data:image/png;base64,...'}]
  }

  const trimmed = rawContent.trim()
  if (!trimmed || !/^[\[{]/.test(trimmed)) return null

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
    ? [{ type: 'text', text: normalizeDisplayText(rawContent) } satisfies RenderableContentPart]
    : []

  const parsed = tryParseStructuredContent(rawContent)

  if (!Array.isArray(parsed)) return fallback

  const parts: RenderableContentPart[] = []
  for (const item of parsed) {
    if (!item || typeof item !== 'object') continue
    const record = item as Record<string, unknown>
    const itemType = String(record.type ?? '').trim()
    if (
      itemType === 'input_image' &&
      typeof record.image_url === 'string' &&
      isBase64DataImageUrl(record.image_url)
    ) {
      parts.push({
        type: 'image',
        src: record.image_url,
        detail:
          typeof record.detail === 'string' && record.detail.trim()
            ? record.detail.trim()
            : undefined,
      })
      continue
    }
    if (
      (itemType === 'input_text' ||
        itemType === 'output_text' ||
        itemType === 'text') &&
      typeof record.text === 'string' &&
      record.text.trim()
    ) {
      parts.push({ type: 'text', text: normalizeDisplayText(record.text) })
      continue
    }
    if (!itemType && typeof record.text === 'string' && record.text.trim()) {
      parts.push({ type: 'text', text: normalizeDisplayText(record.text) })
    }
  }

  return parts.length > 0 ? parts : fallback
}

export function renderMessageContent(rawContent: string) {
  const parts = parseRenderableContent(rawContent)
  if (parts.length === 0) return null

  return parts.map((part, index) => {
    if (part.type === 'image') {
      return (
        <figure key={`${part.src.slice(0, 32)}-${index}`} className="message-image-card">
          <img src={part.src} alt={`message image ${index + 1}`} className="message-image" />
          {part.detail ? <figcaption>detail: {part.detail}</figcaption> : null}
        </figure>
      )
    }
    return (
      <div key={`text-${index}`} className="message-text-block">
        {part.text}
      </div>
    )
  })
}
