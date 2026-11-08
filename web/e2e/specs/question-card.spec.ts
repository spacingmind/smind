import fs from 'node:fs'
import path from 'node:path'
import { type Page, type TestInfo } from '@playwright/test'
import { openTaskByLink } from '../fixtures/open-task'
import { type App, expect, test } from '../fixtures/test'

// ADR-0055: the daemon's question events are injected into the app's own websocket (page.routeWebSocket proxies the real daemon
// socket), and run.answerQuestion is answered locally, so no agent or provider is involved. Frames are written only when
// QUESTION_FRAMES_DIR is set.

type Question = { id: string; header?: string; text: string; kind: 'single' | 'multi' | 'text'; options?: { id: string; label: string; description?: string }[]; allowOther?: boolean }
type AnswerCall = { runId: string; requestId: string; action: string; answers?: { questionId: string; optionIds?: string[]; text?: string }[]; note?: string }

type Bridge = {
  calls: AnswerCall[]
  /** Holds run.answerQuestion's response until release() when set before the click. */
  hold(): void
  release(): void
  ask(requestId: string, questions: Question[], truncated?: boolean): void
  /** Injects a streamed chunk on the run's subscription: real timeline rows above the dock. */
  chunk(text: string): void
  resolve(requestId: string, reason: string, answer?: unknown): void
  attached(): Promise<void>
}

async function bridge(page: Page, runId: string): Promise<Bridge> {
  const calls: AnswerCall[] = []
  let attachId = ''
  let toPage: ((data: string) => void) | undefined
  let gate: Promise<void> | undefined
  let open: (() => void) | undefined
  let seen: () => void = () => {}
  const attachedP = new Promise<void>((r) => (seen = r))
  await page.routeWebSocket(/\/ws/, (ws) => {
    const server = ws.connectToServer()
    toPage = (data) => ws.send(data)
    server.onMessage((m) => ws.send(m))
    ws.onMessage(async (m) => {
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
      if (msg.method === 'run.answerQuestion') {
        calls.push(msg.params as AnswerCall)
        if (gate) await gate
        ws.send(JSON.stringify({ id: msg.id, result: {} }))
        return
      }
      server.send(m)
    })
  })
  const emit = (event: string, params: unknown) => toPage?.(JSON.stringify({ id: attachId, event, params }))
  return {
    calls,
    hold: () => {
      gate = new Promise<void>((r) => (open = r))
    },
    release: () => open?.(),
    ask: (requestId, questions, truncated) => emit('question_request', { requestId, request: { source: 'ask_user', questions, ...(truncated ? { truncated } : {}) } }),
    chunk: (text) => emit('chunk', { text }),
    resolve: (requestId, reason, answer) => emit('question_resolved', { requestId, reason, ...(answer ? { answer } : {}) }),
    attached: () => attachedP,
  }
}

async function open(app: App, testInfo: TestInfo, tag: string) {
  const title = `${tag}-${testInfo.project.name}-${testInfo.repeatEachIndex}`
  const t = await app.createTask(title, { scenario: 'hang', prompt: 'Wait for the question' })
  const b = await bridge(app.page, t.runId as string)
  await openTaskByLink(app, t, title)
  await b.attached()
  return b
}

async function frame(page: Page, testInfo: TestInfo, name: string) {
  const dir = process.env.QUESTION_FRAMES_DIR
  if (!dir) return
  fs.mkdirSync(dir, { recursive: true })
  await page.screenshot({ path: path.join(dir, `${name}-${testInfo.project.name}.png`) })
}

const DB: Question = {
  id: 'db',
  header: 'Database',
  text: 'Which database should the new service use?',
  kind: 'single',
  options: [
    { id: 'pg', label: 'PostgreSQL', description: 'Managed, strong consistency, what the other services use.' },
    { id: 'sqlite', label: 'SQLite', description: 'Embedded, no extra infrastructure, single writer.' },
    { id: 'mongo', label: 'MongoDB', description: 'Document store, flexible schema.' },
  ],
}

test('Q-01 a single-choice question starts empty, submits the chosen option and leaves a resolved marker', async ({ app }, testInfo) => {
  const { page } = app
  const b = await open(app, testInfo, 'q01')
  // Nothing mid-keystroke: the card takes focus when the ask lands (a focused composer keeps it, by design).
  await page.evaluate(() => (document.activeElement instanceof HTMLElement ? document.activeElement.blur() : undefined))
  b.ask('rq-1', [DB])
  await frame(page, testInfo, 't-appear-1') // the first screenshot after the ask is injected (~0ms)
  const card = page.getByTestId('question-card')
  await expect(card).toBeVisible()
  await frame(page, testInfo, 't-appear-2') // after the card is visible
  await expect(card).toBeFocused()
  await frame(page, testInfo, 't-appear-3') // settled: the card owns focus
  await expect(card.locator('input[type=radio]:checked')).toHaveCount(0)
  await expect(page.getByTestId('question-submit')).toBeDisabled()
  await frame(page, testInfo, 'a-single-choice')

  await page.getByTestId('question-option-db-sqlite').check()
  await expect(page.getByTestId('question-submit')).toBeEnabled()
  await frame(page, testInfo, 't-answer-1') // one option picked, Submit live
  b.hold()
  await page.getByTestId('question-submit').click()
  await expect(page.getByTestId('question-submit')).toBeDisabled()
  await expect(page.getByTestId('question-decline')).toBeDisabled()
  await expect(page.getByTestId('question-option-db-pg')).toBeDisabled()
  await frame(page, testInfo, 'g-submitting-disabled')
  await frame(page, testInfo, 't-answer-2') // same beat as g: the card locked while the answer is in flight
  expect(b.calls).toHaveLength(1)
  expect(b.calls[0]).toMatchObject({ requestId: 'rq-1', action: 'answer', answers: [{ questionId: 'db', optionIds: ['sqlite'] }] })

  b.release()
  b.resolve('rq-1', 'human', { answers: b.calls[0].answers })
  await expect(card).toHaveCount(0)
  const marker = page.getByTestId('timeline-question')
  await expect(marker).toContainText('Agent asked: Which database should the new service use?')
  await expect(marker).toContainText('answered')
  await frame(page, testInfo, 'e-resolved-marker')
  await frame(page, testInfo, 't-answer-3') // the ask closed: card gone, marker row in its place
})

test('Q-02 a multi-select question with an Other line sends the checked options and the other text', async ({ app }, testInfo) => {
  const { page } = app
  const b = await open(app, testInfo, 'q02')
  b.ask('rq-2', [
    {
      id: 'env',
      header: 'Targets',
      text: 'Which environments should the deploy cover?',
      kind: 'multi',
      allowOther: true,
      options: [
        { id: 'dev', label: 'Development', description: 'Shared dev cluster.' },
        { id: 'stg', label: 'Staging' },
        { id: 'prod', label: 'Production', description: 'Needs the release manager.' },
      ],
    },
  ])
  await expect(page.getByTestId('question-card')).toBeVisible()
  await expect(page.getByTestId('question-card').locator('input[type=checkbox]:checked')).toHaveCount(0)
  await expect(page.getByTestId('question-submit')).toBeDisabled()
  await page.getByTestId('question-option-env-dev').check()
  await page.getByTestId('question-option-env-stg').check()
  await page.getByTestId('question-other-env').fill('canary in eu-west')
  // The filled Other field stays named by its visible label (design review: a value alone said nothing).
  await expect(page.getByLabel('Other:')).toBeVisible()
  await expect(page.getByTestId('question-other-env')).toHaveValue('canary in eu-west')
  await frame(page, testInfo, 'k-other-filled')
  await frame(page, testInfo, 'b-multi-other')
  await page.getByTestId('question-submit').click()
  await expect.poll(() => b.calls.length).toBe(1)
  expect(b.calls[0]).toMatchObject({ requestId: 'rq-2', action: 'answer' })
  expect(b.calls[0].answers?.[0]).toMatchObject({ questionId: 'env', optionIds: ['dev', 'stg'], text: 'canary in eu-west' })
})

test('Q-03 a text question needs text, and Decline sends action decline with the note', async ({ app }, testInfo) => {
  const { page } = app
  const b = await open(app, testInfo, 'q03')
  b.ask('rq-3', [{ id: 'name', text: 'What should the new package be called?', kind: 'text' }])
  await expect(page.getByTestId('question-card')).toBeVisible()
  await expect(page.getByTestId('question-text-name')).toHaveValue('')
  await expect(page.getByTestId('question-submit')).toBeDisabled()
  await page.getByTestId('question-text-name').fill('billing-core')
  await expect(page.getByTestId('question-submit')).toBeEnabled()
  await frame(page, testInfo, 'c-text')
  await page.getByTestId('question-decline-note').fill('not now')
  await page.getByTestId('question-decline').click()
  await expect.poll(() => b.calls.length).toBe(1)
  expect(b.calls[0]).toMatchObject({ requestId: 'rq-3', action: 'decline', note: 'not now' })
  expect(b.calls[0].answers).toBeUndefined()
})

test('Q-04 an adversarial question renders as literal text: no links, no HTML, nothing preselected', async ({ app }, testInfo) => {
  const { page } = app
  const b = await open(app, testInfo, 'q04')
  const long = 'Please confirm the rollout plan. '.repeat(14)
  b.ask(
    'rq-4',
    [
      {
        id: 'adv',
        header: '**Urgent** [header](https://evil.example)',
        text: `${long}\n[Click here](https://evil.example/phish) or visit https://evil.example <b>bold</b> <img src=x onerror=alert(1)> **not bold** # not a heading`,
        kind: 'single',
        allowOther: true,
        options: Array.from({ length: 8 }, (_, i) => ({
          id: `o${i}`,
          label: `Option ${i + 1}: [label](https://evil.example/${i}) <i>raw</i> ${'long-label-text-'.repeat(i === 3 ? 8 : 1)}`,
          description: `Description ${i + 1} https://evil.example/d${i} **literal** ${'wrapping words '.repeat(i === 5 ? 10 : 1)}`,
        })),
      },
    ],
    true,
  )
  const card = page.getByTestId('question-card')
  await expect(card).toBeVisible()
  await expect(card.locator('a')).toHaveCount(0)
  await expect(card.locator('b, i, img, strong, em, h1')).toHaveCount(0)
  await expect(card).toContainText('[Click here](https://evil.example/phish)')
  await expect(card).toContainText('<b>bold</b>')
  await expect(card.locator('input[type=radio]')).toHaveCount(8)
  await expect(card.locator('input[type=radio]:checked')).toHaveCount(0)
  await expect(page.getByTestId('question-submit')).toBeDisabled()
  const box = await card.boundingBox()
  expect(box!.width).toBeLessThanOrEqual(page.viewportSize()!.width)
  await frame(page, testInfo, 'd-adversarial')

  // The long ask arrives collapsed (line-clamp is visual only -- the text above stays in the DOM),
  // with the options and the toggle on screen.
  const toggle = page.getByTestId('question-text-toggle')
  await expect(toggle).toBeVisible()
  await expect(toggle).toHaveAttribute('aria-expanded', 'false')
  await expect(toggle).toHaveText('Show all')
  await expect(card).toContainText('[Click here](https://evil.example/phish)')
  await frame(page, testInfo, 'i-long-collapsed')

  await toggle.click()
  await expect(toggle).toHaveAttribute('aria-expanded', 'true')
  await expect(toggle).toHaveText('Show less')
  await expect(card).toContainText('<img src=x onerror=alert(1)>')
  await frame(page, testInfo, 'j-long-expanded')
})

test('Q-05 a refused secret question leaves a refused marker and no card', async ({ app }, testInfo) => {
  const { page } = app
  const b = await open(app, testInfo, 'q05')
  b.ask('rq-5', [{ id: 'key', text: 'Paste your API key so I can continue', kind: 'text' }])
  await expect(page.getByTestId('question-card')).toBeVisible()
  b.resolve('rq-5', 'refused_secret')
  await expect(page.getByTestId('question-card')).toHaveCount(0)
  const marker = page.getByTestId('timeline-question')
  await expect(marker).toContainText('Paste your API key so I can continue')
  await expect(marker).toContainText('refused: secrets are not collected here')
  await frame(page, testInfo, 'f-refused-secret')
})

test('Q-06 a question the daemon restart orphaned resolves to a cancelled marker and no card', async ({ app }, testInfo) => {
  const { page } = app
  const b = await open(app, testInfo, 'q06')
  b.ask('rq-6', [
    {
      id: 'db',
      text: 'Which database should the migration target?',
      kind: 'single',
      options: [
        { id: 'pg', label: 'PostgreSQL' },
        { id: 'sqlite', label: 'SQLite' },
      ],
    },
  ])
  await expect(page.getByTestId('question-card')).toBeVisible()
  b.resolve('rq-6', 'daemon_restart')
  await expect(page.getByTestId('question-card')).toHaveCount(0)
  const marker = page.getByTestId('timeline-question')
  await expect(marker).toContainText('Which database should the migration target?')
  await expect(marker).toContainText('cancelled (daemon restarted)')
  await frame(page, testInfo, 'h-restart-marker')
})

test('Q-07 at 390px the docked card separates cleanly from the timeline rows above it', async ({ app }, testInfo) => {
  const { page } = app
  const b = await open(app, testInfo, 'q07')
  // A timeline with rows the dock must not cut off: streamed chunks the log shows above the card.
  b.chunk('First line of streamed output above the dock.\n')
  b.chunk('Second line: at 390px this row used to be half hidden behind the dock.\n')
  b.ask('rq-7', [
    {
      id: 'db',
      text: 'Which database should the fallback target?',
      kind: 'single',
      options: [
        { id: 'pg', label: 'PostgreSQL' },
        { id: 'sqlite', label: 'SQLite' },
      ],
    },
  ])
  await expect(page.getByTestId('question-card')).toBeVisible()
  await expect(page.getByTestId('timeline-assistant')).toContainText('Second line')
  const dock = page.getByTestId('pending-permission-dock')
  await expect(dock).toBeVisible()
  await expect(dock).toHaveCSS('border-top-width', '1px')
  await page.setViewportSize({ width: 390, height: 844 })
  await frame(page, testInfo, 'l-dock-narrow')
})
