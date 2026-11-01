import { defineConfig } from '@playwright/test'

// Smoke suite of the web demo (ADR-0051, docs/plans/active/web-demo-mode.md): the real UI against the scripted in-browser daemon.
// It builds web/packages/ui/dist-demo and serves it with `vite preview` on 127.0.0.1:4173 -- no daemon, no Go, no network.
//   cd web/e2e && bunx playwright test -c playwright.demo.config.ts
//   E2E_FULL=1 ...   adds dark-1280 and light-390
//   DEMO_FRAMES_DIR=<dir> ... also writes the light/dark 1280/390 frames (see DM-12)
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
  testDir: './specs-demo',
  fullyParallel: true,
  workers: 2,
  retries: ci ? 1 : 0,
  timeout: 45_000,
  expect: { timeout: 10_000 },
  reporter: [['list']],
  use: {
    baseURL: 'http://127.0.0.1:4173',
    actionTimeout: 7_000,
    trace: ci ? 'on-first-retry' : 'off',
    video: 'off',
    screenshot: 'only-on-failure',
    launchOptions: executablePath ? { executablePath } : {},
  },
  webServer: {
    command: 'bun run --cwd ../packages/ui build:demo && bun run --cwd ../packages/ui preview:demo',
    url: 'http://127.0.0.1:4173',
    reuseExistingServer: !ci,
    timeout: 240_000,
  },
  projects: full ? [light1280, dark390, dark1280, light390] : [light1280, dark390],
})
