import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import test from 'node:test'
import {
  AI_CRAWLERS,
  OG_IMAGE_HEIGHT,
  OG_IMAGE_URL,
  OG_IMAGE_WIDTH,
  REPO_ROOT,
  SIDEBAR_GROUPS,
  buildLlmsFullTxt,
  buildLlmsTxt,
  buildRobotsTxt,
  extractFaq,
  jsonLd,
  parseFrontMatter,
  readPages,
  rawMarkdown,
  rewriteLinks,
  sidebarGroups,
  version,
} from '../lib/pages.mjs'
import { allInlineScriptHashes, docsCsp, docsHeadersFile, inlineScriptHashes } from '../lib/headers.mjs'

const pages = readPages()

test('front matter is complete', () => {
  assert.ok(pages.length >= 16, `expected the public-docs pages, got ${pages.length}`)
  const orders = new Set()
  for (const p of pages) {
    for (const k of ['title', 'description', 'summary']) assert.ok(p.fm[k]?.trim(), `${p.file}: front matter ${k} is missing`)
    assert.ok(Number.isInteger(p.order) && p.order > 0, `${p.file}: order must be a positive integer`)
    assert.ok(!orders.has(p.order), `${p.file}: duplicate order ${p.order}`)
    orders.add(p.order)
    // A bare ": " in a plain YAML scalar breaks VitePress's parser; quote the value.
    const raw = fs.readFileSync(path.join(REPO_ROOT, 'public-docs', p.file), 'utf8').split('\n---')[0]
    for (const line of raw.split('\n').slice(1)) {
      const v = line.replace(/^[\w-]+:\s*/, '')
      assert.ok(/^["']/.test(v) || !v.includes(': '), `${p.file}: quote this value: ${line}`)
    }
    assert.ok(p.fm.description.length <= 300, `${p.file}: description too long for a meta description`)
  }
})

test('llms.txt lists every page', () => {
  const txt = buildLlmsTxt(pages)
  for (const p of pages) {
    assert.ok(txt.includes(`.md): ${p.fm.description}`), `${p.file}: description missing from llms.txt`)
    assert.ok(txt.includes(`(https://docs.spacingmind.com${p.slug === 'index' ? '/index' : '/' + p.slug}.md)`), `${p.file}: link missing from llms.txt`)
  }
})

test('llms.txt order and shape', () => {
  const lines = buildLlmsTxt(pages).split('\n')
  assert.equal(lines[0], '# sMind by Spacing Mind')
  assert.ok(lines[2].startsWith('> '))
  const links = lines.filter((l) => l.startsWith('- ['))
  assert.deepEqual(links.map((l) => /\[(.*?)\]/.exec(l)[1]), pages.map((p) => p.fm.title))
})

test('llms-full.txt contains every page body', () => {
  const full = buildLlmsFullTxt(pages)
  for (const p of pages) {
    const h1 = /^# .+$/m.exec(p.body)[0]
    assert.equal(full.split(`\n${h1}\n`).length - 1, 1, `${p.file}: "${h1}" should appear once`)
    assert.ok(full.includes(p.fm.summary), `${p.file}: summary missing`)
  }
})

test('docs links are rewritten', () => {
  assert.equal(
    rewriteLinks('[x](../docs/decisions/0042-plugin-system.md) [y](providers.md)'),
    '[x](https://github.com/spacingmind/smind/blob/develop/docs/decisions/0042-plugin-system.md) [y](providers.md)',
  )
  for (const p of pages) assert.ok(!rawMarkdown(p).includes('](../docs/'), `${p.file}: raw copy still links ../docs`)
})

test('jsonld', () => {
  for (const p of pages) {
    const types = jsonLd(p).map((g) => g['@type'])
    assert.ok(types.includes('TechArticle') && types.includes('BreadcrumbList'), p.file)
    assert.equal(types.includes('SoftwareApplication'), p.slug === 'index', `${p.file}: SoftwareApplication only on the home page`)
  }
  const faq = { slug: 'x', fm: { title: 'X', description: 'd', summary: 's' }, body: '# X\n\n## Does it work?\n\nYes, it does.\n\n## Other\n\ntext\n' }
  assert.deepEqual(extractFaq(faq.body), [{ q: 'Does it work?', a: 'Yes, it does.' }])
  assert.ok(jsonLd(faq).some((g) => g['@type'] === 'FAQPage'))
})

test('og card', () => {
  const png = fs.readFileSync(path.join(REPO_ROOT, 'docs-site', 'assets', 'og-card.png'))
  assert.ok(png.length < 200000, `og-card.png too large: ${png.length} bytes`)
  assert.ok(png.subarray(0, 8).equals(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])), 'og-card.png is not a PNG')
  assert.equal(png.readUInt32BE(16), OG_IMAGE_WIDTH, 'PNG IHDR width')
  assert.equal(png.readUInt32BE(20), OG_IMAGE_HEIGHT, 'PNG IHDR height')
  assert.equal(OG_IMAGE_WIDTH, 1200)
  assert.equal(OG_IMAGE_HEIGHT, 630)
  assert.ok(OG_IMAGE_URL.startsWith('https://docs.spacingmind.com/'), OG_IMAGE_URL)
  assert.ok(OG_IMAGE_URL.endsWith('/og-card.png'), OG_IMAGE_URL)
  for (const p of pages) {
    const graph = jsonLd(p)
    const article = graph.find((g) => g['@type'] === 'TechArticle')
    assert.equal(article.image, OG_IMAGE_URL, `${p.file}: TechArticle image`)
    const app = graph.find((g) => g['@type'] === 'SoftwareApplication')
    if (app) assert.equal(app.image, OG_IMAGE_URL, `${p.file}: SoftwareApplication image`)
  }
  const config = fs.readFileSync(path.join(REPO_ROOT, 'docs-site', '.vitepress', 'config.mts'), 'utf8')
  assert.ok(config.includes("'summary_large_image'"), 'config: summary_large_image')
  assert.ok(config.includes("'og:image'"), 'config: og:image')
  assert.ok(config.includes("'twitter:image'"), 'config: twitter:image')
  assert.ok(config.includes('og-card.png'), 'config: og-card.png copy')
})

test('code block language label is readable', () => {
  const css = fs.readFileSync(path.join(REPO_ROOT, 'docs-site', '.vitepress', 'theme', 'custom.css'), 'utf8')
  assert.match(css, /span\.lang\s*\{[^}]*--vp-c-text-2/)
})

test('syntax theme is high contrast', () => {
  const config = fs.readFileSync(path.join(REPO_ROOT, 'docs-site', '.vitepress', 'config.mts'), 'utf8')
  assert.ok(config.includes('github-light-high-contrast'), 'config: github-light-high-contrast')
})

test('version line from manifest', () => {
  const manifest = JSON.parse(fs.readFileSync(path.join(REPO_ROOT, '.release-please-manifest.json'), 'utf8'))
  assert.equal(version(), manifest['.'])
  assert.ok(rawMarkdown(pages[0]).includes(`version ${manifest['.']}`))
  assert.ok(!pages.some((p) => /This page describes the \d/.test(p.body)), 'no per-page hard-coded version line')
})

test('robots.txt allows the AI crawlers', () => {
  assert.equal(AI_CRAWLERS.length, 8)
  const robots = buildRobotsTxt()
  for (const a of AI_CRAWLERS) assert.match(robots, new RegExp(`User-agent: ${a}\\nAllow: /\\n`))
  assert.match(robots, /Sitemap: https:\/\/docs\.spacingmind\.com\/sitemap\.xml/)
})

test('workflow shape: Cloudflare Worker deploy, no GitHub Pages', () => {
  const dir = path.join(REPO_ROOT, '.github', 'workflows')
  assert.ok(!fs.existsSync(path.join(dir, 'docs-pages.yml')), 'docs-pages.yml was replaced by docs-deploy.yml')
  const wf = fs.readFileSync(path.join(dir, 'docs-deploy.yml'), 'utf8')
  // no GitHub Pages leftovers
  assert.doesNotMatch(wf, /pages: write|id-token|configure-pages|upload-pages-artifact|deploy-pages|github-pages/)
  assert.match(wf, /permissions:\n\s+contents: read/)
  assert.doesNotMatch(wf, /permissions:[^]*\n\s+(write|read)-all/)
  assert.match(wf, /timeout-minutes: 15/)
  assert.match(wf, /concurrency:\n\s+group: docs-deploy\n\s+cancel-in-progress: false/)
  // every action pinned by a full commit SHA
  const uses = [...wf.matchAll(/uses: (\S+)/g)].map((m) => m[1])
  assert.ok(uses.length >= 2)
  for (const u of uses) assert.match(u, /@[0-9a-f]{40}$/, `${u} is not pinned by SHA`)
  // triggers
  assert.match(wf, /public-docs\/\*\*/)
  assert.match(wf, /\.release-please-manifest\.json/)
  // A release deploys through release-please.yml (workflow_call): a `release: published` trigger would never fire for a release it
  // publishes with the default GITHUB_TOKEN. A push to develop and a pull request only build and check.
  assert.doesNotMatch(wf, /\n {2}release:/)
  assert.match(wf, /workflow_call:\n\s+inputs:\n\s+ref:[^]*?required: true/)
  assert.match(wf, /workflow_dispatch:\n\s+inputs:\n\s+ref:/)
  assert.match(wf, /pull_request:\n\s+paths:/)
  assert.match(wf, /push:\n\s+branches: \[develop\]/)
  // only a run that was given a ref (workflow_call, workflow_dispatch) deploys
  assert.match(wf, /id: mode[^]*?if \[ -n "\$REF_INPUT" \]/)
  // pinned wrangler, the Worker config
  assert.match(wf, /wrangler@\d+\.\d+\.\d+ deploy --config wrangler\.docs\.jsonc/)
  const deploy = wf.slice(wf.indexOf('- name: Deploy to Cloudflare'))
  assert.match(deploy, /if: steps\.mode\.outputs\.deploy == 'true' && steps\.secrets\.outputs\.present == 'true'/)
  // a called workflow sees its caller's event name, so the deploy decision must not depend on it
  assert.doesNotMatch(wf.replace(/^\s*#.*$/gm, ''), /github\.event_name/)
  // the secrets are mapped into the check step and the deploy step only, never the job or the build steps
  const withSecrets = wf.split('\n').filter((l) => /secrets\.CLOUDFLARE_/.test(l))
  assert.equal(withSecrets.length, 4, 'two secrets in two steps')
  assert.ok(wf.indexOf('secrets.CLOUDFLARE_API_TOKEN') > wf.indexOf('- name: Check built pages'), 'secrets appear only after the build and check steps')
  assert.match(wf, /::notice title=docs-deploy skipped the deploy::/)
})

test('Worker config: static assets only, the live Worker settings', () => {
  const raw = fs.readFileSync(path.join(REPO_ROOT, 'docs-site', 'wrangler.docs.jsonc'), 'utf8')
  const cfg = JSON.parse(raw.replace(/^\s*\/\/.*$/gm, ''))
  assert.equal(cfg.name, 'smind-docs')
  assert.equal(cfg.workers_dev, false)
  assert.equal(cfg.preview_urls, false)
  assert.equal(cfg.main, undefined, 'no Worker script')
  assert.deepEqual(cfg.routes, [{ pattern: 'docs.spacingmind.com', custom_domain: true }])
  assert.equal(cfg.assets.directory, './.vitepress/dist')
  assert.equal(cfg.assets.html_handling, 'auto-trailing-slash')
  assert.equal(cfg.assets.not_found_handling, '404-page')
})

test('the CSP lists the hash of every executable inline script, and only those', () => {
  const page = '<html><head><script>window.a=1</script><script type="module">import "/x"</script><script type="application/ld+json">{"@type":"X"}</script><script src="/app.js" type="module"></script></head></html>'
  const hashes = inlineScriptHashes(page)
  assert.equal(hashes.length, 2, 'the classic and the module inline script; not ld+json, not a script with src')
  for (const h of hashes) assert.match(h, /^'sha256-[A-Za-z0-9+/]{43}='$/)
  // the same script on two pages is one hash; output is sorted and stable
  const all = allInlineScriptHashes([page, page, '<script>window.b=2</script>'])
  assert.equal(all.length, 3)
  assert.deepEqual(all, [...all].sort())
  const csp = docsCsp(all)
  for (const h of all) assert.ok(csp.includes(h))
  const scriptSrc = /script-src ([^;]*)/.exec(csp)[1]
  assert.doesNotMatch(scriptSrc, /unsafe-eval|unsafe-inline/)
  for (const d of ["default-src 'self'", "frame-ancestors 'none'", "object-src 'none'", "base-uri 'self'", "form-action 'none'"]) assert.ok(csp.includes(d), d)
})

test('_headers: security headers everywhere, immutable caching only for the hashed assets', () => {
  const file = docsHeadersFile(["'sha256-AAAA'"])
  const [everything, assets] = file.trim().split('\n\n')
  assert.ok(everything.startsWith('/*\n'))
  for (const h of ['Content-Security-Policy:', 'X-Content-Type-Options: nosniff', 'Referrer-Policy: strict-origin-when-cross-origin', 'X-Frame-Options: DENY', 'Permissions-Policy:']) assert.ok(everything.includes(h), h)
  assert.ok(!everything.includes('Cache-Control'))
  assert.equal(assets, '/assets/*\n  Cache-Control: public, max-age=31536000, immutable')
  assert.ok(!file.includes('interest-cohort'), 'no obsolete Permissions-Policy feature (Chrome logs a warning)')
})

test('the built pages carry no GitHub Pages files', () => {
  const config = fs.readFileSync(path.join(REPO_ROOT, 'docs-site', '.vitepress', 'config.mts'), 'utf8')
  assert.doesNotMatch(config, /CNAME|\.nojekyll/)
  assert.match(config, /_headers/)
})

test('parseFrontMatter handles quotes', () => {
  assert.equal(parseFrontMatter('---\ntitle: "A: b"\n---\nx').fm.title, 'A: b')
})

test('every page is in exactly one sidebar section, and every listed slug exists', () => {
  const slugs = new Set(pages.map((p) => p.slug))
  const listed = SIDEBAR_GROUPS.flatMap((g) => g.slugs)
  assert.equal(new Set(listed).size, listed.length, 'a slug is listed in two sections')
  for (const s of listed) assert.ok(slugs.has(s), `SIDEBAR_GROUPS lists ${s}, but public-docs/${s}.md does not exist`)
  for (const p of pages) assert.ok(listed.includes(p.slug), `${p.file}: add it to SIDEBAR_GROUPS in docs-site/lib/pages.mjs`)
  assert.ok(!sidebarGroups(pages).some((g) => g.title === 'More'))
})

test('theme tokens are the product site tokens', () => {
  // Copied from spacingmind/spacing-mind-landing-page apps/web/src/index.css.
  const css = fs.readFileSync(path.join(REPO_ROOT, 'docs-site', '.vitepress', 'theme', 'custom.css'), 'utf8')
  for (const token of [
    '--background: #f8f8f8;',
    '--background: #111111;',
    '--brand: oklch(0.489 0.08 194.8);',
    '--brand: oklch(0.904 0.136 196.2);',
    '--card: #ffffff;',
    '--card: #1a1a1a;',
    '--radius: 0.5rem;',
    '--muted-foreground: oklch(0.5 0 0);',
    '--muted-foreground: oklch(0.65 0 0);',
  ]) {
    assert.ok(css.includes(token), `custom.css: ${token}`)
  }
  assert.match(css, /--vp-font-family-base:\s*'Geist Variable'/)
  const config = fs.readFileSync(path.join(REPO_ROOT, 'docs-site', '.vitepress', 'config.mts'), 'utf8')
  assert.ok(config.includes("appearance: 'dark'"), 'dark by default, like the product site')
})
