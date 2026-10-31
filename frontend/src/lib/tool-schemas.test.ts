import assert from 'node:assert/strict'
import { describe, it } from 'node:test'

import type { MessageItem } from '../types/chat.ts'
import { getLastToolSchemas } from './tool-schemas.ts'

function requestMessage(requestId: string, tools?: unknown): MessageItem {
  // Pass through wire values, including null, as emitted by the backend.
  return JSON.parse(JSON.stringify({
    id: requestId,
    role: 'user',
    content: 'Hello',
    created_at: '2026-01-01T00:00:00Z',
    metadata: { request_debug: { request_id: requestId, tool_schemas: tools } },
  })) as MessageItem
}

const parameters = { type: 'object', properties: { city: { type: 'string' } } }
const previousRequest = requestMessage('previous', [
  { type: 'function', function: { name: 'lookup_weather', parameters } },
])
const assistantMessage: MessageItem = {
  id: 'assistant',
  role: 'assistant',
  content: 'Hello',
  created_at: '2026-01-01T00:00:01Z',
  metadata: { response_mode: 'assistant_message' },
}

describe('getLastToolSchemas', () => {
  for (const [label, tools] of [
    ['empty', []],
    ['null', null],
    ['omitted', undefined],
    ['malformed', {}],
  ] as const) {
    it(`does not reuse previous tools when the latest request has ${label} tools`, () => {
      assert.deepEqual(getLastToolSchemas([
        previousRequest,
        requestMessage('latest', tools),
        assistantMessage,
      ]), [])
    })
  }

  it('ignores messages without request metadata when finding the latest request', () => {
    assert.deepEqual(getLastToolSchemas([previousRequest, assistantMessage]), [
      { name: 'lookup_weather', description: '', parameters },
    ])
  })

  it('uses the latest request tools instead of merging historical tools', () => {
    assert.deepEqual(getLastToolSchemas([
      previousRequest,
      requestMessage('latest', [{ type: 'function', name: 'lookup_city', parameters }]),
      assistantMessage,
    ]), [{ name: 'lookup_city', description: '', parameters }])
  })

  it('preserves Anthropic input schemas and tool descriptions', () => {
    assert.deepEqual(getLastToolSchemas([
      requestMessage('latest', [{ name: 'lookup_city', description: 'Find a city', input_schema: parameters }]),
    ]), [{ name: 'lookup_city', description: 'Find a city', parameters }])
  })

  it('does not fall back to historical tools when the latest tool entries are invalid', () => {
    assert.deepEqual(getLastToolSchemas([
      previousRequest,
      requestMessage('latest', [null, {}, { name: ' ' }]),
    ]), [])
  })

  it('returns no tools when there are no request messages', () => {
    assert.deepEqual(getLastToolSchemas([]), [])
    assert.deepEqual(getLastToolSchemas([assistantMessage]), [])
  })
})
