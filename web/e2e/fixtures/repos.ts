import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'

export type Repos = { alpha: string; beta: string; gamma: string }

// Three disposable git repos (port of the phase 1 make-repos.sh). No global git config, no remote.
export function makeRepos(root: string): Repos {
  const env = { ...process.env, GIT_CONFIG_GLOBAL: '/dev/null', GIT_CONFIG_SYSTEM: '/dev/null', GIT_CONFIG_NOSYSTEM: '1' }
  const git = (dir: string, ...args: string[]) => execFileSync('git', ['-C', dir, ...args], { env, stdio: 'pipe' })
  const write = (dir: string, rel: string, body: string) => {
    fs.mkdirSync(path.dirname(path.join(dir, rel)), { recursive: true })
    fs.writeFileSync(path.join(dir, rel), body)
  }
  const append = (dir: string, rel: string, body: string) => fs.appendFileSync(path.join(dir, rel), body)
  const init = (name: string) => {
    const dir = path.join(root, name)
    fs.mkdirSync(dir, { recursive: true })
    git(dir, 'init', '-q', '-b', 'main')
    git(dir, 'config', 'user.name', 'E2E Bot')
    git(dir, 'config', 'user.email', 'e2e@example.invalid')
    git(dir, 'config', 'commit.gpgsign', 'false')
    return dir
  }
  const commit = (dir: string, msg: string, ...extra: string[]) => {
    git(dir, 'add', '-A')
    git(dir, 'commit', '-qm', msg, ...extra)
  }

  const A = init('alpha')
  write(A, 'README.md', '# alpha\n\nA disposable repo for UAT.\n')
  commit(A, 'init: readme')
  for (const f of ['main', 'app', 'config']) {
    write(A, `src/${f}.go`, `package src\n\n// ${f} is part of alpha.\nfunc ${f}() string { return "${f}" }\n`)
  }
  commit(A, 'feat: core files')
  for (const f of ['router', 'retry', 'policy']) write(A, `src/routing/${f}.go`, `package routing\n\nfunc ${f}() int { return 1 }\n`)
  write(A, 'src/routing/retry.go', 'package routing\n\nimport "time"\n\nfunc Backoff(attempt int) time.Duration {\n\tbackoff := time.Second\n\treturn backoff\n}\n')
  commit(A, 'feat: routing')
  for (const f of ['strings', 'paths', 'clock']) write(A, `src/util/${f}.go`, `package util\n\nfunc ${f}() {}\n`)
  commit(A, 'feat: util helpers')
  write(A, 'src/app_test.go', 'package src\n\nfunc test() {}\n')
  write(A, 'src/routing/router_test.go', 'package routing\n\nfunc testRouter() {}\n')
  write(A, 'README.md', '# alpha\n\nA disposable repo for UAT.\n\n## Usage\n\nRun nothing.\n')
  commit(A, 'test: add tests, document usage')
  // uncommitted: modify a tracked file (+ readme), add an untracked file
  write(A, 'src/app.go', 'package src\n\n// app is part of alpha.\nfunc app() string { return "app v2" }\n\nfunc extra() {}\n')
  append(A, 'README.md', '\n## Uncommitted\n\nthis line is not committed.\n')
  write(A, 'src/scratch.go', 'package src\n\n// scratch is untracked.\nfunc scratch() {}\n')

  const B = init('beta')
  write(B, 'README.md', '# beta\n')
  write(B, 'main.py', 'print("beta")\n')
  commit(B, 'init beta')
  write(B, 'lib.py', 'def add(a, b):\n    return a + b\n')
  commit(B, 'add lib')

  const G = init('gamma')
  git(G, 'commit', '-q', '--allow-empty', '-m', 'init gamma')

  // the seed writes a `scenario` file into every task worktree; keep it out of git status/diff counts
  for (const n of ['alpha', 'beta', 'gamma']) append(path.join(root, n), '.git/info/exclude', 'scenario\n')
  return { alpha: A, beta: B, gamma: G }
}
