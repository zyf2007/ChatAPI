import { defineConfig, devices } from '../../frontend/node_modules/@playwright/test'
import { existsSync } from 'node:fs'

export default defineConfig({
  testDir: '.',
  timeout: 30_000,
  expect: { timeout: 5_000 },
  fullyParallel: false,
  retries: process.env.CI ? 2 : 0,
  reporter: [['list'], ['html', { outputFolder: 'test-results/report', open: 'never' }]],
  outputDir: 'test-results/artifacts',
  use: {
    baseURL: process.env.CHATAPI_TEST_WEB_BASE_URL || 'http://127.0.0.1:5193',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    ...devices['Desktop Chrome'],
    launchOptions: {
      executablePath: process.env.CHATAPI_CHROMIUM_PATH || (existsSync('/usr/bin/chromium') ? '/usr/bin/chromium' : undefined),
      headless: process.env.CHATAPI_HEADED !== '1',
    },
  },
  projects: [
    { name: 'desktop-chromium', use: { ...devices['Desktop Chrome'] } },
    { name: 'mobile-chromium', use: { ...devices['iPhone 13'], browserName: 'chromium' } },
  ],
  webServer: process.env.CHATAPI_TEST_WEB_BASE_URL ? undefined : {
    command: 'npm run dev -- --host 127.0.0.1 --port 5193 --strictPort',
    cwd: process.cwd(),
    url: 'http://127.0.0.1:5193',
    reuseExistingServer: !process.env.CI,
    timeout: 30_000,
    env: process.env.CHATAPI_TEST_API_BASE_URL
      ? { VITE_HOMEPAGE_API_BASE_URL: process.env.CHATAPI_TEST_API_BASE_URL }
      : undefined,
  },
})
