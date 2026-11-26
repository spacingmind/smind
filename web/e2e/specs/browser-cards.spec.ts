import fs from 'node:fs'
import path from 'node:path'
import { type Locator, type Page, type TestInfo } from '@playwright/test'
import { openTaskByLink } from '../fixtures/open-task'
import { type App, expect, test } from '../fixtures/test'

// The daemon's browser permission cards (ADR-0061) in the real web UI: the open, close and read cards with the daemon's own
// text (copied from internal/browsercard), plus a long-option fixture. The permission_request events are injected into the app's
// own websocket (page.routeWebSocket proxies the real daemon socket, as the grants spec does): no agent, no browser, no model.
// docs/plans/completed/browser-for-agents-live-findings.md AC1 to AC3 and AC8. Frames are written only when BROWSER_CARD_FRAMES_DIR is set.

const OPEN_GRANT = 'Allow opening any page the browser policy allows (localhost and your added hosts) and closing tabs, for this run'
const CLOSE_GRANT = 'Allow closing tabs, and opening any page the browser policy allows (localhost and your added hosts), for this run'
const LONG_FIXTURE =
  'Allow every page the browser policy allows, on this computer and on the hosts you added in this workspace, for every tab this run opens, until the run ends or you turn the browser off'

type Option = { id: string; label: string; kind: string }
const allowOnce: Option = { id: 'allow_once', label: 'Allow once', kind: 'allow_once' }
const deny: Option = { id: 'deny', label: 'Deny', kind: 'reject_once' }

const CARDS: Record<string, { summary: string; options: Option[] }> = {
  open: {
    summary: 'Open http://127.0.0.1:4751/second (+query, 13 characters) (+fragment) in the browser? (localhost) "r4 close card" in "live" has used the browser 0 times. (daemon check)',
    options: [allowOnce, { id: 'allow_run', label: OPEN_GRANT, kind: 'allow_once' }, deny],
  },
  close: {
    summary: 'Close a browser tab? "r4 close card" in "live". (daemon check)',
    options: [allowOnce, { id: 'allow_run', label: CLOSE_GRANT, kind: 'allow_once' }, deny],
  },
  read: {
    summary:
      'Let this run read browser pages? "r1 main flow" wants to use the browser in "live" (read-only: tabs, snapshot, screenshot, console, wait). It can reach this computer\'s localhost and: 192.168.100.214. Page text is untrusted. (daemon check)',
    options: [allowOnce, { id: 'allow_run', label: 'Allow reads for this run', kind: 'allow_once' }, deny],
  },
  long: {
    summary: 'Open http://127.0.0.1:4751/ in the browser? (localhost) "long option fixture" in "live" has used the browser 0 times. (daemon check)',
    options: [allowOnce, { id: 'allow_run', label: LONG_FIXTURE, kind: 'allow_once' }, deny],
  },
}

type Bridge = { permission(requestId: string, card: keyof typeof CARDS): void; attached(): Promise<void> }

async function bridge(page: Page, runId: string): Promise<Bridge> {
  let attachId = ''
  let toPage: ((data: string) => void) | undefined
  let seen: () => void = () => {}
  const attachedP = new Promise<void>((r) => (seen = r))
  await page.routeWebSocket(/\/ws/, (ws) => {
    const server = ws.connectToServer()
    toPage = (data) => ws.send(data)
    server.onMessage((m) => ws.send(m))
    ws.onMessage((m) => {
      let msg: { id?: string; method?: string; params?: Record<string, unknown> } = {}
      try {
        msg = JSON.parse(String(m))
      } catch {
        /* not json */
      }
      if (msg.method === 'run.attach' && msg.params?.runId === runId) {
        attachId = String(msg.id)
        seen()
      }
      server.send(m)
    })
  })
  return {
    permission: (requestId, card) =>
      toPage?.(
        JSON.stringify({
          id: attachId,
          event: 'permission_request',
          params: { requestId, summary: CARDS[card].summary, options: CARDS[card].options, detail: { kind: 'browser' } },
        }),
      ),
    attached: () => attachedP,
  }
}

async function open(app: App, testInfo: TestInfo, tag: string) {
  const title = `${tag}-${testInfo.project.name}-${testInfo.repeatEachIndex}`
  const t = await app.createTask(title, { scenario: 'hang', prompt: 'Wait for a request' })
  const b = await bridge(app.page, t.runId as string)
  await openTaskByLink(app, t, title)
  await b.attached()
  return b
}

async function frame(page: Page, testInfo: TestInfo, name: string) {
  const dir = process.env.BROWSER_CARD_FRAMES_DIR
  if (!dir) return
  fs.mkdirSync(dir, { recursive: true })
  await page.screenshot({ path: path.join(dir, `${name}-${testInfo.project.name}.png`) })
}

const noOverflow = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)

/** Every option button sits inside the card's border box, and none is cut off. */
async function optionsStayInsideTheCard(card: Locator) {
  const box = await card.boundingBox()
  expect(box).not.toBeNull()
  const buttons = card.locator('[data-testid^="chat-permission-option-"]')
  const n = await buttons.count()
  expect(n).toBeGreaterThan(0)
  for (let i = 0; i < n; i++) {
    const b = await buttons.nth(i).boundingBox()
    expect(b, `option ${i}`).not.toBeNull()
    expect(b!.x, `option ${i} left edge`).toBeGreaterThanOrEqual(box!.x - 0.5)
    expect(b!.x + b!.width, `option ${i} right edge past the card (${box!.x + box!.width})`).toBeLessThanOrEqual(box!.x + box!.width + 0.5)
    const clipped = await buttons.nth(i).evaluate((el) => el.scrollWidth > el.clientWidth + 1)
    expect(clipped, `option ${i} label is cut off`).toBe(false)
  }
}

for (const name of ['open', 'close', 'read', 'long'] as const) {
  test(`BC-${name} the ${name} card: options wrap inside the card and none is the recommended action`, async ({ app }, testInfo) => {
    const { page } = app
    const b = await open(app, testInfo, `bc-${name}`)
    b.permission(`rq-${name}`, name)
    const card = page.getByTestId('pending-permission')
    await expect(card).toBeVisible()
    await expect(card).toContainText('(daemon check)')
    await optionsStayInsideTheCard(card)
    expect(await noOverflow(page)).toBe(true)
    // neutral: no green "approval" action on a browser security card, Deny is the only coloured text
    await expect(card.locator('[data-variant="approval"]')).toHaveCount(0)
    const allow = card.getByTestId('chat-permission-option-0')
    await expect(allow).toHaveText('Allow once')
    await expect(allow).toHaveAttribute('data-variant', 'outline')
    if (name === 'open') await expect(card).toContainText('(+query, 13 characters) (+fragment)')
    if (name === 'close') await expect(card).toContainText(CLOSE_GRANT)
    if (name === 'open' || name === 'long') {
      // a long label takes more than one line at every width where it does not fit on one
      const grant = card.getByTestId('chat-permission-option-1')
      const h = (await grant.boundingBox())!.height
      const narrow = (page.viewportSize()?.width ?? 1280) < 768
      if (narrow || name === 'long') expect(h, 'the long option wraps to several lines').toBeGreaterThan(48)
    }
    await frame(page, testInfo, `bc-${name}`)
  })
}
