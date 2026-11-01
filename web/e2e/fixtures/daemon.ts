import { type ChildProcess, spawn } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { MARKER, assertSafeHome, assertSafePort, getFreePort } from './guard'
import { type Repos, makeRepos } from './repos'

const here = path.dirname(fileURLToPath(import.meta.url))
const AGENT = path.join(here, 'uat-agent.cjs')

export type Daemon = {
  url: string
  port: number
  home: string
  /** Pid of the current child (undefined once stopped). */
  pid(): number | undefined
  repos: Repos
  /** Filled by seedDaemon(). */
  ids: Record<string, any>
  restart(): Promise<void>
  stop(): Promise<void>
}

const live = new Set<Daemon>()
let exitHooked = false

function binPath(): string {
  const bin = process.env.SMIND_BIN || path.resolve(here, '../../../bin/smind')
  if (!fs.existsSync(bin)) throw new Error(`smind binary not found at ${bin}: run \`task build\` (or set SMIND_BIN)`)
  return bin
}

async function waitHealthy(url: string, child: ChildProcess, log: string): Promise<void> {
  for (let i = 0; i < 120; i++) {
    if (child.exitCode !== null) throw new Error(`smind serve exited (${child.exitCode}); log:\n${fs.readFileSync(log, 'utf8').slice(-2000)}`)
    try {
      if ((await fetch(`${url}/healthz`)).ok) return
    } catch {
      /* not up yet */
    }
    await new Promise((r) => setTimeout(r, 250))
  }
  throw new Error(`${url}/healthz never answered; log:\n${fs.readFileSync(log, 'utf8').slice(-2000)}`)
}

export async function startDaemon(): Promise<Daemon> {
  const bin = binPath()
  const home = fs.mkdtempSync(path.join(os.tmpdir(), 'smind-e2e-'))
  fs.writeFileSync(path.join(home, MARKER), `${process.pid}\n`)
  assertSafeHome(home)
  const port = await getFreePort()
  assertSafePort(port)
  fs.writeFileSync(
    path.join(home, 'config.yaml'),
    `server:\n  port: ${port}\nproviders:\n  custom:\n    - id: uat\n      label: UAT Agent\n      command: ${JSON.stringify(process.execPath)}\n      args: [${JSON.stringify(AGENT)}]\n`,
  )
  const repos = makeRepos(path.join(home, 'repos'))
  const url = `http://127.0.0.1:${port}`
  const logPath = path.join(home, 'daemon.log')
  let child: ChildProcess | undefined

  const spawnChild = async () => {
    assertSafeHome(home)
    assertSafePort(port)
    const fd = fs.openSync(logPath, 'a')
    const c = spawn(bin, ['serve'], { env: { ...process.env, SMIND_HOME: home, HOME: home }, stdio: ['ignore', fd, fd], detached: true })
    fs.closeSync(fd)
    child = c
    await waitHealthy(url, c, logPath)
  }
  const groupAlive = (pgid: number) => {
    try {
      process.kill(-pgid, 0)
      return true
    } catch (e) {
      return (e as NodeJS.ErrnoException).code !== 'ESRCH'
    }
  }
  const signalGroup = (pgid: number, sig: NodeJS.Signals) => {
    try {
      process.kill(-pgid, sig)
    } catch {
      /* group already gone */
    }
  }
  // Kills the daemon's whole process group (provider probes and agent children included) and returns once it is empty.
  const kill = async () => {
    const c = child
    child = undefined
    const pgid = c?.pid
    if (!c || pgid === undefined) return
    const exited = c.exitCode !== null ? Promise.resolve() : new Promise<void>((r) => c.once('exit', () => r()))
    signalGroup(pgid, 'SIGTERM')
    const timer = setTimeout(() => signalGroup(pgid, 'SIGKILL'), 5_000)
    await exited // SIGKILL after 5 s still ends in an 'exit' event
    clearTimeout(timer)
    for (let i = 0; groupAlive(pgid); i++) {
      if (i === 50) signalGroup(pgid, 'SIGKILL')
      if (i >= 150) throw new Error(`e2e: process group ${pgid} survived SIGKILL`)
      await new Promise((r) => setTimeout(r, 100))
    }
  }

  const d: Daemon = {
    url,
    port,
    home,
    pid: () => child?.pid,
    repos,
    ids: {},
    restart: async () => {
      await kill()
      await spawnChild()
    },
    stop: async () => {
      live.delete(d)
      const keep = process.env.E2E_LOG_DIR
      try {
        await kill()
        if (keep) {
          fs.mkdirSync(keep, { recursive: true })
          fs.copyFileSync(logPath, path.join(keep, `daemon-${port}.log`))
        }
        assertSafeHome(home)
        // agent and worktree git children can still be writing for a moment after the daemon exits
        const rm = () => fs.promises.rm(home, { recursive: true, force: true, maxRetries: 8, retryDelay: 250 })
        await rm()
        if (fs.existsSync(home)) await rm() // something re-created it after the first pass
      } catch (e) {
        console.error(`e2e: cleanup of ${home} failed: ${(e as Error).message}`)
      }
    },
  }
  live.add(d)
  if (!exitHooked) {
    exitHooked = true
    // An aborted run must not leave a daemon or a temp home behind (sync: 'exit' handlers cannot await).
    const killAll = () => {
      for (const x of live) (x as any).__killSync?.()
    }
    process.on('exit', killAll)
    for (const sig of ['SIGINT', 'SIGTERM'] as const) {
      process.on(sig, () => {
        killAll()
        process.exit(1)
      })
    }
  }
  ;(d as any).__killSync = () => {
    if (child?.pid !== undefined) signalGroup(child.pid, 'SIGKILL')
    try {
      assertSafeHome(home)
      fs.rmSync(home, { recursive: true, force: true })
    } catch {
      /* already gone */
    }
  }
  try {
    await spawnChild()
  } catch (e) {
    await d.stop()
    throw e
  }
  return d
}
