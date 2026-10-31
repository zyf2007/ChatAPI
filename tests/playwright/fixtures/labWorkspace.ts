import { randomUUID } from 'node:crypto'
import { expect, type APIRequestContext, type Page } from '../../../frontend/node_modules/@playwright/test'
import type { Conversation } from '../../../frontend/src/types/chat'
import { apiUrl } from './apiEndpoint'

/** Records actual API failures and calls to Serve-only features during a journey. */
export function auditLabRequests(page: Page) {
  const failures: string[] = []
  page.on('response', (response) => {
    const path = new URL(response.url()).pathname
    if (path.startsWith('/api/') && response.status() >= 400) {
      failures.push(`${response.status()} ${path}`)
    }
  })
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    const workspaceAPI = /^\/api\/user\/(virtual-models(?:\/[^/]+)?|config)$/.test(path)
      || /^\/api\/admin\/settings\/(media|access|catalog|overview|runtime)$/.test(path)
      || path === '/api/admin/monitor/stream' || path === '/api/admin/audit/logs'
    if (/^\/api\/(user|admin|automation|statistics)\//.test(path) && !workspaceAPI) {
      failures.push(`Serve-only API requested: ${path}`)
    }
  })
  page.on('pageerror', (error) => failures.push(error.message))
  return failures
}

export async function openLabSidebar(page: Page, isMobile: boolean) {
  if (isMobile) await page.locator('.menu-button').click()
  return page.locator(isMobile ? '.mobile-drawer .sidebar-inner' : '.sidebar .sidebar-inner')
}

/** Starts a real waiting turn without blocking the test until the operator replies. */
export async function startLabTurn(request: APIRequestContext) {
  const marker = `playwright-lab-${randomUUID()}`
  const controller = new AbortController()
  const response = fetch(apiUrl('v1/responses'), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ model: 'lab-test', input: marker }),
    signal: controller.signal,
  }).then(async (result) => ({ status: result.status, body: await result.json() }))
    .catch((error: Error) => ({ status: 0, body: { error: error.message } }))
  let conversation: Conversation | undefined
  try {
    await expect.poll(async () => {
      const result = await request.get('/api/lab/workspace')
      expect(result.ok()).toBeTruthy()
      const data = await result.json()
      conversation = data.conversations.find((item: Conversation) => item.last_user_text === marker)
      return conversation?.id
    }).toBeTruthy()
  } catch (error) {
    controller.abort()
    throw error
  }
  const created = conversation!
  return {
    conversation: created,
    response,
    async dispose() {
      try {
        const workspace = await (await request.get('/api/lab/workspace')).json()
        const existing = workspace.conversations.find((item: Conversation) => item.id === created.id)
        if (!existing) return
        if (existing.status === 'waiting' || existing.status === 'streaming') {
          const aborted = await request.post(apiUrl(`lab/requests/${created.request_id}/abort`), {
            data: { error: 'Playwright cleanup' },
          })
          expect(aborted.ok()).toBeTruthy()
          await response
        }
        expect((await request.delete(`/api/conversations/${created.id}`)).ok()).toBeTruthy()
      } finally {
        controller.abort()
      }
    },
  }
}

/** Opens shared settings consistently on desktop and mobile. */
export async function openLabSettings(page: Page, isMobile: boolean, tab: string) {
  await page.goto('/app')
  const sidebar = await openLabSidebar(page, isMobile)
  await sidebar.locator('.footer-settings-button').click()
  const modal = page.getByRole('dialog', { name: '设置', exact: true })
  await modal.getByRole('tab', { name: tab, exact: true }).click()
  return modal
}
