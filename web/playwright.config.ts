import { defineConfig, devices } from '@playwright/test'

const port = 4173
// In environments with a preinstalled Chromium, point Playwright at it.
const executablePath = process.env.PLAYWRIGHT_CHROMIUM_PATH

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    timezoneId: 'UTC',
    trace: 'retain-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'], ...(executablePath ? { launchOptions: { executablePath } } : {}) },
    },
  ],
  // The real binary with the dashboard embedded, serving the sample fixture.
  webServer: {
    command: `cd .. && go run -tags webui ./cmd/tracklens -serve 127.0.0.1:${port} -file testdata/sample.json`,
    url: `http://127.0.0.1:${port}/healthz`,
    reuseExistingServer: !process.env.CI,
    timeout: 180_000,
  },
})
