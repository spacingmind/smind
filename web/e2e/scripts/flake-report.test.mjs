import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { percentile } from './flake-report.mjs'

const here = path.dirname(fileURLToPath(import.meta.url))
const script = path.join(here, 'flake-report.mjs')
const fixtureRun = path.join(here, 'fixtures', 'run-a')

function run(...args) {
  return execFileSync('node', [script, ...args], { encoding: 'utf8' })
}

function tmpDir() {
  return fs.mkdtempSync(path.join(os.tmpdir(), 'flake-report-test-'))
}

// One synthetic run: a single spec `S` that is flaky when `flaky`, whole run lasting `wall` ms.
function writeRun(root, id, { flaky = false, duration = 1000, wall = 60_000 } = {}) {
  const dir = path.join(root, String(id))
  fs.mkdirSync(dir, { recursive: true })
  const results = flaky
    ? [
        { status: 'failed', duration, retry: 0, attachments: [] },
        { status: 'passed', duration, retry: 1, attachments: [] },
      ]
    : [{ status: 'passed', duration, retry: 0, attachments: [] }]
  const report = {
    suites: [
      {
        title: 's.spec.ts',
        specs: [{ title: 'S', tests: [{ projectName: 'light-1280', status: flaky ? 'flaky' : 'expected', results }] }],
      },
    ],
    stats: { duration: wall, expected: flaky ? 0 : 1, flaky: flaky ? 1 : 0, unexpected: 0, skipped: 0 },
  }
  fs.writeFileSync(path.join(dir, 'results.json'), JSON.stringify(report))
  fs.writeFileSync(path.join(dir, 'flaky.json'), '[]')
}

test('flake_report_lists_retried_pass', () => {
  const out = run('--summary', fixtureRun)
  assert.match(out, /Flaky \(passed only on retry\)/)
  assert.match(out, /J19 terminal tabs keep their order/)
  assert.match(out, /dark-390/)
  // totals: 1 passed + 1 flaky, 1 failed, 1 skipped; wall 61.5 s
  assert.match(out, /passed[^\n]*\b2\b/i)
  assert.match(out, /failed[^\n]*\b1\b/i)
  assert.match(out, /skipped[^\n]*\b1\b/i)
  assert.match(out, /61\.5 ?s/)
  assert.match(out, /Slowest 5/)
  // first failing spec with the trace command
  assert.match(out, /J20 tab close moves selection/)
  assert.match(out, /bunx playwright show-trace \S*tabs-J20-light-1280-retry1\/trace\.zip/)
})

test('flake_report_weekly_flags_over_two_flakes_in_twenty_runs', () => {
  const three = tmpDir()
  const two = tmpDir()
  try {
    for (let id = 100; id < 120; id++) {
      writeRun(three, id, { flaky: id < 103 })
      writeRun(two, id, { flaky: id < 102 })
    }
    // an older (21st) run with a flake must fall outside the window
    writeRun(two, 50, { flaky: true })
    // The per-spec table also holds the row: only the candidates section proves the threshold.
    const candidates = (out) => out.split('### Quarantine candidates')[1] ?? ''
    const flagged = candidates(run('--weekly', three))
    assert.match(flagged, /\| S \| light-1280 \|/)

    const clean = candidates(run('--weekly', two))
    assert.doesNotMatch(clean, /\| S \| light-1280 \|/)
    assert.match(clean, /None\./)
  } finally {
    fs.rmSync(three, { recursive: true, force: true })
    fs.rmSync(two, { recursive: true, force: true })
  }
})

test('flake_report_p95', () => {
  const durations = Array.from({ length: 20 }, (_, i) => (i + 1) * 100)
  assert.equal(percentile(durations, 95), 1900)
  assert.equal(percentile(durations, 50), 1000)
  assert.equal(percentile([], 95), 0)

  const root = tmpDir()
  try {
    for (let i = 0; i < 20; i++) writeRun(root, 200 + i, { duration: durations[i], wall: durations[i] * 1000 })
    const out = run('--weekly', root)
    assert.match(out, /p95 run wall time[^\n]*1900 ?s/i)
    assert.match(out, /\| S \| light-1280 \| 20 \| 0 \| 0\.0% \| 1\.0 ?s \| 1\.9 ?s \|/)
  } finally {
    fs.rmSync(root, { recursive: true, force: true })
  }
})

test('flake_report_empty_dir_is_not_an_error', () => {
  const root = tmpDir()
  try {
    assert.match(run('--summary', root), /No results/i)
    assert.match(run('--weekly', root), /No runs/i)
    assert.match(run('--summary', path.join(root, 'missing')), /No results/i)
  } finally {
    fs.rmSync(root, { recursive: true, force: true })
  }
})
