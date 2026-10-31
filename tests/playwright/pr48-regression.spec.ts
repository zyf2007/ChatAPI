import { expect, test } from '../../frontend/node_modules/@playwright/test'
import { installWorkspaceHarness } from './fixtures/workspaceHarness'

test.describe('PR 48 delayed command failure', () => {
  test.skip(!process.env.CHATAPI_TEST_PR48, 'Point CHATAPI_TEST_WEB_BASE_URL at the PR 48 frontend snapshot')

  test('does not restore an old chunk into another conversation', async ({ page, isMobile }) => {
    test.fail(true, 'PR 48 restores rawChunk into the currently selected conversation after a delayed failure')
    const harness = await installWorkspaceHarness(page)
    await page.goto('/app')
    if (isMobile) await page.locator('.menu-button').click()
    const sidebar = page.locator(isMobile ? '.mobile-drawer .sidebar-inner' : '.sidebar .sidebar-inner')
    await sidebar.locator('.conversation-item').filter({ hasText: 'Conversation first' }).click()
    const composer = page.locator('.composer-card textarea').first()
    await composer.fill('private text for first conversation')
    await composer.press('Enter')
    const command = await harness.commandReceived
    expect(command.conversation_id).toBe('first')
    if (isMobile) await page.locator('.menu-button').click()
    await sidebar.locator('.conversation-item').filter({ hasText: 'Conversation second' }).click()
    await expect(composer).toHaveValue('')
    harness.rejectCommand(command)
    await expect(page.getByText('Request changed while sending', { exact: true })).toBeVisible()
    await expect(composer).toHaveValue('')
  })
})
