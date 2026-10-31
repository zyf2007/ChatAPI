import type { JsonSchema, MessageItem, ToolSchemaOption } from '../types/chat'

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

export function getLastToolSchemas(items: MessageItem[]): ToolSchemaOption[] {
  for (let index = items.length - 1; index >= 0; index -= 1) {
    const requestDebug = items[index]?.metadata?.request_debug
    if (!requestDebug) continue
    // Tool availability belongs to the latest request, even when it has no tools.
    const candidate = requestDebug.tool_schemas
    if (!Array.isArray(candidate) || candidate.length === 0) return []
    return candidate
      .map((item) => toToolSchemaOption(item))
      .filter((item): item is ToolSchemaOption => item !== null)
  }
  return []
}
