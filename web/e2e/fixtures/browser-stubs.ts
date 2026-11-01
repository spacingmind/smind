import type { Page } from '@playwright/test'

/**
 * A FAITHFUL websocket refusal (as a refused TCP connect looks to the page): the socket never opens, fires
 * `error` then `close(1006)`. page.routeWebSocket + close() would open a mocked socket and the app would never
 * see a failure. `window.__refuse = false` lets later sockets through to the daemon.
 */
export async function refuseWebSocket(page: Page): Promise<void> {
  await page.addInitScript(() => {
    const w = window as any
    const Real = window.WebSocket
    w.__refuse = true
    function Fake(this: unknown, url: string | URL, protocols?: string | string[]) {
      if (!w.__refuse || !/\/ws/.test(String(url))) return protocols ? new Real(url, protocols) : new Real(url)
      const t = new EventTarget()
      const o: any = {
        url: String(url),
        readyState: 0,
        protocol: '',
        extensions: '',
        bufferedAmount: 0,
        binaryType: 'blob',
        close() {},
        send() {},
        onopen: null,
        onmessage: null,
        onerror: null,
        onclose: null,
        addEventListener: (...a: [string, any]) => t.addEventListener(...a),
        removeEventListener: (...a: [string, any]) => t.removeEventListener(...a),
        dispatchEvent: (e: Event) => t.dispatchEvent(e),
      }
      setTimeout(() => {
        o.readyState = 3
        const e = new Event('error')
        o.onerror?.(e)
        t.dispatchEvent(e)
        const c = new CloseEvent('close', { code: 1006, wasClean: false })
        o.onclose?.(c)
        t.dispatchEvent(c)
      }, 30)
      return o
    }
    Object.assign(Fake, { CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3 })
    Fake.prototype = Real.prototype
    w.WebSocket = Fake
  })
}

export const INVITE = 'smind-host://v1#eyJ2IjoxfQ'

/** Stubs fleet.host.* over the page's /ws (the isolated daemon may not serve them); everything else goes to the daemon. */
export async function stubFleet(page: Page): Promise<void> {
  const off = new Set(['fleet.host.add', 'fleet.host.approve', 'fleet.host.reject', 'fleet.host.rm', 'fleet.host.revoke', 'fleet.host.rename', 'fleet.host.grant', 'fleet.host.test'])
  await page.routeWebSocket(/\/ws/, (sock) => {
    const server = sock.connectToServer()
    sock.onMessage((raw) => {
      let m: any
      try {
        m = JSON.parse(String(raw))
      } catch {
        return server.send(raw)
      }
      const method: string = m.method || ''
      if (m.id && method === 'fleet.host.list') return sock.send(JSON.stringify({ id: m.id, result: { hosts: [] } }))
      if (m.id && method === 'fleet.host.invite') return sock.send(JSON.stringify({ id: m.id, result: { invite: INVITE, expiresAt: new Date(Date.now() + 600e3).toISOString() } }))
      if (m.id && off.has(method)) return sock.send(JSON.stringify({ id: m.id, result: {} }))
      server.send(raw)
    })
    server.onMessage((raw) => sock.send(raw))
  })
}

/**
 * axe-core 4.10 cannot parse the app's `oklch()` token colors: color-contrast then reports neither a pass, a
 * violation nor an incomplete (0 nodes judged), so a "zero violations" result would prove nothing. Before a scan,
 * pin every element's computed color / background-color to an sRGB value (resolved through a canvas pixel) so axe can
 * read it. Only the scanned page is touched; call it right before AxeBuilder.analyze().
 */
export async function pinColorsToRgb(page: Page): Promise<void> {
  await page.evaluate(() => {
    const cv = document.createElement('canvas')
    cv.width = cv.height = 1
    const cx = cv.getContext('2d', { willReadFrequently: true })!
    const rgba = (c: string) => {
      cx.clearRect(0, 0, 1, 1)
      cx.fillStyle = '#000'
      cx.fillStyle = c
      cx.fillRect(0, 0, 1, 1)
      const d = cx.getImageData(0, 0, 1, 1).data
      return `rgba(${d[0]}, ${d[1]}, ${d[2]}, ${(d[3] / 255).toFixed(3)})`
    }
    const cache = new Map<string, string>()
    const conv = (c: string) => {
      let v = cache.get(c)
      if (!v) cache.set(c, (v = /^rgba?\(/.test(c) ? c : rgba(c)))
      return v
    }
    for (const e of document.querySelectorAll<HTMLElement>('body, body *')) {
      const cs = getComputedStyle(e)
      e.style.setProperty('color', conv(cs.color), 'important')
      if (cs.backgroundColor !== 'rgba(0, 0, 0, 0)') e.style.setProperty('background-color', conv(cs.backgroundColor), 'important')
    }
    const root = getComputedStyle(document.documentElement).backgroundColor
    document.documentElement.style.setProperty('background-color', conv(root === 'rgba(0, 0, 0, 0)' ? getComputedStyle(document.body).backgroundColor : root), 'important')
  })
}
