// Post-build check: VitePress logs an SSR error and still writes an empty page,
// so assert every page really has its content. Usage: node scripts/check-dist.mjs [dist]
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { inlineScriptHashes } from '../lib/headers.mjs'
import { OG_IMAGE_HEIGHT, OG_IMAGE_URL, OG_IMAGE_WIDTH, SITE_URL, SITE_NAME, pageTitle, pageUrl, readPages } from '../lib/pages.mjs'

const dist = path.resolve(process.argv[2] ?? path.join(path.dirname(fileURLToPath(import.meta.url)), '..', '.vitepress', 'dist'))
const problems = []
const need = (ok, msg) => ok || problems.push(msg)

for (const p of readPages()) {
  const html = fs.readFileSync(path.join(dist, p.slug === 'index' ? 'index.html' : `${p.slug}.html`), 'utf8')
  const w = p.slug
  need((html.match(/<h1[ >]/g) ?? []).length === 1, `${w}: want exactly one <h1>`)
  need(html.includes(`<title>${pageTitle(p)}</title>`), `${w}: <title>`)
  need(html.includes(`rel="canonical" href="${pageUrl(w)}"`), `${w}: canonical`)
  need(html.includes('name="description"'), `${w}: meta description`)
  need(html.includes(`property="og:image" content="${OG_IMAGE_URL}"`), `${w}: og:image`)
  need(html.includes(`name="twitter:image" content="${OG_IMAGE_URL}"`), `${w}: twitter:image`)
  need(
    html.includes(`property="og:image:width" content="${OG_IMAGE_WIDTH}"`) && html.includes(`property="og:image:height" content="${OG_IMAGE_HEIGHT}"`),
    `${w}: og:image dimensions`,
  )
  need(html.includes('name="twitter:card" content="summary_large_image"'), `${w}: twitter:card summary_large_image`)
  need(html.includes(`"image":"${OG_IMAGE_URL}"`), `${w}: JSON-LD image`)
  need(html.includes('property="og:title"') && html.includes('name="twitter:card"'), `${w}: og/twitter`)
  need(html.includes('class="page-summary"'), `${w}: summary paragraph`)
  need(html.includes('"@type":"TechArticle"') && html.includes('"@type":"BreadcrumbList"'), `${w}: JSON-LD`)
  need(html.includes('"@context":"https://schema.org","@type":"SoftwareApplication"') === (w === 'index'), `${w}: SoftwareApplication only on home`)
  need(/<main[\s\S]*<p/.test(html) && html.length > 12000, `${w}: body missing (SSR error?)`)
  need(!html.includes('&lt;!--'), `${w}: HTML comment shown as text`)
  need(fs.existsSync(path.join(dist, `${w}.md`)), `${w}: raw .md copy`)
}
for (const f of ['llms.txt', 'llms-full.txt', 'robots.txt', 'sitemap.xml', '404.html', '_headers']) need(fs.existsSync(path.join(dist, f)), `missing ${f}`)
need(fs.existsSync(path.join(dist, 'og-card.png')), 'missing og-card.png')
// The site is served by a Cloudflare Worker (docs-site/wrangler.docs.jsonc), not GitHub Pages: no Pages leftovers.
for (const f of ['CNAME', '.nojekyll']) need(!fs.existsSync(path.join(dist, f)), `${f} is a GitHub Pages file and must not be built`)
// Security headers: the CSP must list the hash of every executable inline script of every built page, and stay strict.
if (fs.existsSync(path.join(dist, '_headers'))) {
  const headers = fs.readFileSync(path.join(dist, '_headers'), 'utf8')
  for (const h of ['X-Content-Type-Options: nosniff', 'Referrer-Policy: strict-origin-when-cross-origin', 'X-Frame-Options: DENY', 'Permissions-Policy:', 'frame-ancestors \'none\'', 'Cache-Control: public, max-age=31536000, immutable']) {
    need(headers.includes(h), `_headers: missing ${h}`)
  }
  const scriptSrc = /script-src ([^;\n]*)/.exec(headers)?.[1] ?? ''
  need(!/'unsafe-eval'|'unsafe-inline'/.test(scriptSrc), "_headers: script-src must not allow 'unsafe-eval' or 'unsafe-inline'")
  for (const f of fs.readdirSync(dist).filter((n) => n.endsWith('.html'))) {
    for (const hash of inlineScriptHashes(fs.readFileSync(path.join(dist, f), 'utf8'))) need(scriptSrc.includes(hash), `${f}: an inline script is not in the CSP (${hash})`)
  }
}
need(fs.readFileSync(path.join(dist, 'sitemap.xml'), 'utf8').includes(`${SITE_URL}/getting-started`), 'sitemap lists pages')
if (problems.length) {
  console.error(problems.join('\n'))
  process.exit(1)
}
console.log('docs-site dist ok')
