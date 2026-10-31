import assert from 'node:assert/strict'
import { after, describe, it } from 'node:test'
import { createServer } from 'vite'

import type { JsonSchema } from '../types/chat.ts'

// Use the existing Vite transformer to load the production TSX dependency.
const server = await createServer({
  configFile: false,
  server: { middlewareMode: true, watch: null, hmr: false },
  appType: 'custom',
})
after(() => server.close())
const { toolArgumentsToFormValues, validateToolArguments } = await server.ssrLoadModule('/src/lib/tool-arguments.ts') as typeof import('./tool-arguments')
const { buildToolCallPayload } = await server.ssrLoadModule('/src/hooks/chatWorkspace/buildToolCallPayload.ts') as typeof import('../hooks/chatWorkspace/buildToolCallPayload')

describe('assisted tool arguments', () => {
  for (const [type, value, choices] of [
    ['integer', 2, [1, 2, 3]],
    ['number', 1.5, [0.5, 1.5]],
    ['integer', 0, [0, 1]],
    ['string', '2', ['1', '2']],
  ] as const) {
    it(`preserves ${type} enum value ${JSON.stringify(value)} through form submission`, () => {
      const schema: JsonSchema = {
        type: 'object', properties: { level: { type, enum: [...choices] } }, required: ['level'],
      }
      const original = { level: value }
      validateToolArguments(original, schema)
      const values = toolArgumentsToFormValues(original, schema)
      // The Select uses the schema enum values as option values.
      assert.equal(values.level, value)
      const output = JSON.parse(buildToolCallPayload({
        selectedToolSchema: { parameters: schema }, toolFormValues: values, toolName: 'set_level',
      }))
      assert.deepEqual(output, original)
      validateToolArguments(output, schema)
    })
  }

  it('still formats ordinary numbers and structured values for text inputs', () => {
    const schema: JsonSchema = { type: 'object', properties: {
      count: { type: 'integer' }, options: { type: 'object' }, names: { type: 'array' },
    } }
    const input = { count: 3, options: { active: true }, names: ['first'] }
    const values = toolArgumentsToFormValues(input, schema)
    assert.equal(values.count, '3')
    assert.equal(typeof values.options, 'string')
    assert.equal(typeof values.names, 'string')
    assert.deepEqual(JSON.parse(buildToolCallPayload({
      selectedToolSchema: { parameters: schema }, toolFormValues: values, toolName: 'configure',
    })), input)
  })
})
