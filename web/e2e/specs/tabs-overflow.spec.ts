import fs from 'node:fs'
import path from 'node:path'
import { type Page, type TestInfo } from '@playwright/test'
import { openTaskByLink } from '../fixtures/open-task'
import { expect, test } from '../fixtures/test'

// DT-UI D1: the tab strip's chips, edge fades and overflow menu, on the real built UI. Frames only when TAB_FRAMES_DIR is set.
async function frame(page: Page, testInfo: TestInfo, name: string) {
  const dir = process.env.TAB_FRAMES_DIR
  if (!dir) return
  fs.mkdirSync(dir, { recursive: true })
  // Frame the settled page: nothing animating in or out.
  await page.evaluate(() =>
    Promise.all(document.getAnimations().filter((a) => a.effect?.getComputedTiming().iterations !== Infinity).map((a) => a.finished.catch(() => undefined))),
  )
  await page.screenshot({ path: path.join(dir, `${name}-${testInfo.project.name}.png`) })
}

/** Opens a base tab kind from the "+" menu (the menu closes on choice). */
async function openFromPlus(page: Page, label: RegExp) {
  const before = await page.locator('[role="tab"]').count()
  await page.getByTestId('tabs-new-tab').click()
  await page.getByTestId('tabs-new-tab-item').filter({ hasText: label }).first().click()
  await expect.poll(() => page.locator('[role="tab"]').count()).toBeGreaterThanOrEqual(before)
  await expect(page.locator('[role="menu"]')).toHaveCount(0)
}

async function newChat(page: Page) {
  const before = await page.locator('[role="tab"]').count()
  await page.getByTestId('tabs-new-tab').click()
  await page.getByTestId('tabs-new-tab-new-chat').click()
  // The chat is created over RPC; the next "+" must not race it.
  await expect.poll(() => page.locator('[role="tab"]').count()).toBe(before + 1)
  await expect(page.locator('[role="menu"]')).toHaveCount(0)
}

const scroller = (page: Page) => page.getByTestId('tab-scroller')
const scrollTo = (page: Page, x: 'start' | 'middle' | 'end') =>
  scroller(page).evaluate((el, target) => {
    const max = el.scrollWidth - el.clientWidth
    el.scrollLeft = target === 'start' ? 0 : target === 'end' ? max : max / 2
  }, x)

test('TO-01 a few tabs sit at their natural width with no fades and no overflow button', async ({ app }, testInfo) => {
  const { page } = app
  await openTaskByLink(app, { id: app.daemon.ids.tasks['idle-done'] }, 'idle-done')
  await expect(page.getByTestId('tab-overflow-button')).toHaveCount(0)
  await expect(page.getByTestId('tab-fade-start')).toHaveCount(0)
  await expect(page.getByTestId('tab-fade-end')).toHaveCount(0)
  const widths = await page.locator('[role="tab"]').evaluateAll((els) => els.map((e) => e.getBoundingClientRect().width))
  for (const w of widths) expect(w).toBeLessThanOrEqual(160.5)
  await frame(page, testInfo, 'a-few-tabs')
})

test('TO-02 many tabs shrink to 64px then scroll, with fades and an overflow menu that activates a tab', async ({ app }, testInfo) => {
  test.setTimeout(150_000)
  const { page } = app
  await openTaskByLink(app, { id: app.daemon.ids.tasks['idle-done'] }, 'idle-done')
  await openFromPlus(page, /diff/i)
  await openFromPlus(page, /files/i)
  await openFromPlus(page, /terminal/i)
  const tabs = page.locator('[role="tab"]')
  // Add chats until the strip scrolls (at 390 a few are enough, at 1280 about twenty; 64px chips are the floor).
  const scrolls = () => scroller(page).evaluate((el) => el.scrollWidth > el.clientWidth + 1)
  for (let i = 0; i < 40 && !(await scrolls()); i++) await newChat(page)
  expect(await scrolls(), 'the strip overflowed').toBe(true)
  // The last chat is active and scrolled into view before the test scrolls the strip itself.
  await expect(tabs.last()).toHaveAttribute('aria-selected', 'true')

  // The strip scrolls only after the chips reached their minimum: every non-active tab is >= 64px, none above 160px.
  const widths = await tabs.evaluateAll((els) => els.map((e) => ({ w: e.getBoundingClientRect().width, active: e.getAttribute('aria-selected') === 'true' })))
  for (const { w, active } of widths) {
    expect(w).toBeGreaterThanOrEqual((active ? 96 : 64) - 0.5)
    expect(w).toBeLessThanOrEqual(160.5)
  }
  const overflowing = await scroller(page).evaluate((el) => el.scrollWidth > el.clientWidth + 1)
  expect(overflowing).toBe(true)
  await expect(page.getByTestId('tab-overflow-button')).toBeVisible()

  await scrollTo(page, 'start')
  await expect(page.getByTestId('tab-fade-start')).toHaveCount(0)
  await expect(page.getByTestId('tab-fade-end')).toBeVisible()
  await frame(page, testInfo, 'b-overflow-at-start')

  await scrollTo(page, 'middle')
  await expect(page.getByTestId('tab-fade-start')).toBeVisible()
  await expect(page.getByTestId('tab-fade-end')).toBeVisible()
  await frame(page, testInfo, 'c-overflow-in-the-middle')

  await scrollTo(page, 'end')
  await expect(page.getByTestId('tab-fade-start')).toBeVisible()
  await expect(page.getByTestId('tab-fade-end')).toHaveCount(0)
  await frame(page, testInfo, 'd-overflow-at-the-end')

  // The overflow menu lists every tab; choosing the first one activates it and scrolls it back into view.
  await page.getByTestId('tab-overflow-button').click()
  const items = page.getByTestId('tab-overflow-item')
  await expect(items).toHaveCount(await tabs.count())
  await expect(page.getByTestId('tab-overflow-menu')).toBeVisible()
  await page.getByTestId('tab-overflow-menu').evaluate((el) => Promise.all(el.getAnimations().map((a) => a.finished)))
  await frame(page, testInfo, 'e-overflow-menu')
  await items.first().click()
  await expect(tabs.first()).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByTestId('tab-fade-start')).toHaveCount(0)
})
