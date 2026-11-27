import fs from 'node:fs'
import path from 'node:path'
import { type Page, type TestInfo } from '@playwright/test'
import { expect, test } from '../fixtures/test'

// B5 (docs/plans/active/config-switches-ui.md): the global config keys have a switch in Settings. UC5-01..05 run
// against the REAL daemon the harness starts (a direct loopback connection, so config.set is allowed) and read its
// config.yaml back; UC5-06..08 stub config.get / config.set through page.routeWebSocket for the read-only, env and
// refusal states. Each test starts from the config.yaml the harness wrote (the daemon re-reads it on every get and
// set), so tests never depend on each other or on their order. Frames only when B5_FRAMES_DIR is set.

const baseline = new Map<string, string>()

/** Puts the harness config back (plus the comment the edit must keep) before the page loads. */
function resetConfig(home: string, comment = false) {
  const file = path.join(home, 'config.yaml')
  if (!baseline.has(home)) baseline.set(home, fs.readFileSync(file, 'utf8'))
  fs.writeFileSync(file, baseline.get(home)! + (comment ? '# keep me\n' : ''))
  return file
}

const readConfig = (file: string) => fs.readFileSync(file, 'utf8')

const dialog = (page: Page) => page.getByTestId('settings-dialog')

async function openSettings(page: Page, sectionId: string) {
  await page.evaluate((id) => window.dispatchEvent(new CustomEvent('smind:open-settings', { detail: { sectionId: id } })), sectionId)
  await expect(dialog(page)).toBeVisible()
}

async function closeSettings(page: Page) {
  const narrowLayout = (page.viewportSize()?.width ?? 1280) < 768
  for (let i = 0; i < (narrowLayout ? 3 : 1); i++) {
    if (!(await dialog(page).isVisible())) break
    await page.keyboard.press('Escape')
  }
  await expect(dialog(page)).toBeHidden()
}

/** From the Automations settings section, the way a person gets to the page. */
async function openAutomations(page: Page) {
  await page.getByTestId('settings-open-automations').click()
  await expect(page.getByTestId('automations-screen')).toBeVisible()
}

async function settled(page: Page) {
  await page.evaluate(() =>
    Promise.all(
      document
        .getAnimations()
        .filter((a) => a.effect?.getComputedTiming().iterations !== Infinity)
        .map((a) => a.finished.catch(() => undefined)),
    ),
  )
}

async function shot(page: Page, testInfo: TestInfo, name: string) {
  const dir = process.env.B5_FRAMES_DIR
  if (!dir) return
  fs.mkdirSync(dir, { recursive: true })
  await settled(page)
  await page.screenshot({ path: path.join(dir, `${name}-${testInfo.project.name}.png`) })
}

const sw = (page: Page, id: string) => page.getByTestId(`${id}-switch`)

/** Turns a key on through its row: the confirm dialog opens, `confirm` clicks it. */
async function turnOn(page: Page, id: string) {
  await sw(page, id).click()
  const confirm = page.getByTestId(`${id}-dialog-confirm`)
  await expect(page.getByTestId(`${id}-dialog`)).toBeVisible()
  return confirm
}

const KEYS = ['automations.enabled', 'automations.paused', 'plugins.enabled', 'fleet.enabled', 'learning.enabled']
const HOMES = ['config-automations-enabled', 'config-plugins-enabled', 'config-fleet-enabled', 'config-learning-enabled']

type Row = { key: string; value: string; default: string; source: string; restart: boolean; writable: boolean; env?: string }
const row = (key: string, over: Partial<Row> = {}): Row => ({
  key,
  value: key === 'learning.enabled' ? 'true' : 'false',
  default: key === 'learning.enabled' ? 'true' : 'false',
  source: 'default',
  restart: key === 'fleet.enabled' || key === 'learning.enabled',
  writable: true,
  ...over,
})

/** Proxies the daemon, answering config.get from `rows` and recording every config.set; `setError` answers config.set with an error. */
async function stubConfig(page: Page, opts: { rows?: Row[]; setError?: string }) {
  const sets: unknown[] = []
  await page.routeWebSocket(/\/ws/, (ws) => {
    const server = ws.connectToServer()
    server.onMessage((m) => ws.send(m))
    ws.onMessage((m) => {
      let msg: { id?: string | number; method?: string; params?: unknown }
      try {
        msg = JSON.parse(String(m))
      } catch {
        return server.send(m)
      }
      if (msg.method === 'config.get' && opts.rows) return ws.send(JSON.stringify({ id: msg.id, result: { keys: opts.rows } }))
      if (msg.method === 'config.set') {
        sets.push(msg.params)
        if (opts.setError) return ws.send(JSON.stringify({ id: msg.id, error: { message: opts.setError } }))
      }
      server.send(m)
    })
  })
  return sets
}

test('UC5-01 automations switch end to end on the real daemon', async ({ app }, testInfo) => {
  const { page, daemon } = app
  const file = resetConfig(daemon.home, true)
  await app.openApp()
  await openSettings(page, 'automations')
  const id = 'config-automations-enabled'
  await expect(sw(page, id)).toHaveAttribute('aria-checked', 'false')

  const confirm = await turnOn(page, id)
  await expect(page.getByTestId(`${id}-dialog`)).toContainText(/\S/)
  expect(readConfig(file)).not.toContain('automations')
  await shot(page, testInfo, 'UC5-01-confirm')
  await confirm.click()

  await expect(sw(page, id)).toHaveAttribute('aria-checked', 'true')
  await expect.poll(() => readConfig(file)).toContain('automations:')
  const text = readConfig(file)
  expect(text).toMatch(/enabled: true/)
  expect(text).toContain('# keep me')
  await shot(page, testInfo, 'UC5-01-on')

  await openAutomations(page)
  await expect(page.getByTestId('automations-off-card')).toHaveCount(0)
})

test('UC5-02 plugins on the real daemon', async ({ app }, testInfo) => {
  const { page, daemon } = app
  const file = resetConfig(daemon.home)
  await app.openApp()
  await openSettings(page, 'plugins')
  const id = 'config-plugins-enabled'
  await expect(sw(page, id)).toHaveAttribute('aria-checked', 'false')
  await (await turnOn(page, id)).click()
  await expect(sw(page, id)).toHaveAttribute('aria-checked', 'true')
  await expect.poll(() => readConfig(file)).toContain('plugins:')
  expect(readConfig(file)).toMatch(/enabled: true/)
  await shot(page, testInfo, 'UC5-02-on')
})

test('UC5-03 fleet on the real daemon shows the restart notice', async ({ app }, testInfo) => {
  const { page, daemon } = app
  const file = resetConfig(daemon.home)
  await app.openApp()
  await openSettings(page, 'hosts')
  const id = 'config-fleet-enabled'
  await expect(page.getByTestId('hosts-off')).toBeVisible()
  await shot(page, testInfo, 'UC5-03-off')
  await (await turnOn(page, id)).click()
  await expect(page.getByTestId(`${id}-restart`)).toContainText('Takes effect after the daemon restarts')
  await expect.poll(() => readConfig(file)).toContain('fleet:')
  expect(readConfig(file)).toMatch(/enabled: true/)
  await shot(page, testInfo, 'UC5-03-on')
})

test('UC5-04 learning kill switch on the real daemon', async ({ app }, testInfo) => {
  const { page, daemon } = app
  const file = resetConfig(daemon.home)
  await app.openApp()
  await openSettings(page, 'learned-skills')
  const id = 'config-learning-enabled'
  await expect(sw(page, id)).toBeVisible()
  await expect(sw(page, id)).toHaveAttribute('aria-checked', 'true')

  await sw(page, id).click() // turning off asks nothing
  await expect(page.getByTestId(`${id}-dialog`)).toHaveCount(0)
  await expect(sw(page, id)).toHaveAttribute('aria-checked', 'false')
  await expect.poll(() => readConfig(file)).toContain('learning:')
  expect(readConfig(file)).toMatch(/enabled: false/)
  await expect(page.getByTestId(`${id}-restart`)).toBeVisible()
  await shot(page, testInfo, 'UC5-04-off')

  await (await turnOn(page, id)).click()
  await expect(sw(page, id)).toHaveAttribute('aria-checked', 'true')
  await expect.poll(() => readConfig(file)).toMatch(/enabled: true/)
})

test('UC5-05 pause and resume on the real daemon', async ({ app }, testInfo) => {
  const { page, daemon } = app
  const file = resetConfig(daemon.home)
  await app.openApp()
  await openSettings(page, 'automations')
  const on = 'config-automations-enabled'
  await (await turnOn(page, on)).click()
  await expect(sw(page, on)).toHaveAttribute('aria-checked', 'true')
  await openAutomations(page)
  await page.getByTestId('automations-pause').click()
  await expect(page.getByTestId('automations-paused')).toBeVisible()
  await expect.poll(() => readConfig(file)).toMatch(/paused: true/)
  await shot(page, testInfo, 'UC5-05-paused')

  const paused = 'config-automations-paused'
  await sw(page, paused).click()
  await page.getByTestId(`${paused}-dialog-confirm`).click()
  await expect.poll(() => readConfig(file)).toMatch(/paused: false/)
  await expect(page.getByTestId('automations-paused')).toHaveCount(0)
})

test('UC5-06 relay-style read-only', async ({ app }, testInfo) => {
  const { page } = app
  const sets = await stubConfig(page, { rows: KEYS.map((k) => row(k, { writable: false })) })
  await app.openApp()
  for (const [section, id] of [
    ['automations', HOMES[0]],
    ['plugins', HOMES[1]],
    ['hosts', HOMES[2]],
    ['learned-skills', HOMES[3]],
  ]) {
    await openSettings(page, section)
    await expect(page.getByTestId(`${id}-read-only`)).toContainText("Change this on the daemon's machine")
    await expect(page.getByTestId(`${id}-switch`)).toHaveCount(0)
    await shot(page, testInfo, `UC5-06-${section}`)
    await closeSettings(page)
  }
  expect(sets).toEqual([])
})

test('UC5-07 env-overridden', async ({ app }, testInfo) => {
  const { page } = app
  const sets = await stubConfig(page, {
    rows: KEYS.map((k) => (k === 'learning.enabled' ? row(k, { value: 'false', source: 'env', env: 'SMIND_LEARNING', writable: false }) : row(k))),
  })
  await app.openApp()
  await openSettings(page, 'learned-skills')
  const id = 'config-learning-enabled'
  await expect(sw(page, id)).toBeDisabled()
  await expect(page.getByTestId(`${id}-env`)).toHaveText('Overridden by SMIND_LEARNING')
  await shot(page, testInfo, 'UC5-07-env')
  expect(sets).toEqual([])
})

test('UC5-08 refusal', async ({ app }, testInfo) => {
  const { page } = app
  const refusal = 'overridden by SMIND_LEARNING, unset it first'
  const sets = await stubConfig(page, { setError: refusal })
  await app.openApp()
  await openSettings(page, 'learned-skills')
  const id = 'config-learning-enabled'
  await expect(sw(page, id)).toHaveAttribute('aria-checked', 'true')
  await sw(page, id).click()
  await expect(page.getByTestId(`${id}-error`)).toContainText('Overridden by SMIND_LEARNING, unset it first')
  await expect(sw(page, id)).toHaveAttribute('aria-checked', 'true')
  expect(sets).toEqual([{ key: 'learning.enabled', value: 'false' }])
  await shot(page, testInfo, 'UC5-08-refusal')
})
