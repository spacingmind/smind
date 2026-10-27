# Docs site: `public-docs/` on GitHub Pages at docs.spacingmind.com

Status: active (2026-10-27). Human-approved scope: SEO + GEO (generative-engine optimisation).

Size: M (4) — scope 1 (new `docs-site/` dir, one workflow, one node test), PRs 2, risk 1 (publishing; Pages and DNS stay a Human step), decisions 0, verification 2 (built site served locally, visual check, curl of generated files).

The site is built from `public-docs/` with VitePress. There is no content fork: the site reads the existing Markdown and front matter, and nav order comes from the `order` key. Every page is static HTML. The site config lives in `docs-site/`, outside the Go and `web/` workspaces.

## Acceptance criteria

AC1 `docs-site/` builds the 16 `public-docs/*.md` pages into static HTML (`docs-site/.vitepress/dist`) with `bun`/`npm` only; `srcDir` is `../public-docs`, nothing is copied into the repo. Sidebar and prev/next follow `order`.
AC2 Every built page has its own `<title>` ("<title> | sMind by Spacing Mind"), `<meta name="description">` from front matter, `<link rel="canonical">` on `https://docs.spacingmind.com`, OpenGraph and Twitter tags, one `<h1>`, a `lastUpdated` time, and its body is in the HTML (no JS-only content).
AC3 `sitemap.xml` lists every page (canonical URLs, lastmod); `robots.txt` allows GPTBot, OAI-SearchBot, ChatGPT-User, ClaudeBot, Claude-User, PerplexityBot, Google-Extended and Applebot-Extended explicitly and points to the sitemap.
AC4 `/llms.txt` (llmstxt.org format: H1, blockquote summary, a link list with one-line summaries, in `order`) and `/llms-full.txt` (every page concatenated as Markdown) are served; each page also has a raw Markdown copy at `<page>.md`.
AC5 JSON-LD: `SoftwareApplication` on the home page; `TechArticle` and `BreadcrumbList` on every page; `FAQPage` only on a page that has Q&A (none today unless a page adds a `## FAQ`-style question list; the code path is tested with a fixture).
AC6 Every page has a `summary` front matter key (what it is, who it is for), rendered as a short paragraph under the H1 and included in the raw copies and llms files; every page shows a version line ("sMind <version> by Spacing Mind") derived from `.release-please-manifest.json`, never hard-coded per page. Entity naming is "sMind by Spacing Mind".
AC7 Relative links into `../docs/...` (internal records) become absolute GitHub links in HTML and raw copies; the build fails on any other dead link.
AC8 `.github/workflows/docs-pages.yml` builds on pushes to `develop` touching `public-docs/**`, `docs-site/**` or the workflow, on `release: published` and on `workflow_dispatch`; it deploys with `actions/upload-pages-artifact` and `actions/deploy-pages` with `pages: write` and `id-token: write`; the artifact contains `CNAME` = `docs.spacingmind.com`. A pull request that touches these paths runs the build only (no deploy). Pages and DNS are not enabled by this work.
AC9 Drift test in `task test`/CI: every `public-docs` page has `title`, `description`, `summary` and `order`, and appears in the generated `llms.txt`; the generator is the same module the build uses.
AC10 `task lint` and `task test` pass; the existing `public-docs` drift test still passes; the new workflow passes `actionlint` if installed.

## Test scenarios (`docs-site/scripts/docs-site.test.mjs`, run by `node --test`)

| test | rule | mutation that must fail it |
|---|---|---|
| `front matter is complete` | every page has non-empty title, description, summary, positive unique order; no unquoted `: ` in a value | delete `summary` of a page |
| `llms.txt lists every page` | each page's slug link and description appear in `buildLlmsTxt()` | drop a page from the generator's list |
| `llms.txt order and shape` | starts with `# sMind by Spacing Mind`, a `>` summary, pages sorted by `order` | sort by title instead |
| `llms-full.txt contains every page body` | every page's H1 and first body line appear once | skip one page |
| `docs links are rewritten` | `../docs/x.md` becomes the GitHub URL; other links unchanged | remove the rewrite |
| `jsonld` | `TechArticle`+`BreadcrumbList` per page, `SoftwareApplication` only on the home page, `FAQPage` from a Q&A fixture | emit SoftwareApplication on all pages |
| `version line from manifest` | version equals `.release-please-manifest.json` | hard-code a version |
| `robots.txt allows the AI crawlers` | all eight agents have an `Allow: /` group | remove one agent |
| `workflow shape` | `docs-pages.yml` has `pages: write`, `id-token: write`, both pages actions, no deploy step outside a push/release/dispatch guard | drop `id-token: write` |

Built-output checks (manual, logged in Validation): `curl` of llms.txt, robots.txt, sitemap.xml, one page's JSON-LD; light and dark screenshots at 1280 and 390; Lighthouse SEO if it can run cheaply.

## Decisions

1. **VitePress**, config in `docs-site/` with its own `package.json`; not part of the `web/` bun workspace; ignored by Go. Node is used only to build.
2. **No dependency in the generators**: `docs-site/lib/pages.mjs` (front matter, llms files, JSON-LD, link rewrite) uses node builtins, so the drift test needs no `npm install` and runs in `task test` on any machine that already runs the desktop node tests.
3. **`summary` is a new front matter key** next to `description`. `description` stays the one-line meta description and the llms.txt summary; `summary` is the longer "what it is, who it is for" paragraph shown on the page. Both are additive and checked against the code and CLI.
4. **Version** comes from `.release-please-manifest.json` at build time.
5. **Clean URLs** (`/getting-started`), raw Markdown at `/getting-started.md`.
6. **Deploy only from `develop`, release and manual dispatch**; PRs build without deploying. Human steps (not done here): Settings → Pages → Source = GitHub Actions; Cloudflare CNAME `docs` → `spacingmind.github.io` (DNS only); Enforce HTTPS.

## PR sequence

PR1 this plan only. PR2 site, generators, workflow, summaries, drift test.

## Progress

- 2026-10-27 plan written.

## Validation

(filled in PR2)
