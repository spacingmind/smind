// Build guard of the demo bundle (ADR-0051 sections 6, 7 and 10): no way to reach a daemon, a CSP that forbids all network,
// and the size budget. Run by `bun run build:demo` / `check:demo`; the budget numbers below are the decision, so changing
// one is a visible diff.
//
//   node scripts/check-demo-build.mjs [dist-dir]            (default dist-demo)
//   node scripts/check-demo-build.mjs --production <dist>   (G-05: a non-demo build contains no demo code)
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";

import { inlineScriptHashes, readMetaCsp } from "./demo-csp.mjs";

/** The budget (bytes, gzip level 9). Baseline: the production entry chunk was 1,067,437 B gzip on origin/develop 5e09caf1. */
export const BUDGET = {
  /** The lazily imported chunk that holds the scripted daemon (dispatcher, scripts, scenario). */
  demoChunkGzip: 60 * 1024,
  /** Entry chunk plus anything index.html preloads. */
  initialJsGzip: 1_150_000,
  initialCssGzip: 35 * 1024,
  /** Any other single JS chunk. */
  singleChunkGzip: 30 * 1024,
};

/** What no demo bundle may contain: a path that could reach a daemon, a relay or the local machine. */
export const FORBIDDEN = [
  { name: "/api/token", re: /\/api\/token/ },
  { name: "/api/login", re: /\/api\/login/ },
  { name: "new WebSocket(", re: /new WebSocket\(/ },
  { name: "ws:// or wss:// URL", re: /wss?:\/\// },
  // A URL to the local machine. The two placeholders below are text in settings forms the demo refuses, not a connection.
  { name: "http(s):// URL to localhost / 127.0.0.1", re: /https?:\/\/(?:localhost|127\.0\.0\.1|\[::1\])(?!:54545\/callback|:8080)/ },
  { name: "relay.spacingmind", re: /relay\.spacingmind/ },
  { name: "smind.bearer", re: /smind\.bearer/ },
  { name: "grpc", re: /grpc/i },
  { name: "/healthz", re: /\/healthz/ },
  { name: "EventSource(", re: /new EventSource\(/ },
  { name: "sendBeacon(", re: /sendBeacon\(/ },
];

/** The marker only the scripted daemon contains (its refusal code), used to find its chunk. */
const DEMO_CHUNK_MARKER = "demo_unsupported";
const BANNER_TEXT = "Demo — not connected";

const TEXT_EXT = new Set([".js", ".mjs", ".css", ".html", ".json", ".map"]);

function listFiles(dir) {
  const out = [];
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) out.push(...listFiles(p));
    else out.push(p);
  }
  return out;
}

const gz = (buf) => gzipSync(buf, { level: 9 }).length;

/**
 * Checks a built demo directory. Returns `{ errors, metrics }`; errors is empty when the build passes.
 * `budget` and `forbidden` are injectable for tests.
 */
export function checkDemoBuild(dir, { budget = BUDGET, forbidden = FORBIDDEN } = {}) {
  const errors = [];
  const metrics = {};
  if (!existsSync(path.join(dir, "index.html"))) return { errors: [`${dir}/index.html is missing: build the demo first`], metrics };

  const files = listFiles(dir);
  const rel = (f) => path.relative(dir, f).split(path.sep).join("/");

  // 1. forbidden strings, in every text file
  for (const f of files) {
    if (!TEXT_EXT.has(path.extname(f))) continue;
    const text = readFileSync(f, "utf8");
    for (const { name, re } of forbidden) {
      if (re.test(text)) errors.push(`${rel(f)} contains ${name}`);
    }
  }
  if (existsSync(path.join(dir, "pdf-viewer.html"))) errors.push("pdf-viewer.html must not ship in the demo (no file preview)");

  // 2. CSP: meta and headers, with the hash of the inline bootstrap script
  const html = readFileSync(path.join(dir, "index.html"), "utf8");
  const meta = readMetaCsp(html);
  if (!meta) errors.push("index.html has no Content-Security-Policy <meta>");
  else {
    if (!/(^|;\s*)connect-src 'none'(;|$)/.test(meta)) errors.push("CSP must contain connect-src 'none'");
    if (!/(^|;\s*)default-src 'none'(;|$)/.test(meta)) errors.push("CSP must contain default-src 'none'");
    const script = /(?:^|;\s*)script-src ([^;]*)/.exec(meta)?.[1] ?? "";
    if (/'unsafe-eval'|'unsafe-inline'/.test(script)) errors.push("CSP script-src must not allow 'unsafe-eval' or 'unsafe-inline'");
    for (const h of inlineScriptHashes(html)) if (!script.includes(h)) errors.push(`CSP script-src is missing the hash of the inline script (${h})`);
  }
  const headersPath = path.join(dir, "_headers");
  if (!existsSync(headersPath)) errors.push("_headers is missing");
  else {
    const headers = readFileSync(headersPath, "utf8");
    for (const need of ["Content-Security-Policy:", "frame-ancestors 'none'", "connect-src 'none'", "X-Content-Type-Options: nosniff", "Referrer-Policy:", "Permissions-Policy:"]) {
      if (!headers.includes(need)) errors.push(`_headers is missing ${need}`);
    }
  }

  // 3. size budget
  const entrySrc = /<script[^>]*type="module"[^>]*src="\.?\/?([^"]+)"/.exec(html)?.[1];
  const preloads = [...html.matchAll(/<link[^>]*rel="modulepreload"[^>]*href="\.?\/?([^"]+)"/g)].map((m) => m[1]);
  const cssRefs = [...html.matchAll(/<link[^>]*rel="stylesheet"[^>]*href="\.?\/?([^"]+)"/g)].map((m) => m[1]);
  const size = (relPath) => gz(readFileSync(path.join(dir, relPath)));
  const initialJs = [entrySrc, ...preloads].filter(Boolean);
  metrics.initialJsGzip = initialJs.reduce((n, p) => n + size(p), 0);
  metrics.initialCssGzip = cssRefs.reduce((n, p) => n + size(p), 0);
  if (metrics.initialJsGzip > budget.initialJsGzip) errors.push(`initial JS is ${metrics.initialJsGzip} B gzip, over the budget of ${budget.initialJsGzip}`);
  if (metrics.initialCssGzip > budget.initialCssGzip) errors.push(`initial CSS is ${metrics.initialCssGzip} B gzip, over the budget of ${budget.initialCssGzip}`);

  const jsFiles = files.filter((f) => path.extname(f) === ".js");
  const demoChunks = jsFiles.filter((f) => readFileSync(f, "utf8").includes(DEMO_CHUNK_MARKER));
  if (demoChunks.length === 0) errors.push(`no chunk contains the scripted daemon (${DEMO_CHUNK_MARKER}): is this a demo build?`);
  if (demoChunks.length > 0 && demoChunks.every((f) => initialJs.includes(rel(f)))) errors.push("the scripted daemon is in the entry chunk: it must be a lazily imported chunk");
  metrics.demoChunkGzip = demoChunks.reduce((n, f) => n + gz(readFileSync(f)), 0);
  if (metrics.demoChunkGzip > budget.demoChunkGzip) errors.push(`demo code is ${metrics.demoChunkGzip} B gzip, over the budget of ${budget.demoChunkGzip}`);
  for (const f of jsFiles) {
    if (initialJs.includes(rel(f))) continue;
    const n = gz(readFileSync(f));
    if (n > budget.singleChunkGzip && !demoChunks.includes(f)) errors.push(`${rel(f)} is ${n} B gzip, over the single-chunk budget of ${budget.singleChunkGzip}`);
  }

  return { errors, metrics };
}

/** G-05: a build that is not the demo contains no demo code. */
export function checkProductionBuild(dir) {
  const errors = [];
  if (!existsSync(dir) || !statSync(dir).isDirectory()) return { errors: [`${dir} is not a directory`] };
  for (const f of listFiles(dir)) {
    if (path.extname(f) !== ".js" && path.extname(f) !== ".html") continue;
    const text = readFileSync(f, "utf8");
    if (text.includes(BANNER_TEXT)) errors.push(`${path.relative(dir, f)} contains the demo banner text`);
    if (text.includes(DEMO_CHUNK_MARKER)) errors.push(`${path.relative(dir, f)} contains the scripted daemon (${DEMO_CHUNK_MARKER})`);
  }
  return { errors };
}

function main(argv) {
  const prod = argv[0] === "--production";
  const dir = path.resolve(prod ? argv[1] : (argv[0] ?? "dist-demo"));
  const { errors, metrics } = prod ? checkProductionBuild(dir) : checkDemoBuild(dir);
  if (metrics) console.log(`demo build: initial JS ${metrics.initialJsGzip ?? "?"} B gzip, initial CSS ${metrics.initialCssGzip ?? "?"} B, demo code ${metrics.demoChunkGzip ?? "?"} B`);
  if (errors.length > 0) {
    for (const e of errors) console.error(`check-demo-build: ${e}`);
    process.exit(1);
  }
  console.log(prod ? "check-demo-build: the build contains no demo code" : "check-demo-build: ok");
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main(process.argv.slice(2));
