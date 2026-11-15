import fs from 'node:fs'
import path from 'node:path'
import { type Page, type TestInfo } from '@playwright/test'
import { openTaskByLink } from '../fixtures/open-task'
import { type App, expect, test } from '../fixtures/test'

// Saved grants (docs/plans/active/saved-grants.md, PR4b): the daemon's permission events and the grant RPCs are injected into the
// app's own websocket (page.routeWebSocket proxies the real daemon socket) and answered locally, so no agent, provider or grant
// store is involved. Frames are written only when GRANT_FRAMES_DIR is set.

type Offer = { command: string; project: string; folder: string; expiresInDays: number; class: string; requiresOptIn: boolean; optInLabel?: string; note: string }
type Grant = { id: number; workspaceId: number; command: string; folder: string; class: string; runnerOptIn: boolean; createdAt: string; expiresAt: string; createdBy: string; useCount: number; lastUsedAt?: string; status: string }

type Bridge = {
  answers: Record<string, unknown>[]
  questionAnswers: Record<string, unknown>[]
  revokes: number[]
  /** The next run.answerPermission / run.answerQuestion fails with this message. */
  failNext(message: string): void
  setGrants(grants: Grant[]): void
  permission(requestId: string, detail?: Record<string, unknown>): void
  ask(requestId: string): void
  attached(): Promise<void>
}

async function bridge(page: Page, runId: string): Promise<Bridge> {
  const answers: Record<string, unknown>[] = []
  const questionAnswers: Record<string, unknown>[] = []
  const revokes: number[] = []
  let grants: Grant[] = []
  let failure: string | undefined
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
      const reply = (result: unknown) => ws.send(JSON.stringify({ id: msg.id, result }))
      const refuse = () => {
        const message = failure as string
        failure = undefined
        ws.send(JSON.stringify({ id: msg.id, error: { code: 'invalid', message } }))
      }
      if (msg.method === 'run.answerPermission') {
        answers.push(msg.params ?? {})
        return failure ? refuse() : reply({})
      }
      if (msg.method === 'run.answerQuestion') {
        questionAnswers.push(msg.params ?? {})
        return failure ? refuse() : reply({})
      }
      if (msg.method === 'permission.grants.list') return reply({ grants })
      if (msg.method === 'permission.grants.revoke') {
        const id = Number(msg.params?.grantId)
        revokes.push(id)
        grants = grants.map((g) => (g.id === id ? { ...g, status: 'revoked' } : g))
        return reply({ revoked: true })
      }
      server.send(m)
    })
  })
  const emit = (event: string, params: unknown) => toPage?.(JSON.stringify({ id: attachId, event, params }))
  return {
    answers,
    questionAnswers,
    revokes,
    failNext: (m) => (failure = m),
    setGrants: (g) => (grants = g),
    permission: (requestId, detail) =>
      emit('permission_request', {
        requestId,
        summary: 'run Bash',
        options: [
          { id: 'allow-1', label: 'Allow', kind: 'allow_once' },
          { id: 'deny-1', label: 'Deny', kind: 'reject_once' },
        ],
        ...(detail ? { detail } : {}),
      }),
    ask: (requestId) =>
      emit('question_request', {
        requestId,
        request: { source: 'ask_user', questions: [{ id: 'db', header: 'Database', text: 'Which database should the new service use?', kind: 'single', options: [{ id: 'pg', label: 'PostgreSQL' }, { id: 'sqlite', label: 'SQLite' }] }] },
      }),
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
  const dir = process.env.GRANT_FRAMES_DIR
  if (!dir) return
  fs.mkdirSync(dir, { recursive: true })
  await page.screenshot({ path: path.join(dir, `${name}-${testInfo.project.name}.png`) })
}

const noOverflow = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)

const READONLY: Offer = {
  command: 'git status',
  project: 'alpha',
  folder: '/work/alpha',
  expiresInDays: 30,
  class: 'readonly',
  requiresOptIn: false,
  note: 'Saves this exact read-only command for this project for 30 days.',
}
const RUNNER: Offer = {
  command: 'make build',
  project: 'alpha',
  folder: '/work/alpha',
  expiresInDays: 30,
  class: 'runner',
  requiresOptIn: true,
  optInLabel: "Also allow running this project's code (the agent can change it)",
  note: 'This command runs code defined in this project (a Makefile, script or test) that the agent can change later. Saving it needs the extra opt-in.',
}

test('G-01 a read-only offer shows its exact scope and saves with no opt-in', async ({ app }, testInfo) => {
  const { page } = app
  const b = await open(app, testInfo, 'g01')
  b.permission('rq-1', { kind: 'execute', command: 'git status', grant: READONLY })
  const control = page.getByTestId('grant-control')
  await expect(control).toBeVisible()
  await expect(control.getByTestId('grant-command')).toHaveText('git status')
  await expect(control.getByTestId('grant-project')).toContainText('alpha (/work/alpha)')
  await expect(control.getByTestId('grant-expiry')).toContainText('30 days')
  await expect(control.getByTestId('grant-class')).toHaveText('Read-only')
  await expect(control.getByTestId('grant-optin')).toHaveCount(0)
  expect(await noOverflow(page)).toBe(true)
  await frame(page, testInfo, 'a-readonly-offer')
  await control.getByTestId('grant-save').click()
  await expect.poll(() => b.answers.length).toBe(1)
  expect(b.answers[0]).toEqual({ runId: expect.any(String), requestId: 'rq-1', optionId: 'allow-1', scope: 'project' })
})

test('G-02 a runner offer needs the opt-in: unticked, disabled until ticked, then sends allowProjectCode', async ({ app }, testInfo) => {
  const { page } = app
  const b = await open(app, testInfo, 'g02')
  b.permission('rq-2', { kind: 'execute', command: 'make build', grant: RUNNER })
  const control = page.getByTestId('grant-control')
  await expect(control).toBeVisible()
  await expect(control.getByTestId('grant-class')).toHaveText('Runs project code')
  await expect(control.getByTestId('grant-optin')).toHaveAttribute('aria-checked', 'false')
  await expect(control.getByText("Also allow running this project's code (the agent can change it)")).toBeVisible()
  await expect(control.getByTestId('grant-save')).toBeDisabled()
  await expect(control.getByTestId('grant-hint')).toBeVisible()
  expect(await noOverflow(page)).toBe(true)
  await frame(page, testInfo, 'b-runner-unticked')
  await control.getByTestId('grant-optin').click()
  await expect(control.getByTestId('grant-save')).toBeEnabled()
  await frame(page, testInfo, 'c-runner-ticked')
  await control.getByTestId('grant-save').click()
  await expect.poll(() => b.answers.length).toBe(1)
  expect(b.answers[0]).toMatchObject({ requestId: 'rq-2', optionId: 'allow-1', scope: 'project', allowProjectCode: true })
})

test('G-03 the daemon refusing a save shows its words and leaves the card usable', async ({ app }, testInfo) => {
  const { page } = app
  const b = await open(app, testInfo, 'g03')
  b.permission('rq-3', { kind: 'execute', command: 'git status', grant: READONLY })
  await expect(page.getByTestId('grant-control')).toBeVisible()
  b.failNext('run.answerPermission: this request cannot be saved as an always-allow')
  await page.getByTestId('grant-save').click()
  // the Lane A card words a failed answer as a sentence, the daemon's own text on the line below it
  await expect(page.getByTestId('permission-respond-error')).toContainText('Could not send your answer')
  await expect(page.getByTestId('permission-respond-error')).toContainText('run.answerPermission: this request cannot be saved')
  await expect(page.getByTestId('grant-save')).toBeEnabled()
  await frame(page, testInfo, 'd-save-refused')
})

test('G-04 a request with no offer (the demo, a floor-judged or git request) shows no always-allow control', async ({ app }, testInfo) => {
  const { page } = app
  const b = await open(app, testInfo, 'g04')
  b.permission('rq-4', { kind: 'execute', command: 'rm -rf build' })
  await expect(page.getByTestId('pending-permission')).toBeVisible()
  await expect(page.getByTestId('grant-control')).toHaveCount(0)
  await frame(page, testInfo, 'e-no-offer')
})

test('G-05 Settings lists the saved grants with their class and status, and Revoke asks first', async ({ app }, testInfo) => {
  const { page } = app
  const b = await open(app, testInfo, 'g05')
  const now = Date.now()
  const day = 86_400_000
  const g = (over: Partial<Grant>): Grant => ({ id: 1, workspaceId: 1, command: 'git status', folder: '/work/alpha', class: 'readonly', runnerOptIn: false, createdAt: new Date(now - 2 * day).toISOString(), expiresAt: new Date(now + 28 * day).toISOString(), createdBy: 'local', useCount: 3, lastUsedAt: new Date(now - 3_600_000).toISOString(), status: 'active', ...over })
  b.setGrants([
    g({}),
    g({ id: 2, command: 'make build', class: 'runner', runnerOptIn: true, useCount: 12 }),
    g({ id: 3, command: 'make test', class: 'runner', runnerOptIn: true, status: 'void' }),
    g({ id: 4, command: 'ls -la', status: 'expired', expiresAt: new Date(now - day).toISOString(), useCount: 0, lastUsedAt: undefined }),
    g({ id: 5, command: 'echo <b>x</b> https://evil.example', status: 'revoked' }),
  ])
  await page.evaluate(() => window.dispatchEvent(new CustomEvent('smind:open-settings', { detail: { sectionId: 'saved-grants' } })))
  await expect(page.getByTestId('settings-section-saved-grants')).toBeVisible()
  await expect(page.getByTestId('grant-1-status')).toHaveText('Active')
  await expect(page.getByTestId('grant-2-class')).toHaveText('Runs project code')
  await expect(page.getByTestId('grant-3-status')).toContainText('Void')
  await expect(page.getByTestId('grant-4-status')).toHaveText('Expired')
  await expect(page.getByTestId('grant-5-command')).toHaveText('echo <b>x</b> https://evil.example')
  // void, expired and revoked rows drop the countdown; only an active one shows it
  for (const id of [3, 4, 5]) await expect(page.getByTestId(`grant-${id}-expiry`)).toHaveCount(0)
  for (const id of [1, 2]) await expect(page.getByTestId(`grant-${id}-expiry`)).toContainText('Expires in')
  await expect(page.getByTestId('grant-5-revoke')).toHaveCount(0)
  await expect(page.getByTestId('grant-5').locator('a, b')).toHaveCount(0)
  expect(await noOverflow(page)).toBe(true)
  await frame(page, testInfo, 'f-grants-list')
  await page.getByTestId('grant-5').scrollIntoViewIfNeeded()
  await frame(page, testInfo, 'f2-grants-list-end')
  await page.getByTestId('grant-2').scrollIntoViewIfNeeded()
  await page.getByTestId('grant-2-revoke').click()
  await expect(page.getByTestId('grant-revoke-copy')).toHaveText('make build will ask again in this project.')
  await frame(page, testInfo, 'g-revoke-confirm')
  await page.getByTestId('grant-revoke-confirm').click()
  await expect(page.getByTestId('grant-2-status')).toHaveText('Revoked')
  expect(b.revokes).toEqual([2])
  await frame(page, testInfo, 'h-after-revoke')
})

test('G-06 Settings says so when nothing is saved', async ({ app }, testInfo) => {
  const { page } = app
  await open(app, testInfo, 'g06')
  await page.evaluate(() => window.dispatchEvent(new CustomEvent('smind:open-settings', { detail: { sectionId: 'saved-grants' } })))
  await expect(page.getByTestId('grants-empty')).toContainText('Nothing saved')
  expect(await noOverflow(page)).toBe(true)
  await frame(page, testInfo, 'i-grants-empty')
})

test('G-07 a refused question answer reads as words with the daemon text under Details, and the note says when it is sent', async ({ app }, testInfo) => {
  const { page } = app
  const b = await open(app, testInfo, 'g07')
  b.ask('rq-q')
  await expect(page.getByTestId('question-card')).toBeVisible()
  await expect(page.getByTestId('question-note-helper')).toHaveText('Sent only if you decline.')
  await expect(page.getByTestId('question-decline-note')).toHaveAttribute('placeholder', 'Note for the agent (optional)')
  await page.getByTestId('question-option-db-sqlite').check()
  b.failNext('run.answerQuestion: runs: question request "rq-q": question "db": unknown option "sqlite"')
  await page.getByTestId('question-submit').click()
  await expect(page.getByTestId('question-error')).toContainText('That choice is no longer offered. Pick again.')
  await expect(page.getByTestId('question-error-details')).not.toHaveAttribute('open', '')
  await frame(page, testInfo, 'j-question-error')
  await page.getByTestId('question-error-details').locator('summary').click()
  await expect(page.getByTestId('question-error-details')).toContainText('unknown option "sqlite"')
  await frame(page, testInfo, 'k-question-error-details')
  expect(b.questionAnswers.length).toBe(1)
})
