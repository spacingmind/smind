import fs from 'node:fs'
import net from 'node:net'
import os from 'node:os'
import path from 'node:path'

export const MARKER = '.smind-e2e'
const RESERVED_PORTS = new Set([4648, 4870])
const allocated = new Set<number>()

// A daemon home is only ever a temp dir this fixture created: under the OS temp dir and carrying the marker it wrote.
export function assertSafeHome(dir: string): void {
  let real: string
  try {
    real = fs.realpathSync(dir)
  } catch {
    throw new Error(`e2e guard: refusing home ${dir}: it does not exist`)
  }
  const tmp = fs.realpathSync(os.tmpdir())
  if (!(real + path.sep).startsWith(tmp + path.sep) || real === tmp) {
    throw new Error(`e2e guard: refusing home ${dir}: resolves to ${real}, not inside ${tmp}`)
  }
  if (!fs.existsSync(path.join(real, MARKER))) {
    throw new Error(`e2e guard: refusing home ${dir}: no ${MARKER} marker written by the fixture`)
  }
}

export function assertSafePort(port: number): void {
  if (RESERVED_PORTS.has(port)) throw new Error(`e2e guard: port ${port} is reserved for the real smind`)
  if (!allocated.has(port)) throw new Error(`e2e guard: port ${port} was not allocated by getFreePort() in this process`)
}

export function getFreePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = net.createServer()
    srv.once('error', reject)
    srv.listen(0, '127.0.0.1', () => {
      const { port } = srv.address() as net.AddressInfo
      srv.close(() => {
        if (RESERVED_PORTS.has(port)) return resolve(getFreePort())
        allocated.add(port)
        resolve(port)
      })
    })
  })
}
