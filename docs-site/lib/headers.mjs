// The docs site's security headers, as a Cloudflare Workers static-assets `_headers` file (written into the build output by
// .vitepress/config.mts, checked by scripts/check-dist.mjs, tested in scripts/docs-site.test.mjs).
//
// VitePress emits exactly three executable inline scripts, the same on every page (the appearance bootstrap, the "mac" class and the
// page-hash map); the JSON-LD blocks are data, not script. So the CSP can be strict: `script-src 'self'` plus the SHA-256 of each inline
// script found in the built pages, computed at build time. If VitePress ever changes one, the build computes the new hash; if a page
// gains an inline script its hash is added too; the dist check fails when a page has an inline script the policy does not list.
import { createHash } from 'node:crypto'

/** Inline `<script>` elements that the browser would execute: no `src`, and a classic or module type (not ld+json or other data). */
const INLINE_SCRIPT = /<script(?![^>]*\ssrc=)([^>]*)>([\s\S]*?)<\/script>/gi

/** `'sha256-<base64>'` source expressions for every executable inline script in one HTML document. */
export function inlineScriptHashes(html) {
  const out = []
  for (const m of html.matchAll(INLINE_SCRIPT)) {
    const type = /\stype="([^"]*)"/i.exec(m[1])?.[1]
    if (type !== undefined && type !== 'module' && !/javascript/i.test(type)) continue
    out.push(`'sha256-${createHash('sha256').update(m[2], 'utf8').digest('base64')}'`)
  }
  return out
}

/** The union of the inline-script hashes of several documents, sorted so the output is stable. */
export function allInlineScriptHashes(htmlDocuments) {
  return [...new Set(htmlDocuments.flatMap(inlineScriptHashes))].sort()
}

export function docsCsp(scriptHashes) {
  return [
    ["default-src", "'self'"],
    ["script-src", ["'self'", ...scriptHashes].join(' ')],
    // VitePress and Vue set inline styles (the --vp-vh variable, positioning); scripts have no 'unsafe-inline' and no 'unsafe-eval'.
    ["style-src", "'self' 'unsafe-inline'"],
    ["img-src", "'self' data:"],
    ["font-src", "'self'"],
    // The local search index and page chunks are fetched from the site itself.
    ["connect-src", "'self'"],
    ["worker-src", "'self' blob:"],
    ["manifest-src", "'self'"],
    ["base-uri", "'self'"],
    ["form-action", "'none'"],
    ["object-src", "'none'"],
    ["frame-ancestors", "'none'"],
  ]
    .map(([k, v]) => `${k} ${v}`)
    .join('; ')
}

/** The `_headers` file: security headers on everything, a year of immutable caching for the hashed build assets. */
export function docsHeadersFile(scriptHashes) {
  return `/*
  Content-Security-Policy: ${docsCsp(scriptHashes)}
  X-Content-Type-Options: nosniff
  Referrer-Policy: strict-origin-when-cross-origin
  X-Frame-Options: DENY
  Permissions-Policy: camera=(), microphone=(), geolocation=(), payment=(), usb=()

/assets/*
  Cache-Control: public, max-age=31536000, immutable
`
}
