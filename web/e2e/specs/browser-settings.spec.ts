import fs from 'node:fs'
import path from 'node:path'
import { type Page, type TestInfo } from '@playwright/test'
import { openRpc } from '../fixtures/rpc'
import { type App, expect, test, waitForRpc } from '../fixtures/test'

// Settings -> Browser (docs/plans/completed/browser-for-agents-settings-ui.md). BS-01..BS-04 drive the REAL daemon's human-only
// browser.settings RPCs. BS-F* put the section in states a spec cannot reach without a model call or another daemon
// environment (kill switch, no browser installed, a live session) by answering the browser.* RPCs locally through
// page.routeWebSocket (the same proxying the grants spec uses). Frames are written only when BROWSER_FRAMES_DIR is set;
// every frame also asserts the theme's luminance and, at narrow widths, that nothing scrolls sideways.

type View = {
  workspaceId: number
  enabled: boolean
  allowHosts: string[]
  profileMode: string
  killSwitch: boolean
  available: boolean
  chromeInstalled: boolean
  installStep?: string
  note: string
  providers: { id: string; label: string; reach: string; why?: string; images: string }[]
  sessionLive: boolean
}

const NOTE =
  'A host name may only resolve to public addresses; to allow an intranet or local-network host, add its IP address instead. Loopback (localhost, 127.0.0.1, ::1) is always allowed and needs no entry.'
const PROVIDERS: View['providers'] = [
  { id: 'claude-native', label: 'Claude Code', reach: 'yes', images: 'yes' },
  { id: 'glm', label: 'GLM', reach: 'conditional', why: 'an ACP agent takes the browser only if it advertises MCP support; sMind cannot tell in advance; GLM cannot read images, so only the text tools are useful', images: 'no' },
  { id: 'opencode', label: 'OpenCode', reach: 'conditional', why: 'an ACP agent takes the browser only if it advertises MCP support; sMind cannot tell in advance', images: 'yes' },
  { id: 'codex-native', label: 'Codex', reach: 'no', why: 'Codex takes no per-run MCP servers', images: 'yes' },
]
const baseView = (over: Partial<View> = {}): View => ({
  workspaceId: 1,
  enabled: false,
  allowHosts: [],
  profileMode: 'ephemeral',
  killSwitch: false,
  available: true,
  chromeInstalled: true,
  note: NOTE,
  providers: PROVIDERS,
  sessionLive: false,
  ...over,
})

/** Answers browser.settings.get/set and browser.data.clear locally; everything else goes to the real daemon. */
async function stubBrowser(page: Page, initial: Partial<View>) {
  let view = baseView(initial)
  const calls: { method: string; params: Record<string, unknown> }[] = []
  await page.routeWebSocket(/\/ws/, (ws) => {
    const server = ws.connectToServer()
    server.onMessage((m) => ws.send(m))
    ws.onMessage((m) => {
      let msg: { id?: string; method?: string; params?: Record<string, unknown> } = {}
      try {
        msg = JSON.parse(String(m))
      } catch {
        /* not json */
      }
      const reply = (result: unknown) => ws.send(JSON.stringify({ id: msg.id, result }))
      const refuse = (message: string) => ws.send(JSON.stringify({ id: msg.id, error: { code: 'invalid', message } }))
      if (!msg.method?.startsWith('browser.')) return server.send(m)
      calls.push({ method: msg.method, params: msg.params ?? {} })
      if (msg.method === 'browser.settings.get') return reply(view)
      if (msg.method === 'browser.data.clear') {
        view = { ...view, sessionLive: false }
        return reply({})
      }
      if (msg.method === 'browser.settings.set') {
        const p = msg.params ?? {}
        if (typeof p.enabled === 'boolean') view = { ...view, enabled: p.enabled }
        for (const h of (p.allowAdd as string[] | undefined) ?? []) {
          if (/[*/@?# \\]|:\/\//.test(h)) return refuse(`browser.settings.set: browser: allowlist entry "${h}" must be one exact host, no scheme, wildcard, path or userinfo`)
          if (view.allowHosts.length >= 50) return refuse('browser.settings.set: at most 50 hosts may be allowed (51 given)')
          if (!view.allowHosts.includes(h.toLowerCase())) view = { ...view, allowHosts: [...view.allowHosts, h.toLowerCase()] }
        }
        for (const h of (p.allowRemove as string[] | undefined) ?? []) view = { ...view, allowHosts: view.allowHosts.filter((x) => x !== h) }
        return reply(view)
      }
      return refuse(`${msg.method}: not stubbed`)
    })
  })
  return { calls, set: (over: Partial<View>) => (view = { ...view, ...over }) }
}

/** True when neither the document nor the section nor any scrolling ancestor of it scrolls sideways (the app scrolls inside its own panes). */
const noSideScroll = (page: Page) =>
  page.evaluate(() => {
    const section = document.querySelector('[data-testid="settings-section-browser"]') as HTMLElement | null
    if (!section) return false
    for (let el: HTMLElement | null = section; el; el = el.parentElement) {
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
  expect(await noSideScroll(page), 'the section and its scrolling panes do not scroll sideways').toBe(true)
  const dir = process.env.BROWSER_FRAMES_DIR
  if (!dir) return
  fs.mkdirSync(dir, { recursive: true })
  // The app scrolls inside its own panes, so a plain full-page shot cuts the section off: grow the viewport until the
  // whole section is on screen, shoot it, and put the viewport back.
  const vp = page.viewportSize() as { width: number; height: number }
  await page.setViewportSize({ width: vp.width, height: 3200 })
  await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(() => r(0)))))
  const bottom = await page.evaluate(() => Math.ceil((document.querySelector('[data-testid="settings-section-browser"]') as HTMLElement).getBoundingClientRect().bottom))
  await page.screenshot({ path: path.join(dir, `${name}-${testInfo.project.name}.png`), clip: { x: 0, y: 0, width: vp.width, height: Math.min(3200, bottom + 48) } })
  await page.setViewportSize(vp)
}

async function openSection(app: App) {
  await app.openApp()
  await app.page.evaluate(() => window.dispatchEvent(new CustomEvent('smind:open-settings', { detail: { sectionId: 'browser' } })))
  await expect(app.page.getByTestId('settings-section-browser')).toBeVisible()
  // the section opens on the first workspace of the tree: the specs below (and the reset) assume it is alpha
  await expect(app.page.getByTestId('browser-workspace-select')).toContainText('alpha')
  // the dialog opens with a zoom animation that scales boxes: measure only once it has settled
  await app.page.evaluate(() => Promise.all(document.getAnimations().filter((a) => a.effect?.getComputedTiming().iterations !== Infinity).map((a) => a.finished.catch(() => undefined))))
}

/** Puts the first workspace's browser settings back to the defaults (the worker's daemon is shared by every spec). */
async function resetDaemonSettings(app: App) {
  const rpc = await openRpc(app.daemon.url)
  try {
    const id = app.daemon.ids.workspaces.alpha
    const cur = await rpc.call('browser.settings.get', { workspaceId: id })
    await rpc.call('browser.settings.set', { workspaceId: id, enabled: false, allowHosts: [] })
    expect(cur.workspaceId).toBe(id)
  } finally {
    rpc.close()
  }
}

test('BS-01 the section is off by default, the switch persists across a reload and turns off again', async ({ app }, testInfo) => {
  const { page } = app
  try {
    await openSection(app)
    const sw = page.getByTestId('browser-switch')
    await expect(sw).toHaveAttribute('aria-checked', 'false')
    await expect(page.getByTestId('browser-switch-state')).toHaveText('Off')
    await expect(page.getByTestId('browser-live')).toHaveCount(0)
    await expect(page.getByTestId('browser-allow-empty')).toHaveText('Only localhost is reachable.')
    const set = waitForRpc(page, 'browser.settings.set')
    await sw.click()
    const answered = (await set) as { result?: { enabled?: boolean } }
    expect(answered.result?.enabled).toBe(true)
    await expect(sw).toHaveAttribute('aria-checked', 'true')
    await expect(page.getByTestId('browser-switch-state')).toHaveText('On')
    await frame(page, testInfo, 'real-on-empty')
    await openSection(app) // a reload: the daemon stored it
    await expect(page.getByTestId('browser-switch')).toHaveAttribute('aria-checked', 'true')
    await page.getByTestId('browser-switch').click()
    await expect(page.getByTestId('browser-switch')).toHaveAttribute('aria-checked', 'false')
  } finally {
    await resetDaemonSettings(app)
  }
})

test("BS-02 an allowlist host can be added and removed, and an invalid one shows the daemon's own text", async ({ app }, testInfo) => {
  const { page } = app
  try {
    await openSection(app)
    const input = page.getByTestId('browser-allow-input')
    await input.fill('Intranet.Example')
    await page.getByTestId('browser-allow-add').click()
    await expect(page.getByTestId('browser-allow-list').getByText('intranet.example')).toBeVisible() // the daemon canonicalised it
    await expect(input).toHaveValue('')
    await expect(page.getByTestId('browser-allow-count')).toContainText('1 of 50')
    await input.fill('*.example.com')
    await page.getByTestId('browser-allow-add').click()
    const err = page.getByTestId('browser-allow-error')
    await expect(err).toContainText('must be one exact host, no scheme, wildcard, path or userinfo')
    await expect(err).not.toContainText('browser.settings.set') // the daemon's sentence, without its internal method and package labels
    expect(((await err.textContent()) ?? '').startsWith('Allowlist entry "*.example.com"')).toBe(true)
    await expect(input).toHaveAttribute('aria-invalid', 'true')
    await expect(input).toHaveValue('*.example.com') // what was typed is kept
    await expect(page.getByTestId('browser-allow-list').getByText('example.com')).toHaveCount(0)
    await frame(page, testInfo, 'real-invalid-host')
    await page.getByRole('button', { name: 'Remove intranet.example' }).click()
    await expect(page.getByTestId('browser-allow-empty')).toBeVisible()
  } finally {
    await resetDaemonSettings(app)
  }
})

test('BS-03 the residual text matches the public page and the providers list comes from the daemon', async ({ app }) => {
  const { page } = app
  await openSection(app)
  await expect(page.getByTestId('browser-residual-local')).toContainText('talks to the daemon without saying who it is counts as you')
  await expect(page.getByTestId('browser-residual-shell')).toContainText('protect against the model, not against a shell-capable agent')
  await expect(page.getByTestId('browser-residual-windows')).toHaveText('On Windows the browser is not offered.')
  const reach = page.getByTestId('browser-reach')
  await expect(reach.getByTestId('browser-reach-no')).toContainText('Codex')
  await expect(reach.getByTestId('browser-reach-yes')).toContainText('Claude Code')
  await expect(reach.getByTestId('browser-reach-yes')).toContainText('Screenshots')
  await expect(reach.getByTestId('browser-reach-no')).not.toContainText('Screenshots') // moot for an agent without the browser
})

test('BS-04 the section does not overflow and the clear-data dialog asks first', async ({ app }) => {
  const { page } = app
  await openSection(app)
  expect(await noSideScroll(page)).toBe(true)
  await page.getByTestId('browser-clear').click()
  await expect(page.getByTestId('browser-clear-dialog')).toBeVisible()
  await page.getByTestId('browser-clear-dialog').getByRole('button', { name: 'Cancel' }).click()
  await expect(page.getByTestId('browser-clear-dialog')).toHaveCount(0)
})

test('BS-F1 frames: off, on with no hosts, on with hosts', async ({ app }, testInfo) => {
  const { page } = app
  const stub = await stubBrowser(page, {})
  await openSection(app)
  await expect(page.getByTestId('browser-switch-state')).toHaveText('Off')
  await frame(page, testInfo, '01-off')
  await page.getByTestId('browser-switch').click()
  await expect(page.getByTestId('browser-switch-state')).toHaveText('On')
  await frame(page, testInfo, '02-on-empty')
  for (const h of ['intranet.example', '192.0.2.10']) {
    await page.getByTestId('browser-allow-input').fill(h)
    await page.getByTestId('browser-allow-add').click()
    await expect(page.getByTestId('browser-allow-list').getByText(h)).toBeVisible()
  }
  await frame(page, testInfo, '03-on-with-hosts')
  const ws = app.daemon.ids.workspaces.alpha
  expect(stub.calls.filter((c) => c.method !== 'browser.settings.get').map((c) => c.params)).toEqual([
    { workspaceId: ws, enabled: true },
    { workspaceId: ws, allowAdd: ['intranet.example'] },
    { workspaceId: ws, allowAdd: ['192.0.2.10'] },
  ])
})

test('BS-F2 frames: an invalid host shows the daemon text and keeps the input', async ({ app }, testInfo) => {
  const { page } = app
  await stubBrowser(page, { enabled: true, allowHosts: ['intranet.example'] })
  await openSection(app)
  await page.getByTestId('browser-allow-input').fill('https://*.example.com/path')
  await page.getByTestId('browser-allow-add').click()
  await expect(page.getByTestId('browser-allow-error')).toContainText('must be one exact host')
  await expect(page.getByTestId('browser-allow-input')).toHaveAttribute('aria-invalid', 'true')
  await frame(page, testInfo, '04-invalid-host')
})

test('BS-F3 frames: a live session, the kill switch and no browser installed', async ({ app }, testInfo) => {
  const { page } = app
  const stub = await stubBrowser(page, { enabled: true, allowHosts: ['intranet.example'], sessionLive: true })
  await openSection(app)
  await expect(page.getByTestId('browser-live')).toContainText('In use now')
  await expect(page.getByTestId('browser-end-session')).toBeVisible()
  const heightLive = (await page.getByTestId('browser-access').boundingBox())?.height ?? 0
  // the Profile card's text must keep a readable column next to the two buttons (it was squeezed to one word per line)
  const textBox = await page.getByTestId('browser-profile-text').boundingBox()
  expect(textBox?.width ?? 0, 'the profile text column').toBeGreaterThanOrEqual(240)
  expect(textBox?.height ?? 999, 'the profile text is not one word per line').toBeLessThanOrEqual(90)
  await frame(page, testInfo, '05-live')
  await page.getByTestId('browser-end-session').click()
  await expect(page.getByTestId('browser-live')).toHaveCount(0, { timeout: 10_000 }) // the next 3 s poll re-reads the view
  await expect(page.getByTestId('browser-idle')).toHaveText('Not in use right now.')
  // a run starting or ending must not move the page: the access card is exactly as tall either way
  expect(Math.abs(((await page.getByTestId('browser-access').boundingBox())?.height ?? 0) - heightLive)).toBeLessThanOrEqual(1)
  stub.set({ killSwitch: true, enabled: true })
  await openSection(app)
  await expect(page.getByTestId('browser-kill-switch')).toContainText('switched off by the daemon')
  await expect(page.getByTestId('browser-switch')).toBeDisabled()
  await frame(page, testInfo, '06-kill-switch')
  stub.set({ killSwitch: false, chromeInstalled: false, installStep: 'Install Google Chrome or Chromium (for example: brew install --cask chromium), then try again.' })
  await openSection(app)
  await expect(page.getByTestId('browser-no-chrome')).toContainText('No browser installed')
  await expect(page.getByTestId('browser-install-step')).toContainText('brew install --cask chromium')
  await frame(page, testInfo, '07-chrome-missing')
})

test('BS-F4 frames: the live Profile card at a mid width keeps its text readable (the card, not the viewport, decides the layout)', async ({ app }, testInfo) => {
  test.skip(app.narrow, 'a 700 px window is a mid width; the narrow projects are covered by BS-F3')
  const { page } = app
  await stubBrowser(page, { enabled: true, allowHosts: ['intranet.example'], sessionLive: true })
  await page.setViewportSize({ width: 700, height: 900 })
  await openSection(app)
  await expect(page.getByTestId('browser-live')).toBeVisible()
  const textBox = await page.getByTestId('browser-profile-text').boundingBox()
  expect(textBox?.width ?? 0, 'the profile text column at 700 px').toBeGreaterThanOrEqual(240)
  expect(textBox?.height ?? 999).toBeLessThanOrEqual(90)
  await frame(page, testInfo, '08-live-700')
})
