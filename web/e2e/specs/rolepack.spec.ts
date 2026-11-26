import fs from 'node:fs'
import path from 'node:path'
import { type Page, type TestInfo } from '@playwright/test'
import { openRpc } from '../fixtures/rpc'
import { type App, expect, test } from '../fixtures/test'

// Role-pack sync, PR 2 (docs/plans/active/role-pack-sync.md): the Settings -> Profiles banner and review dialog.
// RP-01 drives the REAL daemon's profile.rolePack.* RPCs. RP-02..RP-07 put the section in states a real daemon cannot
// be put in cheaply (drifted seats, a conflicting or refused apply, a locked database) by answering ONLY
// profile.rolePack.check and profile.rolePack.apply locally through page.routeWebSocket; everything else is proxied.
// Frames are written only when ROLEPACK_FRAMES_DIR is set; every frame also asserts the theme's luminance and that
// nothing scrolls sideways.

type Field = { name: string; kind: 'prompt' | 'capability'; state: 'current' | 'stale' | 'customised'; diff: string; stored?: string; want?: string }
type Seat = { name: string; id: number; status: 'ok' | 'missing' | 'owned'; updatedAt: string; untouched: boolean; hasUpdates: boolean; fields: Field[] }
type Check = { packVersion: number; seatsWithUpdates: number; seats: Seat[] }
type ApplyResult = { backupDir: string; dryRun: boolean; results: { seat: string; id: number; fields: string[]; outcome: string; error?: string }[] }
type ApplyAnswer = { result: ApplyResult } | { error: string }

const LEAD_DIFF =
  ' You are a Lead on sMind. Read the workspace protocol first.\n-You own one work item end to end: spec, code, tests.\n+You own one work item end to end: spec, implementation, tests, adversarial review, PR.\n Do small and medium items yourself.\n-Launch a Peer for any parallel part.\n+Launch a Peer only for an independent parallel part or as an independent verifier.'
const VALIDATOR_DIFF = '-My own validator notes: be extra strict about frames.\n+Judges one commit against one contract in a fresh context.'

const cur = (name: string, kind: Field['kind']): Field => ({ name, kind, state: 'current', diff: '' })
const missing = (name: string): Seat => ({ name, id: 0, status: 'missing', updatedAt: '', untouched: false, hasUpdates: false, fields: [] })

/** The drifted pack: lead has a stale prompt and a stale capability, validator has one customised prompt. */
const driftedCheck = (): Check => ({
  packVersion: 9,
  seatsWithUpdates: 1,
  seats: [
    missing('peer-sonnet'),
    {
      name: 'lead',
      id: 11,
      status: 'ok',
      updatedAt: '2026-11-20T10:00:00Z',
      untouched: true,
      hasUpdates: true,
      fields: [
        { name: 'instructions', kind: 'prompt', state: 'stale', diff: LEAD_DIFF },
        cur('notes', 'prompt'),
        cur('skills', 'prompt'),
        cur('model', 'capability'),
        { name: 'permissionMode', kind: 'capability', state: 'stale', diff: '-default\n+plan', stored: 'default', want: 'plan' },
        cur('orchestrationTools', 'capability'),
        cur('childPolicy', 'capability'),
        cur('onRateLimit', 'capability'),
      ],
    },
    {
      name: 'validator',
      id: 12,
      status: 'ok',
      updatedAt: '2026-11-20T10:00:00Z',
      untouched: false,
      hasUpdates: false,
      fields: [
        { name: 'instructions', kind: 'prompt', state: 'current', diff: '' },
        { name: 'notes', kind: 'prompt', state: 'customised', diff: VALIDATOR_DIFF },
        cur('skills', 'prompt'),
        cur('model', 'capability'),
        cur('permissionMode', 'capability'),
        cur('orchestrationTools', 'capability'),
        cur('childPolicy', 'capability'),
        cur('onRateLimit', 'capability'),
      ],
    },
    missing('steward'),
    missing('steward-standards'),
  ],
})

/** Every field current (no update, nothing customised). */
const currentCheck = (): Check => {
  const c = driftedCheck()
  c.seatsWithUpdates = 0
  for (const s of c.seats) {
    s.hasUpdates = false
    for (const f of s.fields) Object.assign(f, { state: 'current', diff: '', stored: undefined, want: undefined })
  }
  c.seats[1].untouched = true
  return c
}

const UPDATED: ApplyResult = {
  backupDir: '/home/user/.smind/backups/role-pack/20261126T101500Z-pack-v9',
  dryRun: false,
  results: [{ seat: 'lead', id: 11, fields: ['instructions'], outcome: 'updated' }],
}
const CHANGED: ApplyResult = { backupDir: '', dryRun: false, results: [{ seat: 'lead', id: 11, fields: ['instructions'], outcome: 'changed_meanwhile' }] }

/**
 * Answers profile.rolePack.check and profile.rolePack.apply locally; everything else goes to the real daemon.
 * `checkErrors` are answered (as errors) to the first checks in order; `applyAnswers` are consumed one per apply.
 * After an apply with an `updated` result the stale fields of that seat become current in `check`, so the refetch
 * shows the new state.
 */
async function stubRolePack(page: Page, opts: { check?: Check; checkErrors?: string[]; applyAnswers?: ApplyAnswer[] } = {}) {
  const check = opts.check ?? driftedCheck()
  const checkErrors = [...(opts.checkErrors ?? [])]
  const applyAnswers = [...(opts.applyAnswers ?? [])]
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
      const fail = (message: string) => ws.send(JSON.stringify({ id: msg.id, error: { code: 'internal', message } }))
      if (msg.method !== 'profile.rolePack.check' && msg.method !== 'profile.rolePack.apply') return server.send(m)
      calls.push({ method: msg.method, params: msg.params ?? {} })
      if (msg.method === 'profile.rolePack.check') {
        const err = checkErrors.shift()
        return err !== undefined ? fail(err) : reply(check)
      }
      const answer = applyAnswers.shift()
      if (!answer) return fail('profile.rolePack.apply: no answer queued in the stub')
      if ('error' in answer) return fail(answer.error)
      for (const r of answer.result.results) {
        if (r.outcome !== 'updated') continue
        const seat = check.seats.find((s) => s.name === r.seat)
        if (!seat) continue
        for (const f of seat.fields) if (f.state === 'stale' && (r.fields.includes(f.name) || f.kind === 'prompt')) Object.assign(f, { state: 'current', diff: '', stored: undefined, want: undefined })
        seat.hasUpdates = seat.fields.some((f) => f.state === 'stale')
        check.seatsWithUpdates = check.seats.filter((s) => s.hasUpdates).length
      }
      return reply(answer.result)
    })
  })
  const applies = () => calls.filter((c) => c.method === 'profile.rolePack.apply')
  const checks = () => calls.filter((c) => c.method === 'profile.rolePack.check')
  return { calls, applies, checks }
}

const SECTION = '[data-testid="settings-section-profiles"]'

/** True when neither the document nor the section nor any scrolling ancestor of it scrolls sideways. */
const noSideScroll = (page: Page) =>
  page.evaluate((sel) => {
    const section = document.querySelector(sel) as HTMLElement | null
    if (!section) return false
    for (let el: HTMLElement | null = section; el; el = el.parentElement) {
      if (el.scrollWidth > el.clientWidth + 1) return false
    }
    return document.documentElement.scrollWidth <= document.documentElement.clientWidth
  }, SECTION)

/** `kind: 'section'` grows the viewport and clips to the section; `'dialog'` takes a plain viewport shot with the dialog open. */
async function frame(page: Page, testInfo: TestInfo, name: string, kind: 'section' | 'dialog' = 'section') {
  const luminance = await page.evaluate(() => {
    const rgb = getComputedStyle(document.body).backgroundColor.match(/\d+(\.\d+)?/g)?.map(Number) ?? [0, 0, 0]
    return (0.2126 * rgb[0] + 0.7152 * rgb[1] + 0.0722 * rgb[2]) / 255
  })
  if (testInfo.project.use.colorScheme === 'dark') expect(luminance, 'dark frame: page background').toBeLessThan(0.35)
  else expect(luminance, 'light frame: page background').toBeGreaterThan(0.65)
  expect(await noSideScroll(page), 'the section and its scrolling panes do not scroll sideways').toBe(true)
  const dir = process.env.ROLEPACK_FRAMES_DIR
  if (!dir) return
  fs.mkdirSync(dir, { recursive: true })
  const file = path.join(dir, `${name}-${testInfo.project.name}.png`)
  if (kind === 'dialog') {
    await page.screenshot({ path: file })
    return
  }
  const vp = page.viewportSize() as { width: number; height: number }
  await page.setViewportSize({ width: vp.width, height: 3200 })
  await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(() => r(0)))))
  const bottom = await page.evaluate((sel) => Math.ceil((document.querySelector(sel) as HTMLElement).getBoundingClientRect().bottom), SECTION)
  await page.screenshot({ path: file, clip: { x: 0, y: 0, width: vp.width, height: Math.min(3200, bottom + 48) } })
  await page.setViewportSize(vp)
}

async function openSection(app: App) {
  await app.openApp()
  await app.page.evaluate(() => window.dispatchEvent(new CustomEvent('smind:open-settings', { detail: { sectionId: 'agents' } })))
  await expect(app.page.getByTestId('settings-section-profiles')).toBeVisible()
}

test('RP-01 the real daemon answers the check in the documented shape', async ({ app }, testInfo) => {
  const { page } = app
  const rpc = await openRpc(app.daemon.url)
  try {
    const check = await rpc.call('profile.rolePack.check')
    expect(typeof check.packVersion).toBe('number')
    expect(check.seats.map((s: { name: string }) => s.name)).toEqual(['peer-sonnet', 'lead', 'validator', 'steward', 'steward-standards'])
    for (const s of check.seats) expect(Array.isArray(s.fields), `${s.name}.fields`).toBe(true)
    expect(typeof check.seatsWithUpdates).toBe('number')
    await expect(rpc.call('profile.rolePack.apply', { take: ['nosuchseat'] })).rejects.toThrow(/nosuchseat/)
    const dry = await rpc.call('profile.rolePack.apply', { dryRun: true })
    expect(Array.isArray(dry.results)).toBe(true)
    expect(dry.dryRun).toBe(true)
  } finally {
    rpc.close()
  }
  await openSection(app)
  await expect(page.getByTestId('role-pack-banner').or(page.getByTestId('role-pack-current'))).toBeVisible()
  await frame(page, testInfo, 'real-banner-or-current')
})

test('RP-02 the banner offers Review and applies nothing on its own', async ({ app }, testInfo) => {
  const { page } = app
  const stub = await stubRolePack(page)
  await openSection(app)
  const banner = page.getByTestId('role-pack-banner')
  await expect(banner).toBeVisible()
  await expect(banner).toContainText('Role prompts have updates for 1 seat')
  await expect(page.getByTestId('role-pack-review-open')).toBeVisible()
  expect(stub.checks()).toHaveLength(1)
  expect(stub.applies()).toHaveLength(0)
  await frame(page, testInfo, '01-banner')
})

test('RP-03 the review lists the diff, marks the capability field and Apply sends no take', async ({ app }, testInfo) => {
  const { page } = app
  const stub = await stubRolePack(page, { applyAnswers: [{ result: UPDATED }] })
  await openSection(app)
  await page.getByTestId('role-pack-review-open').click()
  const review = page.getByTestId('role-pack-review')
  await expect(review).toBeVisible()
  const instructions = review.getByTestId('role-pack-field-lead-instructions')
  await expect(instructions).toContainText('Update available')
  await expect(instructions.locator('[data-diff="add"]').first()).toContainText('+You own one work item end to end: spec, implementation')
  await expect(instructions.locator('[data-diff="del"]').first()).toContainText('-You own one work item end to end: spec, code, tests.')
  const perm = review.getByTestId('role-pack-field-lead-permissionMode')
  await expect(perm).toContainText('capability')
  await expect(perm).toContainText('Stored: default')
  await expect(perm).toContainText('Pack: plan')
  await expect(review.getByTestId('role-pack-field-validator-notes')).toContainText('Edited by you')
  expect(stub.applies()).toHaveLength(0)
  await frame(page, testInfo, '02-dialog-diff', 'dialog')

  await page.getByTestId('role-pack-apply').click()
  await expect(page.getByTestId('role-pack-result')).toContainText('Updated instructions')
  await expect(page.getByTestId('role-pack-backup')).toContainText('/home/user/.smind/backups/role-pack/20261126T101500Z-pack-v9')
  expect(stub.applies()).toHaveLength(1)
  expect(stub.applies()[0].params.take).toEqual([])
  // the refetch shows the new state: the prompt is current, the capability field (never written by default) is still offered
  await expect(review.getByTestId('role-pack-field-lead-instructions')).toHaveCount(0)
  await expect(review.getByTestId('role-pack-field-lead-permissionMode')).toBeVisible()
  expect(stub.checks()).toHaveLength(2)
  await frame(page, testInfo, '03-after-apply', 'dialog')
})

test('RP-04 a refused or conflicting apply is shown as received', async ({ app }, testInfo) => {
  const { page } = app
  const refusal = 'profile.rolePack.apply: backup directory /root/x: permission denied'
  const stub = await stubRolePack(page, { applyAnswers: [{ result: CHANGED }, { error: refusal }] })
  await openSection(app)
  await page.getByTestId('role-pack-review-open').click()
  await expect(page.getByTestId('role-pack-review')).toBeVisible()
  await page.getByTestId('role-pack-apply').click()
  await expect(page.getByTestId('role-pack-result')).toContainText('lead changed since the check')
  await frame(page, testInfo, '04-conflict', 'dialog')
  await page.getByTestId('role-pack-apply').click()
  const err = page.getByTestId('role-pack-apply-error')
  await expect(err).toContainText('backup directory /root/x: permission denied')
  await expect(page.getByTestId('role-pack-review')).toBeVisible() // the dialog stays open on error
  expect(stub.applies()).toHaveLength(2)
  await frame(page, testInfo, '04-refused', 'dialog')
})

test('RP-05 when every seat is current there is no banner', async ({ app }, testInfo) => {
  const { page } = app
  const stub = await stubRolePack(page, { check: currentCheck() })
  await openSection(app)
  await expect(page.getByTestId('role-pack-current')).toContainText('Role prompts match pack v9')
  await expect(page.getByTestId('role-pack-banner')).toHaveCount(0)
  await expect(page.getByTestId('role-pack-review-open')).toHaveCount(0)
  expect(stub.applies()).toHaveLength(0)
  await frame(page, testInfo, '05-all-current')
})

test('RP-06 a failed check shows the error with a Retry', async ({ app }, testInfo) => {
  const { page } = app
  const stub = await stubRolePack(page, { checkErrors: ['profile.rolePack.check: database is locked'] })
  await openSection(app)
  const err = page.getByTestId('role-pack-error')
  await expect(err).toContainText('database is locked')
  await expect(page.getByTestId('role-pack-retry')).toBeVisible()
  await expect(page.getByTestId('role-pack-banner')).toHaveCount(0)
  await frame(page, testInfo, '06-check-error')
  await page.getByTestId('role-pack-retry').click()
  await expect(page.getByTestId('role-pack-banner')).toBeVisible()
  await expect(err).toHaveCount(0)
  expect(stub.checks()).toHaveLength(2)
  expect(stub.applies()).toHaveLength(0)
})

test('RP-07 a capability take asks first', async ({ app }, testInfo) => {
  const { page } = app
  const stub = await stubRolePack(page, { applyAnswers: [{ result: { backupDir: '/b', dryRun: false, results: [{ seat: 'lead', id: 11, fields: ['permissionMode'], outcome: 'updated' }] } }] })
  await openSection(app)
  await page.getByTestId('role-pack-review-open').click()
  await page.getByTestId('role-pack-take-lead-permissionMode').click()
  const confirm = page.getByTestId('role-pack-capability-confirm')
  await expect(confirm).toBeVisible()
  await expect(confirm).toContainText('default')
  await expect(confirm).toContainText('plan')
  expect(stub.applies()).toHaveLength(0)
  await frame(page, testInfo, '07-capability-confirm', 'dialog')
  await page.getByTestId('role-pack-capability-confirm-cancel').click()
  await expect(confirm).toHaveCount(0)
  expect(stub.applies()).toHaveLength(0)
  await page.getByTestId('role-pack-take-lead-permissionMode').click()
  await page.getByTestId('role-pack-capability-confirm-confirm').click()
  await expect(page.getByTestId('role-pack-result')).toContainText('Updated permissionMode')
  expect(stub.applies().map((c) => c.params.take)).toEqual([['lead.permissionMode']])
})
