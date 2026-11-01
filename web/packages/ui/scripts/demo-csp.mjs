// The demo build's security policy (ADR-0051 section 10), as pure functions shared by the Vite plugin
// (vite-plugin-demo-security.ts), scripts/check-demo-build.mjs and their tests.
import { createHash } from "node:crypto";

/** Matches an inline classic `<script>` (no src attribute) and captures its text. */
const INLINE_SCRIPT = /<script(?![^>]*\ssrc=)(?![^>]*type="module")[^>]*>([\s\S]*?)<\/script>/gi;

/** `'sha256-<base64>'` source expressions for every inline classic script in html, in document order. */
export function inlineScriptHashes(html) {
  const out = [];
  for (const m of html.matchAll(INLINE_SCRIPT)) {
    out.push(`'sha256-${createHash("sha256").update(m[1], "utf8").digest("base64")}'`);
  }
  return out;
}

/**
 * The policy as an ordered directive list. `frameAncestors` is header-only: a <meta> CSP ignores it (and the
 * browser logs a warning), so the meta variant leaves it out.
 */
export function cspDirectives(scriptHashes, { frameAncestors }) {
  const d = [
    ["default-src", "'none'"],
    ["script-src", ["'self'", ...scriptHashes].join(" ")],
    // xterm.js and Radix set inline styles (production needs the same); script-src has no 'unsafe-inline'/'unsafe-eval'.
    ["style-src", "'self' 'unsafe-inline'"],
    ["img-src", "'self' data: blob:"],
    ["font-src", "'self'"],
    // No fetch, XHR, WebSocket, EventSource or beacon to any origin, same-origin included: the demo needs none.
    ["connect-src", "'none'"],
    ["worker-src", "'self' blob:"],
    ["manifest-src", "'self'"],
    ["base-uri", "'none'"],
    ["form-action", "'none'"],
    ["object-src", "'none'"],
  ];
  if (frameAncestors) d.push(["frame-ancestors", "'none'"]);
  return d;
}

export function cspString(scriptHashes, opts) {
  return cspDirectives(scriptHashes, opts)
    .map(([k, v]) => `${k} ${v}`)
    .join("; ");
}

/** The `<meta http-equiv>` tag injected into index.html. */
export function cspMetaTag(scriptHashes) {
  return `<meta http-equiv="Content-Security-Policy" content="${cspString(scriptHashes, { frameAncestors: false })}" />`;
}

/**
 * The Cloudflare Workers static-assets / Pages `_headers` file.
 *
 * `Cache-Control: public, no-transform` is on every response that is not a build asset, i.e. the HTML (`/`, `/index.html` and the
 * single-page-application fallback, which answers any unknown path with index.html and can only be matched by `/*`). Cloudflare Web
 * Analytics' automatic setup (enabled zone-wide on spacingmind.com) injects a beacon <script> into proxied HTML, which this page's
 * CSP blocks (two console errors); its docs say the beacon is not injected when the response has `Cache-Control: public,
 * no-transform` ("Cloudflare proxy will not be able to modify the original payload of the website. Therefore, the Beacon script will
 * not be automatically injected", developers.cloudflare.com/web-analytics/get-started/, "Sites proxied through Cloudflare"). The value
 * has no max-age, so the HTML has no freshness lifetime and is revalidated by its ETag on every load. `/assets/*` drops that value
 * (`! Cache-Control`) and keeps a year of immutable caching.
 */
export function headersFile(scriptHashes) {
  return `/*
  Content-Security-Policy: ${cspString(scriptHashes, { frameAncestors: true })}
  X-Content-Type-Options: nosniff
  Referrer-Policy: no-referrer
  Permissions-Policy: camera=(), microphone=(), geolocation=(), clipboard-read=()
  Cross-Origin-Opener-Policy: same-origin
  Cache-Control: public, no-transform

/assets/*
  ! Cache-Control
  Cache-Control: public, max-age=31536000, immutable
`;
}

/** Pulls the content of the CSP meta tag out of html, or null. */
export function readMetaCsp(html) {
  const m = /<meta\s+http-equiv="Content-Security-Policy"\s+content="([^"]*)"/i.exec(html);
  return m ? m[1] : null;
}
