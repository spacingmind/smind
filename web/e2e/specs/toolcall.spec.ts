import fs from 'node:fs'
import path from 'node:path'
import { expect, test } from '../fixtures/test'

test('A-10 an Edit tool call expands to an inline diff, not raw JSON', async ({ app }) => {
  const { page } = app
  await app.openApp()
  await app.openTask('idle-done')
  const toggle = page.getByTestId('tool-call-toggle').filter({ hasText: 'retry.go' }).first()
  await toggle.scrollIntoViewIfNeeded()
  await toggle.click()
  const main = page.getByTestId('app-main-content')
  await expect(main).not.toContainText(/"type":\s*"diff"/)
  // one removed and one added row (the fake agent's oldText/newText are single lines)
  const rows = () =>
    main.evaluate((m) => {
      const lines = ((m as HTMLElement).innerText.match(/^[+-].*$/gm) ?? []).filter((l: string) => l.includes('backoff'))
      return { minus: lines.filter((l: string) => l[0] === '-'), plus: lines.filter((l: string) => l[0] === '+') }
    })
  await expect.poll(async () => (await rows()).minus.length).toBe(1)
  const r = await rows()
  expect(r.plus.length).toBe(1)
  expect(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1)).toBe(false)
})

test('A-12 retrying an errored task shows the resume note as its own blockquote paragraph', async ({ app }) => {
  const { page } = app
  const title = `a12-${test.info().project.name}`
  const t = await app.createTask(title, { scenario: 'error', prompt: 'This will fail' })
  await app.openApp()
  await app.openTask(title)
  await expect(page.getByTestId('run-error-box').first()).toBeVisible({ timeout: 20_000 })
  // the retry must produce assistant text after the note: switch the fake agent to its short streaming scenario
  // never retry with a real provider: the composer chip must say UAT first
  await expect(page.getByTestId('composer-model-picker').first()).toHaveText(/uat/i)
  fs.writeFileSync(path.join(t.worktree, 'scenario'), 'slow\n')
  await page.getByTestId('run-retry').or(page.getByTestId('run-rerun')).first().click()
  const main = page.getByTestId('app-main-content')
  await expect(main).toContainText('Chunk 5 of 5', { timeout: 30_000 })
  // reload: the replayed log is what carries the note
  await page.reload()
  await app.openTask(title)
  await expect(main).toContainText('Chunk 5 of 5', { timeout: 30_000 })
  // the note is a session_note chunk on the replay path (run.logs); it is its own blockquote and the next text follows it
  const quote = main.locator('blockquote').filter({ hasText: /does not support resuming a session/ })
  await expect(quote).toHaveCount(1)
  await expect(quote).not.toContainText('Chunk')
  const next = quote.locator('xpath=following-sibling::*[1]')
  await expect(next).toContainText('Chunk 1 of 5')
  await expect(next).not.toContainText('does not support resuming')
})
