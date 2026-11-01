import fs from 'node:fs'
import path from 'node:path'
import { expect, test, type Page } from '@playwright/test'

// Smoke suite of the web demo build (ADR-0051). Every test loads the page fresh, so each starts a fresh scripted session.
// IDs are the plan's (docs/plans/active/web-demo-mode.md). No waitForTimeout: web-first assertions only.

const main = (page: Page) => page.getByTestId('app-main-content')
const card = (page: Page) => page.getByTestId('permission-card')

/** Collects what the page asks the network for; the demo may only fetch its own static files. */
function watchNetwork(page: Page) {
  const websockets: string[] = []
  const foreign: string[] = []
  page.on('websocket', (ws) => websockets.push(ws.url()))
  page.on('request', (r) => {
    const u = r.url()
    if (u.startsWith('http://127.0.0.1:4173/') || u.startsWith('data:') || u.startsWith('blob:')) return
    foreign.push(u)
  })
  return { websockets, foreign }
}

/** Settings is view state, not a route: the app opens a section on this window event (the existing e2e suite does the same). */
async function openSettings(page: Page, sectionId: string) {
  await page.evaluate((id) => window.dispatchEvent(new CustomEvent('smind:open-settings', { detail: { sectionId: id } })), sectionId)
  await expect(page.getByTestId('settings-screen').first()).toBeVisible()
}

/** The sidebar footer (inbox badge, settings) sits in a drawer on a narrow screen. */
async function openSidebar(page: Page) {
  const badge = page.getByTestId('inbox-badge').first()
  if (await badge.isVisible()) return
  await page.getByRole('button', { name: 'Toggle Sidebar' }).first().click()
  await expect(badge).toBeVisible()
}

async function openDemo(page: Page) {
  await page.goto('/')
  await expect(page.getByTestId('demo-banner')).toBeVisible()
}

test('DM-01 loads with the banner and the sidebar tour; zero websockets; only same-origin static requests', async ({ page }, testInfo) => {
  const net = watchNetwork(page)
  await openDemo(page)
  await expect(page.getByTestId('demo-banner')).toContainText('Demo — not connected to a daemon. Scripted data; nothing you type leaves your browser.')
  await expect(page.getByTestId('app-header-title').filter({ hasText: 'Add rate limiting to the public API' }).first()).toBeVisible()
  if (testInfo.project.name.endsWith('1280')) {
    await expect(page.getByText('Demo (scripted)').first()).toBeVisible()
    for (const w of ['acme-api', 'web-dashboard', 'docs-site']) await expect(page.getByText(w, { exact: true }).first()).toBeVisible()
  }
  await expect(card(page)).toBeVisible({ timeout: 25_000 }) // the run streams on its own and reaches the permission request
  expect(net.websockets).toEqual([])
  expect(net.foreign).toEqual([])
})

test('DM-02 the live task streams tool calls and reaches the permission card; Allow finishes the run and adds the diff file', async ({ page }) => {
  await openDemo(page)
  await expect(main(page).getByText('Read', { exact: true }).first()).toBeVisible({ timeout: 15_000 })
  await expect(card(page)).toBeVisible({ timeout: 25_000 })
  await expect(card(page)).toContainText('internal/ratelimit/limiter.go')
  await card(page).getByRole('button', { name: 'Allow' }).click()
  await expect(main(page)).toContainText('All tests pass', { timeout: 25_000 })
  await expect(card(page)).toHaveCount(0)
  await expect(page.getByTestId('composer-diff-stat').first()).toContainText('+50')
})

test('DM-03 Deny finishes the run on the deny branch', async ({ page }) => {
  await openDemo(page)
  await expect(card(page)).toBeVisible({ timeout: 25_000 })
  await card(page).getByRole('button', { name: 'Deny' }).click()
  await expect(main(page)).toContainText("I won't write that file", { timeout: 15_000 })
  await expect(page.getByTestId('composer-diff-stat').first()).not.toContainText('+50')
})

test('DM-04 the Diff tab renders at least three files', async ({ page }) => {
  await openDemo(page)
  await page.getByTestId('composer-diff-stat').first().click()
  const pane = page.getByTestId('diff-viewer-pane')
  await expect(pane).toBeVisible()
  for (const f of ['cmd/api/router.go', 'cmd/api/config.go', 'internal/middleware/auth_test.go']) await expect(pane).toContainText(f)
})

test('DM-05 the Terminal tab shows the canned output and the prompt', async ({ page }) => {
  await openDemo(page)
  await page.getByTestId('app-header-terminal').click()
  const rows = page.locator('.xterm-rows:visible').first()
  await expect(rows).toContainText('go test ./...', { timeout: 15_000 })
  await expect(rows).toContainText('acme/internal/store', { timeout: 15_000 })
  await expect(rows).toContainText(/demo@smind:~\/acme-api\$\s*$/m, { timeout: 15_000 })
})

test('DM-06 the inbox shows the items; resolving one lowers the badge', async ({ page }) => {
  await openDemo(page)
  await openSidebar(page)
  await page.getByTestId('inbox-badge').first().click()
  const screen = page.getByTestId('inbox-screen')
  await expect(screen).toBeVisible()
  for (const t of ['Add rate limiting to the public API', 'Upgrade the module to Go 1.24', 'Dark mode toggle on the settings page', 'Paginate the audit log']) {
    await expect(screen).toContainText(t)
  }
  await expect(page.getByTestId('inbox-header')).toContainText('3 unseen')
  await screen.getByRole('button', { name: 'Resolve' }).first().click()
  await expect(page.getByTestId('inbox-header')).toContainText('2 unseen')
})

test('DM-07 the Usage view renders the plan windows and the token table', async ({ page }) => {
  await openDemo(page)
  await openSettings(page, 'usage')
  const content = page.getByTestId('settings-content')
  await expect(content).toContainText('Plan usage')
  await expect(content).toContainText('62%')
  await expect(content.getByTestId('usage-row-runs-1')).toBeVisible()
})

test('DM-08 sending a prompt streams the canned reply', async ({ page }) => {
  await openDemo(page)
  await expect(card(page)).toBeVisible({ timeout: 25_000 })
  await card(page).getByRole('button', { name: 'Deny' }).click()
  await expect(main(page)).toContainText('scripted demo reply', { timeout: 15_000 })
  const box = page.getByTestId('composer').locator('textarea').first()
  await box.fill('What does this demo do?')
  await page.getByTestId('chat-send-button').first().click()
  await expect(main(page)).toContainText('no model is running and nothing you type leaves your browser', { timeout: 15_000 })
})

test('DM-09 a settings write shows the read-only message', async ({ page }) => {
  await openDemo(page)
  await openSettings(page, 'agents')
  await page.getByRole('button', { name: /new agent/i }).first().click()
  await page.getByRole('textbox').first().fill('Demo agent')
  await page.getByRole('button', { name: /^add agent$/i }).first().click()
  await expect(page.getByText('Read-only in the demo.').first()).toBeVisible()
})

test('DM-10 the banner is visible and does not overlap the shell', async ({ page }) => {
  await openDemo(page)
  const banner = page.getByTestId('demo-banner')
  const bb = (await banner.boundingBox())!
  const header = (await page.getByTestId('app-header').first().boundingBox())!
  expect(bb.y).toBe(0)
  expect(header.y).toBeGreaterThanOrEqual(bb.y + bb.height - 1)
  const vp = page.viewportSize()!
  expect(bb.width).toBeGreaterThanOrEqual(vp.width - 1)
  expect(await page.evaluate(() => document.documentElement.scrollHeight <= window.innerHeight + 1)).toBe(true)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true)
})

test('DM-11 the CSP blocks an injected fetch and WebSocket', async ({ page }) => {
  await openDemo(page)
  const result = await page.evaluate(async () => {
    const out: { fetch: string; ws: string } = { fetch: 'allowed', ws: 'allowed' }
    try {
      await fetch('/api/token')
    } catch {
      out.fetch = 'blocked'
    }
    out.ws = await new Promise<string>((resolve) => {
      try {
        const ws = new WebSocket('ws://127.0.0.1:1')
        ws.onerror = () => resolve('blocked')
        ws.onopen = () => resolve('allowed')
        setTimeout(() => resolve('blocked'), 3000)
      } catch {
        resolve('blocked')
      }
    })
    return out
  })
  expect(result).toEqual({ fetch: 'blocked', ws: 'blocked' })
})

// DM-12: not an assertion suite; with DEMO_FRAMES_DIR set it writes the review frames of this project (light/dark x 1280/390).
test('DM-12 frames @frames', async ({ page }, testInfo) => {
  const dir = process.env.DEMO_FRAMES_DIR
  test.skip(!dir, 'set DEMO_FRAMES_DIR to write the frames')
  fs.mkdirSync(dir!, { recursive: true })
  const snap = (name: string) => page.screenshot({ path: path.join(dir!, `${testInfo.project.name}-${name}.png`) })
  await openDemo(page)
  await expect(card(page)).toBeVisible({ timeout: 25_000 })
  await snap('01-permission')
  await card(page).getByRole('button', { name: 'Allow' }).click()
  await expect(main(page)).toContainText('All tests pass', { timeout: 25_000 })
  await snap('02-allowed')
  await page.getByTestId('composer-diff-stat').first().click()
  await expect(page.getByTestId('diff-viewer-pane')).toContainText('internal/ratelimit/limiter.go')
  await snap('03-diff')
  await page.getByTestId('app-header-terminal').click()
  await expect(page.locator('.xterm-rows:visible').first()).toContainText('acme/internal/store', { timeout: 15_000 })
  await snap('04-terminal')
  await openSidebar(page)
  await page.getByTestId('inbox-badge').first().click()
  await expect(page.getByTestId('inbox-screen')).toBeVisible()
  await snap('05-inbox')
  await openSettings(page, 'usage')
  await expect(page.getByTestId('settings-content')).toContainText('Plan usage')
  await snap('06-usage')
})
