import AxeBuilder from '@axe-core/playwright'
import type { Locator, Page } from '@playwright/test'
import { pinColorsToRgb } from '../fixtures/browser-stubs'
import { expect, test } from '../fixtures/test'

// B-05 fixed --muted-foreground, --warning-text and the sidebar row time only (docs: contrast-table). Quiet text built on the
// separate --foreground-subtle / --foreground-subtlest tokens (60% / 40% alpha, 2.7-4.2:1 by design) was out of B-05's scope and is
// excluded here, element by element. Muted and orange text is NOT excluded.
const EXCLUDE: string[] = [
  '.text-foreground-subtle', // e.g. inbox group headings, settings group labels, the dashed "Set budget" chip
  '.text-foreground-subtlest',
  '[data-testid=run-footer]', // run footer meta line (model, elapsed, provenance): foreground-subtle segments
  '[data-testid=task-budget-chip]', // dashed "Set budget" chip: foreground-subtle when no cap is set
]

async function contrast(page: Page, label: string, include?: string[]) {
  await pinColorsToRgb(page)
  let b = new AxeBuilder({ page }).withRules(['color-contrast'])
  for (const sel of include ?? []) b = b.include(sel)
  for (const sel of EXCLUDE) b = b.exclude(sel)
  const res = await b.analyze()
  // the rule must have actually judged text, or a zero-violation result proves nothing
  const judged = res.passes.concat(res.violations).filter((r) => r.id === 'color-contrast').reduce((n, r) => n + r.nodes.length, 0)
  expect(judged, `${label}: color-contrast checked nodes`).toBeGreaterThan(0)
  const found = res.violations.flatMap((v) => v.nodes.map((n) => `${n.target.join(' ')} :: ${(n.any[0]?.message ?? '').slice(0, 200)}`))
  expect(found, `${label}: color-contrast violations`).toEqual([])
}

const ring = (el: Locator) =>
  el.evaluate((e) => {
    const c = getComputedStyle(e)
    const outline = c.outlineStyle !== 'none' && parseFloat(c.outlineWidth) > 0
    const shadow = !!c.boxShadow && c.boxShadow !== 'none'
    return { focusVisible: e.matches(':focus-visible'), outline, shadow }
  })

// Focus via the keyboard (so :focus-visible applies): land near the element, then Tab onto it.
async function tabOnto(page: Page, el: Locator) {
  await el.focus()
  await page.keyboard.press('Shift+Tab')
  for (let i = 0; i < 6; i++) {
    await page.keyboard.press('Tab')
    if (await el.evaluate((e) => document.activeElement === e)) return
  }
  throw new Error('could not Tab onto the element')
}

test('J49 keyboard only: every Tab stop shows a focus ring, Permission mode and collapse toggle ring, Escape closes the palette (B-08)', async ({ app }, testInfo) => {
  test.skip(testInfo.project.name !== 'light-1280', 'keyboard journey runs at desktop width')
  const { page } = app
  await app.openApp()
  await app.openTask('idle-done')

  // 1) tab walk from the top: every focused element carries an outline or a box-shadow
  await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur())
  const stops: string[] = []
  const missing: string[] = []
  for (let i = 0; i < 30; i++) {
    await page.keyboard.press('Tab')
    const info = await page.evaluate(() => {
      const e = document.activeElement as HTMLElement | null
      if (!e || e === document.body) return null
      const c = getComputedStyle(e)
      const ring = (c.outlineStyle !== 'none' && parseFloat(c.outlineWidth) > 0) || (!!c.boxShadow && c.boxShadow !== 'none')
      return { id: `${e.tagName.toLowerCase()}:${e.dataset.testid || e.getAttribute('aria-label') || (e.innerText || '').trim().slice(0, 24)}`, ring }
    })
    if (!info) continue
    stops.push(info.id)
    if (!info.ring) missing.push(info.id)
  }
  expect(stops.length, 'the tab walk reached real controls').toBeGreaterThan(10)
  // soft: the rest of the keyboard journey still runs when a stop has no ring
  expect.soft(missing, 'tab stops without an outline or box-shadow').toEqual([])

  // 2) Permission mode and a sidebar collapse toggle show a ring when reached by keyboard
  const perm = page.getByRole('button', { name: /permission mode/i }).or(page.locator('[aria-label="Permission mode"]')).first()
  await expect(perm).toBeVisible()
  await tabOnto(page, perm)
  expect(await ring(perm)).toMatchObject({ focusVisible: true })
  const permRing = await ring(perm)
  expect(permRing.outline || permRing.shadow, 'Permission mode focus ring').toBe(true)

  const toggle = page.getByTestId('task-collapse-toggle').first()
  await toggle.waitFor({ state: 'attached' })
  await tabOnto(page, toggle)
  const tr = await ring(toggle)
  expect(tr.focusVisible).toBe(true)
  expect(tr.outline || tr.shadow, 'task-collapse-toggle focus ring').toBe(true)

  // 3) Escape closes the palette
  await page.keyboard.press('ControlOrMeta+k')
  const palette = page.getByTestId('command-palette')
  await expect(palette).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(palette).toBeHidden()
})

test('B-05 axe color-contrast: sidebar and home', async ({ app }) => {
  const { page } = app
  await app.openApp()
  await app.openDrawer()
  await expect(page.getByTestId('sidebar-quick-actions').first()).toBeVisible()
  await contrast(page, 'sidebar+home')
})

test('B-05 axe color-contrast: a finished task timeline', async ({ app }) => {
  const { page } = app
  await app.openApp()
  await app.openTask('idle-done')
  await expect(page.getByTestId('app-main-content')).toContainText(/retry backoff/i)
  // whole-page scans of the task screen return no result at all (axe gives up on the large DOM), so scan its regions
  await contrast(page, 'task timeline', ['[data-testid=run-log-column]', 'header', '[aria-label=Prompt]'])
})

test('B-05 axe color-contrast: Inbox', async ({ app }) => {
  const { page } = app
  await app.openApp()
  await app.openDrawer()
  await page.getByTestId('sidebar-footer-inbox').click()
  await expect(page.getByTestId('inbox-screen')).toBeVisible()
  await contrast(page, 'inbox')
})

test('B-05 axe color-contrast: Settings -> General', async ({ app }) => {
  const { page } = app
  await app.openApp()
  await page.evaluate(() => window.dispatchEvent(new CustomEvent('smind:open-settings', { detail: { sectionId: 'general' } })))
  await expect(page.getByTestId('settings-screen').first()).toBeVisible()
  await contrast(page, 'settings general')
})
