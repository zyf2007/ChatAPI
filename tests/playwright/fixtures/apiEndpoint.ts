import assert from 'node:assert/strict'

export type ApiEndpointConfig = {
  baseUrl: string
  apiKey?: string
  model?: string
}

function readUrl(name: string, fallback: string): string {
  const value = (process.env[name] || fallback).trim().replace(/\/$/, '')
  assert.match(value, /^https?:\/\//, `${name} must be an absolute http(s) URL`)
  return value
}

/** Shared endpoint setup for browser tests that need an OpenAI-compatible API. */
export function apiEndpoint(): ApiEndpointConfig {
  return {
    baseUrl: readUrl('CHATAPI_TEST_API_BASE_URL', 'http://127.0.0.1:5000'),
    apiKey: process.env.CHATAPI_TEST_API_KEY?.trim() || undefined,
    model: process.env.CHATAPI_TEST_MODEL?.trim() || undefined,
  }
}

export function apiUrl(path: string): string {
  const base = apiEndpoint().baseUrl
  return `${base}/${path.replace(/^\//, '')}`
}
