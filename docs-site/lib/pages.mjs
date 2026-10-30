// Shared by the VitePress config and the drift test. Node builtins only, so
// `node --test` needs no `npm install`.
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

export const SITE_URL = 'https://docs.spacingmind.com'
export const SITE_NAME = 'sMind by Spacing Mind'
export const SITE_SUMMARY =
  'sMind by Spacing Mind is a self-hosted platform for running coding agents: one Go binary with a web app, desktop app, command line and mobile access.'
export const REPO_URL = 'https://github.com/spacingmind/smind'
export const OG_IMAGE_URL = SITE_URL + '/og-card.png'
export const OG_IMAGE_WIDTH = 1200
export const OG_IMAGE_HEIGHT = 630
export const OG_IMAGE_ALT = 'sMind by Spacing Mind: run coding agents from one place'

export const AI_CRAWLERS = [
  'GPTBot',
  'OAI-SearchBot',
  'ChatGPT-User',
  'ClaudeBot',
  'Claude-User',
  'PerplexityBot',
  'Google-Extended',
  'Applebot-Extended',
]

const here = path.dirname(fileURLToPath(import.meta.url))
export const REPO_ROOT = path.resolve(here, '..', '..')
export const DOCS_DIR = path.join(REPO_ROOT, 'public-docs')

export function parseFrontMatter(text) {
  const m = /^---\n([\s\S]*?)\n---\n?/.exec(text)
  if (!m) return { fm: {}, body: text }
  const fm = {}
  for (const line of m[1].split('\n')) {
    const kv = /^([A-Za-z][\w-]*):\s*(.*)$/.exec(line)
    if (!kv) continue
    let v = kv[2].trim()
    if (/^".*"$/.test(v)) v = v.slice(1, -1).replace(/\\"/g, '"')
    else if (/^'.*'$/.test(v)) v = v.slice(1, -1).replace(/''/g, "'")
    fm[kv[1]] = v
  }
  return { fm, body: text.slice(m[0].length) }
}

export function version() {
  const m = JSON.parse(fs.readFileSync(path.join(REPO_ROOT, '.release-please-manifest.json'), 'utf8'))
  return m['.']
}

export function readPages(dir = DOCS_DIR) {
  return fs
    .readdirSync(dir)
    .filter((f) => f.endsWith('.md'))
    .map((f) => {
      const slug = f.slice(0, -3)
      const { fm, body } = parseFrontMatter(fs.readFileSync(path.join(dir, f), 'utf8'))
      return { slug, file: f, fm, body, order: Number(fm.order) }
    })
    .sort((a, b) => a.order - b.order)
}

export const pagePath = (slug) => (slug === 'index' ? '/' : `/${slug}`)
export const pageUrl = (slug) => SITE_URL + pagePath(slug)
// A title that already carries the entity name (the home page) gets no suffix.
export const pageTitle = (p) => (p.fm.title.includes(SITE_NAME) ? p.fm.title : `${p.fm.title} | ${SITE_NAME}`)

// Links into the internal docs/ tree do not exist on the site: point them at the repo.
export function rewriteLinks(md) {
  return md.replace(/\]\((?:\.\.\/)+docs\//g, `](${REPO_URL}/blob/develop/docs/`)
}

export function versionLine(v = version()) {
  return `${SITE_NAME}, version ${v}. This page describes the ${v} release.`
}

// The page as Markdown for crawlers: summary and version line under the H1.
export function rawMarkdown(p, v = version()) {
  const body = rewriteLinks(p.body)
  const lines = body.split('\n')
  const i = lines.findIndex((l) => /^# /.test(l))
  const add = [`> ${p.fm.summary}`, '', `*${versionLine(v)}*`, '']
  if (i < 0) return [`# ${p.fm.title}`, '', ...add, body].join('\n')
  lines.splice(i + 1, 0, '', ...add.slice(0, -1))
  return lines.join('\n')
}

export function buildLlmsTxt(pages) {
  const out = [`# ${SITE_NAME}`, '', `> ${SITE_SUMMARY}`, '', `Version ${version()}. Every page is also available as raw Markdown at its URL plus \`.md\`; the full documentation is in ${SITE_URL}/llms-full.txt.`, '', '## Documentation', '']
  for (const p of pages) out.push(`- [${p.fm.title}](${pageUrl(p.slug)}${p.slug === 'index' ? 'index' : ''}.md): ${p.fm.description}`)
  return out.join('\n') + '\n'
}

export function buildLlmsFullTxt(pages) {
  const v = version()
  const parts = [`# ${SITE_NAME} documentation\n\n> ${SITE_SUMMARY}\n\nVersion ${v}.\n`]
  for (const p of pages) parts.push(`<!-- source: ${pageUrl(p.slug)} -->\n\n${rawMarkdown(p, v).trim()}\n`)
  return parts.join('\n---\n\n')
}

export function buildRobotsTxt() {
  const groups = AI_CRAWLERS.map((a) => `User-agent: ${a}\nAllow: /\n`)
  return `User-agent: *\nAllow: /\n\n${groups.join('\n')}\nSitemap: ${SITE_URL}/sitemap.xml\n`
}

// Q&A: a heading ending in "?" followed by its answer text, up to the next heading.
export function extractFaq(body) {
  const out = []
  const lines = body.split('\n')
  let fence = false
  for (let i = 0; i < lines.length; i++) {
    if (/^\s*```/.test(lines[i])) fence = !fence
    const h = !fence && /^#{2,4}\s+(.+\?)\s*$/.exec(lines[i])
    if (!h) continue
    const ans = []
    for (let j = i + 1; j < lines.length && !/^#{1,6}\s/.test(lines[j]); j++) ans.push(lines[j])
    const text = ans.join('\n').replace(/```[\s\S]*?```/g, '').replace(/\s+/g, ' ').trim()
    if (text) out.push({ q: h[1], a: text })
  }
  return out
}

export function jsonLd(p, { lastUpdated, v = version() } = {}) {
  const url = pageUrl(p.slug)
  const publisher = { '@type': 'Organization', name: 'Spacing Mind', url: 'https://github.com/spacingmind' }
  const graph = []
  if (p.slug === 'index') {
    graph.push({
      '@context': 'https://schema.org',
      '@type': 'SoftwareApplication',
      name: SITE_NAME,
      alternateName: 'smind',
      applicationCategory: 'DeveloperApplication',
      operatingSystem: 'macOS, Linux, Windows',
      softwareVersion: v,
      description: SITE_SUMMARY,
      url: SITE_URL + '/',
      codeRepository: REPO_URL,
      image: OG_IMAGE_URL,
      author: publisher,
    })
  }
  const article = {
    '@context': 'https://schema.org',
    '@type': 'TechArticle',
    headline: p.fm.title,
    description: p.fm.description,
    abstract: p.fm.summary,
    url,
    mainEntityOfPage: url,
    inLanguage: 'en',
    image: OG_IMAGE_URL,
    author: publisher,
    publisher,
    about: { '@type': 'SoftwareApplication', name: SITE_NAME, softwareVersion: v },
  }
  if (lastUpdated) article.dateModified = new Date(lastUpdated).toISOString()
  graph.push(article)
  const crumbs = [{ name: SITE_NAME, url: SITE_URL + '/' }]
  if (p.slug !== 'index') crumbs.push({ name: p.fm.title, url })
  graph.push({
    '@context': 'https://schema.org',
    '@type': 'BreadcrumbList',
    itemListElement: crumbs.map((c, i) => ({ '@type': 'ListItem', position: i + 1, name: c.name, item: c.url })),
  })
  const faq = extractFaq(p.body)
  if (faq.length) {
    graph.push({
      '@context': 'https://schema.org',
      '@type': 'FAQPage',
      mainEntity: faq.map((f) => ({ '@type': 'Question', name: f.q, acceptedAnswer: { '@type': 'Answer', text: f.a } })),
    })
  }
  return graph
}
