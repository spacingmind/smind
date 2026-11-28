import { type Page } from '@playwright/test'
import { expect, test, waitForRpc } from '../fixtures/test'

// B6 (docs/plans/active/ui-cleanup-b6.md, D5 / AC8): a relay session may brake an automation (`automation.disable`)
// and nothing else that mutates; `automation.enable` and `automation.resume` are human + local only and answer
// `forbidden` (ADR-0044: "a brake, never an accelerator"). UC6-04 pins the Automations page over such a connection.
// STUB: a relay session cannot be started by the harness, so the proxy below answers the automation.* RPCs the way
// the daemon would from one; everything else is forwarded to the real daemon.

const listing = {
  enabled: true,
  paused: false,
  automations: [
    {
      id: 1,
      name: 'Nightly triage',
      enabled: true,
      workspaceId: 7,
      signal: { kind: 'cron', cron: '0 3 * * *' },
      template: {
        profileId: 3,
        model: 'haiku',
        mode: 'default',
        titleTemplate: 'Triage {{signal.scheduledAt}}',
        promptTemplate: 'Look at the repo.',
        budgetUsd: 3,
        maxRunMinutes: 45,
      },
      rails: { maxRunsPerHour: 2, dedupeWindowMinutes: 0, pauseAfterFailures: 3, memoryEnabled: true, memoryMaxBytes: 16384 },
      status: 'active',
      pausedReason: '',
      pausedAt: null,
      consecutiveFailures: 0,
      budgetBlocked: false,
      lastFiredAt: null,
      nextRunAt: '2026-10-28T03:00:00Z',
      nextRuns: ['2026-10-28T03:00:00Z', '2026-10-29T03:00:00Z', '2026-10-30T03:00:00Z'],
      memoryEnabled: true,
      createdAt: '2026-10-26T00:00:00Z',
      updatedAt: '2026-10-26T00:00:00Z',
    },
    {
      id: 2,
      name: 'CI fixer',
      enabled: false,
      workspaceId: 7,
      signal: { kind: 'github.workflow_run', workflow: 'ci', branch: 'develop', conclusions: ['failure'] },
      template: {
        profileId: 3,
        model: 'haiku',
        mode: 'default',
        titleTemplate: 'Triage {{signal.scheduledAt}}',
        promptTemplate: 'Look at the repo.',
        budgetUsd: 3,
        maxRunMinutes: 45,
      },
      rails: { maxRunsPerHour: 2, dedupeWindowMinutes: 1440, pauseAfterFailures: 3, memoryEnabled: false, memoryMaxBytes: 16384 },
      status: 'active',
      pausedReason: '',
      pausedAt: null,
      consecutiveFailures: 0,
      budgetBlocked: false,
      lastFiredAt: null,
      nextRunAt: null,
      memoryEnabled: false,
      createdAt: '2026-10-26T00:00:00Z',
      updatedAt: '2026-10-26T00:00:00Z',
    },
  ],
}

/** Proxies the daemon, answering the automation surface like a relay session: disable passes, enable / resume refuse. */
async function stubRelayAutomations(page: Page) {
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
      if (msg.method === 'automation.list') return ws.send(JSON.stringify({ id: msg.id, result: listing }))
      if (msg.method === 'automation.disable') {
        const id = (msg.params as { id?: number } | undefined)?.id
        const found = listing.automations.find((a) => a.id === id)
        return ws.send(JSON.stringify({ id: msg.id, result: { ...found, enabled: false } }))
      }
      if (msg.method === 'automation.enable' || msg.method === 'automation.resume') {
        return ws.send(
          JSON.stringify({
            id: msg.id,
            error: { message: `${msg.method}: forbidden: not available over the relay`, code: 'forbidden' },
          }),
        )
      }
      server.send(m)
    })
  })
}

async function openSettings(page: Page, sectionId: string) {
  await page.evaluate((id) => window.dispatchEvent(new CustomEvent('smind:open-settings', { detail: { sectionId: id } })), sectionId)
  await expect(page.getByTestId('settings-dialog')).toBeVisible()
}

// UC6-01 (B6 D4 / AC9): NO stub -- the group talks to the real e2e daemon, which runs no relay bridge, so no phone
// is ever paired or push-registered there. Whatever it answers for relay.device.list / push.test, the group must
// show the matching honest state: a count (or "Not known on this daemon." if the list is refused), and either the
// "no device" copy or the push_disabled sentence -- never an error banner, never a raw "push.test:" prefix.
test('UC6-01 mobile push on the real daemon: no relay bridge, no device is honest', async ({ app }) => {
  const { page } = app
  const pageErrors: string[] = []
  page.on('pageerror', (e) => pageErrors.push(e.message))
  await app.openApp()
  const listAnswer = waitForRpc(page, 'relay.device.list')
  await openSettings(page, 'notifications')
  await expect(page.getByTestId('mobile-push-group')).toBeVisible()
  const list = await listAnswer
  if (list.error) {
    await expect(page.getByTestId('push-count')).toHaveText('Not known on this daemon.')
  } else {
    const devices = (list.result as { devices?: { push?: boolean }[] }).devices ?? []
    const withPush = devices.filter((d) => d.push === true).length
    await expect(page.getByTestId('push-count')).toHaveText(`${withPush} with push on`)
  }

  const testAnswer = waitForRpc(page, 'push.test')
  await page.getByTestId('push-test').click()
  const answer = await testAnswer
  if (answer.error) {
    await expect(page.getByTestId('push-error')).toContainText('disabled')
  } else {
    await expect(page.getByTestId('push-no-device')).toBeVisible()
  }

  await expect(page.getByTestId('mobile-push-group')).not.toContainText('push.test:')
  expect(pageErrors).toEqual([])
})

// UC6-02 (B6 D4 / AC7): one push-registered phone and a sent test. STUB: the e2e daemon has no relay bridge, so
// no phone is ever paired or push-registered there; the proxy answers relay.device.list and push.test the way a
// daemon with one registered phone would, everything else is forwarded to the real daemon. The device's public key
// is deliberately a distinctive string: the group must never render it (the wire carries no secret, none is echoed).
const pushDevice = {
  device_id: 'b6push00000000001',
  device_pub: 'SECRETKEYBYTES',
  label: 'Pixel',
  first_seen: '2026-11-01T09:00:00Z',
  last_seen: '2026-11-26T09:00:00Z',
  state: 'approved',
  connected: true,
  push: true,
}

/** Answers the push surface with one registered device; flip `sent` between clicks to change what push.test says. */
async function stubMobilePush(page: Page) {
  const state = { sent: true }
  await page.routeWebSocket(/\/ws/, (ws) => {
    const server = ws.connectToServer()
    server.onMessage((m) => ws.send(m))
    ws.onMessage((m) => {
      let msg: { id?: string | number; method?: string }
      try {
        msg = JSON.parse(String(m))
      } catch {
        return server.send(m)
      }
      if (msg.method === 'relay.device.list') {
        return ws.send(JSON.stringify({ id: msg.id, result: { devices: [pushDevice], window: null } }))
      }
      if (msg.method === 'push.test') {
        return ws.send(
          JSON.stringify({
            id: msg.id,
            result: {
              results: [{ deviceId: pushDevice.device_id, sent: state.sent, error: state.sent ? undefined : 'send_failed' }],
            },
          }),
        )
      }
      server.send(m)
    })
  })
  return state
}

test('UC6-02 mobile push with a registered device (stub)', async ({ app }) => {
  const { page } = app
  const stub = await stubMobilePush(page)
  await app.openApp()
  await openSettings(page, 'notifications')
  await expect(page.getByTestId('mobile-push-group')).toBeVisible()
  await expect(page.getByTestId('push-count')).toHaveText('1 with push on')

  await page.getByTestId('push-test').click()
  await expect(page.getByTestId(`push-result-state-${pushDevice.device_id}`)).toHaveText('Sent')
  await expect(page.locator('body')).not.toContainText('SECRETKEYBYTES')

  stub.sent = false
  await page.getByTestId('push-test').click()
  await expect(page.getByTestId(`push-result-state-${pushDevice.device_id}`)).toHaveText(
    'The push service did not accept the notification.',
  )
})

// UC6-03 (B6 D4 / AC7): relay.device.* is local-only, so a relay-like connection gets `forbidden` and the group
// turns read-only instead of erroring. STUB: the e2e daemon has no relay bridge, so a relay session cannot be
// started for real; the proxy answers relay.device.list the way the daemon would from one, everything else is
// forwarded to the real daemon.
async function stubRelayRefusal(page: Page) {
  await page.routeWebSocket(/\/ws/, (ws) => {
    const server = ws.connectToServer()
    server.onMessage((m) => ws.send(m))
    ws.onMessage((m) => {
      let msg: { id?: string | number; method?: string }
      try {
        msg = JSON.parse(String(m))
      } catch {
        return server.send(m)
      }
      if (msg.method === 'relay.device.list') {
        return ws.send(
          JSON.stringify({
            id: msg.id,
            error: { message: 'relay.device.list: forbidden: relay.device.* is not available over the relay', code: 'forbidden' },
          }),
        )
      }
      server.send(m)
    })
  })
}

test('UC6-03 mobile push over a relay-like connection is read-only (stub)', async ({ app }) => {
  const { page } = app
  await stubRelayRefusal(page)
  await app.openApp()
  await openSettings(page, 'notifications')
  await expect(page.getByTestId('mobile-push-group')).toBeVisible()
  await expect(page.getByTestId('push-read-only')).toHaveText("Send test notifications from the daemon's machine.")
  await expect(page.getByTestId('push-test')).toHaveCount(0)
})

test('UC6-04 relay client: Disable works, Enable and Resume are refused with a sentence', async ({ app }) => {
  const { page } = app
  const pageErrors: string[] = []
  page.on('pageerror', (e) => pageErrors.push(e.message))
  await stubRelayAutomations(page)
  await app.openApp()
  await openSettings(page, 'automations')
  await page.getByTestId('settings-open-automations').click()
  await expect(page.getByTestId('automations-screen')).toBeVisible()
  await expect(page.getByTestId('automation-switch-1')).toHaveAttribute('aria-checked', 'true')
  await expect(page.getByTestId('automation-switch-2')).toHaveAttribute('aria-checked', 'false')

  // Disable is the brake: it goes through and the row shows disabled.
  await page.getByTestId('automation-switch-1').click()
  await expect(page.getByTestId('automation-status-1')).toHaveText('Disabled')
  await expect(page.getByTestId('automation-switch-1')).toHaveAttribute('aria-checked', 'false')

  // Enable is refused: a sentence is shown, nothing flips, the page does not crash.
  await page.getByTestId('automation-switch-2').click()
  await expect(page.getByTestId('automations-read-only')).toContainText('not available over the relay')
  await expect(page.getByTestId('automation-row-error-2')).toContainText('not available over the relay')
  await expect(page.getByTestId('automation-switch-2')).toHaveAttribute('aria-checked', 'false')
  await expect(page.getByTestId('automation-row-2')).toBeVisible()
  expect(pageErrors).toEqual([])
})
