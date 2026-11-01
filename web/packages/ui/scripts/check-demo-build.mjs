// Build guard of the demo bundle (ADR-0051 sections 6, 7 and 10): no way to reach a daemon, a CSP that forbids all network,
// and the size budget. Run by `bun run build:demo` / `check:demo`; the budget numbers below are the decision, so changing
// one is a visible diff.
//
//   node scripts/check-demo-build.mjs [dist-dir]                         (default dist-demo; absolute budgets)
//   node scripts/check-demo-build.mjs [dist-dir] --baseline <prod-dist>  (initial JS/CSS budgets relative to a production build)
//   node scripts/check-demo-build.mjs --production <dist>                (G-05: a non-demo build contains no demo code)
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";

import { inlineScriptHashes, readMetaCsp } from "./demo-csp.mjs";

/**
 * The budget (bytes, gzip level 9). The two initial figures are absolute fallbacks (the production entry chunk was 1,067,437 B gzip on
 * origin/develop 5e09caf1); CI passes `--baseline` and gets `RELATIVE` instead, so an unrelated PR that grows the web entry
 * chunk cannot turn the demo check red: only the demo's own additions count.
 */
export const BUDGET = {
  /** The lazily imported chunk that holds the scripted daemon (dispatcher, scripts, scenario). */
  demoChunkGzip: 60 * 1024,
  /** Entry chunk plus anything index.html preloads. */
  initialJsGzip: 1_150_000,
  initialCssGzip: 35 * 1024,
  /** Any other single JS chunk. */
  singleChunkGzip: 30 * 1024,
};

/**
 * Allowed growth of the demo over a production build built in the same job. Measured demo overhead: +4.4 KB JS (banner, flag
 * guards, entry glue) and +0.2 KB CSS; the scripted daemon is a separate lazy chunk with its own budget. 32 KB JS is about 7x the
 * measured overhead: it goes red when the demo's entry code really grows, not for noise.
 */
export const RELATIVE = { initialJsGzip: 32 * 1024, initialCssGzip: 8 * 1024 };

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
/**
 * Strings that exist only if a module under src/demo/ (or its stylesheet) is in the output: the banner, the scripted daemon, the
 * landing route of the tour, the banner's layout variable. A stray static `@/demo/*` import that tree-shaking did not remove
 * shows up as one of them.
 */
const DEMO_ONLY_MARKERS = [
  { name: "the demo banner text", text: "Demo — not connected" },
  { name: "the scripted daemon (demo_unsupported)", text: DEMO_CHUNK_MARKER },
  { name: "the tour's landing route (src/demo/land)", text: "#/workspace/1/task/1/chat/1" },
  { name: "the banner layout variable (src/demo/demo.css)", text: "--demo-banner-h" },
];

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

/** gzip size of what index.html loads up front: [entry + modulepreloads] JS and the stylesheets. */
export function initialSizes(dir) {
  const html = readFileSync(path.join(dir, "index.html"), "utf8");
  const entry = /<script[^>]*type="module"[^>]*src="(?:\.\/)?([^"]+)"/.exec(html)?.[1];
  const preloads = [...html.matchAll(/<link[^>]*rel="modulepreload"[^>]*href="(?:\.\/)?([^"]+)"/g)].map((m) => m[1]);
  const css = [...html.matchAll(/<link[^>]*rel="stylesheet"[^>]*href="(?:\.\/)?([^"]+)"/g)].map((m) => m[1]);
  const size = (p) => gz(readFileSync(path.join(dir, p)));
  const js = [entry, ...preloads].filter(Boolean);
  return { jsFiles: js, jsGzip: js.reduce((n, p) => n + size(p), 0), cssGzip: css.reduce((n, p) => n + size(p), 0) };
}

/**
 * Checks a built demo directory. Returns `{ errors, metrics }`; errors is empty when the build passes.
 * `budget` and `forbidden` are injectable for tests.
 */
export function checkDemoBuild(dir, { budget = BUDGET, forbidden = FORBIDDEN, baseline = null, relative = RELATIVE } = {}) {
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
    // Cloudflare Web Analytics' auto-install injects a beacon <script> into HTML that its CSP blocks; it skips a response marked
    // `Cache-Control: public, no-transform`. Every non-asset response (the HTML, the SPA fallback) must carry it, and /assets/*
    // must detach it and keep its immutable caching.
    const blocks = Object.fromEntries(headers.split(/\n\s*\n/).map((b) => [b.split("\n")[0].trim(), b]));
    if (!/^\s+Cache-Control: public, no-transform$/m.test(blocks["/*"] ?? "")) errors.push("_headers: the /* rule must set Cache-Control: public, no-transform (keeps Cloudflare's beacon out of the HTML)");
    const assets = blocks["/assets/*"] ?? "";
    if (!/^\s+! Cache-Control$/m.test(assets) || !/^\s+Cache-Control: public, max-age=31536000, immutable$/m.test(assets)) {
      errors.push("_headers: /assets/* must detach Cache-Control and set it to public, max-age=31536000, immutable");
    }
  }

  // 3. size budget
  const initial = initialSizes(dir);
  const initialJs = initial.jsFiles;
  metrics.initialJsGzip = initial.jsGzip;
  metrics.initialCssGzip = initial.cssGzip;
  let jsLimit = budget.initialJsGzip;
  let cssLimit = budget.initialCssGzip;
  if (baseline) {
    const base = initialSizes(baseline);
    metrics.baselineJsGzip = base.jsGzip;
    metrics.baselineCssGzip = base.cssGzip;
    jsLimit = base.jsGzip + relative.initialJsGzip;
    cssLimit = base.cssGzip + relative.initialCssGzip;
  }
  metrics.initialJsLimit = jsLimit;
  metrics.initialCssLimit = cssLimit;
  if (metrics.initialJsGzip > jsLimit) errors.push(`initial JS is ${metrics.initialJsGzip} B gzip, over the budget of ${jsLimit}${baseline ? " (production entry + " + relative.initialJsGzip + ")" : ""}`);
  if (metrics.initialCssGzip > cssLimit) errors.push(`initial CSS is ${metrics.initialCssGzip} B gzip, over the budget of ${cssLimit}${baseline ? " (production CSS + " + relative.initialCssGzip + ")" : ""}`);

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
    if (![".js", ".html", ".css"].includes(path.extname(f))) continue;
    const text = readFileSync(f, "utf8");
    for (const { name, text: needle } of DEMO_ONLY_MARKERS) {
      if (text.includes(needle)) errors.push(`${path.relative(dir, f)} contains ${name}`);
    }
  }
  return { errors };
}

function main(argv) {
  const prod = argv[0] === "--production";
  const bi = argv.indexOf("--baseline");
  const baseline = bi >= 0 ? path.resolve(argv[bi + 1]) : null;
  const positional = argv.filter((a, i) => !a.startsWith("--") && i !== bi + 1);
  const dir = path.resolve(positional[0] ?? "dist-demo");
  const { errors, metrics } = prod ? checkProductionBuild(dir) : checkDemoBuild(dir, { baseline });
  if (metrics) {
    console.log(`demo build: initial JS ${metrics.initialJsGzip} B gzip (limit ${metrics.initialJsLimit}), initial CSS ${metrics.initialCssGzip} B (limit ${metrics.initialCssLimit}), demo code ${metrics.demoChunkGzip} B`);
    if (baseline) console.log(`production baseline: JS ${metrics.baselineJsGzip} B gzip, CSS ${metrics.baselineCssGzip} B`);
  }
  if (errors.length > 0) {
    for (const e of errors) console.error(`check-demo-build: ${e}`);
    process.exit(1);
  }
  console.log(prod ? "check-demo-build: the build contains no demo code" : "check-demo-build: ok");
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main(process.argv.slice(2));
