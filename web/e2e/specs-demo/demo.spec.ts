import fs from 'node:fs'
import path from 'node:path'
import { expect, test, type Page } from '@playwright/test'

// Smoke suite of the web demo build (ADR-0051). Every test loads the page fresh, so each starts a fresh scripted session.
// IDs are the plan's (docs/plans/active/web-demo-mode.md). No waitForTimeout: web-first assertions only.
// The tasks are the landing hero's nine (checkout-api, web-app, platform); their ids are the scenario's (src/demo/scenario/index.ts).

const main = (page: Page) => page.getByTestId('app-main-content')
const card = (page: Page) => page.getByTestId('permission-card')

/** The route of a task's chat (the scenario gives a task and its chat the same id). */
const taskRoute = (workspaceId: number, taskId: number) => `/#/workspace/${workspaceId}/task/${taskId}/chat/${taskId}`

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

/** Opens another task of the tour in the running page (a hash change: the session, and the live run, carry on). */
async function openTask(page: Page, workspaceId: number, taskId: number, title: string) {
  await page.evaluate((hash) => (window.location.hash = hash), taskRoute(workspaceId, taskId).slice(1))
  await expect(page.getByTestId('app-header-title').filter({ hasText: title }).first()).toBeVisible()
}

test('DM-01 loads with the banner and the sidebar tour; zero websockets; only same-origin static requests', async ({ page }, testInfo) => {
  const net = watchNetwork(page)
  await openDemo(page)
  await expect(page.getByTestId('demo-banner')).toContainText('Demo — not connected to a daemon. Scripted data; nothing you type leaves your browser.')
  await expect(page.getByTestId('app-header-title').filter({ hasText: 'Rate-limit the webhook handler' }).first()).toBeVisible()
  if (testInfo.project.name.endsWith('1280')) {
    await expect(page.getByText('Demo (scripted)').first()).toBeVisible()
    for (const w of ['checkout-api', 'web-app', 'platform']) await expect(page.getByText(w, { exact: true }).first()).toBeVisible()
    // the landing hero's nine tasks, the design Lead's two Peers included
    for (const t of [
      'Rate-limit the webhook handler',
      'Why does TestRetry fail on CI only?',
      'Add a unit test for parseDate',
      'Review PR #42 for race conditions',
      'Why did p99 latency double since Tuesday?',
      'Explain this repo to me',
      'Design a multi-region job queue',
      'Queue schema and leases',
      'Failover modes',
    ]) {
      await expect(page.getByTestId('task-title').filter({ hasText: t }).first()).toBeVisible()
    }
  }
  await expect(card(page)).toBeVisible({ timeout: 25_000 }) // the run streams on its own and reaches the permission request
  expect(net.websockets).toEqual([])
  expect(net.foreign).toEqual([])
})

test('DM-02 the live task streams a tool call and reaches the permission card; Allow finishes the run and adds the diff file', async ({ page }) => {
  await openDemo(page)
  await expect(main(page).getByText('Read', { exact: true }).first()).toBeVisible({ timeout: 15_000 })
  await expect(main(page)).toContainText('I need to add the limiter file.')
  await expect(card(page)).toBeVisible({ timeout: 25_000 })
  await expect(card(page)).toContainText('internal/webhook/limiter.go')
  await expect(page.getByTestId('composer-diff-stat')).toHaveCount(0) // nothing is written until it is allowed
  await card(page).getByRole('button', { name: 'Allow once' }).click()
  await expect(main(page)).toContainText('Tests pass.', { timeout: 25_000 })
  await expect(card(page)).toHaveCount(0)
  await expect(page.getByTestId('composer-diff-stat').first()).toContainText('+44')
})

test('DM-03 Deny finishes the run on the deny branch', async ({ page }) => {
  await openDemo(page)
  await expect(card(page)).toBeVisible({ timeout: 25_000 })
  await card(page).getByRole('button', { name: 'Deny' }).click()
  await expect(main(page)).toContainText("I won't create", { timeout: 15_000 })
  await expect(page.getByTestId('composer-diff-stat')).toHaveCount(0)
})

test('DM-04 the Diff tab renders the debug fix (+2 −1) and the first task test (+24)', async ({ page }) => {
  await openDemo(page)
  await openTask(page, 1, 2, 'Why does TestRetry fail on CI only?')
  await page.getByTestId('composer-diff-stat').first().click()
  const pane = page.getByTestId('diff-viewer-pane')
  await expect(pane).toBeVisible()
  await expect(pane).toContainText('internal/retry/backoff.go')
  await expect(pane).toContainText('seed := testSeed()')
  await openTask(page, 1, 3, 'Add a unit test for parseDate')
  await page.getByTestId('composer-diff-stat').first().click()
  await expect(page.getByTestId('diff-viewer-pane')).toContainText('internal/dates/parse_test.go')
})

test('DM-05 the Terminal tab shows the canned output and the prompt', async ({ page }) => {
  await openDemo(page)
  await page.getByTestId('app-header-terminal').click()
  const rows = page.locator('.xterm-rows:visible').first()
  await expect(rows).toContainText('go test ./internal/webhook/...', { timeout: 15_000 })
  await expect(rows).toContainText('checkout/internal/webhook', { timeout: 15_000 })
  await expect(rows).toContainText(/demo@smind:~\$\s*$/m, { timeout: 15_000 })
})

test('DM-06 the inbox shows the items; resolving one lowers the badge', async ({ page }) => {
  await openDemo(page)
  await openSidebar(page)
  await page.getByTestId('inbox-badge').first().click()
  const screen = page.getByTestId('inbox-screen')
  await expect(screen).toBeVisible()
  for (const t of ['Rate-limit the webhook handler', 'Why does TestRetry fail on CI only?', 'Add a unit test for parseDate']) {
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

test('DM-13 each level tells the landing hero\'s story: debug, first task, review, analysis, onboarding', async ({ page }) => {
  await openDemo(page)
  const stories: [number, number, string, string, string][] = [
    [1, 2, 'Why does TestRetry fail on CI only?', 'The jitter is seeded from the wall clock', 'pinned the seed in the test; 20 repeated runs now pass'],
    [1, 3, 'Add a unit test for parseDate', 'Added a table test with 5 cases', 'go test is green'],
    [1, 4, 'Review PR #42 for race conditions', 'Three problems, no code changed', 'wg.Add after the go statement can race with Wait'],
    [1, 5, 'Why did p99 latency double since Tuesday?', 'Pool shrunk 40 → 10', 'restore the pool size behind a flag and compare p99'],
    [2, 6, 'Explain this repo to me', 'A Next.js app with three parts', 'Start with src/app/page.tsx'],
  ]
  for (const [ws, id, title, a, b] of stories) {
    await openTask(page, ws, id, title)
    await expect(main(page)).toContainText(a)
    await expect(main(page)).toContainText(b)
  }
})

test('DM-14 the design task is a Lead with two Peers: the tree, the orchestration tool calls and the summary table', async ({ page }, testInfo) => {
  await openDemo(page)
  await openTask(page, 3, 12, 'Design a multi-region job queue')
  await expect(main(page)).toContainText('Both Peers are done. Summary (ADR draft):')
  await expect(main(page).getByRole('table')).toContainText('Queue per region')
  await expect(main(page)).toContainText('task_new')
  await expect(main(page)).toContainText('task_wait')
  await expect(main(page)).toContainText('a queue per region, jobs pinned to their home region')
  if (testInfo.project.name.endsWith('1280')) {
    // the task header's tree chips: both Peers, as pills, and the subtree summary
    const pills = page.getByTestId('task-child-pill')
    await expect(pills).toHaveCount(2)
    await expect(pills.filter({ hasText: 'Queue schema and leases' })).toHaveCount(1)
    await expect(pills.filter({ hasText: 'Failover modes' })).toHaveCount(1)
    await expect(page.getByTestId('task-tree-summary')).toContainText('2 done')
    // a Peer knows its Lead
    await pills.filter({ hasText: 'Failover modes' }).click()
    await expect(page.getByTestId('task-spawned-by')).toContainText('Design a multi-region job queue')
  } else {
    await openSidebar(page)
    await expect(page.getByTestId('task-title').filter({ hasText: 'Queue schema and leases' }).first()).toBeVisible()
  }
})

// DM-12: not an assertion suite; with DEMO_FRAMES_DIR set it writes the review frames of this project (light/dark x 1280/390):
// the debug task, the feature task's permission Allow flow, and the design tree.
test('DM-12 frames @frames', async ({ page }, testInfo) => {
  const dir = process.env.DEMO_FRAMES_DIR
  test.skip(!dir, 'set DEMO_FRAMES_DIR to write the frames')
  fs.mkdirSync(dir!, { recursive: true })
  const snap = (name: string) => page.screenshot({ path: path.join(dir!, `${testInfo.project.name}-${name}.png`) })
  await openDemo(page)
  // debug: the answer, then its diff
  await openTask(page, 1, 2, 'Why does TestRetry fail on CI only?')
  await expect(main(page)).toContainText('20 repeated runs now pass')
  await snap('01-debug')
  await page.getByTestId('composer-diff-stat').first().click()
  await expect(page.getByTestId('diff-viewer-pane')).toContainText('seed := testSeed()')
  await snap('02-debug-diff')
  // feature: the permission card, the Allow, the result and the diff it added (the run is already waiting by now)
  await openTask(page, 1, 1, 'Rate-limit the webhook handler')
  await expect(card(page)).toBeVisible({ timeout: 25_000 })
  await snap('03-feature-permission')
  await card(page).getByRole('button', { name: 'Allow once' }).click()
  await expect(main(page)).toContainText('Tests pass.', { timeout: 25_000 })
  await snap('04-feature-allowed')
  await page.getByTestId('composer-diff-stat').first().click()
  await expect(page.getByTestId('diff-viewer-pane')).toContainText('internal/webhook/limiter.go')
  await snap('05-feature-diff')
  // design: the Lead with its Peers
  await openTask(page, 3, 12, 'Design a multi-region job queue')
  await expect(main(page)).toContainText('Both Peers are done')
  await snap('06-design-tree')
})
