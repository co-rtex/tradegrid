import { defineConfig, devices } from '@playwright/test'
import { backendPort, backendURL, frontendPort } from './e2e/ports.ts'

// Browser smoke tests (#97) against the real Go backend and Vite frontend.
//
// Every run starts its own backend and frontend on ports reserved for these
// tests, never reusing a server you have open, so each run begins from the
// seeded demo state (the exchange is in memory and resets on restart). The
// scenarios build on each other in order, so they run serially in one worker.

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: 0,
  timeout: 30_000,
  expect: { timeout: 5_000 },
  reporter: [['list'], ['html', { open: 'never', outputFolder: 'playwright-report' }]],
  outputDir: 'test-results',
  use: {
    baseURL: `http://localhost:${frontendPort}`,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: [
    {
      command: 'go run ./cmd/server',
      cwd: '../backend',
      env: { TRADEGRID_HTTP_PORT: backendPort },
      url: `${backendURL}/health`,
      reuseExistingServer: false,
      timeout: 120_000,
      stdout: 'pipe',
    },
    {
      command: `npx vite --port ${frontendPort} --strictPort`,
      env: { TRADEGRID_HTTP_PORT: backendPort },
      url: `http://localhost:${frontendPort}`,
      reuseExistingServer: false,
      timeout: 60_000,
    },
  ],
})
