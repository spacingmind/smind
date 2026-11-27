import fs from 'node:fs'
import path from 'node:path'
import { type Page, type TestInfo } from '@playwright/test'
import { type App, expect, test } from '../fixtures/test'

// The ambient quota notice (docs/plans/active/ambient-quota.md, ADR-0059 F): one dismissible row above the
// composer's input card when a plan window of the composer provider's account reaches 90% used. The notice sends
// NO `account.usage` request of any mode: it only reads the shared cache that a usage surface fills. So AQ-00
// (a cold start with only a task open) sees no notice and no request at all, and AQ-01..AQ-06 first load the
// reports the way a person would: Settings -> Usage (a touching surface whose single `touch` -- or `cached` read
// when the window is not focused -- the stub answers) then back to the task. No test ever reaches a vendor:
// every `account.usage` request is answered locally through page.routeWebSocket, and the real daemon's
// `provider.list` answer is proxied with `accountProvider: "anthropic"` added to the seeded `uat` provider (a
// custom provider has no account provider, and the notice only shows for a provider with one). Frames are
// written only when QUOTA_FRAMES_DIR is set; every frame also asserts the theme's luminance and that nothing
// scrolls sideways.

type WindowSpec = { id: string; label: string; usedPct: number; resetsInMs: number; durationSeconds: number }

const fiveHour = (usedPct: number): WindowSpec => ({ id: 'five_hour', label: '5-hour', usedPct, resetsInMs: 2 * 3600_000, durationSeconds: 18000 })
const weekly = (usedPct: number): WindowSpec => ({ id: 'weekly', label: 'Weekly', usedPct, resetsInMs: 3 * 24 * 3600_000, durationSeconds: 604800 })

/** The one account the reports hold: provider "anthropic" (the patched uat provider's account provider). */
const usageAnswer = (start: number, windows: WindowSpec[], fetchedAgoMs: number) => ({
  enabled: true,
  ttlSeconds: 300,
  reports: [
    {
      accountId: 1,
      key: 'anthropic-1',
      provider: 'anthropic',
      displayName: 'main',
      credentialType: 'oauth',
      stale: false,
      nextRefreshAt: null,
      refreshBlocked: null,
      inFlight: false,
      lastError: null,
      report: {
        v: 2,
        status: 'available',
        planLabel: 'Max',
        fetchedAt: new Date(start - fetchedAgoMs).toISOString(),
        windows: windows.map((w) => ({
          id: w.id,
          label: w.label,
          usedPct: w.usedPct,
          remainingPct: 100 - w.usedPct,
          resetsAt: new Date(start + w.resetsInMs).toISOString(),
          durationSeconds: w.durationSeconds,
        })),
      },
    },
  ],
})

/**
 * Answers every `account.usage` request locally (never forwarded, so no vendor is ever called) and records the
 * mode each call asked for. Everything else is proxied to the real daemon, except a `provider.list` response,
 * whose provider with id `uat` gains `accountProvider: "anthropic"` before it reaches the page (request ids are
 * tracked by method the way fixtures/test.ts trackRpc does).
 */
async function stubUsage(page: Page, opts: { windows: WindowSpec[]; fetchedAgoMs?: number }) {
  const start = Date.now()
  const modes: (string | undefined)[] = []
  const proxied: string[] = []
  await page.routeWebSocket(/\/ws/, (ws) => {
    const server = ws.connectToServer()
    const methodById = new Map<string, string>()
    server.onMessage((m) => {
      let msg: { id?: string | number; event?: unknown; result?: { providers?: { id?: string }[] } }
      try {
        msg = JSON.parse(String(m))
      } catch {
        return ws.send(m)
      }
      const method = msg.id !== undefined ? methodById.get(String(msg.id)) : undefined
      if (msg.event !== undefined || !method || msg.result === undefined) return ws.send(m)
      methodById.delete(String(msg.id))
      if (method !== 'provider.list') return ws.send(m)
      const providers = (msg.result.providers ?? []).map((p) => (p.id === 'uat' ? { ...p, accountProvider: 'anthropic' } : p))
      ws.send(JSON.stringify({ ...msg, result: { ...msg.result, providers } }))
    })
    ws.onMessage((m) => {
      let msg: { id?: string | number; method?: string; params?: { mode?: string } }
      try {
        msg = JSON.parse(String(m))
      } catch {
        return server.send(m)
      }
      if (!msg.method) return server.send(m)
      if (msg.id !== undefined) methodById.set(String(msg.id), msg.method)
      if (msg.method === 'account.usage') {
        modes.push(msg.params?.mode)
        return ws.send(JSON.stringify({ id: msg.id, result: usageAnswer(start, opts.windows, opts.fetchedAgoMs ?? 60_000) }))
      }
      proxied.push(msg.method)
      server.send(m)
    })
  })
  return { modes, proxied }
}

/** True when neither the document nor the composer nor any scrolling ancestor of it scrolls sideways. */
const noSideScroll = (page: Page) =>
  page.evaluate(() => {
    const composer = document.querySelector('[data-testid="composer"]') as HTMLElement | null
    if (!composer) return false
    for (let el: HTMLElement | null = composer; el; el = el.parentElement) {
      if (el.scrollWidth > el.clientWidth + 1) return false
    }
    return document.documentElement.scrollWidth <= document.documentElement.clientWidth
  })

async function frame(page: Page, testInfo: TestInfo, name: string) {
  const luminance = await page.evaluate(() => {
    const rgb = getComputedStyle(document.body).backgroundColor.match(/\d+(\.\d+)?/g)?.map(Number) ?? [0, 0, 0]
    return (0.2126 * rgb[0] + 0.7152 * rgb[1] + 0.0722 * rgb[2]) / 255
  })
  if (testInfo.project.use.colorScheme === 'dark') expect(luminance, 'dark frame: page background').toBeLessThan(0.35)
  else expect(luminance, 'light frame: page background').toBeGreaterThan(0.65)
  expect(await noSideScroll(page), 'the composer and its scrolling panes do not scroll sideways').toBe(true)
  const dir = process.env.QUOTA_FRAMES_DIR
  if (!dir) return
  fs.mkdirSync(dir, { recursive: true })
  // The composer sits at the bottom of the viewport, so a plain viewport shot frames the notice.
  await page.screenshot({ path: path.join(dir, `${name}-${testInfo.project.name}.png`) })
}

/** Opens the seeded finished task whose chat is bound to the uat provider, and waits for its composer. */
async function openSeededTask(app: App) {
  const { page } = app
  await app.openApp()
  await app.openTask('idle-done')
  await expect(page.getByTestId('composer')).toBeVisible()
  // Off the sidebar row and the header tab: their hover cards would hang over the timeline for the rest of the test.
  const vp = page.viewportSize()
  if (vp) await page.mouse.move(vp.width / 2, vp.height / 3)
}

/**
 * Loads the usage reports like a person would: Settings -> Usage is a touching surface, so opening it sends the
 * one `account.usage` request of the whole flow (`touch`, or a `cached` read when the window is not focused) and
 * fills the shared cache; the rendered list proves the reports reached the page. Then back to the task, whose
 * notice reads the cache and never asks.
 */
async function loadReports(app: App) {
  const { page } = app
  await page.evaluate(() => window.dispatchEvent(new CustomEvent('smind:open-settings', { detail: { sectionId: 'usage' } })))
  await expect(page.getByTestId('settings-section-usage')).toBeVisible()
  await expect(page.getByTestId('usage-list')).toBeVisible()
  // The section opens as a detail page at narrow widths: that back button returns to the section list first.
  if (app.narrow) await page.getByTestId('settings-detail-back').click()
  // The two-column modal has no back button: Escape closes it.
  await page.keyboard.press('Escape')
  await expect(page.getByTestId('settings-dialog')).toHaveCount(0)
  await expect(page.getByTestId('composer')).toBeVisible()
  // Off the settings sidebar, whose hover state would sit over the timeline for the rest of the test.
  const vp = page.viewportSize()
  if (vp) await page.mouse.move(vp.width / 2, vp.height / 3)
}

test('AQ-00 a cold start shows no notice and sends no account.usage request', async ({ app }, testInfo) => {
  const { page } = app
  const stub = await stubUsage(page, { windows: [fiveHour(92)] }) // over the threshold: any request would show the notice
  await openSeededTask(app)
  await expect(page.getByTestId('ambient-quota-notice')).toHaveCount(0)
  expect(stub.proxied.length, 'the app is live on the stubbed socket').toBeGreaterThan(0)
  expect(stub.modes, 'no account.usage request of any mode before a usage surface opens').toEqual([])
  await frame(page, testInfo, '00-cold-start')
})

test('AQ-01 a window at 92 percent shows the notice with its name and local reset time', async ({ app }, testInfo) => {
  const { page } = app
  const stub = await stubUsage(page, { windows: [fiveHour(92)] })
  await openSeededTask(app)
  await loadReports(app)
  const notice = page.getByTestId('ambient-quota-notice')
  await expect(notice).toBeVisible()
  await expect(page.getByTestId('ambient-quota-text')).toHaveText(/^5-hour window 92% used, resets (\w{3} )?\d\d:\d\d$/) // a reset past local midnight adds the weekday
  const noticeBox = await notice.boundingBox()
  const cardBox = await page.getByTestId('composer-card').boundingBox()
  if (!noticeBox || !cardBox) throw new Error('no bounding box')
  expect(noticeBox.y + noticeBox.height, 'the notice ends above the input card').toBeLessThanOrEqual(cardBox.y + 1)
  expect(stub.modes.length, 'the reports arrived through one usage request').toBeGreaterThan(0)
  expect(stub.modes.every((mode) => mode === 'touch' || mode === 'cached'), 'only the Usage section own read, never a refresh').toBe(true)
  // The composer stays usable: typing lands and sends nothing (a send is a live call, never clicked here).
  const afterLoad = stub.modes.length
  const textarea = page.locator('[aria-label="Prompt"]').first()
  await textarea.click()
  await textarea.fill('A draft kept while the quota notice shows')
  await expect(textarea).toHaveValue('A draft kept while the quota notice shows')
  expect(stub.modes.length, 'back on the task the notice made no request of its own').toBe(afterLoad)
  expect(stub.proxied.includes('run.start'), 'nothing was sent').toBe(false)
  await frame(page, testInfo, '01-notice-92')
})

test('AQ-02 at 100 percent it says used up', async ({ app }, testInfo) => {
  const { page } = app
  await stubUsage(page, { windows: [fiveHour(100)] })
  await openSeededTask(app)
  await loadReports(app)
  await expect(page.getByTestId('ambient-quota-notice')).toBeVisible()
  await expect(page.getByTestId('ambient-quota-text')).toHaveText(/used up, resets/)
  await frame(page, testInfo, '02-notice-100')
})

test('AQ-03 a dismissed notice stays gone, also after a reload', async ({ app }, testInfo) => {
  const { page } = app
  await stubUsage(page, { windows: [fiveHour(92)] })
  await openSeededTask(app)
  await loadReports(app)
  await expect(page.getByTestId('ambient-quota-notice')).toBeVisible()
  await page.getByTestId('ambient-quota-dismiss').click()
  await expect(page.getByTestId('ambient-quota-notice')).toHaveCount(0)
  await page.reload()
  await openSeededTask(app)
  // The reload dropped the in-memory cache: the reports come back through Settings -> Usage, not through the notice.
  await loadReports(app)
  await expect(page.getByTestId('ambient-quota-notice')).toHaveCount(0)
  await frame(page, testInfo, '03-dismissed')
})

test('AQ-04 below the threshold there is no notice', async ({ app }, testInfo) => {
  const { page } = app
  await stubUsage(page, { windows: [fiveHour(80)] })
  await openSeededTask(app)
  await loadReports(app)
  await expect(page.getByTestId('ambient-quota-notice')).toHaveCount(0)
  await frame(page, testInfo, '04-below-threshold')
})

test('AQ-05 two windows: the higher shows first, the other after a dismissal', async ({ app }, testInfo) => {
  const { page } = app
  await stubUsage(page, { windows: [fiveHour(92), weekly(95)] })
  await openSeededTask(app)
  await loadReports(app)
  const text = page.getByTestId('ambient-quota-text')
  await expect(text).toHaveText(/^Weekly window 95% used, resets/)
  await frame(page, testInfo, '05-two-windows')
  await page.getByTestId('ambient-quota-dismiss').click()
  await expect(text).toHaveText(/^5-hour window 92% used, resets/)
  await frame(page, testInfo, '05b-two-windows-next')
})

test('AQ-06 a stale reading shows nothing', async ({ app }, testInfo) => {
  const { page } = app
  await stubUsage(page, { windows: [fiveHour(92)], fetchedAgoMs: 2 * 3600_000 })
  await openSeededTask(app)
  await loadReports(app)
  await expect(page.getByTestId('ambient-quota-notice')).toHaveCount(0)
  await frame(page, testInfo, '06-stale-reading')
})
