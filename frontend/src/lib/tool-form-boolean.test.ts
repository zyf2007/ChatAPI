import assert from 'node:assert/strict'
import { after, describe, it } from 'node:test'
import { createServer } from 'vite'

import type { JsonSchema } from '../types/chat.ts'

// Load the actual TSX implementation without changing its production imports.
const server = await createServer({
  configFile: false,
  server: { middlewareMode: true, watch: null, hmr: false },
  appType: 'custom',
})
after(() => server.close())
const { buildInitialToolFormValues } = await server.ssrLoadModule('/src/lib/chat-format.tsx') as typeof import('./chat-format')
const { validateToolArguments } = await server.ssrLoadModule('/src/lib/tool-arguments.ts') as typeof import('./tool-arguments')
const { buildToolCallPayload } = await server.ssrLoadModule('/src/hooks/chatWorkspace/buildToolCallPayload.ts') as typeof import('../hooks/chatWorkspace/buildToolCallPayload')

describe('boolean tool fields', () => {
  it('submits the initial unchecked state for required booleans without a default', () => {
    const schema: JsonSchema = {
      type: 'object', properties: { enabled: { type: 'boolean' } }, required: ['enabled'],
    }
    const values = buildInitialToolFormValues(schema)
    assert.equal(values.enabled, false)
    const output = JSON.parse(buildToolCallPayload({
      selectedToolSchema: { parameters: schema }, toolFormValues: values, toolName: 'set_enabled',
    }))
    assert.deepEqual(output, { enabled: false })
    validateToolArguments(output, schema)
  })

  it('preserves explicit true and false defaults', () => {
    for (const value of [true, false]) {
      assert.deepEqual(buildInitialToolFormValues({
        properties: { enabled: { type: 'boolean', default: value } }, required: ['enabled'],
      }), { enabled: value })
    }
  })

  it('keeps optional booleans absent when no default is specified', () => {
    const schema: JsonSchema = { type: 'object', properties: { enabled: { type: 'boolean' } } }
    const values = buildInitialToolFormValues(schema)
    assert.deepEqual(values, {})
    assert.equal(buildToolCallPayload({
      selectedToolSchema: { parameters: schema }, toolFormValues: values, toolName: 'configure',
    }), '{}')
  })

  it('does not invent a false selection for a boolean enum', () => {
    assert.deepEqual(buildInitialToolFormValues({
      properties: { enabled: { type: 'boolean', enum: [true] } }, required: ['enabled'],
    }), {})
  })
})
