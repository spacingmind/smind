// pluginSafety.test.ts: AM5 / AC30 note. No eval or `new Function` anywhere in mobile/src or app/, the plugin screens
// draw plugin text only through <Text>, and nothing fetches a remote image or URL from plugin data.

import { mkdtempSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { pluginDetailView, pluginsListView } from '../plugins/listView';
import { slashSuggestions, buildSlashCommands } from '../plugins/slashCommands';
import { cmd, consent, listing, plugin } from './pluginFixtures';

const ROOT = join(__dirname, '..', '..');

function files(dir: string): string[] {
  const out: string[] = [];
  for (const e of readdirSync(dir)) {
    const full = join(dir, e);
    if (statSync(full).isDirectory()) {
      if (e !== '__tests__' && e !== 'node_modules') out.push(...files(full));
    } else if (/\.(ts|tsx)$/.test(e)) out.push(full);
  }
  return out;
}

const strip = (s: string) => s.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/[^\n]*/g, '$1');

/** What neither the app nor the plugin renderer may ever do: execute a string as code. */
const BANNED = /\beval\s*\(|\bnew\s+Function\b|\bFunction\s*\(\s*['"`]/;

/** The files whose comment-stripped source still matches BANNED. */
const scan = (paths: string[]) => paths.filter((f) => BANNED.test(strip(readFileSync(f, 'utf8'))));

const SOURCES = ['src', 'app'].flatMap((d) => files(join(ROOT, d)));

describe('lint: no eval / new Function in mobile source', () => {
  it('scans every mobile/src and mobile/app file', () => {
    expect(SOURCES.length).toBeGreaterThan(50);
    expect(scan(SOURCES)).toEqual([]);
  });

  it('the scanner itself catches the patterns it bans', () => {
    for (const bad of ['eval("1")', 'x = eval ( y )', 'new Function("a", "return a")', 'new  Function(code)', 'Function("return 1")()']) expect(BANNED.test(bad), bad).toBe(true);
    for (const ok of ['evaluate(x)', 'retrieval(x)', 'const f: Function = g']) expect(BANNED.test(ok), ok).toBe(false);
  });

  it('the scanner catches a planted violation in a temp file', () => {
    const dir = mkdtempSync(join(tmpdir(), 'plugin-safety-'));
    try {
      const planted = join(dir, 'planted.ts');
      writeFileSync(planted, 'const f = new Function("return 1")\n');
      const clean = join(dir, 'clean.ts');
      writeFileSync(clean, 'const one = 1\n');
      expect(scan([planted, clean])).toEqual([planted]);
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });
});

describe('no eval / new Function in the web plugin renderer', () => {
  it('scans web/packages/ui/src/components/plugin-view (the renderer mobile shares the view schema with)', () => {
    const renderer = files(join(ROOT, '..', 'web', 'packages', 'ui', 'src', 'components', 'plugin-view'));
    expect(renderer.length).toBeGreaterThan(0);
    expect(scan(renderer)).toEqual([]);
  });
});

describe('plugin screens render plugin text as plain text', () => {
  const screens = ['src/screens/PluginsScreen.tsx', 'src/screens/PluginDetailScreen.tsx', 'src/screens/PluginPanelScreen.tsx', 'src/screens/plugins/PluginRow.tsx', 'src/screens/plugins/ThemePicker.tsx', 'src/screens/task/SlashSuggestions.tsx'];

  it('no HTML sinks, no markdown renderer, no image, no URL fetch, no dynamic import in the plugin screens', () => {
    for (const rel of screens) {
      const src = strip(readFileSync(join(ROOT, rel), 'utf8'));
      expect(src, rel).not.toMatch(/dangerouslySetInnerHTML|innerHTML|WebView|<Image\b|Image\.|source=\{\{\s*uri|\bfetch\s*\(|XMLHttpRequest|Linking\.openURL|import\(|require\(|Markdown/);
    }
  });

  it('hostile strings pass through the view models verbatim (rendered later as <Text>, never parsed)', () => {
    const evil = ['<img src=x onerror=alert(1)>', '[click](javascript:alert(1))', '![x](https://evil.example/p.png)', '<script>alert(1)</script>', '{{constructor.constructor("x")()}}', '‮gnp.exe'];
    for (const s of evil) {
      const info = plugin({
        id: 'evil',
        name: s,
        lastError: s,
        consent: consent({ description: s, capabilities: [s], skills: [s], mcpServers: [{ name: s, transport: s }], profiles: [{ name: s, provider: s, escalations: [s] }] }),
        conflicts: [{ plugin: 'evil', kind: s, name: s, reason: s }],
      });
      const row = pluginsListView(listing([info]));
      expect(row.kind === 'list' && row.rows[0].name).toBe(s);
      expect(row.kind === 'list' && row.rows[0].lastError).toBe(s);
      const d = pluginDetailView(info, [cmd('evil', s, s, s)]);
      expect(d.description).toBe(s);
      expect(d.capabilities).toEqual([s]);
      expect(d.profiles[0].escalations).toEqual([s]);
      expect(d.conflicts[0].reason).toBe(s);
      const rows = slashSuggestions(buildSlashCommands([], [cmd('evil', 'x', s, s)]), '/');
      expect(rows[0].text).toBe(s);
      expect(rows[0].description).toBe(s);
    }
  });
});
