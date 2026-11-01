// Flake policy: no fixed sleeps in specs or fixtures; wait on the page or on an RPC response instead.
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.join(path.dirname(fileURLToPath(import.meta.url)), '..')
const hits = []
const walk = (dir) => {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name)
    if (e.isDirectory()) walk(p)
    else if (/\.(ts|mjs|cjs|js)$/.test(e.name)) {
      fs.readFileSync(p, 'utf8').split('\n').forEach((line, i) => {
        if (line.includes('waitForTimeout')) hits.push(`${path.relative(root, p)}:${i + 1}: ${line.trim()}`)
      })
    }
  }
}
for (const d of ['specs', 'fixtures']) walk(path.join(root, d))
if (hits.length) {
  console.error('waitForTimeout is banned in specs/ and fixtures/:\n' + hits.join('\n'))
  process.exit(1)
}
