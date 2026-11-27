import fs from 'node:fs'
import path from 'node:path'
import { type Locator, type Page, type TestInfo } from '@playwright/test'
import { expect, test } from '../fixtures/test'

// B6 frames: one gallery shot per state of the desktop-only Settings -> Daemon and Notifications groups (B6 D1-D4:
// Paired devices, Diagnostics, Mobile push), light and dark, at 1280x800 and at 800x520 (the settings sheet). A normal
// web e2e cannot reach them: the Daemon section exists only in the desktop build, so this spec needs the web UI built
// with VITE_SMIND_DESKTOP=1 and a daemon binary that embeds that build, plus the mocked Tauri bridge injected by
// installBridge below (task-window-desktop-mode.spec.ts's bridge, extended with the daemon_status answer the Daemon
// section asks the shell for, and the connection kind). It is skipped otherwise, so the normal suite is unaffected.
// WEB-HARNESS: desktop build in a browser with a mocked Tauri shell; relay.device.* / push.test stubbed where named.
// Frames are written only when B6_FRAMES_DIR is set; without it every test stops right after the trivial "the section
// opened" assertion, so the run stays cheap. The real daemon the harness starts drives every state except the stubs
// named in each test; the behaviour behind each state is ui-cleanup-b6.spec.ts's (UC6-0x) and the component tests' job.
// This file only frames.

test.skip(process.env.E2E_DESKTOP_MODE !== '1', 'needs the desktop-mode web build (E2E_DESKTOP_MODE=1)')

const MAC_UA = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36'
test.use({ userAgent: MAC_UA })

/** The shape lib/relay-devices.ts reads; device_pub is a distinctive string because it must never be rendered. */
interface RelayDevice {
  device_id: string
  device_pub: string
  label: string
  first_seen: string
  last_seen: string
  state: 'pending' | 'approved' | 'revoked'
  connected: boolean
  push?: boolean
}

interface RelayDeviceWindow {
  expires_at: string
  remaining: number
}

/** A plausible `daemon_status` answer for a mac the app manages: the shape desktop-section.tsx reads off the shell. */
const DAEMON_STATUS = {
  platform: 'macos',
  reachable: true,
  daemonVersion: '0.7.0',
  appVersion: '0.7.0',
  comparison: 'same',
  managedState: 'managed',
  managedBy: null,
  pid: 4213,
  installedPath: '/usr/local/bin/smind',
  logPath: '/Users/e2e/Library/Logs/smind/smind.log',
  binaryInstalled: true,
  localService: 'smind',
  portOccupied: false,
  wslDaemonSuspected: false,
  gitFound: null,
  prerequisiteMessage: null,
}

interface BridgeOptions {
  /** The connection kind the mocked shell reports: "relay" turns the local-only groups read-only with no call made. */
  kind?: 'local' | 'relay'
}

/** Injects window.__TAURI_INTERNALS__ (the stand-in shell), recording every invoke; the connection kind is configurable. */
async function installBridge(page: Page, opts: BridgeOptions = {}): Promise<void> {
  await page.addInitScript(
    (o: { kind: 'local' | 'relay'; status: typeof DAEMON_STATUS }) => {
      const calls: Array<[string, unknown]> = []
      let nextId = 1
      ;(window as unknown as { __bridgeCalls: typeof calls }).__bridgeCalls = calls
      const dark = matchMedia('(prefers-color-scheme: dark)').matches
      const connection =
        o.kind === 'relay'
          ? { id: 'relay1', kind: 'relay', label: 'Relay', baseUrl: location.origin }
          : { id: 'local', kind: 'local', label: 'This computer', baseUrl: location.origin }
      ;(window as unknown as Record<string, unknown>).__TAURI_INTERNALS__ = {
        metadata: { currentWindow: { label: 'main' }, currentWebview: { windowLabel: 'main', label: 'main' } },
        transformCallback: (cb: (v: unknown) => void) => {
          const id = nextId++
          ;(window as unknown as Record<string, unknown>)[`_cb${id}`] = cb
          return id
        },
        convertFileSrc: (p: string) => p,
        invoke: async (cmd: string, args: Record<string, unknown> = {}) => {
          calls.push([cmd, args])
          switch (cmd) {
            case 'plugin:event|listen':
              return nextId++
            case 'plugin:event|unlisten':
            case 'window_ready':
            case 'window_set_theme':
              return null
            case 'plugin:window|is_maximized':
            case 'plugin:window|is_fullscreen':
              return false
            case 'plugin:window|theme':
              return dark ? 'dark' : 'light'
            case 'connections_get_current':
              return connection
            case 'connections_list':
              return [connection]
            case 'app_settings_get':
              return {
                onboardingCompleted: true,
                manageLocalDaemon: true,
                notificationsOn: true,
                notifyPermission: true,
                notifyQuestion: true,
                notifyFailure: true,
              }
            case 'daemon_status':
              return o.status
            case 'editors_list':
              return []
            case 'context_menu_show':
              return 1
            default:
              return null
          }
        },
      }
      ;(window as unknown as Record<string, unknown>).__TAURI_EVENT_PLUGIN_INTERNALS__ = { unregisterListener: () => {} }
    },
    { kind: opts.kind ?? 'local', status: DAEMON_STATUS },
  )
}

/** Opens a settings section the way the app's own shortcuts do (the same window event UC6-04 uses). */
async function openSettings(page: Page, sectionId: string) {
  await page.evaluate((id) => window.dispatchEvent(new CustomEvent('smind:open-settings', { detail: { sectionId: id } })), sectionId)
  await expect(page.getByTestId('settings-dialog')).toBeVisible()
}

/** Frames the settled page: no dialog, sheet or toast still animating in or out. */
async function settled(page: Page) {
  await page.evaluate(() =>
    Promise.all(
      document.getAnimations().filter((a) => a.effect?.getComputedTiming().iterations !== Infinity).map((a) => a.finished.catch(() => undefined)),
    ),
  )
}

/**
 * One state, two frames: 1280x800 first, then the same state resized to 800x520 (the settings sheet), each settled.
 * `target` is scrolled into view first: the section content scrolls inside the dialog, and a state below the fold
 * (Paired devices sits under Daemon and Diagnostics) must be on screen, not cut off.
 */
async function shot(page: Page, testInfo: TestInfo, state: string, target?: Locator) {
  const dir = process.env.B6_FRAMES_DIR
  if (!dir) return
  fs.mkdirSync(dir, { recursive: true })
  const size = page.viewportSize()!
  for (const [width, height] of [
    [1280, 800],
    [800, 520],
  ]) {
    await page.setViewportSize({ width, height })
    if (target) await target.scrollIntoViewIfNeeded()
    await settled(page)
    await page.screenshot({ path: path.join(dir, `${state}-${testInfo.project.name}-${width}x${height}.png`) })
  }
  await page.setViewportSize(size)
  await settled(page)
}

/** A browser has no native vibrancy: paint the sidebar colour where the shell's vibrancy view would be. */
async function standInForVibrancy(page: Page) {
  await page.addStyleTag({ content: 'html[data-desktop-os="macos"] body { background: var(--sidebar) !important; }' })
}

const MINUTE = 60_000
const HOUR = 60 * MINUTE
const iso = (msAgo: number) => new Date(Date.now() - msAgo).toISOString()

/** The plan's three devices: pending, approved (connected, with push on), revoked. */
function threeDevices(): RelayDevice[] {
  return [
    { device_id: '11111111-aaaa', device_pub: 'SECRETKEYBYTES', label: 'Pixel', first_seen: iso(26 * 24 * HOUR), last_seen: iso(2 * HOUR), state: 'pending', connected: false },
    { device_id: '22222222-bbbb', device_pub: 'SECRETKEYBYTES', label: 'iPhone', first_seen: iso(80 * 24 * HOUR), last_seen: iso(5 * MINUTE), state: 'approved', connected: true, push: true },
    { device_id: '33333333-cccc', device_pub: 'SECRETKEYBYTES', label: 'Old tablet', first_seen: iso(200 * 24 * HOUR), last_seen: iso(40 * 24 * HOUR), state: 'revoked', connected: false },
  ]
}

type ListAnswer =
  | { result: { devices: RelayDevice[]; window: RelayDeviceWindow | null } }
  | { error: { message: string; code?: string } }

/** Answers relay.device.list locally (the one stubbed input of a frame); everything else is forwarded to the daemon. */
async function stubRelayDeviceList(page: Page, answer: ListAnswer) {
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
      if (msg.method !== 'relay.device.list') return server.send(m)
      if ('error' in answer) return ws.send(JSON.stringify({ id: msg.id, error: answer.error }))
      return ws.send(JSON.stringify({ id: msg.id, result: answer.result }))
    })
  })
}

/** Holds every doctor.sessions request back from the daemon until release() forwards it, so "Running..." stays on screen. */
async function holdDoctorSessions(page: Page) {
  const held: string[] = []
  let forward: (m: string) => void = () => {}
  await page.routeWebSocket(/\/ws/, (ws) => {
    const server = ws.connectToServer()
    forward = (m) => server.send(m)
    server.onMessage((m) => ws.send(m))
    ws.onMessage((m) => {
      let msg: { id?: string | number; method?: string }
      try {
        msg = JSON.parse(String(m))
      } catch {
        return forward(String(m))
      }
      if (msg.method === 'doctor.sessions') {
        held.push(String(m))
        return
      }
      forward(String(m))
    })
  })
  return { held: () => held.length, release: () => { for (const m of held.splice(0)) forward(m) } }
}

/** Answers doctor.sessions with the daemon-shaped error the doctor-error frame needs; everything else is forwarded. */
async function stubDoctorSessionsError(page: Page) {
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
      if (msg.method === 'doctor.sessions') {
        return ws.send(JSON.stringify({ id: msg.id, error: { message: 'doctor.sessions: store: disk is full' } }))
      }
      server.send(m)
    })
  })
}

test('devices-empty: the group with no devices and the pairing window closed', async ({ app }, testInfo) => {
  const { page } = app
  // WEB-HARNESS: desktop build in a browser with a mocked Tauri shell; relay.device.* / push.test stubbed where named.
  // STUB: relay.device.list answers an empty list locally; everything else is forwarded to the real daemon.
  await stubRelayDeviceList(page, { result: { devices: [], window: null } })
  await installBridge(page)
  await app.openApp()
  await standInForVibrancy(page)
  await openSettings(page, 'daemon')
  await expect(page.getByTestId('settings-section-daemon')).toBeVisible()
  if (!process.env.B6_FRAMES_DIR) return
  await expect(page.getByTestId('relay-devices-empty')).toContainText('No devices are paired yet')
  await expect(page.getByTestId('relay-window-open')).toBeVisible()
  await shot(page, testInfo, 'devices-empty', page.getByTestId('relay-devices-group'))
})

test('devices-states: the three device states with the actions each offers', async ({ app }, testInfo) => {
  const { page } = app
  // WEB-HARNESS: desktop build in a browser with a mocked Tauri shell; relay.device.* / push.test stubbed where named.
  // STUB: relay.device.list answers the three-device list locally; everything else is forwarded to the real daemon.
  await stubRelayDeviceList(page, { result: { devices: threeDevices(), window: null } })
  await installBridge(page)
  await app.openApp()
  await standInForVibrancy(page)
  await openSettings(page, 'daemon')
  await expect(page.getByTestId('settings-section-daemon')).toBeVisible()
  if (!process.env.B6_FRAMES_DIR) return
  await expect(page.getByTestId('relay-device-state-11111111-aaaa')).toHaveText('Pending')
  await expect(page.getByTestId('relay-device-state-22222222-bbbb')).toHaveText('Approved')
  await expect(page.getByTestId('relay-device-state-33333333-cccc')).toHaveText('Revoked')
  await expect(page.getByTestId('relay-device-22222222-bbbb')).toContainText('Connected now')
  await expect(page.getByTestId('relay-device-22222222-bbbb')).toContainText('Mobile push on')
  await expect(page.getByTestId('relay-device-approve-11111111-aaaa')).toBeVisible()
  await expect(page.getByTestId('relay-device-revoke-22222222-bbbb')).toBeVisible()
  await expect(page.getByTestId('relay-device-rename-33333333-cccc')).toBeVisible()
  // The wire's public key bytes never reach the screen (B6 D1).
  await expect(page.locator('body')).not.toContainText('SECRETKEYBYTES')
  await shot(page, testInfo, 'devices-states', page.getByTestId('relay-devices-group'))
})

test('devices-approve-confirm: Approve asks first and waits (not confirmed)', async ({ app }, testInfo) => {
  const { page } = app
  // WEB-HARNESS: desktop build in a browser with a mocked Tauri shell; relay.device.* / push.test stubbed where named.
  // STUB: relay.device.list answers the three-device list locally; everything else is forwarded to the real daemon.
  await stubRelayDeviceList(page, { result: { devices: threeDevices(), window: null } })
  await installBridge(page)
  await app.openApp()
  await standInForVibrancy(page)
  await openSettings(page, 'daemon')
  await expect(page.getByTestId('settings-section-daemon')).toBeVisible()
  if (!process.env.B6_FRAMES_DIR) return
  await page.getByTestId('relay-device-approve-11111111-aaaa').click()
  const dialog = page.getByTestId('relay-confirm-dialog')
  await expect(dialog).toBeVisible()
  await expect(dialog).toContainText('Approve Pixel?')
  // Not confirmed: the dialog is the state being framed, and the device is still pending behind it.
  await shot(page, testInfo, 'devices-approve-confirm')
  await expect(page.getByTestId('relay-device-state-11111111-aaaa')).toHaveText('Pending')
})

test('devices-revoke-confirm: Revoke asks first and waits (not confirmed)', async ({ app }, testInfo) => {
  const { page } = app
  // WEB-HARNESS: desktop build in a browser with a mocked Tauri shell; relay.device.* / push.test stubbed where named.
  // STUB: relay.device.list answers the three-device list locally; everything else is forwarded to the real daemon.
  await stubRelayDeviceList(page, { result: { devices: threeDevices(), window: null } })
  await installBridge(page)
  await app.openApp()
  await standInForVibrancy(page)
  await openSettings(page, 'daemon')
  await expect(page.getByTestId('settings-section-daemon')).toBeVisible()
  if (!process.env.B6_FRAMES_DIR) return
  await page.getByTestId('relay-device-revoke-22222222-bbbb').click()
  const dialog = page.getByTestId('relay-confirm-dialog')
  await expect(dialog).toBeVisible()
  await expect(dialog).toContainText('Revoke iPhone?')
  // Not confirmed: the device is still approved behind the dialog.
  await shot(page, testInfo, 'devices-revoke-confirm')
  await expect(page.getByTestId('relay-device-state-22222222-bbbb')).toHaveText('Approved')
})

test('devices-rename: the rename dialog with the current name filled in', async ({ app }, testInfo) => {
  const { page } = app
  // WEB-HARNESS: desktop build in a browser with a mocked Tauri shell; relay.device.* / push.test stubbed where named.
  // STUB: relay.device.list answers the three-device list locally; everything else is forwarded to the real daemon.
  await stubRelayDeviceList(page, { result: { devices: threeDevices(), window: null } })
  await installBridge(page)
  await app.openApp()
  await standInForVibrancy(page)
  await openSettings(page, 'daemon')
  await expect(page.getByTestId('settings-section-daemon')).toBeVisible()
  if (!process.env.B6_FRAMES_DIR) return
  await page.getByTestId('relay-device-rename-11111111-aaaa').click()
  await expect(page.getByTestId('relay-rename-dialog')).toBeVisible()
  await expect(page.getByTestId('relay-rename-input')).toHaveValue('Pixel')
  await shot(page, testInfo, 'devices-rename')
})

test('devices-window-open: an open pairing window says until when and how many devices are left', async ({ app }, testInfo) => {
  const { page } = app
  // WEB-HARNESS: desktop build in a browser with a mocked Tauri shell; relay.device.* / push.test stubbed where named.
  // STUB: relay.device.list answers the three-device list and an open window locally (expires 10 minutes from now,
  // one device left); everything else is forwarded to the real daemon.
  await stubRelayDeviceList(page, {
    result: { devices: threeDevices(), window: { expires_at: new Date(Date.now() + 10 * MINUTE).toISOString(), remaining: 1 } },
  })
  await installBridge(page)
  await app.openApp()
  await standInForVibrancy(page)
  await openSettings(page, 'daemon')
  await expect(page.getByTestId('settings-section-daemon')).toBeVisible()
  if (!process.env.B6_FRAMES_DIR) return
  await expect(page.getByTestId('relay-window-state')).toContainText('1 device(s) left')
  await expect(page.getByTestId('relay-window-open')).toHaveCount(0)
  await shot(page, testInfo, 'devices-window-open', page.getByTestId('relay-devices-group'))
})

test('devices-relay-refused: over a relay connection the group is read-only and makes no call', async ({ app }, testInfo) => {
  const { page } = app
  // WEB-HARNESS: desktop build in a browser with a mocked Tauri shell; relay.device.* / push.test stubbed where named.
  // STUB: none on the wire -- the mocked shell reports the connection kind "relay", so the group (and Diagnostics)
  // make no local-only call at all and show the read-only copy (B6 D2).
  await installBridge(page, { kind: 'relay' })
  await app.openApp()
  await standInForVibrancy(page)
  await openSettings(page, 'daemon')
  await expect(page.getByTestId('settings-section-daemon')).toBeVisible()
  if (!process.env.B6_FRAMES_DIR) return
  await expect(page.getByTestId('relay-devices-read-only')).toHaveText("Manage paired devices on the daemon's machine.")
  await expect(page.getByTestId('doctor-read-only')).toBeVisible()
  await expect(page.getByTestId('relay-devices-refresh')).toHaveCount(0)
  await shot(page, testInfo, 'devices-relay-refused', page.getByTestId('relay-devices-group'))
})

test('devices-no-bridge: a daemon without the relay bridge says so', async ({ app }, testInfo) => {
  const { page } = app
  // WEB-HARNESS: desktop build in a browser with a mocked Tauri shell; relay.device.* / push.test stubbed where named.
  // STUB: relay.device.list answers "method not found" the way a bridgeless daemon does; everything else is
  // forwarded to the real daemon.
  await stubRelayDeviceList(page, { error: { message: 'method not found: relay.device.list' } })
  await installBridge(page)
  await app.openApp()
  await standInForVibrancy(page)
  await openSettings(page, 'daemon')
  await expect(page.getByTestId('settings-section-daemon')).toBeVisible()
  if (!process.env.B6_FRAMES_DIR) return
  await expect(page.getByTestId('relay-devices-no-bridge')).toHaveText('Relay is not set up on this daemon.')
  await expect(page.getByTestId('relay-devices-refresh')).toBeVisible()
  await shot(page, testInfo, 'devices-no-bridge', page.getByTestId('relay-devices-group'))
})

test('doctor-idle: the probe has not been run yet', async ({ app }, testInfo) => {
  const { page } = app
  // WEB-HARNESS: desktop build in a browser with a mocked Tauri shell; relay.device.* / push.test stubbed where named.
  // STUB: none -- the real daemon answers doctor.sessions when asked; this state never asks.
  await installBridge(page)
  await app.openApp()
  await standInForVibrancy(page)
  await openSettings(page, 'daemon')
  await expect(page.getByTestId('settings-section-daemon')).toBeVisible()
  if (!process.env.B6_FRAMES_DIR) return
  await expect(page.getByTestId('doctor-idle')).toHaveText('Not run yet.')
  await expect(page.getByTestId('doctor-run')).toHaveText('Run checks')
  await shot(page, testInfo, 'doctor-idle', page.getByTestId('doctor-run'))
})

test('doctor-running: the probe in flight while the daemon answer is held back', async ({ app }, testInfo) => {
  const { page } = app
  // WEB-HARNESS: desktop build in a browser with a mocked Tauri shell; relay.device.* / push.test stubbed where named.
  // STUB: doctor.sessions is held back from the daemon until after the shot, so "Running..." stays on screen;
  // everything else is forwarded to the real daemon.
  const hold = await holdDoctorSessions(page)
  await installBridge(page)
  await app.openApp()
  await standInForVibrancy(page)
  await openSettings(page, 'daemon')
  await expect(page.getByTestId('settings-section-daemon')).toBeVisible()
  if (!process.env.B6_FRAMES_DIR) return
  await page.getByTestId('doctor-run').click()
  await expect(page.getByTestId('doctor-run')).toHaveText('Running...')
  // The request has reached the hold (never the daemon) before the frame is taken.
  await expect.poll(() => hold.held()).toBe(1)
  await shot(page, testInfo, 'doctor-running', page.getByTestId('doctor-run'))
  // Forwarded after the shot, the held probe completes: the frame showed a real in-flight check, not a dead one.
  hold.release()
  await expect(page.getByTestId('doctor-result')).toBeVisible()
})

test('doctor-result: the counts the real daemon answers (no stub)', async ({ app }, testInfo) => {
  const { page } = app
  // WEB-HARNESS: desktop build in a browser with a mocked Tauri shell; relay.device.* / push.test stubbed where named.
  // STUB: none -- the real daemon answers doctor.sessions with its own counts.
  await installBridge(page)
  await app.openApp()
  await standInForVibrancy(page)
  await openSettings(page, 'daemon')
  await expect(page.getByTestId('settings-section-daemon')).toBeVisible()
  if (!process.env.B6_FRAMES_DIR) return
  await page.getByTestId('doctor-run').click()
  await expect(page.getByTestId('doctor-result')).toBeVisible()
  await expect(page.getByTestId('doctor-runs')).toHaveText(/^Finished runs\d+$/)
  await expect(page.getByTestId('doctor-checked')).toHaveText(/^Checked\d+$/)
  await expect(page.getByTestId('doctor-idle')).toHaveCount(0)
  await shot(page, testInfo, 'doctor-result', page.getByTestId('doctor-result'))
})

test('doctor-error: a failed probe is one sentence and the button stays usable', async ({ app }, testInfo) => {
  const { page } = app
  // WEB-HARNESS: desktop build in a browser with a mocked Tauri shell; relay.device.* / push.test stubbed where named.
  // STUB: doctor.sessions answers the daemon-shaped error locally; everything else is forwarded to the real daemon.
  await stubDoctorSessionsError(page)
  await installBridge(page)
  await app.openApp()
  await standInForVibrancy(page)
  await openSettings(page, 'daemon')
  await expect(page.getByTestId('settings-section-daemon')).toBeVisible()
  if (!process.env.B6_FRAMES_DIR) return
  await page.getByTestId('doctor-run').click()
  await expect(page.getByTestId('doctor-error')).toContainText('Disk is full')
  await expect(page.getByTestId('doctor-run')).toHaveText('Run checks')
  await expect(page.getByTestId('doctor-run')).toBeEnabled()
  await shot(page, testInfo, 'doctor-error', page.getByTestId('doctor-run'))
})
