#!/usr/bin/env node
// Markdown reports over Playwright's JSON reporter output (test-results/results.json)
// and reporters/flaky.ts (test-results/flaky.json).
//   --summary <dir>   one run, for $GITHUB_STEP_SUMMARY
//   --weekly <dir>    a directory of run directories (one downloaded e2e-timing artifact each)
// Exit code is 0 except on usage errors: the workflow decides what a report means.
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const WINDOW = 20
const QUARANTINE_OVER = 2

export function percentile(values, p) {
  if (values.length === 0) return 0
  const sorted = [...values].sort((a, b) => a - b)
  return sorted[Math.max(0, Math.ceil((p / 100) * sorted.length) - 1)]
}

function readJson(file) {
  try {
    return JSON.parse(fs.readFileSync(file, 'utf8'))
  } catch {
    return undefined
  }
}

// Every test of a Playwright JSON report, walking nested suites.
export function collectTests(report) {
  const out = []
  const walk = (suite) => {
    for (const spec of suite.specs ?? []) {
      for (const t of spec.tests ?? []) {
        const results = t.results ?? []
        out.push({
          title: spec.title,
          file: spec.file ?? suite.file ?? '',
          project: t.projectName ?? '',
          status: t.status,
          results,
          duration: results.reduce((sum, r) => sum + (r.duration ?? 0), 0),
        })
      }
    }
    for (const child of suite.suites ?? []) walk(child)
  }
  for (const suite of report?.suites ?? []) walk(suite)
  return out
}

const secs = (ms) => `${(ms / 1000).toFixed(1)} s`
const cell = (s) => String(s).replace(/\|/g, '\\|')

function loadRun(dir) {
  const report = readJson(path.join(dir, 'results.json'))
  const flakyFile = readJson(path.join(dir, 'flaky.json'))
  return { report, tests: collectTests(report), flakyFile: Array.isArray(flakyFile) ? flakyFile : [] }
}

function tracePath(test) {
  for (const r of [...test.results].reverse()) {
    const trace = (r.attachments ?? []).find((a) => a.name === 'trace' && a.path)
    if (trace) return trace.path
  }
  return undefined
}

export function summary(dir) {
  const { report, tests, flakyFile } = loadRun(dir)
  if (!report) return `## e2e\n\nNo results found in \`${dir}\` (results.json missing or unreadable).\n`

  const count = (s) => tests.filter((t) => t.status === s).length
  const flaky = tests.filter((t) => t.status === 'flaky')
  const passed = count('expected') + flaky.length
  const failed = count('unexpected')
  const skipped = count('skipped')
  const lines = ['## e2e', '']
  lines.push(`**Totals:** passed ${passed}, failed ${failed}, flaky ${flaky.length}, skipped ${skipped}. Wall time ${secs(report.stats?.duration ?? 0)}.`, '')

  lines.push('### Slowest 5 specs', '', '| spec | project | duration |', '|---|---|---|')
  for (const t of [...tests].filter((x) => x.status !== 'skipped').sort((a, b) => b.duration - a.duration).slice(0, 5)) {
    lines.push(`| ${cell(t.title)} | ${t.project} | ${secs(t.duration)} |`)
  }
  lines.push('')

  lines.push('### Flaky (passed only on retry)', '')
  const flakyRows = flaky.length > 0 ? flaky.map((t) => ({ title: t.title, project: t.project, retries: t.results.length - 1 })) : flakyFile
  if (flakyRows.length === 0) {
    lines.push('None.', '')
  } else {
    lines.push('| spec | project | retries |', '|---|---|---|')
    for (const f of flakyRows) lines.push(`| ${cell(f.title)} | ${f.project} | ${f.retries} |`)
    lines.push('')
  }

  const firstFailing = tests.find((t) => t.status === 'unexpected')
  if (firstFailing) {
    const trace = tracePath(firstFailing)
    lines.push('### First failing spec', '', `\`${firstFailing.title}\` (${firstFailing.project})`, '')
    if (trace) {
      lines.push('Open its trace from the `e2e-failures-<run>` artifact:', '', '```sh', `bunx playwright show-trace ${trace}`, '```', '')
    } else {
      lines.push('No trace was recorded; see the screenshots and daemon logs in the `e2e-failures-<run>` artifact.', '')
    }
  }
  return lines.join('\n')
}

export function weekly(dir) {
  let names = []
  try {
    names = fs.readdirSync(dir, { withFileTypes: true }).filter((e) => e.isDirectory()).map((e) => e.name)
  } catch {
    // a missing directory is the same as no runs
  }
  // Run ids grow with time: the last WINDOW runs are the largest ids.
  names.sort((a, b) => (Number(a) - Number(b)) || a.localeCompare(b))
  const runs = names
    .map((name) => ({ name, ...loadRun(path.join(dir, name)) }))
    .filter((r) => r.report)
    .slice(-WINDOW)
  if (runs.length === 0) return `## e2e weekly report\n\nNo runs with results found in \`${dir}\`.\n`

  const specs = new Map()
  let total = 0
  let flakyTotal = 0
  for (const run of runs) {
    for (const t of run.tests) {
      if (t.status === 'skipped') continue
      const key = `${t.title}\u0000${t.project}`
      const s = specs.get(key) ?? { title: t.title, project: t.project, runs: 0, flaky: 0, durations: [] }
      s.runs++
      if (t.status === 'flaky') s.flaky++
      s.durations.push(t.duration)
      specs.set(key, s)
      total++
      if (t.status === 'flaky') flakyTotal++
    }
  }
  const rows = [...specs.values()].sort((a, b) => b.flaky - a.flaky || a.title.localeCompare(b.title) || a.project.localeCompare(b.project))
  const walls = runs.map((r) => r.report.stats?.duration ?? 0)
  const rate = (f, n) => (n === 0 ? '0.0%' : `${((f / n) * 100).toFixed(1)}%`)

  const lines = ['## e2e weekly report', '']
  lines.push(`Runs analysed: ${runs.length} (last ${WINDOW} completed)`, '')
  lines.push(`- Overall flake rate: ${rate(flakyTotal, total)} (${flakyTotal} of ${total} spec runs)`)
  lines.push(`- p95 run wall time: ${Math.round(percentile(walls, 95) / 1000)} s (p50 ${Math.round(percentile(walls, 50) / 1000)} s)`, '')
  lines.push('### Per spec', '', '| spec | project | runs | flaky | flake rate | p50 | p95 |', '|---|---|---|---|---|---|---|')
  for (const s of rows) {
    lines.push(`| ${cell(s.title)} | ${s.project} | ${s.runs} | ${s.flaky} | ${rate(s.flaky, s.runs)} | ${secs(percentile(s.durations, 50))} | ${secs(percentile(s.durations, 95))} |`)
  }
  lines.push('')
  lines.push('### Quarantine candidates', '', `Specs with more than ${QUARANTINE_OVER} flaky runs among the last ${WINDOW}.`, '')
  const candidates = rows.filter((s) => s.flaky > QUARANTINE_OVER)
  if (candidates.length === 0) {
    lines.push('None.', '')
  } else {
    lines.push('| spec | project | flaky runs | flake rate |', '|---|---|---|---|')
    for (const s of candidates) lines.push(`| ${cell(s.title)} | ${s.project} | ${s.flaky} | ${rate(s.flaky, s.runs)} |`)
    lines.push('')
  }
  return lines.join('\n')
}

function main(argv) {
  const [mode, dir] = argv
  if ((mode !== '--summary' && mode !== '--weekly') || !dir) {
    process.stderr.write('usage: flake-report.mjs --summary <results-dir> | --weekly <dir-of-run-dirs>\n')
    return 2
  }
  process.stdout.write((mode === '--summary' ? summary(dir) : weekly(dir)) + '\n')
  return 0
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  process.exitCode = main(process.argv.slice(2))
}
