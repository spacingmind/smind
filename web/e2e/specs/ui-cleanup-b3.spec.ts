import fs from 'node:fs'
import path from 'node:path'
import { type Page, type TestInfo } from '@playwright/test'
import { expect, test } from '../fixtures/test'

// UI cleanup B3 (docs/plans/active/ui-cleanup-b3.md): Settings is a modal dialog over the workspace.
// Frames only when UICLEAN3_FRAMES_DIR is set.

const widthOf = (testInfo: TestInfo) => testInfo.project.use.viewport?.width ?? 1280

const dialog = (page: Page) => page.getByTestId('settings-dialog')

async function openSettings(page: Page, sectionId?: string) {
  await page.evaluate((id) => window.dispatchEvent(new CustomEvent('smind:open-settings', { detail: id ? { sectionId: id } : {} })), sectionId)
  await expect(dialog(page)).toBeVisible()
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

// Escape closes the dialog; in the narrow layout a deep-linked detail page pops to the list first.
async function closeByEscape(page: Page, testInfo: TestInfo) {
  if (widthOf(testInfo) < 768) {
    await page.keyboard.press('Escape')
    await expect(page.locator('[data-testid^="settings-section-"]')).toHaveCount(0)
  }
  await page.keyboard.press('Escape')
  await expect(dialog(page)).toBeHidden()
}

function framesDir() {
  const dir = process.env.UICLEAN3_FRAMES_DIR
  if (dir) fs.mkdirSync(dir, { recursive: true })
  return dir
}

async function shot(page: Page, testInfo: TestInfo, name: string) {
  const dir = framesDir()
  if (!dir) return
  await page.screenshot({ path: path.join(dir, `${name}-${testInfo.project.name}.png`) })
}

async function settledShot(page: Page, testInfo: TestInfo, name: string) {
  if (!framesDir()) return
  await settled(page)
  await shot(page, testInfo, name)
}

test('UC3-01 Settings opens as a modal and closes by Esc, the X and the backdrop', async ({ app }, testInfo) => {
  const { page } = app
  const width = widthOf(testInfo)
  const height = testInfo.project.use.viewport?.height ?? 800
  await app.openApp()
  await app.openTask('idle-done')

  await openSettings(page)
  await page.keyboard.press('Escape')
  await expect(dialog(page)).toBeHidden()

  await page.keyboard.press('ControlOrMeta+,')
  await expect(dialog(page)).toBeVisible()
  if (width >= 768) await page.getByTestId('settings-close-button').click()
  else await page.getByTestId('settings-back-button').first().click()
  await expect(dialog(page)).toBeHidden()

  if (width > 860 && height > 560) {
    await openSettings(page)
    await page.locator('[data-slot="dialog-overlay"]').click({ position: { x: 5, y: 5 } })
    await expect(dialog(page)).toBeHidden()
  }

  await page.keyboard.press('ControlOrMeta+,')
  await expect(dialog(page)).toBeVisible()
  await page.keyboard.press('ControlOrMeta+,')
  await expect(dialog(page)).toBeHidden()
})

test('UC3-02 the workspace stays visible and a draft survives', async ({ app }, testInfo) => {
  const { page } = app
  await app.openApp()
  await app.openTask('idle-done')
  const prompt = page.locator('[aria-label="Prompt"]').first()
  await prompt.fill('draft kept')
  await openSettings(page)
  // At phone width the shell is covered by the sheet and content-region is not rendered (isMobile): only the wide layout keeps it live.
  if (widthOf(testInfo) >= 768) {
    const region = page.getByTestId('content-region')
    await expect(region).toBeVisible()
    await expect(region).not.toHaveAttribute('inert', '')
  }
  await closeByEscape(page, testInfo)
  await expect(prompt).toHaveValue('draft kept')
})

test('UC3-03 shortcuts are restricted while open and deep links land on the section', async ({ app }, testInfo) => {
  const { page } = app
  await app.openApp()
  await app.openTask('idle-done')
  const title = page.getByTestId('app-header-title').first()
  await expect(title).toContainText('idle-done')

  await openSettings(page, 'shortcuts')
  await expect(page.getByTestId('settings-section-shortcuts')).toBeVisible()
  await page.keyboard.press('ControlOrMeta+]')
  await expect(page.getByTestId('settings-section-shortcuts')).toBeVisible()
  await expect(title).toContainText('idle-done')

  await closeByEscape(page, testInfo)
  await page.keyboard.press('ControlOrMeta+]')
  await expect(title).not.toContainText('idle-done')
})

test('UC3-04 small sizes are a full-bleed sheet', async ({ app }, testInfo) => {
  test.skip(widthOf(testInfo) < 768, 'the viewport is resized from desktop sizes only')
  const { page } = app
  await app.openApp()
  await page.setViewportSize({ width: 800, height: 520 })
  await openSettings(page)
  await expect.poll(async () => (await dialog(page).boundingBox())?.width).toBe(800)
  const sheet = await dialog(page).boundingBox()
  expect(sheet).toMatchObject({ x: 0, y: 0, width: 800, height: 520 })
  expect(await dialog(page).evaluate((e) => getComputedStyle(e).borderTopLeftRadius)).toBe('0px')

  await page.setViewportSize({ width: 1280, height: 800 })
  await expect.poll(async () => (await dialog(page).boundingBox())?.width ?? 9999).toBeLessThanOrEqual(960)
  await settled(page)
  const box = (await dialog(page).boundingBox())!
  expect(box.width).toBeLessThanOrEqual(960)
  expect(box.x).toBeCloseTo((1280 - box.width) / 2, 0)
  const radius = await dialog(page).evaluate((e) => parseFloat(getComputedStyle(e).borderTopLeftRadius))
  expect(radius).toBeGreaterThan(0)
})

test('UC3-05 transition frames', async ({ app }, testInfo) => {
  const { page } = app
  const frames = !!process.env.UICLEAN3_FRAMES_DIR
  await app.openApp()
  await app.openTask('idle-done')
  await settledShot(page, testInfo, 'closed')

  const burst = async (prefix: string) => {
    for (let i = 0; i < 5; i++) await shot(page, testInfo, `${prefix}-${i}`)
  }

  await openSettings(page)
  if (frames) {
    await burst('tr-open')
    await settledShot(page, testInfo, 'open-settled')
  }
  await page.keyboard.press('Escape')
  if (frames) await burst('tr-close')
  await expect(dialog(page)).toBeHidden()

  if (frames) {
    for (const id of ['general', 'shortcuts', 'automations', 'browser']) {
      await openSettings(page, id)
      await settledShot(page, testInfo, `section-${id}`)
      await page.keyboard.press('Escape')
      await expect(dialog(page)).toBeHidden()
    }
    await page.keyboard.press('Shift+Slash')
    await expect(page.getByTestId('shortcuts-overlay')).toBeVisible()
    await settledShot(page, testInfo, 'nested-overlay')
    await page.getByRole('button', { name: 'Customize in Settings' }).click()
    await expect(dialog(page)).toBeVisible()
    await settledShot(page, testInfo, 'nested-after')
  }
})
