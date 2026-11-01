// pluginView.test.ts drives the pure plugin view model with the daemon's own
// fixture suite (internal/plugins/uiview/testdata/fixtures -- the same
// documents the Go validator is tested against) plus the web renderer's
// golden strings, so the mobile model can never drift from the schema or from
// the web behaviour it mirrors.
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

import { describe, expect, it } from 'vitest';

import { openUrlHost, parseAction, rpcRequest } from '../plugins/view/actions';
import { pluginIcon } from '../plugins/view/icons';
import { mdLinkAction, pluginMarkdown } from '../plugins/view/markdown';
import { buildNode, buildView } from '../plugins/view/model';
import type { ViewNode } from '../plugins/view/types';
import type { Block, Inline } from '../transcript/markdown';

const FIXTURES_DIR = join(__dirname, '..', '..', '..', 'internal', 'plugins', 'uiview', 'testdata', 'fixtures');

type Fixture = {
  name: string;
  env: { rpcs: string[]; panels: string[] };
  doc: unknown;
  expect: { valid: true; normalized: { ui: number; title?: string; root: unknown } } | { valid: false; invalid: string };
};

function loadFixtures(): Fixture[] {
  const files = readdirSync(FIXTURES_DIR).filter((f) => f.endsWith('.json')).sort();
  expect(files.length, 'fixture files in the daemon testdata').toBeGreaterThanOrEqual(30);
  return files.map((f) => JSON.parse(readFileSync(join(FIXTURES_DIR, f), 'utf8')) as Fixture);
}

const FIXTURES = loadFixtures();

/** The model tree of a fixture: the normalised document when valid, else the raw one. */
function fixtureView(name: string): { supported: true; title?: string; root: ViewNode } {
  const fx = FIXTURES.find((f) => f.name === name);
  if (!fx) throw new Error(`missing fixture ${name}`);
  const view = buildView(fx.expect.valid ? fx.expect.normalized : fx.doc);
  if (view.supported !== true) throw new Error(`fixture ${name} did not build`);
  return view;
}

function inlineStrings(inlines: Inline[]): string[] {
  return inlines.flatMap((i) => {
    switch (i.type) {
      case 'text':
      case 'code':
      case 'link':
        return [i.text];
      default:
        return inlineStrings(i.children);
    }
  });
}

/** Every string a renderer would draw for the tree, joined like textContent. */
function flatten(node: ViewNode): string {
  switch (node.kind) {
    case 'stack':
      return node.children.map(flatten).join('');
    case 'heading':
    case 'text':
    case 'badge':
    case 'code':
      return node.text;
    case 'markdown':
      return node.blocks
        .map((b: Block) => {
          if (b.type === 'list') return b.items.flatMap(inlineStrings).join('');
          if (b.type === 'code') return b.text;
          return inlineStrings(b.inlines).join('');
        })
        .join('');
    case 'button':
      return node.label;
    case 'list':
      return node.items.map((i) => `${i.title}${i.subtitle}${i.badge}`).join('');
    case 'keyValue':
      return node.items.map((i) => `${i.key}${i.value}`).join('');
    case 'table':
      return [...node.columns.map((c) => c.label), ...node.rows.flat()].join('');
    case 'diff':
      return node.lines.map((l) => l.text).join('');
    case 'progress':
      return node.label;
    case 'form':
      return [...node.fields.flatMap((f) => [f.label, ...f.options.map((o) => o.label)]), node.submitLabel].join('');
    case 'empty':
    case 'error':
      return `${node.title}${node.text}`;
    case 'unsupported':
      return '';
  }
}

function walkInlines(blocks: Block[], visit: (inline: Inline) => void): void {
  const walk = (inlines: Inline[]) => {
    for (const i of inlines) {
      visit(i);
      if (i.type === 'bold' || i.type === 'italic') walk(i.children);
    }
  };
  for (const b of blocks) {
    if (b.type === 'list') b.items.forEach(walk);
    else if (b.type !== 'code') walk(b.inlines);
  }
}

describe('plugin view model', () => {
  it('builds every shared fixture without throwing', () => {
    expect(FIXTURES.length).toBeGreaterThanOrEqual(30);
    for (const fx of FIXTURES) {
      const view = buildView(fx.expect.valid ? fx.expect.normalized : fx.doc);
      if (fx.expect.valid && fx.expect.normalized.ui === 1) expect(view.supported, fx.name).toBe(true);
    }
  });

  const GOLDEN: Record<string, string[]> = {
    stack: ['Overview', 'Two nodes stacked vertically.'],
    heading: ['Release 1.2'],
    text: ['Small muted footnote.'],
    badge: ['beta'],
    button: ['Insert summary'],
    list: ['Re-run suite', 'pytest -q', 'fast', 'Open logs'],
    keyValue: ['Branch', 'main', 'Coverage', '82%'],
    table: ['File', 'Lines', 'a.go', '120', 'Yes', 'No'],
    code: ['fmt.Println'],
    diff: ['+new', '-old'],
    progress: ['Coverage'],
    form: ['Title', 'Severity', 'API token', 'File report'],
    empty: ['Nothing to review', 'Merge main to see changes.'],
    error: ['Panel failed', 'The plugin process is not running.'],
    'actions-all': ['Refresh', 'Ask', 'Command', 'Panel', 'Open'],
    'tones-all': ['success', 'warning', 'danger', 'muted'],
    row: ['beta', 'Ask'],
    markdown: ['Some', 'bold', 'docs', 'one', 'two'],
  };

  it('golden text per fixture', () => {
    for (const [name, strings] of Object.entries(GOLDEN)) {
      const flat = flatten(fixtureView(name).root);
      for (const s of strings) expect(flat, `${name}: ${s}`).toContain(s);
    }
  });

  it('unknown-node-placeholder', () => {
    const view = buildView({ ui: 1, root: { type: 'stack', children: [{ type: 'text', text: 'kept' }, { type: 'hologram', text: 'x' }] } });
    if (view.supported !== true) throw new Error('expected a supported view');
    expect(view.root.kind).toBe('stack');
    const children = view.root.kind === 'stack' ? view.root.children : [];
    expect(children[0]).toMatchObject({ kind: 'text', text: 'kept', path: 'root.children[0]' });
    expect(children[1]).toMatchObject({ kind: 'unsupported', nodeType: 'hologram', path: 'root.children[1]' });
  });

  it('a node whose property getter throws degrades to unsupported, siblings survive', () => {
    const bad = Object.defineProperty({ type: 'text' }, 'text', { get() { throw new Error('boom'); }, enumerable: true });
    const doc = { ui: 1, root: { type: 'stack', children: [{ type: 'text', text: 'before' }, bad, { type: 'text', text: 'after' }] } };
    const view = buildView(doc);
    if (view.supported !== true) throw new Error('expected a supported view');
    expect(view.root.kind).toBe('stack');
    const children = view.root.kind === 'stack' ? view.root.children : [];
    expect(children).toHaveLength(3);
    expect(children[0]).toMatchObject({ kind: 'text', text: 'before' });
    expect(children[2]).toMatchObject({ kind: 'text', text: 'after' });
    expect(children[1]).toMatchObject({ kind: 'unsupported', path: 'root.children[1]' });
  });

  it('unsupported ui level', () => {
    expect(buildView({ ui: 2, root: { type: 'text', text: 'x' } })).toEqual({ supported: false });
    expect(buildView('garbage')).toEqual({ supported: false });
    expect(buildView(null)).toEqual({ supported: false });
  });

  it('markdown-strips-raw-html', () => {
    const blocks = pluginMarkdown('<script>alert(1)</script>hi <b>x</b>');
    const texts: string[] = [];
    walkInlines(blocks, (i) => texts.push(...inlineStrings([i])));
    for (const t of texts) expect(t.includes('<')).toBe(false);
    expect(texts.join('')).toContain('alert(1)');
    expect(texts.join('')).toContain('hi x');
  });

  it('remote-image-not-fetched', () => {
    const blocks = pluginMarkdown('![logo](https://evil.example/p.png) after');
    const links: string[] = [];
    walkInlines(blocks, (i) => {
      if (i.type === 'link') links.push(i.url);
    });
    expect(links).toEqual([]);
    const flat = blocks.map((b) => (b.type === 'code' ? b.text : b.type === 'list' ? '' : inlineStrings(b.inlines).join(''))).join('');
    expect(flat).toContain('logo');
    expect(flat).toContain('after');
  });

  it('markdown links: only http(s) become links', () => {
    const blocks = pluginMarkdown('[a](https://x.example/p) [b](javascript:alert(1)) [c](mailto:a@b.example) [d](data:text/html,x)');
    const links: { text: string; url: string }[] = [];
    walkInlines(blocks, (i) => {
      if (i.type === 'link') links.push({ text: i.text, url: i.url });
    });
    expect(links).toEqual([{ text: 'a', url: 'https://x.example/p' }]);
    expect(mdLinkAction('https://x.example/p')).toEqual({ kind: 'openUrl', url: 'https://x.example/p', host: 'x.example' });
    expect(mdLinkAction('javascript:alert(1)')).toBeNull();
    expect(mdLinkAction('mailto:a@b.example')).toBeNull();
    expect(mdLinkAction('data:text/html,x')).toBeNull();
  });

  it('openUrl-host-derived-not-trusted', () => {
    const action = parseAction({ kind: 'openUrl', url: 'https://good.example:8443/a', host: 'evil.com' });
    expect(action).toEqual({ kind: 'openUrl', url: 'https://good.example:8443/a', host: 'good.example:8443' });
    expect(openUrlHost('https://user:pw@example.com/')).toBeNull();
    expect(openUrlHost('javascript:alert(1)')).toBeNull();
    expect(openUrlHost('file:///etc/passwd')).toBeNull();
    expect(openUrlHost('https://exa\u0001mple.com/')).toBeNull();
    expect(openUrlHost('https://exa\u007fmple.com/')).toBeNull();
    expect(openUrlHost('https:///no-authority')).toBeNull();
    expect(openUrlHost('https://good.example /x')).toBeNull();
    expect(openUrlHost('https://good .example/x')).toBeNull();
    expect(openUrlHost('https://good.example\\@evil.com/')).toBeNull();
    expect(openUrlHost('https://good.example/a b')).toBe('good.example');
    expect(openUrlHost('HTTPS://Example.COM:80/x')).toBe('example.com:80');
  });

  it('parseAction rejects unchecked shapes', () => {
    expect(parseAction({ kind: 'teleport', to: 'moon' })).toBeNull();
    expect(parseAction({ kind: 'rpc', rpc: '' })).toBeNull();
    expect(parseAction({ kind: 'rpc', rpc: 'refresh', params: 'nope' })).toEqual({ kind: 'rpc', rpc: 'refresh' });
    expect(parseAction(null)).toBeNull();
    expect(parseAction(42)).toBeNull();
    expect(parseAction('rpc')).toBeNull();
    expect(parseAction([1])).toBeNull();
    expect(parseAction({ kind: 'prompt' })).toBeNull();
    expect(parseAction({ kind: 'prompt', text: '' })).toBeNull();
    expect(parseAction({ kind: 'openPanel', panel: 'overview', context: 'x' })).toEqual({ kind: 'openPanel', panel: 'overview' });
  });

  it('rpcRequest uses the caller pluginId and adds values', () => {
    expect(rpcRequest('acme', { rpc: 'refresh', params: { depth: 2 } }, { title: 'Hi' })).toEqual({
      pluginId: 'acme',
      rpc: 'refresh',
      params: { depth: 2, values: { title: 'Hi' } },
    });
    expect(rpcRequest('other', { rpc: 'ping' })).toEqual({ pluginId: 'other', rpc: 'ping', params: {} });
    const params = { depth: 2 };
    rpcRequest('acme', { rpc: 'r', params }, { a: true });
    expect(params).toEqual({ depth: 2 });
  });

  it('prompt-action-model-only', () => {
    const action = parseAction({ kind: 'prompt', text: 'Why did the suite slow down?' });
    expect(action).toEqual({ kind: 'prompt', text: 'Why did the suite slow down?' });
    expect(action !== null && 'send' in action).toBe(false);
  });

  it('node build never throws', () => {
    const junkPool: unknown[] = [
      null, undefined, 0, -7, 1.5, NaN, Infinity, -Infinity, '', 'text', true, false,
      [], [[]], [1, 'two', null], [{ type: 'text' }],
      {}, { type: undefined }, { type: 42 }, { type: '' }, { type: 'hologram' },
      { type: 'text' }, { type: 'text', text: null }, { type: 'text', text: 42 }, { type: 'text', text: ['x'] },
      { type: 'stack', children: 'nope' }, { type: 'stack', children: [null, 7, 'x', { type: 'text', text: 'ok' }] },
      { type: 'stack', gap: 'huge' }, { type: 'row', wrap: 'yes' },
      { type: 'heading', level: 9 }, { type: 'heading', level: '3' },
      { type: 'list', items: [null, 'x', { title: 1 }, { title: 'ok', action: { kind: 'nope' } }] },
      { type: 'keyValue', items: [[], {}] },
      { type: 'table', columns: [null, { key: 9 }, { key: 'k' }], rows: [null, 'x', { k: { deep: [] } }] },
      { type: 'progress', value: 'lots' }, { type: 'progress', value: NaN },
      { type: 'diff', text: null },
      {
        type: 'form',
        fields: [null, { name: '' }, { name: 'x', type: 'weird', default: 9, options: [null, { value: 1 }] }],
        submit: 'x',
      },
      { type: 'markdown', text: { deep: true } },
      { type: 'button', label: null, action: [], icon: 42 },
      { type: 'empty', title: [] }, { type: 'error', text: {} },
    ];
    const junk: unknown[] = [];
    for (let i = 0; i < 200; i++) junk.push(junkPool[i % junkPool.length]);
    for (const value of junk) {
      const built = buildNode(value, 0, 'root');
      expect(typeof built.kind).toBe('string');
      expect(typeof buildView(value)).toBe('object');
    }

    let deep: unknown = { type: 'text', text: 'bottom' };
    for (let i = 0; i < 40; i++) deep = { type: 'stack', children: [deep] };
    let node = buildNode(deep, 0, 'root');
    for (let i = 0; i < 17 && node.kind === 'stack'; i++) node = node.children[0];
    expect(node.kind).toBe('unsupported');

    const cyclic: { type: string; children: unknown[] } = { type: 'stack', children: [] };
    cyclic.children.push(cyclic);
    const cycleNode = buildNode(cyclic, 0, 'root');
    expect(cycleNode.kind).toBe('stack');
  });

  it('icons: lucide registry, generic fallback, svg generic', () => {
    expect(pluginIcon('Check')).toEqual({ kind: 'name', name: 'check' });
    expect(pluginIcon('ChevronRight')).toEqual({ kind: 'name', name: 'chevronRight' });
    expect(pluginIcon('TriangleAlert')).toEqual({ kind: 'name', name: 'warning' });
    expect(pluginIcon('Paperclip')).toEqual({ kind: 'name', name: 'attach' });
    expect(pluginIcon('Zap')).toEqual({ kind: 'generic' });
    expect(pluginIcon({ svg: '<svg viewBox="0 0 24 24"><script>alert(1)</script></svg>' })).toEqual({ kind: 'generic' });
    expect(pluginIcon('constructor')).toEqual({ kind: 'none' });
    expect(pluginIcon(undefined)).toEqual({ kind: 'none' });
    expect(pluginIcon(42)).toEqual({ kind: 'none' });
    expect(pluginIcon(['Check'])).toEqual({ kind: 'none' });
  });
});
