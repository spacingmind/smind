import fs from 'node:fs'
import path from 'node:path'
import { type Page, type TestInfo } from '@playwright/test'
import { type App, expect, test } from '../fixtures/test'

/** Opens the dialog the way a person does: the sidebar footer at desktop sizes, the window event at 390. */
async function openDialog(app: App) {
  const { page } = app
  await app.openDrawer()
  if (app.narrow) {
    // At 390 the sidebar is a closed sheet; every entry point lands on this same event.
    await page.evaluate(() => window.dispatchEvent(new CustomEvent('smind:new-workspace')))
  } else {
    await page.getByTestId('sidebar-footer-add-workspace').click()
  }
  await expect(page.getByTestId('add-project-dialog')).toBeVisible()
}

async function shot(page: Page, testInfo: TestInfo) {
  const dir = process.env.W1_FRAMES_DIR
  if (!dir) return
  fs.mkdirSync(dir, { recursive: true })
  await page.evaluate(() =>
    Promise.all(
      document
        .getAnimations()
        .filter((a) => a.effect?.getComputedTiming().iterations !== Infinity)
        .map((a) => a.finished.catch(() => undefined)),
    ),
  )
  await page.screenshot({ path: path.join(dir, `add-project-open-${testInfo.project.name}.png`) })
}

test('W1-01 the Add project submit is enabled and solid when the dialog opens with an empty path', async ({ app }, testInfo) => {
  const { page } = app
  await app.openApp()
  await openDialog(app)
  try {
    await expect(page.getByTestId('add-project-path-input')).toBeFocused()
    await expect(page.getByTestId('add-project-path-input')).toHaveValue('')
    const submit = page.getByTestId('add-project-submit')
    await expect.soft(submit).toBeEnabled()
    await expect.soft(submit).not.toHaveAttribute('aria-disabled', 'true')
    const style = await submit.evaluate((button) => ({
      opacity: getComputedStyle(button).opacity,
      background: getComputedStyle(button).backgroundColor,
      bodyBackground: getComputedStyle(document.body).backgroundColor,
    }))
    expect.soft(style.opacity).toBe('1')
    // Computed RGB colors omit alpha when opaque; RGBA must explicitly have alpha 1.
    expect.soft(style.background).toMatch(/^rgb\(\s*\d+\s*,\s*\d+\s*,\s*\d+\s*\)$|^rgba\(\s*\d+\s*,\s*\d+\s*,\s*\d+\s*,\s*1(?:\.0+)?\s*\)$/)
    expect.soft(style.background).not.toBe(style.bodyBackground)
  } finally {
    // Preserve the before frame even when the regression assertions fail.
    await shot(page, testInfo)
  }
})

test('W1-02 the path field keeps focus from open: no blur reaches the folder check', async ({ app }) => {
  const { page } = app
  await app.openApp()
  const record = await page.evaluateHandle(() => {
    const state = { focusouts: [] as number[] }
    document.addEventListener('focusout', (event) => {
      if (event.target instanceof Element && event.target.getAttribute('data-testid') === 'add-project-path-input') {
        state.focusouts.push(performance.now())
      }
    }, true)
    return state
  })
  await openDialog(app)
  await expect(page.getByTestId('add-project-path-input')).toBeFocused()
  // Observe a full settling window: an immediate zero could miss a delayed focus transfer.
  const focusedAt = Date.now()
  await expect.poll(() => Date.now() - focusedAt, { intervals: [50, 100, 250] }).toBeGreaterThanOrEqual(750)
  expect(await record.evaluate((state) => state.focusouts.length)).toBe(0)
  await expect(page.getByTestId('add-project-path-input')).toBeFocused()
})
