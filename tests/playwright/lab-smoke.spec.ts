import { expect, test } from '../../frontend/node_modules/@playwright/test'
import { auditLabRequests, openLabSidebar, startLabTurn } from './fixtures/labWorkspace'
import { apiUrl } from './fixtures/apiEndpoint'

test.describe('local Lab workspace', () => {
  test.skip(!process.env.CHATAPI_TEST_LAB, 'Set CHATAPI_TEST_LAB=1 after starting `go run ./cmd/chatapi lab`')

  test('loads the workspace and exposes workspace settings without account controls', async ({ page, request, isMobile }) => {
    const failures = auditLabRequests(page)
    const sessionResponse = page.waitForResponse((response) => response.url().endsWith('/api/auth/session'))
    await page.goto('/app')
    const session = await (await sessionResponse).json()
    expect(session).toMatchObject({ mode: 'lab', authenticated: true, user: { id: 'lab_default', role: 'admin' } })
    await expect(page).toHaveURL(/\/app/)
    await expect(page.locator('body')).not.toContainText('无法初始化登录状态')

    const response = await request.get('/api/lab/workspace')
    expect(response.ok()).toBeTruthy()
    const workspace = await response.json()
    expect(workspace).toMatchObject({ ok: true, mode: 'lab', conversations: expect.any(Array) })
    await expect(page.getByRole('button', { name: '录制自动化' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '退出', exact: true })).toHaveCount(0)
    const sidebar = await openLabSidebar(page, isMobile)
    await expect(sidebar).toContainText('Lab 工作区')
    await sidebar.locator('.footer-settings-button').click()
    const modal = page.getByRole('dialog', { name: '设置', exact: true })
    await expect(modal.getByRole('tab')).toHaveText(['模型列表', '辅助模型', '工作区设置', '媒体设置', '实例设置'])
    await modal.getByRole('tab', { name: '辅助模型', exact: true }).click()
    await expect(modal.getByLabel('Base URL')).toBeVisible()
    await expect(modal.getByLabel('模型', { exact: true })).toBeVisible()
    expect(failures).toEqual([])
  })

  test('hides statistics on the homepage', async ({ page }) => {
    const failures = auditLabRequests(page)
    await page.goto('/')
    const frame = page.frameLocator('iframe[title="ChatAPI Homepage"]')
    await expect(frame.locator('a[href="/statistics"]')).toBeHidden()
    await expect(frame.locator('a[href="/login"]')).toBeVisible()
    expect(failures).toEqual([])
  })

  test('redirects Serve-only routes before their API consumers mount', async ({ page }) => {
    const failures = auditLabRequests(page)
    for (const path of [
      '/stat', '/statistics', '/login', '/register', '/forgot-password',
      ...['users', 'auth', 'automation'].map((section) => `/admin/settings/${section}`),
    ]) {
      await page.goto(path)
      await expect(page).toHaveURL(/\/app$/)
      await expect(page.locator('.chat-pane')).toBeVisible()
    }
    expect(failures).toEqual([])
  })

  test('answers a real Lab request and deletes only that conversation', async ({ page, request, isMobile }) => {
    const failures = auditLabRequests(page)
    const turn = await startLabTurn(request)
    try {
      await page.goto('/app')
      const sidebar = await openLabSidebar(page, isMobile)
      const item = sidebar.locator(`[data-conversation-id="${turn.conversation.id}"]`)
      await expect(item).toBeVisible()
      await item.click()
      const composer = page.locator('.composer-card textarea').first()
      await expect(composer).toBeEnabled()
      await composer.fill('Lab browser regression passed')
      await page.getByRole('button', { name: /结束输出/ }).click()
      const result = await turn.response
      expect(result.status).toBe(200)
      expect(JSON.stringify(result.body)).toContain('Lab browser regression passed')
      const reopened = isMobile ? await openLabSidebar(page, true) : sidebar
      const closedItem = reopened.locator(`[data-conversation-id="${turn.conversation.id}"]`)
      await closedItem.locator('.conversation-delete-button').click()
      await expect(closedItem).toHaveCount(0)
      const remaining = await (await request.get('/api/lab/workspace')).json()
      expect(remaining.conversations.some((item: { id: string }) => item.id === turn.conversation.id)).toBe(false)
      expect(failures).toEqual([])
    } finally {
      await turn.dispose()
    }
  })

  test('prunes closed conversations and preserves the waiting turn', async ({ page, request, isMobile }) => {
    test.skip(!process.env.CHATAPI_TEST_LAB_MUTATIONS, 'Requires an isolated Lab database; pruning deletes old conversations')
    const failures = auditLabRequests(page)
    const closed = await startLabTurn(request)
    const waiting = await startLabTurn(request)
    try {
      // Finish our older turn through the public Lab control endpoint.
      const result = await request.post(apiUrl(`lab/requests/${closed.conversation.request_id}/complete`), {
        data: { text: 'ready for cleanup', mode: 'assistant_message' },
      })
      expect(result.ok()).toBeTruthy()
      expect((await closed.response).status).toBe(200)
      await page.goto('/app')
      const sidebar = await openLabSidebar(page, isMobile)
      await sidebar.getByRole('button', { name: '批量删除旧会话' }).click()
      const modal = page.getByRole('dialog', { name: '批量删除旧会话' })
      await modal.getByRole('spinbutton').fill('0')
      await modal.getByRole('button', { name: /删\s*除/ }).click()
      await expect(modal).toBeHidden()
      await expect.poll(async () => {
        const data = await (await request.get('/api/lab/workspace')).json()
        return data.conversations.map((item: { id: string }) => item.id)
      }).toEqual([waiting.conversation.id])
      expect(failures).toEqual([])
    } finally {
      await closed.dispose()
      await waiting.dispose()
    }
  })
})
