import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { type Page, type TestInfo } from '@playwright/test'
import { expect, test } from '../fixtures/test'

// Multi-window Part 1 (web half), desktop-build mode with a MOCKED Tauri bridge: this spec needs the web UI built with
// VITE_SMIND_DESKTOP=1 and a daemon binary that embeds that build (see web/e2e/README.md, "Desktop-mode specs"). It is skipped
// otherwise, so the normal suite is unaffected. The bridge stands in for the shell: `window_open_task` is recorded (or refused),
// the window label is task-1 or main. macOS chrome is framed with a stand-in for the native vibrancy backdrop (the sidebar
// colour), because a browser has no vibrancy; the traffic-light inset is real. Frames only when TASK_WINDOW_FRAMES_DIR is set.
test.skip(process.env.E2E_DESKTOP_MODE !== '1', 'needs the desktop-mode web build (E2E_DESKTOP_MODE=1)')

const MAC_UA = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36'
test.use({ userAgent: MAC_UA })

interface BridgeOptions {
  label: 'main' | 'task-1'
  /** Message the mocked shell refuses window_open_task with; omitted = it opens the window. */
  refuseWith?: string
}

async function installBridge(page: Page, opts: BridgeOptions): Promise<void> {
  await page.addInitScript((o: BridgeOptions) => {
    const calls: Array<[string, unknown]> = []
    let nextId = 1
    ;(window as unknown as { __bridgeCalls: typeof calls }).__bridgeCalls = calls
    const dark = matchMedia('(prefers-color-scheme: dark)').matches
    const connection = { id: 'local', kind: 'local', label: 'This computer', baseUrl: location.origin }
    ;(window as unknown as Record<string, unknown>).__TAURI_INTERNALS__ = {
      metadata: { currentWindow: { label: o.label }, currentWebview: { windowLabel: o.label, label: o.label } },
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
          case 'app_settings_get':
            return { onboardingCompleted: true, manageLocalDaemon: false, notificationsOn: false, notifyPermission: true, notifyQuestion: true, notifyFailure: true }
          case 'connections_list':
            return [connection]
          case 'editors_list':
            return []
          case 'context_menu_show':
            return 1
          case 'window_open_task':
            if (o.refuseWith) throw o.refuseWith
            return 'task-1'
          default:
            return null
        }
      },
    }
    ;(window as unknown as Record<string, unknown>).__TAURI_EVENT_PLUGIN_INTERNALS__ = { unregisterListener: () => {} }
  }, opts)
}

/** The IPC commands desktop/src-tauri/capabilities/task-window.json grants, in the names the webview invokes them by. */
function taskWindowGrantedCommands(): Set<string> {
  const file = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../desktop/src-tauri/capabilities/task-window.json')
  const permissions: string[] = JSON.parse(fs.readFileSync(file, 'utf8')).permissions
  const out = new Set<string>()
  for (const p of permissions) {
    const core = /^core:(\w+):allow-(.+)$/.exec(p)
    if (core) out.add(`plugin:${core[1]}|${core[2].replace(/-/g, '_')}`)
    else if (p.startsWith('allow-')) out.add(p.slice('allow-'.length).replace(/-/g, '_'))
  }
  return out
}

const bridgeCalls = (page: Page) => page.evaluate(() => (window as unknown as { __bridgeCalls: Array<[string, unknown]> }).__bridgeCalls)

/** A browser has no native vibrancy: paint the sidebar colour where the shell's vibrancy view would be. */
async function standInForVibrancy(page: Page) {
  await page.addStyleTag({ content: 'html[data-desktop-os="macos"] body { background: var(--sidebar) !important; }' })
}

async function frame(page: Page, testInfo: TestInfo, name: string) {
  const dir = process.env.TASK_WINDOW_FRAMES_DIR
  if (!dir) return
  // Frame the settled page: no sheet, menu or toast still animating in or out.
  await page.evaluate(() =>
    Promise.all(
      document
        .getAnimations()
        .filter((a) => a.effect?.getComputedTiming().iterations !== Infinity)
        .map((a) => a.finished.catch(() => undefined)),
    ),
  )
  fs.mkdirSync(dir, { recursive: true })
  await page.screenshot({ path: path.join(dir, `${name}-${testInfo.project.name}.png`) })
}

const narrow = (testInfo: TestInfo) => (testInfo.project.use.viewport?.width ?? 1280) < 768

/** Opens the first task row's ⋯ menu (the in-page dropdown: the right-click menu is native in the desktop build). */
async function openRowMenu(app: { page: Page; openDrawer(): Promise<void> }) {
  const { page } = app
  await app.openDrawer()
  const row = page.getByTestId('sidebar-task-item').first()
  await row.hover()
  await row.getByTestId('sidebar-task-actions-trigger').click()
  const item = page.getByTestId('sidebar-task-open-in-new-window-action')
  await expect(item).toBeVisible()
  // Frame the settled menu, not its enter animation.
  await page.locator('[role="menu"]').first().evaluate((el) => Promise.all(el.getAnimations().map((a) => a.finished)))
  return item
}

test('TW-01 main: the task menu offers Open in new window and asks the shell for that task', async ({ app }, testInfo) => {
  const { page } = app
  await installBridge(page, { label: 'main' })
  await app.openApp()
  await standInForVibrancy(page)
  const item = await openRowMenu(app)
  await expect(item).toBeVisible()
  await expect(item).toBeEnabled()
  await expect(item).toContainText('Open in new window')
  await frame(page, testInfo, 'a-task-menu-open-in-new-window')
  await item.click()
  await expect.poll(async () => (await bridgeCalls(page)).filter(([c]) => c === 'window_open_task').length).toBe(1)
  const [, args] = (await bridgeCalls(page)).find(([c]) => c === 'window_open_task')!
  expect(Object.keys(args as object)).toEqual(['taskId'])
  expect(Number.isInteger((args as { taskId: number }).taskId)).toBe(true)
})

test('TW-02 main: a refusal at the 8-window cap shows as an error notice', async ({ app }, testInfo) => {
  const { page } = app
  await installBridge(page, { label: 'main', refuseWith: 'too many task windows open (8); close one first' })
  await app.openApp()
  await standInForVibrancy(page)
  const item = await openRowMenu(app)
  await item.click()
  const notice = page.getByText('too many task windows open (8); close one first')
  await expect(notice).toBeVisible()
  await expect(page.getByText('Could not open a new window')).toBeVisible()
  await expect(page.locator('[role="menu"]')).toHaveCount(0) // the menu's exit animation is over
  // At 390 the drawer sheet stays open over the toast (every toast is layered under it, not specific to this one): close it to frame the notice.
  if (narrow(testInfo)) {
    await page.keyboard.press('Escape')
    await expect(page.getByTestId('sidebar-quick-actions')).toBeHidden()
    await expect(notice).toBeVisible()
  }
  await frame(page, testInfo, 'c-cap-refusal-notice')
})

test('TW-03 a task window (label task-1) opens at #/task/<id> with no host button and no Settings', async ({ app }, testInfo) => {
  const { page } = app
  await installBridge(page, { label: 'task-1' })
  const id = app.daemon.ids.tasks['idle-done']
  await app.openApp(`#/task/${id}`)
  await standInForVibrancy(page)
  await expect(page.getByTestId('app-header-title').filter({ hasText: 'idle-done' }).first()).toBeVisible()
  // The route was the short one and the normal route took over once the chat tab was active.
  await expect.poll(() => page.evaluate(() => location.hash)).toMatch(/^#\/workspace\/\d+\/task\/\d+\/chat\/\d+$/)
  await app.openDrawer()
  await expect(page.getByTestId('sidebar-footer-host')).toHaveCount(0)
  await expect(page.getByTestId('sidebar-settings-button')).toHaveCount(0)
  await expect(page.getByTestId('sidebar-footer-inbox')).toBeVisible()
  // The Settings shortcut does nothing here.
  await page.keyboard.press('Control+,')
  await expect(page.getByTestId('settings-screen')).toHaveCount(0)
  // And the page only invoked commands the task-window capability grants (read from the real file).
  const granted = taskWindowGrantedCommands()
  const outside = (await bridgeCalls(page)).map(([c]) => c).filter((c) => !granted.has(c))
  expect(outside, 'commands invoked by a task window that task-window.json does not grant').toEqual([])
  if (narrow(testInfo)) await expect(page.getByTestId('sidebar-quick-actions')).toBeVisible()
  await frame(page, testInfo, 'b-task-window-layout')
})

test('TW-04 main for comparison: the same route in the main window keeps the host and Settings buttons', async ({ app }, testInfo) => {
  const { page } = app
  await installBridge(page, { label: 'main' })
  const id = app.daemon.ids.tasks['idle-done']
  await app.openApp(`#/task/${id}`)
  await standInForVibrancy(page)
  await expect(page.getByTestId('app-header-title').filter({ hasText: 'idle-done' }).first()).toBeVisible()
  await app.openDrawer()
  await expect(page.getByTestId('sidebar-footer-host')).toBeVisible()
  await expect(page.getByTestId('sidebar-settings-button')).toBeVisible()
  await frame(page, testInfo, 'd-main-window-layout-for-comparison')
})
