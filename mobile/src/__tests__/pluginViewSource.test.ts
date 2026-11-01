// pluginViewSource.test.ts: source-level guards on the plugin view renderer
// (PluginView.tsx / NodeBoundary.tsx). The renderer is not renderable under
// vitest's plain node transform (react-native imports), so its invariants
// are pinned on the comment-stripped source instead: every ViewNode kind
// has a case, every node goes through the error boundary, and none of the
// sinks an untrusted document must never reach appears.

import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

const ROOT = join(__dirname, '..', '..');
const VIEW = join(ROOT, 'src', 'plugins', 'view');

const strip = (s: string) => s.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/[^\n]*/g, '$1');

const pluginView = strip(readFileSync(join(VIEW, 'PluginView.tsx'), 'utf8'));
const nodeBoundary = strip(readFileSync(join(VIEW, 'NodeBoundary.tsx'), 'utf8'));
const types = readFileSync(join(VIEW, 'types.ts'), 'utf8');

// Only the ViewNode union's own kinds: each member opens with `| { kind: '...'`
// -- `kind:` also appears on ViewAction, IconRef and diff lines earlier in types.ts.
const viewNodeUnion = types.slice(types.indexOf('export type ViewNode'));
const kinds = [...viewNodeUnion.matchAll(/\|\s*\{\s*kind: '([^']+)'/g)].map((m) => m[1]);

describe('plugin view renderer source guards', () => {
  it('handles every ViewNode kind', () => {
    expect(kinds.length).toBe(16);
    for (const kind of kinds) expect(pluginView, kind).toContain(`case '${kind}'`);
  });

  it('every rendered node is wrapped in NodeBoundary', () => {
    expect(pluginView).toContain('<NodeBoundary');
    expect(nodeBoundary).toContain('getDerivedStateFromError');
  });

  it('no HTML sinks, images or fetches', () => {
    for (const [name, source] of [
      ['PluginView.tsx', pluginView],
      ['NodeBoundary.tsx', nodeBoundary],
    ] as const) {
      expect(
        source,
        name,
      ).not.toMatch(
        /dangerouslySetInnerHTML|innerHTML|WebView|<Image\b|Image\.|source=\{\{\s*uri|\bfetch\s*\(|XMLHttpRequest|Linking\.|\beval\s*\(|new\s+Function/,
      );
    }
  });
});
