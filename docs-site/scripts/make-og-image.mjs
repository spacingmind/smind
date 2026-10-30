// Renders docs-site/assets/og-card.svg to a 1200x630 PNG via the Playwright headless shell. Usage: node scripts/make-og-image.mjs (CHROME_PATH optional)
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const scriptDir = path.dirname(fileURLToPath(import.meta.url))
const siteDir = path.resolve(scriptDir, '..')
const svgPath = path.join(siteDir, 'assets', 'og-card.svg')
const outPath = path.join(siteDir, '.vitepress', 'public', 'og-card.png')

function findHeadlessShell() {
  if (process.env.CHROME_PATH) return process.env.CHROME_PATH
  const cacheDir = path.join(os.homedir(), 'Library', 'Caches', 'ms-playwright')
  let entries
  try {
    entries = fs.readdirSync(cacheDir)
  } catch {
    return null
  }
  for (const entry of entries.filter((e) => e.startsWith('chromium_headless_shell-')).sort()) {
    const candidate = path.join(cacheDir, entry, 'chrome-mac', 'headless_shell')
    if (fs.existsSync(candidate)) return candidate
  }
  return null
}

const shell = findHeadlessShell()
if (!shell) {
  console.error(
    'No Playwright headless shell found. Set CHROME_PATH or install Playwright Chromium at ~/Library/Caches/ms-playwright/chromium_headless_shell-*/chrome-mac/headless_shell',
  )
  process.exit(1)
}

fs.mkdirSync(path.dirname(outPath), { recursive: true })

const htmlDir = fs.mkdtempSync(path.join(os.tmpdir(), 'og-html-'))
const profileDir = fs.mkdtempSync(path.join(os.tmpdir(), 'og-profile-'))

try {
  const htmlPath = path.join(htmlDir, 'card.html')
  fs.writeFileSync(
    htmlPath,
    `<html><body style="margin:0"><img src="file://${svgPath}" width=1200 height=630></body></html>`,
  )
  const baseArgs = [
    '--disable-gpu',
    '--no-sandbox',
    '--hide-scrollbars',
    '--force-device-scale-factor=1',
    '--window-size=1200,630',
    `--user-data-dir=${profileDir}`,
    `--screenshot=${outPath}`,
    `file://${htmlPath}`,
  ]

  // The shell sometimes hangs after flushing the screenshot; a written PNG still counts as success.
  try {
    execFileSync(shell, baseArgs, { stdio: 'inherit', timeout: 30000 })
  } catch (err) {
    if (!fs.existsSync(outPath) || fs.statSync(outPath).size === 0) throw err
  }

  if (!fs.existsSync(outPath)) {
    console.error('The headless shell did not write the screenshot file.')
    process.exit(1)
  }
  const bytes = fs.statSync(outPath).size
  console.log(`${outPath}: ${bytes} bytes`)
  if (bytes > 200000) {
    console.error('PNG exceeds 200000 bytes.')
    process.exit(1)
  }
} finally {
  fs.rmSync(htmlDir, { recursive: true, force: true })
  fs.rmSync(profileDir, { recursive: true, force: true })
}
