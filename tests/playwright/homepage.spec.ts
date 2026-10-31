import { expect, test } from '../../frontend/node_modules/@playwright/test'

test.describe('homepage API endpoint presentation', () => {
  test('shows the configured API origin and rotates supported paths', async ({ page }) => {
    await page.goto('/')

    const frame = page.frameLocator('iframe[title="ChatAPI Homepage"]')
    const expectedBase = process.env.CHATAPI_TEST_API_BASE_URL?.replace(/\/$/, '') || new URL(page.url()).origin
    await expect(frame.locator('.url-base')).toHaveText(expectedBase)
    await expect(frame.locator('.url-path')).toHaveText('/v1/messages')

    await expect(frame.locator('.url-path:not(.exit)')).toHaveText('/v1/responses', { timeout: 8_000 })
    await expect(frame.locator('.url-path:not(.exit)')).toHaveText('/v1/chat/completions', { timeout: 8_000 })
  })

  test('copy button copies the currently displayed endpoint', async ({ page, context }) => {
    await page.goto('/')
    await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: new URL(page.url()).origin })
    const frame = page.frameLocator('iframe[title="ChatAPI Homepage"]')
    await frame.locator('body').evaluate(() => {
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: { writeText: async (value: string) => { (window as unknown as { copied?: string }).copied = value } },
      })
    })

    await frame.locator('.copy-btn').click()
    const copied = await frame.locator('body').evaluate(() => (window as unknown as { copied?: string }).copied)
    const expectedBase = process.env.CHATAPI_TEST_API_BASE_URL?.replace(/\/$/, '') || new URL(page.url()).origin
    expect(copied).toBe(`${expectedBase}/v1/messages`)
  })
})
