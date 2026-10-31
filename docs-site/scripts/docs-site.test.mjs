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
  buildLlmsFullTxt,
  buildLlmsTxt,
  buildRobotsTxt,
  extractFaq,
  jsonLd,
  parseFrontMatter,
  readPages,
  rawMarkdown,
  rewriteLinks,
  version,
} from '../lib/pages.mjs'

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

test('workflow shape', () => {
  const wf = fs.readFileSync(path.join(REPO_ROOT, '.github', 'workflows', 'docs-pages.yml'), 'utf8')
  assert.match(wf, /pages: write/)
  assert.match(wf, /id-token: write/)
  assert.match(wf, /actions\/upload-pages-artifact@/)
  assert.match(wf, /actions\/deploy-pages@/)
  assert.match(wf, /public-docs\/\*\*/)
  assert.match(wf, /release:\n\s+types: \[published\]/)
  // deploy never runs for a pull request
  assert.match(wf, /if: github\.event_name != 'pull_request'/)
})

test('parseFrontMatter handles quotes', () => {
  assert.equal(parseFrontMatter('---\ntitle: "A: b"\n---\nx').fm.title, 'A: b')
})
