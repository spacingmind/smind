import fs from 'node:fs'
import path from 'node:path'
import { type Page, type TestInfo } from '@playwright/test'
import { openTaskByLink } from '../fixtures/open-task'
import { expect, test } from '../fixtures/test'

// DT-UI D2: dragging a tab along its own strip reorders it, with a 4px drop indicator at the slot. Frames only when TAB_FRAMES_DIR is set.
async function frame(page: Page, testInfo: TestInfo, name: string) {
  const dir = process.env.TAB_FRAMES_DIR
  if (!dir) return
  fs.mkdirSync(dir, { recursive: true })
  await page.screenshot({ path: path.join(dir, `${name}-${testInfo.project.name}.png`) })
}

async function openFromPlus(page: Page, label: RegExp) {
  const before = await page.locator('[role="tab"]').count()
  await page.getByTestId('tabs-new-tab').click()
  await page.getByTestId('tabs-new-tab-item').filter({ hasText: label }).first().click()
  await expect.poll(() => page.locator('[role="tab"]').count()).toBeGreaterThanOrEqual(before)
  await expect(page.locator('[role="menu"]')).toHaveCount(0)
}

const order = (page: Page) => page.locator('[data-testid="tab-scroller"] [role="tab"]').evaluateAll((els) => els.map((e) => (e.getAttribute('data-testid') ?? '').replace('workspace-tab-', '')))

async function centre(page: Page, kind: string) {
  const box = await page.getByTestId(`workspace-tab-${kind}`).first().boundingBox()
  return { x: box!.x + box!.width / 2, y: box!.y + box!.height / 2, box: box! }
}

test('TR-01 dragging a tab along its strip shows the indicator and reorders it; the order survives a reload', async ({ app }, testInfo) => {
  test.skip((testInfo.project.use.viewport?.width ?? 1280) < 768, 'the compact layout has no drag-and-drop (no split tree), and the desktop window is never that narrow')
  const { page } = app
  await openTaskByLink(app, { id: app.daemon.ids.tasks['idle-done'] }, 'idle-done')
  await openFromPlus(page, /diff/i)
  await openFromPlus(page, /files/i)
  await openFromPlus(page, /terminal/i)
  expect(await order(page)).toEqual(['chat', 'diff', 'files', 'terminal'])

  const from = await centre(page, 'terminal')
  const diff = await centre(page, 'diff')
  await page.mouse.move(from.x, from.y)
  await page.mouse.down()
  await page.mouse.move(from.x - 20, from.y, { steps: 4 }) // past the 8px activation distance
  // Feedback on the held chip: dimmed, grabbing cursor for the whole drag; and no chip reveals its hover controls.
  const held = page.getByTestId('workspace-tab-terminal')
  await expect(held).toHaveCSS('opacity', '0.5')
  await expect(page.locator('html')).toHaveAttribute('data-tab-dragging', 'true')
  expect(await page.evaluate(() => getComputedStyle(document.body).cursor)).toBe('grabbing')
  await frame(page, testInfo, 'a-drag-started')
  // Over the left half of the Diff tab: the indicator marks the slot before it.
  await page.mouse.move(diff.box.x + 8, diff.y, { steps: 10 })
  const indicator = page.getByTestId('tab-drop-indicator')
  await expect(indicator).toBeVisible()
  await expect(indicator).toHaveAttribute('data-slot', '1')
  const ibox = await indicator.boundingBox()
  expect(ibox!.width).toBeCloseTo(4, 0)
  expect(Math.abs(ibox!.x + ibox!.width / 2 - diff.box.x)).toBeLessThan(8)
  // The hovered target keeps its label readable: its split and close controls stay hidden while dragging.
  const hiddenControls = await page.getByTestId('workspace-tab-affordances').evaluateAll((els) => els.map((e) => getComputedStyle(e).opacity))
  expect(hiddenControls.length).toBeGreaterThan(0)
  for (const opacity of hiddenControls) expect(opacity).toBe('0')
  await frame(page, testInfo, 'b-drag-over-indicator')
  await page.mouse.up()
  await expect(indicator).toHaveCount(0)
  await expect.poll(() => order(page)).toEqual(['chat', 'terminal', 'diff', 'files'])
  await expect(page.locator('html')).not.toHaveAttribute('data-tab-dragging', 'true')
  await expect(page.getByTestId('workspace-tab-terminal')).toHaveCSS('opacity', '1')
  await frame(page, testInfo, 'c-after-drop')

  await page.reload()
  await expect(page.locator('[data-testid="tab-scroller"] [role="tab"]').first()).toBeVisible()
  expect(await order(page)).toEqual(['chat', 'terminal', 'diff', 'files'])
})

test('TR-02 dropping a tab back where it was changes nothing, and Escape cancels a drag', async ({ app }, testInfo) => {
  test.skip((testInfo.project.use.viewport?.width ?? 1280) < 768, 'the compact layout has no drag-and-drop')
  const { page } = app
  await openTaskByLink(app, { id: app.daemon.ids.tasks['idle-done'] }, 'idle-done')
  await openFromPlus(page, /diff/i)
  await openFromPlus(page, /files/i)
  const before = await order(page)
  const files = await centre(page, 'files')
  await page.mouse.move(files.x, files.y)
  await page.mouse.down()
  await page.mouse.move(files.x + 10, files.y, { steps: 3 })
  await page.mouse.move(files.x - 2, files.y, { steps: 3 })
  await page.mouse.up()
  expect(await order(page)).toEqual(before)

  const diff = await centre(page, 'diff')
  await page.mouse.move(files.x, files.y)
  await page.mouse.down()
  await page.mouse.move(diff.box.x + 6, diff.y, { steps: 8 })
  await expect(page.getByTestId('tab-drop-indicator')).toBeVisible()
  await page.keyboard.press('Escape')
  await page.mouse.up()
  await expect(page.getByTestId('tab-drop-indicator')).toHaveCount(0)
  expect(await order(page)).toEqual(before)
})
