import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vitepress'
import {
  DOCS_DIR,
  OG_IMAGE_ALT,
  OG_IMAGE_HEIGHT,
  OG_IMAGE_URL,
  OG_IMAGE_WIDTH,
  REPO_URL,
  SITE_NAME,
  SITE_SUMMARY,
  SITE_URL,
  buildLlmsFullTxt,
  buildLlmsTxt,
  buildRobotsTxt,
  jsonLd,
  pagePath,
  pageTitle,
  pageUrl,
  rawMarkdown,
  sidebarGroups,
  readPages,
  rewriteLinks,
  version,
  versionLine,
} from '../lib/pages.mjs'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const pages = readPages()
const v = version()
const bySlug = new Map(pages.map((p) => [p.slug, p]))

export default defineConfig({
  title: SITE_NAME,
  titleTemplate: `:title | ${SITE_NAME}`,
  description: SITE_SUMMARY,
  lang: 'en-US',
  srcDir: '../public-docs',
  cleanUrls: true,
  lastUpdated: true,
  // Dark by default, as on spacingmind.com; the switch in the header still works.
  appearance: 'dark',
  // Any dead link fails the build (links into ../docs are rewritten below).
  ignoreDeadLinks: [/^https?:\/\/localhost/],
  // Pages live outside docs-site/, so bare imports from them must resolve here.
  vite: { resolve: { alias: [{ find: /^vue(\/.*)?$/, replacement: path.resolve(__dirname, '../node_modules/vue$1') }] } },
  sitemap: { hostname: SITE_URL },
  head: [
    // The product site's values: --background in light and dark.
    ['meta', { name: 'theme-color', content: '#f8f8f8', media: '(prefers-color-scheme: light)' }],
    ['meta', { name: 'theme-color', content: '#111111', media: '(prefers-color-scheme: dark)' }],
    ['link', { rel: 'icon', type: 'image/png', sizes: '192x192', href: '/icon-192.png' }],
    ['link', { rel: 'apple-touch-icon', href: '/icon-192.png' }],
  ],
  themeConfig: {
    // Same links as the product site's header; Docs is this site.
    siteTitle: 'sMind',
    logoLink: 'https://spacingmind.com',
    nav: [
      { text: 'Docs', link: '/', activeMatch: '^/' },
      { text: 'Changelog', link: `${REPO_URL}/releases` },
      { text: 'Blog', link: 'https://spacingmind.com/blog' },
      { text: 'Services', link: 'https://spacingmind.com/services' },
    ],
    sidebar: sidebarGroups(pages).map((g) => ({
      text: g.title,
      items: g.pages.map((p) => ({ text: p.slug === 'index' ? 'Overview' : p.fm.title, link: pagePath(p.slug) })),
    })),
    smindVersion: v,
    smindGroups: Object.fromEntries(sidebarGroups(pages).flatMap((g) => g.pages.map((p) => [p.slug, g.title]))),
    socialLinks: [{ icon: 'github', link: REPO_URL }],
    outline: { level: [2, 3] },
    search: { provider: 'local' },
    editLink: { pattern: `${REPO_URL}/edit/develop/public-docs/:path`, text: 'Edit this page on GitHub' },
  },
  markdown: {
    // Lighthouse color-contrast: github-light's green tokens are 4.28:1 on the code block.
    theme: { light: 'github-light-high-contrast', dark: 'github-dark' },
    // The docs write placeholders like <plugin> outside code: show them as text, not Vue tags.
    html: false,
    config(md) {
      // `{{` in prose or inline code would be evaluated by Vue; keep it literal.
      const esc = (t: string) => md.utils.escapeHtml(t).replace(/\{\{/g, '&#123;&#123;')
      md.renderer.rules.code_inline = (tokens, idx, _o, _e, self) =>
        `<code v-pre${self.renderAttrs(tokens[idx])}>${md.utils.escapeHtml(tokens[idx].content)}</code>`
      md.renderer.rules.text = (tokens, idx) => esc(tokens[idx].content)
      // html:false turns whole-line <!-- ... --> markers into visible text; drop them before parsing.
      md.core.ruler.before('block', 'smind-drop-comments', (state) => {
        state.src = state.src.replace(/^<!--.*-->[ \t]*\n?/gm, '')
      })
      // Under the first H1: the page summary and the version line; links into docs/ go to GitHub.
      md.core.ruler.push('smind-summary', (state) => {
        const fm = (state.env as any)?.frontmatter ?? {}
        const toks = state.tokens
        for (const t of toks) {
          if (t.type !== 'inline') continue
          for (const c of t.children ?? []) {
            if (c.type === 'link_open') {
              const href = c.attrGet('href')
              if (href) c.attrSet('href', rewriteLinks(`](${href}`).slice(2))
            }
          }
        }
        const i = toks.findIndex((t) => t.type === 'heading_close' && t.tag === 'h1')
        if (i < 0 || !fm.summary) return
        const mk = (text: string, cls: string) => {
          const open = new state.Token('paragraph_open', 'p', 1)
          open.attrSet('class', cls)
          const inline = new state.Token('inline', '', 0)
          inline.content = text
          const txt = new state.Token('text', '', 0)
          txt.content = text
          inline.children = [txt]
          const close = new state.Token('paragraph_close', 'p', -1)
          return [open, inline, close]
        }
        toks.splice(i + 1, 0, ...mk(fm.summary, 'page-summary'), ...mk(versionLine(v), 'page-version'))
      })
    },
  },
  transformPageData(pageData) {
    const p = bySlug.get(pageData.relativePath.replace(/\.md$/, ''))
    if (p && pageTitle(p) === p.fm.title) pageData.titleTemplate = false
  },
  transformHead({ pageData }) {
    const p = bySlug.get(pageData.relativePath.replace(/\.md$/, ''))
    if (!p) return []
    const url = pageUrl(p.slug)
    const title = pageTitle(p)
    const tags: any[] = [
      ['link', { rel: 'canonical', href: url }],
      ['link', { rel: 'alternate', type: 'text/markdown', href: `${url === SITE_URL + '/' ? SITE_URL + '/index' : url}.md` }],
      ['meta', { property: 'og:type', content: p.slug === 'index' ? 'website' : 'article' }],
      ['meta', { property: 'og:site_name', content: SITE_NAME }],
      ['meta', { property: 'og:title', content: title }],
      ['meta', { property: 'og:description', content: p.fm.description }],
      ['meta', { property: 'og:url', content: url }],
      ['meta', { property: 'og:image', content: OG_IMAGE_URL }],
      ['meta', { property: 'og:image:width', content: String(OG_IMAGE_WIDTH) }],
      ['meta', { property: 'og:image:height', content: String(OG_IMAGE_HEIGHT) }],
      ['meta', { property: 'og:image:alt', content: OG_IMAGE_ALT }],
      ['meta', { name: 'twitter:card', content: 'summary_large_image' }],
      ['meta', { name: 'twitter:title', content: title }],
      ['meta', { name: 'twitter:description', content: p.fm.description }],
      ['meta', { name: 'twitter:image', content: OG_IMAGE_URL }],
      ['meta', { name: 'twitter:image:alt', content: OG_IMAGE_ALT }],
    ]
    for (const g of jsonLd(p, { lastUpdated: pageData.lastUpdated, v })) {
      tags.push(['script', { type: 'application/ld+json' }, JSON.stringify(g)])
    }
    return tags
  },
  buildEnd(config) {
    const out = config.outDir
    fs.writeFileSync(path.join(out, 'CNAME'), 'docs.spacingmind.com\n')
    fs.writeFileSync(path.join(out, '.nojekyll'), '')
    fs.writeFileSync(path.join(out, 'robots.txt'), buildRobotsTxt())
    fs.copyFileSync(path.join(__dirname, '../assets/og-card.png'), path.join(out, 'og-card.png'))
    fs.copyFileSync(path.join(__dirname, '../assets/icon-192.png'), path.join(out, 'icon-192.png'))
    fs.writeFileSync(path.join(out, 'llms.txt'), buildLlmsTxt(pages))
    fs.writeFileSync(path.join(out, 'llms-full.txt'), buildLlmsFullTxt(pages))
    for (const p of pages) fs.writeFileSync(path.join(out, `${p.slug}.md`), rawMarkdown(p, v))
  },
})
