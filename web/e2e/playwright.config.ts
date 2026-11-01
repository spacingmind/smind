import { defineConfig } from '@playwright/test'

const ci = !!process.env.CI
const full = process.env.E2E_FULL === '1'
const executablePath = process.env.PW_CHROMIUM_PATH || undefined

const light1280 = { name: 'light-1280', use: { viewport: { width: 1280, height: 800 }, colorScheme: 'light' as const } }
const dark390 = {
  name: 'dark-390',
  use: { viewport: { width: 390, height: 844 }, colorScheme: 'dark' as const, isMobile: false, hasTouch: true },
}
const dark1280 = { name: 'dark-1280', use: { viewport: { width: 1280, height: 800 }, colorScheme: 'dark' as const } }
const light390 = {
  name: 'light-390',
  use: { viewport: { width: 390, height: 844 }, colorScheme: 'light' as const, isMobile: false, hasTouch: true },
}

export default defineConfig({
  testDir: './specs',
  fullyParallel: true,
  workers: 2,
  retries: ci ? 1 : 0,
  timeout: 45_000,
  expect: { timeout: 7_000 },
  reporter: [['list'], ['./reporters/flaky.ts']],
  use: {
    actionTimeout: 5_000,
    trace: ci ? 'on-first-retry' : 'off',
    video: 'off',
    screenshot: 'only-on-failure',
    launchOptions: executablePath ? { executablePath } : {},
  },
  projects: full ? [light1280, dark390, dark1280, light390] : [light1280, dark390],
})
