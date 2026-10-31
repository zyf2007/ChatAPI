import { expect, test } from '../../frontend/node_modules/@playwright/test'

import { apiEndpoint, apiUrl } from './fixtures/apiEndpoint'

test.use({ trace: 'off', video: 'off' })

test.describe('configured OpenAI-compatible API endpoint', () => {
  test.skip(!process.env.CHATAPI_TEST_API_SMOKE, 'Set CHATAPI_TEST_API_SMOKE=1 to make a remote API request')

  test('lists models without exposing the configured credential', async ({ request }) => {
    const endpoint = apiEndpoint()
    expect(endpoint.apiKey, 'Set CHATAPI_TEST_API_KEY when API smoke testing is enabled').toBeTruthy()
    const response = await request.get(apiUrl('v1/models'), {
      headers: { Authorization: `Bearer ${endpoint.apiKey}` },
    })

    expect(response.status()).toBe(200)
    const payload = await response.json()
    expect(payload).toHaveProperty('data')
    if (endpoint.model) {
      expect(payload.data.map((item: { id?: string }) => item.id)).toContain(endpoint.model)
    }
  })
})
