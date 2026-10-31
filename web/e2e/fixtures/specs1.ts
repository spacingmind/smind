import type { Page } from '@playwright/test'
import { expect } from './test'

/** Moves the pointer off the sidebar so its row hover tooltip cannot cover the composer controls. */
export async function parkMouse(page: Page): Promise<void> {
  await page.mouse.move(700, 300)
  await expect(page.locator('[data-radix-popper-content-wrapper]')).toHaveCount(0)
}
