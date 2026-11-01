// The docs share the product site's look (spacingmind.com): Geist, the same
// tokens, the same header and footer. The tokens live in custom.css.
import { h } from 'vue'
import { useData } from 'vitepress'
import DefaultTheme, { useSidebar } from 'vitepress/theme-without-fonts'
import '@fontsource-variable/geist/wght.css'
import '@fontsource-variable/geist-mono/wght.css'
import './custom.css'
import logoLight from './assets/smind-logo-light.png'
import logoDark from './assets/smind-logo.png'

const SITE = 'https://spacingmind.com'
const REPO = 'https://github.com/spacingmind/smind'

type Link = { text: string; href: string }
const footerColumns: { title: string; items: Link[] }[] = [
  {
    title: 'Product',
    items: [
      { text: 'Download', href: `${SITE}/download` },
      { text: 'Docs', href: '/' },
      { text: 'Changelog', href: `${REPO}/releases` },
      { text: 'Install script', href: `https://raw.githubusercontent.com/spacingmind/smind/master/scripts/install.sh` },
    ],
  },
  {
    title: 'Community',
    items: [
      { text: 'GitHub', href: REPO },
      { text: 'Issues', href: `${REPO}/issues` },
      { text: 'Contributing', href: `${REPO}/blob/develop/CONTRIBUTING.md` },
      { text: 'License', href: `${REPO}/blob/develop/LICENSE` },
    ],
  },
  {
    title: 'Company',
    items: [
      { text: 'Blog', href: `${SITE}/blog` },
      { text: 'Services', href: `${SITE}/services` },
      { text: 'Contact', href: `${SITE}/contact` },
      { text: 'Terms & Privacy', href: `${SITE}/legal` },
    ],
  },
]

const isExternal = (href: string) => /^https?:\/\//.test(href)
const anchor = (l: Link, cls?: string) =>
  h('a', { href: l.href, class: cls, ...(isExternal(l.href) ? { target: '_blank', rel: 'noopener' } : {}) }, l.text)

function Footer(version: string, hasSidebar: boolean) {
  return h('footer', { class: ['lp-footer', { 'has-sidebar': hasSidebar }] }, [
    h('div', { class: 'lp-footer-inner' }, [
      h('div', { class: 'lp-footer-grid' }, [
        h('div', { class: 'lp-footer-brand' }, [
          h('a', { href: SITE, class: 'lp-logo', 'aria-label': 'sMind home' }, [
            h('img', { src: logoLight, alt: '', width: 32, height: 32, class: 'lp-logo-img lp-logo-light' }),
            h('img', { src: logoDark, alt: '', width: 32, height: 32, class: 'lp-logo-img lp-logo-dark' }),
            h('span', { class: 'lp-logo-text' }, 'sMind'),
          ]),
          h('p', null, 'Space for your agents, free for you. Open source, self-hosted.'),
        ]),
        ...footerColumns.map((c) =>
          h('div', null, [h('h3', null, c.title), h('ul', null, c.items.map((i) => h('li', null, [anchor(i)])))]),
        ),
      ]),
      h('div', { class: 'lp-footer-bottom' }, [
        h('p', null, `© ${new Date().getFullYear()} Spacing Mind. Documentation for sMind ${version}.`),
        h('a', { href: 'mailto:hello@spacingmind.com' }, 'hello@spacingmind.com'),
      ]),
    ]),
  ])
}

const DownloadButton = (cls: string) => h('a', { class: cls, href: `${SITE}/download` }, 'Download')

export default {
  extends: DefaultTheme,
  Layout() {
    const { theme, page } = useData()
    const { hasSidebar } = useSidebar()
    const tc = theme.value as { smindVersion?: string; smindGroups?: Record<string, string> }
    const version = String(tc.smindVersion ?? '')
    return h(DefaultTheme.Layout, null, {
      // Logo as on the product site: light and dark mark, teal wordmark, links home.
      'nav-bar-title-before': () => [
        h('img', { src: logoLight, alt: '', width: 32, height: 32, class: 'lp-logo-img lp-logo-light' }),
        h('img', { src: logoDark, alt: '', width: 32, height: 32, class: 'lp-logo-img lp-logo-dark' }),
      ],
      'nav-bar-content-after': () => DownloadButton('lp-download'),
      'nav-screen-content-after': () => DownloadButton('lp-download lp-download-block'),
      // Section name above the H1, like the eyebrow on the product site's section headings.
      'doc-before': () => {
        const group = tc.smindGroups?.[page.value.relativePath.replace(/\.md$/, '')]
        return group ? h('p', { class: 'lp-eyebrow' }, group) : null
      },
      'layout-bottom': () => Footer(version, hasSidebar.value),
    })
  },
}
