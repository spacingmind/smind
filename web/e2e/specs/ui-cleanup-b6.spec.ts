import { type Page } from '@playwright/test'
import { expect, test } from '../fixtures/test'

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
