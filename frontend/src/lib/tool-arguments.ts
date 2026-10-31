import Ajv, { type ErrorObject } from 'ajv'

import type { JsonSchema, ToolFieldValue } from '../types/chat'
import { getSchemaType, normalizeToolFieldValue } from './chat-format'

const ajv = new Ajv({ allErrors: true, strict: false })

function formatValidationError(error: ErrorObject) {
  const field = error.instancePath || '/'
  return `${field} ${error.message || '不符合 schema'}`
}

export function validateToolArguments(value: unknown, schema: JsonSchema): asserts value is Record<string, unknown> {
  let validate
  try {
    validate = ajv.compile(schema)
  } catch (error) {
    throw new Error('当前 Tool Schema 不是受支持的 JSON Schema draft-07 格式', { cause: error })
  }
  if (validate(value)) return
  throw new Error(`参数不符合 Tool Schema：${(validate.errors ?? []).map(formatValidationError).join('；')}`)
}

export function toolArgumentsToFormValues(
  value: Record<string, unknown>,
  schema: JsonSchema,
): Record<string, ToolFieldValue> {
  const properties = schema.properties ?? {}
  return Object.fromEntries(
    Object.entries(value).flatMap(([name, fieldValue]) => {
      const fieldSchema = properties[name]
      if (!fieldSchema) return []
      if (fieldSchema.enum?.length) return [[name, fieldValue as ToolFieldValue]]
      const type = getSchemaType(fieldSchema)
      if (type === 'object' || type === 'array') {
        return [[name, JSON.stringify(fieldValue, null, 2)]]
      }
      if (type === 'number' || type === 'integer') return [[name, String(fieldValue)]]
      return [[name, fieldValue as ToolFieldValue]]
    }),
  )
}

export function formValuesToToolArguments(
  values: Record<string, ToolFieldValue>,
  schema: JsonSchema,
): Record<string, unknown> {
  const properties = schema.properties ?? {}
  const required = new Set(schema.required ?? [])
  const result = Object.fromEntries(
    Object.entries(properties).flatMap(([name, fieldSchema]) => {
      const rawValue = values[name]
      if (rawValue == null || rawValue === '') {
        if (required.has(name)) throw new Error(`请填写必填参数: ${name}`)
        return []
      }
      return [[name, normalizeToolFieldValue(rawValue, fieldSchema)]]
    }),
  )
  return result
}
