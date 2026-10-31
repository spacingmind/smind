import { INVITE, stubFleet } from '../fixtures/peer-sonnet'
import { expect, test } from '../fixtures/test'

// Measured min dimension (px) at 1280: the B-07 growth is narrow-only, so desktop must not change.
const DESKTOP_SIZES: Record<string, number> = {
  toggle: 28,
  'sidebar-quick-new-task': 28,
  'sidebar-quick-search': 28,
  'sidebar-quick-automations': 28,
  'sidebar-group-mode-tree': 26,
  'sidebar-group-mode-status': 26,
  'sidebar-group-mode-stage': 26,
}

const narrowOnly = (testInfo: { project: { name: string } }) => test.skip(testInfo.project.name.endsWith('1280'), 'narrow only')

test('A-01 at 390 palette "New task in alpha" opens its dialog with the drawer closed', async ({ app }, testInfo) => {
  narrowOnly(testInfo)
  const { page } = app
  await app.openApp()
  await app.openTask('idle-done')
  await page.keyboard.press('ControlOrMeta+k')
  await page.getByTestId('command-palette-input').fill('New task in alpha')
  await page.keyboard.press('Enter')
  await expect(page.getByTestId('dialog-new-task-title-input')).toBeVisible()
  await expect(page.getByTestId('sidebar-footer')).toBeHidden()
})

test('A-01 at 390 palette "New workspace" opens its dialog with the drawer closed', async ({ app }, testInfo) => {
  narrowOnly(testInfo)
  const { page } = app
  await app.openApp()
  await app.openTask('idle-done')
  await page.keyboard.press('ControlOrMeta+k')
  await page.getByTestId('command-palette-input').fill('New workspace')
  await page.keyboard.press('Enter')
  await expect(page.getByTestId('dialog-new-workspace-path-input')).toBeVisible()
  await expect(page.getByTestId('sidebar-footer')).toBeHidden()
})

test('A-09 at 390 the folder picker keeps its content inside the dialog', async ({ app }, testInfo) => {
  narrowOnly(testInfo)
  const { page } = app
  await app.openApp()
  await app.openDrawer()
  await page.getByTestId('sidebar-footer-add-workspace').click()
  await expect(page.getByTestId('dialog-new-workspace-path-input')).toBeVisible()
  await page.getByTestId('dialog-new-workspace-browse-button').click()
  const dialog = page.getByRole('dialog', { name: 'Choose a folder' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('button', { name: /use this folder/i })).toBeVisible()
  const g = await dialog.evaluate((d) => {
    const dr = d.getBoundingClientRect()
    const pad = parseFloat(getComputedStyle(d).paddingRight) || 0
    const over: string[] = []
    for (const e of d.querySelectorAll('button, input, [data-testid=folder-row], [data-testid=folder-picker-recent-path]')) {
      // the dialog's own Close (X) sits in the padding by design; everything else must stay inside it
      if (e.getAttribute('data-slot') === 'dialog-close' || e.getAttribute('aria-label') === 'Close' || (e as HTMLElement).innerText?.trim() === 'Close') continue
      const r = e.getBoundingClientRect()
      if (r.width && (r.right > dr.right - pad + 1 || r.left < dr.left - 1)) over.push(`${(e as HTMLElement).dataset.testid || (e as HTMLElement).innerText || e.tagName} right=${Math.round(r.right)}`)
    }
    return { over, sw: d.scrollWidth, cw: d.clientWidth }
  })
  expect(g.over).toEqual([])
  expect(g.sw).toBeLessThanOrEqual(g.cw + 1)
})

test('B-07 tap targets are at least 36px at 390 and unchanged at 1280', async ({ app }) => {
  const { page } = app
  await app.openApp()
  await app.openTask('idle-done')
  const box = async (loc: ReturnType<typeof page.locator>) => {
    await expect(loc.first()).toBeVisible()
    const b = await loc.first().boundingBox()
    return b ? Math.min(b.width, b.height) : 0
  }
  const sizes: Record<string, number> = {}
  sizes.toggle = await box(page.getByRole('button', { name: /toggle sidebar/i }))
  await app.openDrawer()
  for (const id of ['sidebar-quick-new-task', 'sidebar-quick-search', 'sidebar-quick-automations', 'sidebar-group-mode-tree', 'sidebar-group-mode-status', 'sidebar-group-mode-stage']) {
    sizes[id] = await box(page.getByTestId(id))
  }
  if (app.narrow) {
    for (const [name, s] of Object.entries(sizes)) expect(s, `${name} min dimension`).toBeGreaterThanOrEqual(36)
  } else {
    // desktop keeps its compact targets (the 36px floor is coarse-pointer/narrow only)
    expect(sizes).toEqual(DESKTOP_SIZES)
  }
})

test('B-10 invite dialog: the invite value is ellipsized and Copy stays inside the dialog', async ({ app }) => {
  const { page } = app
  await stubFleet(page)
  await app.openApp()
  await page.evaluate(() => window.dispatchEvent(new CustomEvent('smind:open-settings', { detail: { sectionId: 'hosts' } })))
  await expect(page.getByTestId('settings-screen').first()).toBeVisible()
  await page.getByTestId('hosts-invite').click()
  await page.getByTestId('invite-create').click()
  const value = page.getByTestId('invite-value')
  await expect(value).toHaveValue(INVITE)
  await expect(value).toHaveAttribute('title', INVITE)
  expect(await value.evaluate((e) => getComputedStyle(e).textOverflow)).toBe('ellipsis')
  const dlg = await page.getByTestId('invite-host-dialog').boundingBox()
  const copy = await page.getByTestId('invite-copy').boundingBox()
  expect(dlg && copy).toBeTruthy()
  expect(copy!.x).toBeGreaterThanOrEqual(dlg!.x)
  expect(copy!.x + copy!.width).toBeLessThanOrEqual(dlg!.x + dlg!.width + 1)
})

test('start-from: New task dialog labels are short on one line at 390 and full at 1280', async ({ app }) => {
  const { page } = app
  await app.openApp()
  await app.openTask('idle-done')
  await app.openDrawer()
  await page.getByTestId('sidebar-quick-new-task').click()
  await expect(page.getByTestId('dialog-new-task-title-input')).toBeVisible()
  const group = page.getByRole('radiogroup', { name: 'Start from' })
  await expect(group.getByRole('radio')).toHaveCount(4)
  const info = await group.evaluate((g) =>
    [...g.querySelectorAll('[role=radio]')].map((e) => {
      const rg = document.createRange()
      rg.selectNodeContents(e)
      const lines = new Set([...rg.getClientRects()].map((x) => Math.round(x.top))).size
      return { visible: (e as HTMLElement).innerText.trim(), aria: e.getAttribute('aria-label'), lines }
    }),
  )
  expect(info.map((s) => s.lines)).toEqual([1, 1, 1, 1])
  expect(info.map((s) => s.aria)).toEqual(['Workspace HEAD', 'Branch…', 'Pull request…', 'Issue…'])
  expect(info.map((s) => s.visible).join('/')).toBe(app.narrow ? 'HEAD/Branch/PR/Issue' : 'Workspace HEAD/Branch…/Pull request…/Issue…')
  for (const n of ['Workspace HEAD', 'Branch…', 'Pull request…', 'Issue…']) await expect(group.getByRole('radio', { name: n })).toBeVisible()
  const gb = await group.boundingBox()
  const dlg = await page.getByRole('dialog').last().boundingBox()
  expect(gb!.x + gb!.width).toBeLessThanOrEqual(dlg!.x + dlg!.width + 1)
})

test('A-13 at 390 the diff pane header keeps "2 files +5 −0" on one line', async ({ app }, testInfo) => {
  narrowOnly(testInfo)
  const { page } = app
  await app.openApp()
  await app.openTask('idle-done')
  await page.getByTestId('composer-diff-stat').first().click()
  await expect(page.getByTestId('diff-viewer-pane')).toBeVisible()
  const stat = page.getByTestId('diff-stat')
  await expect(stat).toBeVisible()
  await expect(stat).toHaveText(/^2 files\s+\+5\s+−0$/)
  const lines = await stat.evaluate((e) => {
    const rg = document.createRange()
    rg.selectNodeContents(e)
    return new Set([...rg.getClientRects()].map((x) => Math.round(x.top))).size
  })
  expect(lines).toBe(1)
  const sb = await stat.boundingBox()
  expect(sb!.x + sb!.width).toBeLessThanOrEqual(390 + 1)
})

test('Jump to latest slot sits outside the log and never overlaps its text; the end hides it', async ({ app }) => {
  const { page } = app
  await app.openApp()
  await app.openTask('long-timeline')
  const scroll = page.getByTestId('run-log-scroll')
  await expect(scroll).toBeVisible()
  await expect(page.getByTestId('timeline-assistant').last()).toBeVisible()
  const setFromEnd = (d: number) => scroll.evaluate((sc, d) => { sc.scrollTop = Math.max(0, sc.scrollHeight - sc.clientHeight - d) }, d)
  await setFromEnd(0)
  await expect(page.getByTestId('jump-to-latest-slot')).toHaveCount(0)
  await setFromEnd(150)
  const slot = page.getByTestId('jump-to-latest-slot')
  await expect(slot).toBeVisible()
  const g = await page.evaluate(() => {
    const sc = document.querySelector('[data-testid=run-log-scroll]')!
    const slot = document.querySelector('[data-testid=jump-to-latest-slot]')!.getBoundingClientRect()
    const log = sc.getBoundingClientRect()
    // only text that is actually visible in the log viewport counts (the rest is scrolled out and clipped)
    const clip = (r: DOMRect) => ({ left: Math.max(r.left, log.left), right: Math.min(r.right, log.right), top: Math.max(r.top, log.top), bottom: Math.min(r.bottom, log.bottom) })
    const w = document.createTreeWalker(sc, NodeFilter.SHOW_TEXT)
    let hits = 0
    for (let n = w.nextNode(); n; n = w.nextNode()) {
      if (!n.nodeValue?.trim()) continue
      const rg = document.createRange()
      rg.selectNodeContents(n)
      for (const raw of rg.getClientRects()) {
        const r = clip(raw)
        if (r.right > r.left && r.bottom > r.top && Math.min(r.right, slot.right) > Math.max(r.left, slot.left) && Math.min(r.bottom, slot.bottom) > Math.max(r.top, slot.top)) hits++
      }
    }
    return { slotTop: slot.top, slotBottom: slot.bottom, logTop: log.top, logBottom: log.bottom, hits }
  })
  expect(g.slotTop).toBeGreaterThanOrEqual(g.logBottom - 1)
  expect(g.hits).toBe(0)
  await setFromEnd(0)
  await expect(slot).toHaveCount(0)
})
