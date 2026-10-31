import { randomUUID } from 'node:crypto'
import { expect, test } from '../../frontend/node_modules/@playwright/test'
import { auditLabRequests, openLabSettings } from './fixtures/labWorkspace'
import { apiUrl } from './fixtures/apiEndpoint'

test.describe('Lab workspace configuration', () => {
  test.skip(!process.env.CHATAPI_TEST_LAB, 'Requires a local Lab backend')

  test('creates a model, discovers it over the protocol, reloads and deletes it', async ({ page, request, isMobile }) => {
    const failures = auditLabRequests(page)
    const name = `pw-model-${randomUUID()}`
    let modelID = ''
    try {
      let modal = await openLabSettings(page, isMobile, '模型列表')
      await modal.getByLabel('虚拟模型名称', { exact: true }).fill(name)
      const [response] = await Promise.all([
        page.waitForResponse((response) => response.url().endsWith('/api/user/virtual-models') && response.request().method() === 'POST'),
        modal.getByRole('button', { name: /添加虚拟模型/ }).click(),
      ])
      expect(response.status()).toBe(201)
      modelID = (await response.json()).item.id
      await expect(modal.getByRole('row').filter({ hasText: name })).toBeVisible()
      const discovery = await request.get(apiUrl('v1/models'))
      expect(discovery.status()).toBe(200)
      expect((await discovery.json()).data).toContainEqual(expect.objectContaining({ id: name, object: 'model' }))
      modal = await openLabSettings(page, isMobile, '模型列表')
      const row = modal.getByRole('row').filter({ hasText: name })
      await expect(row).toBeVisible()
      await row.getByRole('button', { name: /删\s*除/ }).click()
      await page.locator('.ant-popconfirm').getByRole('button', { name: /删\s*除/ }).click()
      await expect(row).toHaveCount(0)
      modelID = ''
      expect((await (await request.get(apiUrl('v1/models'))).json()).data.map((item: { id: string }) => item.id)).not.toContain(name)
      expect(failures).toEqual([])
    } finally {
      if (modelID) await request.delete(`/api/user/virtual-models/${modelID}`)
    }
  })

  test('persists workspace notification and media settings without account APIs', async ({ page, request, isMobile }) => {
    test.skip(!process.env.CHATAPI_TEST_LAB_MUTATIONS, 'Requires an isolated Lab database for shared policy changes')
    const failures = auditLabRequests(page)
    const configResponse = await (await request.get('/api/user/config')).json()
    const oldConfig = {
      ntfy_url_enabled: Boolean(configResponse.ntfy_url_enabled),
      ntfy_url: String(configResponse.ntfy_url ?? ''),
      messages_per_minute_limit_enabled: Boolean(configResponse.messages_per_minute_limit_enabled),
      messages_per_minute_limit: Number(configResponse.messages_per_minute_limit ?? 0),
    }
    const oldMedia = (await (await request.get('/api/admin/settings/media')).json()).document.values
    const oldAccess = (await (await request.get('/api/admin/settings/access')).json()).document.values
    try {
      // Keep notifications disabled: this test saves a draft without sending external messages.
      expect((await request.post('/api/user/config', { data: { ...oldConfig, ntfy_url_enabled: false } })).ok()).toBeTruthy()
      let modal = await openLabSettings(page, isMobile, '工作区设置')
      await expect(modal.getByText('重置密码', { exact: true })).toHaveCount(0)
      await expect(modal.getByText('消息限流', { exact: true })).toHaveCount(0)
      await modal.getByRole('switch').click()
      const topic = `https://ntfy.sh/playwright-${randomUUID()}`
      await modal.getByPlaceholder('https://ntfy.sh/your-topic').fill(topic)
      await modal.getByRole('switch').click()
      const saved = page.waitForResponse((response) => response.url().endsWith('/api/user/config') && response.request().method() === 'POST')
      await modal.getByRole('button', { name: /保\s*存/ }).click()
      expect((await saved).status()).toBe(200)
      modal = await openLabSettings(page, isMobile, '工作区设置')
      await modal.getByRole('switch').click()
      await expect(modal.getByPlaceholder('https://ntfy.sh/your-topic')).toHaveValue(topic)
      modal = await openLabSettings(page, isMobile, '媒体设置')
      const field = modal.locator('.admin-setting-row').filter({ hasText: '单张图片上限' }).getByRole('spinbutton')
      await expect(field).toHaveValue(String(oldMedia.max_bytes))
      const nextLimit = oldMedia.max_bytes === 8192 ? 16384 : 8192
      await field.fill(String(nextLimit))
      const mediaSaved = page.waitForResponse((response) => response.url().endsWith('/api/admin/settings/media') && response.request().method() === 'PATCH')
      await modal.getByRole('button', { name: /保\s*存/ }).click()
      expect((await mediaSaved).status()).toBe(200)
      modal = await openLabSettings(page, isMobile, '媒体设置')
      await expect(modal.locator('.admin-setting-row').filter({ hasText: '单张图片上限' }).getByRole('spinbutton')).toHaveValue(String(nextLimit))
      await page.goto('/admin/settings/access')
      const connectionLimit = page.locator('.admin-setting-row').filter({ hasText: '单用户单实例上限' }).getByRole('spinbutton')
      await expect(connectionLimit).toHaveValue(String(oldAccess.max_connections_per_user_per_instance))
      const nextConnections = oldAccess.max_connections_per_user_per_instance === 999 ? 1000 : 999
      await connectionLimit.fill(String(nextConnections))
      const accessSaved = page.waitForResponse((response) => response.url().endsWith('/api/admin/settings/access') && response.request().method() === 'PATCH')
      await page.getByRole('button', { name: /保\s*存/ }).click()
      expect((await accessSaved).status()).toBe(200)
      const refreshedSession = page.waitForResponse((response) => response.url().endsWith('/api/auth/session'))
      await page.goto('/app')
      expect((await (await refreshedSession).json()).realtime_max_connections_per_user).toBe(nextConnections)
      expect(failures).toEqual([])
    } finally {
      expect((await request.patch('/api/admin/settings/access', { data: { values: { max_connections_per_user_per_instance: oldAccess.max_connections_per_user_per_instance } } })).ok()).toBeTruthy()
      expect((await request.post('/api/user/config', { data: oldConfig })).ok()).toBeTruthy()
      expect((await request.patch('/api/admin/settings/media', { data: { values: { max_bytes: oldMedia.max_bytes } } })).ok()).toBeTruthy()
    }
  })

  test('opens all supported instance pages and loads real diagnostics and policies', async ({ page }) => {
    const failures = auditLabRequests(page)
    for (const [section, heading] of [['overview', '系统概览'], ['system', '系统设置'], ['access', '访问限流'], ['media', '媒体']]) {
      await page.goto(`/admin/settings/${section}`)
      await expect(page.getByRole('heading', { name: heading, exact: true })).toBeVisible()
      await expect(page.locator('.admin-settings-nav [role="menuitem"]')).toHaveText(['概览', '系统设置', '访问限流', '媒体'])
      if (section === 'overview') await expect(page.locator('.admin-overview-status')).toContainText('实时')
      if (section === 'system') await expect(page.getByText('SQLite 存储维护', { exact: true })).toBeVisible()
      if (section === 'access') {
        await expect(page.getByText('等待请求有效期', { exact: true })).toBeVisible()
        await expect(page.getByText('应用 Key 请求上限', { exact: true })).toHaveCount(0)
      }
    }
    expect(failures).toEqual([])
  })
})
