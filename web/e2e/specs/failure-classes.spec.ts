import fs from 'node:fs'
import path from 'node:path'
import { type Page, type TestInfo } from '@playwright/test'
import { openTaskByLink } from '../fixtures/open-task'
import { type App, expect, test, waitForRpc } from '../fixtures/test'

// R3a-2: the three human-action failure classes and the cli_exited card, shown on a real
// failed run. The e2e daemon's UAT agent fails the run (scenario 'error'); page.routeWebSocket
// proxies the real daemon socket and rewrites the server side, so every payload that names the
// failed run -- run.list rows, run.logs results, run.status events and notifications -- carries
// the class under test (failureKind + failureAction {kind, target?, label, summary}). No agent
// or provider is contacted beyond the fake one; frames are written only when FAILURE_FRAMES_DIR
// is set.

type FailureActionWire = { kind: string; target?: string; label: string; summary: string }
type ClassSpec = { kind: string; action: FailureActionWire }

const CLASSES: Record<string, ClassSpec> = {
  'context-window': {
    kind: 'context_window_exceeded',
    action: { kind: 'new_chat', target: 'chat:new', label: 'Start a fresh chat for this task', summary: "The conversation no longer fits the model's context window." },
  },
  'session-budget': {
    kind: 'session_budget_exceeded',
    action: { kind: 'raise_budget', label: "Raise the session budget in the provider's settings, or start a fresh chat", summary: "The provider's session budget for this run is used up." },
  },
  'too-many-denials': {
    kind: 'too_many_denials',
    action: { kind: 'review_policy', target: 'settings:permission-policy', label: 'Review the permission policy', summary: 'The agent kept asking for actions that were denied, so the provider stopped the run.' },
  },
  'cli-exited': {
    kind: 'cli_exited',
    action: { kind: 'continue', label: 'Continue the run', summary: 'The agent stopped before finishing; its session is saved, so you can continue where it left off.' },
  },
}

/** Rewrites every object that names one of `runs` (a run.list row's PascalCase ID or a camelCase runId) to carry that run's failure class. */
function patchDeep(node: unknown, runs: Map<string, ClassSpec>): void {
  if (Array.isArray(node)) {
    for (const n of node) patchDeep(n, runs)
    return
  }
  if (node === null || typeof node !== 'object') return
  const o = node as Record<string, unknown>
  if (typeof o.ID === 'string' && runs.has(o.ID)) {
    const spec = runs.get(o.ID)!
    o.FailureKind = spec.kind
    o.FailureAction = { ...spec.action }
  } else if (typeof o.runId === 'string' && runs.has(o.runId)) {
    const spec = runs.get(o.runId)!
    o.failureKind = spec.kind
    o.action = { ...spec.action }
  }
  for (const v of Object.values(o)) if (v !== null && typeof v === 'object') patchDeep(v, runs)
}

/** Proxies the app's websocket, rewriting server frames so the failed run reports the class under test. */
async function patchFailureClasses(page: Page, runs: Map<string, ClassSpec>): Promise<void> {
  await page.routeWebSocket(/\/ws/, (ws) => {
    const server = ws.connectToServer()
    server.onMessage((m) => {
      let msg: unknown
      try {
        msg = JSON.parse(String(m))
      } catch {
        ws.send(m)
        return
      }
      patchDeep(msg, runs)
      ws.send(JSON.stringify(msg))
    })
    ws.onMessage((m) => server.send(m))
  })
}

async function frame(page: Page, testInfo: TestInfo, name: string): Promise<void> {
  const dir = process.env.FAILURE_FRAMES_DIR
  if (!dir) return
  fs.mkdirSync(dir, { recursive: true })
  await page.screenshot({ path: path.join(dir, `${name}-${testInfo.project.name}.png`) })
}

/** A failed task whose run reports `spec`, opened on its chat, with the class card asserted visible. */
async function openFailedTask(app: App, testInfo: TestInfo, tag: string, spec: ClassSpec): Promise<Page> {
  const title = `${tag}-${testInfo.project.name}`
  const t = await app.createTask(title, { scenario: 'error', prompt: 'This will fail' })
  if (!t.runId) throw new Error('run.start returned no runId')
  await patchFailureClasses(app.page, new Map([[t.runId, spec]]))
  await openTaskByLink(app, t, title)
  await expect(app.page.getByTestId('run-error-box').first()).toBeVisible({ timeout: 20_000 })
  await expect(app.page.getByTestId('run-error').first()).toHaveText(spec.action.summary)
  return app.page
}

test('F-01 a context-window failure shows the sentence, the reason and Start a fresh chat', async ({ app }, testInfo) => {
  const spec = CLASSES['context-window']
  const page = await openFailedTask(app, testInfo, 'f01', spec)
  await expect(page.getByTestId('run-failure-hint')).toHaveText('Earlier turns stay in this chat.')
  // The footer names the class only; the card above it carries the whole sentence.
  await expect(page.getByTestId('run-stop-reason').first()).toHaveText('Context window full')
  await frame(page, testInfo, 'a-context-window')
  // The control is the "+" tab button's own path: chat.create through the app's smind:new-chat event.
  const chatCreated = waitForRpc(page, 'chat.create')
  await page.getByTestId('run-failure-new-chat').click()
  await chatCreated
})

test('F-02 a session-budget failure shows the sentence and the label as its hint, with no control', async ({ app }, testInfo) => {
  const spec = CLASSES['session-budget']
  const page = await openFailedTask(app, testInfo, 'f02', spec)
  await expect(page.getByTestId('run-failure-hint')).toHaveText(spec.action.label)
  await expect(page.getByTestId('run-stop-reason').first()).toHaveText('Session budget used up')
  await expect(page.getByTestId('run-failure-signin')).toHaveCount(0)
  await expect(page.getByTestId('run-failure-new-chat')).toHaveCount(0)
  await expect(page.getByTestId('run-failure-review-policy')).toHaveCount(0)
  await frame(page, testInfo, 'b-session-budget')
})

test('F-03 too many denials offers Review the permission policy, which opens Settings on that section', async ({ app }, testInfo) => {
  const spec = CLASSES['too-many-denials']
  const page = await openFailedTask(app, testInfo, 'f03', spec)
  // The control's own words are not repeated as a hint.
  await expect(page.getByTestId('run-failure-hint')).toHaveCount(0)
  await expect(page.getByTestId('run-stop-reason').first()).toHaveText('Stopped after too many denials')
  await frame(page, testInfo, 'c-too-many-denials')
  await page.getByTestId('run-failure-review-policy').click()
  await expect(page.getByTestId('settings-screen')).toBeVisible()
  await expect(page.getByTestId('settings-section-permission-policy')).toBeVisible()
  await frame(page, testInfo, 'e-review-policy-opened')
})

test('F-04 a cli_exited failure reads as a sentence a person can act on', async ({ app }, testInfo) => {
  const spec = CLASSES['cli-exited']
  const page = await openFailedTask(app, testInfo, 'f04', spec)
  await expect(page.getByTestId('run-failure-hint')).toHaveCount(0)
  await frame(page, testInfo, 'd-cli-exited')
})

test('F-05 the sidebar shows the failed tasks and each class action label', async ({ app }, testInfo) => {
  test.skip(testInfo.project.name.endsWith('390'), 'wide-only: at 390 the sidebar is a drawer, not a column of rows')
  const specs = ['context-window', 'session-budget', 'too-many-denials']
  const runs = new Map<string, ClassSpec>()
  const titles: string[] = []
  for (const key of specs) {
    const title = `f05-${key}-${testInfo.project.name}`
    const t = await app.createTask(title, { scenario: 'error', prompt: 'This will fail' })
    if (!t.runId) throw new Error('run.start returned no runId')
    runs.set(t.runId, CLASSES[key])
    titles.push(title)
  }
  await patchFailureClasses(app.page, runs)
  await app.openApp()
  await app.openDrawer()
  // Each failed task's row carries its class's action label (the row's hover card).
  for (const title of titles) {
    const row = app.page.getByTestId('sidebar-task-row').filter({ hasText: title }).first()
    await expect(row).toBeVisible()
  }
  const row = app.page.getByTestId('sidebar-task-row').filter({ hasText: titles[0] }).first()
  await row.hover()
  await expect(app.page.getByTestId('sidebar-task-rate-limit').first()).toHaveText(CLASSES['context-window'].action.label)
  await frame(app.page, testInfo, 'f-sidebar-rows')
})
