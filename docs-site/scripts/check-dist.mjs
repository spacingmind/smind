// Post-build check: VitePress logs an SSR error and still writes an empty page,
// so assert every page really has its content. Usage: node scripts/check-dist.mjs [dist]
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { SITE_URL, SITE_NAME, pageTitle, pageUrl, readPages } from '../lib/pages.mjs'

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
  need(html.includes('property="og:title"') && html.includes('name="twitter:card"'), `${w}: og/twitter`)
  need(html.includes('class="page-summary"'), `${w}: summary paragraph`)
  need(html.includes('"@type":"TechArticle"') && html.includes('"@type":"BreadcrumbList"'), `${w}: JSON-LD`)
  need(html.includes('"@context":"https://schema.org","@type":"SoftwareApplication"') === (w === 'index'), `${w}: SoftwareApplication only on home`)
  need(/<main[\s\S]*<p/.test(html) && html.length > 12000, `${w}: body missing (SSR error?)`)
  need(fs.existsSync(path.join(dist, `${w}.md`)), `${w}: raw .md copy`)
}
for (const f of ['llms.txt', 'llms-full.txt', 'robots.txt', 'sitemap.xml', 'CNAME']) need(fs.existsSync(path.join(dist, f)), `missing ${f}`)
need(fs.readFileSync(path.join(dist, 'CNAME'), 'utf8').trim() === 'docs.spacingmind.com', 'CNAME content')
need(fs.readFileSync(path.join(dist, 'sitemap.xml'), 'utf8').includes(`${SITE_URL}/getting-started`), 'sitemap lists pages')
if (problems.length) {
  console.error(problems.join('\n'))
  process.exit(1)
}
console.log('docs-site dist ok')
