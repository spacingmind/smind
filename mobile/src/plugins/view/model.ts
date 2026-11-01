// model.ts turns a daemon-validated ui:1 plugin view document into the pure
// ViewNode tree the mobile UI will draw (the web renderer's twin, minus the
// DOM): every untrusted value is coerced to text at the boundary, one bad
// node degrades to an `unsupported` placeholder instead of blanking the
// panel, and nothing here throws. No react-native import so vitest's plain
// node transform can load it.
import { parseAction } from './actions';
import { pluginIcon } from './icons';
import { pluginMarkdown } from './markdown';
import type { FormField, Tone, ViewNode } from './types';

const MAX_RENDER_DEPTH = 16;

const NODE_TYPES = new Set([
  'badge', 'button', 'code', 'diff', 'empty', 'error', 'form', 'heading',
  'keyValue', 'list', 'markdown', 'progress', 'row', 'stack', 'table', 'text',
]);

function isRecord(x: unknown): x is Record<string, unknown> {
  return typeof x === 'object' && x !== null && !Array.isArray(x);
}

function str(x: unknown): string {
  return typeof x === 'string' ? x : '';
}

function toneOf(x: unknown): Tone | undefined {
  return x === 'success' || x === 'warning' || x === 'danger' || x === 'muted' ? x : undefined;
}

function cellText(cell: unknown): string {
  // A missing cell draws as an en dash, not an empty string, so a null row stays readable.
  if (cell === null || cell === undefined) return '–';
  if (cell === true) return 'Yes';
  if (cell === false) return 'No';
  if (typeof cell === 'number') return String(cell);
  if (typeof cell === 'string') return cell;
  return '';
}

function diffLineKind(line: string): 'add' | 'del' | 'hunk' | 'ctx' {
  if (line.startsWith('@@')) return 'hunk';
  if (line.startsWith('+') && !line.startsWith('+++')) return 'add';
  if (line.startsWith('-') && !line.startsWith('---')) return 'del';
  return 'ctx';
}

function recordItems(x: unknown): Record<string, unknown>[] {
  return (Array.isArray(x) ? x : []).filter(isRecord);
}

function formField(f: Record<string, unknown>): FormField {
  const type = f.type === 'toggle' || f.type === 'select' || f.type === 'secret' ? f.type : 'text';
  return {
    name: f.name as string,
    label: str(f.label),
    type,
    required: f.required === true,
    placeholder: str(f.placeholder),
    options: recordItems(f.options).map((o) => ({ value: str(o.value), label: str(o.label) })),
    default: type === 'toggle' ? f.default === true : typeof f.default === 'string' ? f.default : '',
  };
}

function unsupported(path: string, nodeType?: string): ViewNode {
  return nodeType === undefined ? { kind: 'unsupported', path } : { kind: 'unsupported', path, nodeType };
}

export function buildView(doc: unknown): { supported: true; title?: string; root: ViewNode } | { supported: false } {
  if (!isRecord(doc) || typeof doc.ui !== 'number' || doc.ui > 1 || !('root' in doc)) return { supported: false };
  const view: { supported: true; title?: string; root: ViewNode } = { supported: true, root: buildNode(doc.root, 0, 'root') };
  if (typeof doc.title === 'string') view.title = doc.title;
  return view;
}

export function buildNode(raw: unknown, depth: number, path: string): ViewNode {
  try {
    return node(raw, depth, path);
  } catch {
    return { kind: 'unsupported', path };
  }
}

function node(raw: unknown, depth: number, path: string): ViewNode {
  const nodeType = isRecord(raw) && typeof raw.type === 'string' ? raw.type : undefined;
  if (!isRecord(raw) || depth > MAX_RENDER_DEPTH) return unsupported(path, nodeType);
  const type = raw.type;
  if (typeof type !== 'string' || !NODE_TYPES.has(type)) return unsupported(path, nodeType);

  switch (type) {
    case 'stack':
    case 'row': {
      const children = Array.isArray(raw.children) ? raw.children : [];
      const gap = raw.gap === 'sm' || raw.gap === 'lg' ? raw.gap : 'md';
      return {
        kind: 'stack',
        path,
        children: children.map((child, i) => buildNode(child, depth + 1, `${path}.children[${i}]`)),
        gap,
        row: type === 'row',
        wrap: raw.wrap === true,
      };
    }

    case 'heading': {
      const level = raw.level === 1 || raw.level === 2 || raw.level === 3 || raw.level === 4 ? raw.level : 2;
      return { kind: 'heading', path, text: str(raw.text), level };
    }

    case 'text':
      return { kind: 'text', path, text: str(raw.text), small: raw.size === 'sm', tone: toneOf(raw.tone) };

    case 'markdown':
      return { kind: 'markdown', path, blocks: pluginMarkdown(str(raw.text)) };

    case 'badge':
      return { kind: 'badge', path, text: str(raw.text), tone: toneOf(raw.tone), icon: pluginIcon(raw.icon) };

    case 'button':
      return {
        kind: 'button',
        path,
        label: str(raw.label),
        tone: toneOf(raw.tone),
        disabled: raw.disabled === true,
        icon: pluginIcon(raw.icon),
        action: parseAction(raw.action),
      };

    case 'list':
      return {
        kind: 'list',
        path,
        items: recordItems(raw.items).map((item) => ({
          title: str(item.title),
          subtitle: str(item.subtitle),
          badge: str(item.badge),
          tone: toneOf(item.tone),
          icon: pluginIcon(item.icon),
          action: parseAction(item.action),
        })),
      };

    case 'keyValue':
      return {
        kind: 'keyValue',
        path,
        items: recordItems(raw.items).map((item) => ({ key: str(item.key), value: str(item.value), tone: toneOf(item.tone) })),
      };

    case 'table': {
      const columns = recordItems(raw.columns)
        .filter((c) => typeof c.key === 'string')
        .map((c) => ({ key: c.key as string, label: str(c.label), alignEnd: c.align === 'end' }));
      const rows = recordItems(raw.rows).map((row) => columns.map((c) => cellText(row[c.key])));
      return { kind: 'table', path, columns, rows };
    }

    case 'code':
      return { kind: 'code', path, text: str(raw.text) };

    case 'diff':
      return {
        kind: 'diff',
        path,
        lines: str(raw.text)
          .split('\n')
          .map((line) => ({ text: line, kind: diffLineKind(line) })),
      };

    case 'progress': {
      const value = typeof raw.value === 'number' && Number.isFinite(raw.value) ? raw.value : 0;
      const pct = Math.round(Math.min(1, Math.max(0, value)) * 100);
      return { kind: 'progress', path, label: str(raw.label), pct, tone: toneOf(raw.tone) };
    }

    case 'form': {
      const id = typeof raw.id === 'string' && raw.id ? raw.id : 'form';
      const fields = recordItems(raw.fields)
        .filter((f) => typeof f.name === 'string' && f.name !== '')
        .map(formField);
      const submit = isRecord(raw.submit) ? raw.submit : {};
      return { kind: 'form', path, id, fields, submitLabel: str(submit.label), action: parseAction(submit.action) };
    }

    case 'empty':
      return { kind: 'empty', path, title: str(raw.title), text: str(raw.text), icon: pluginIcon(raw.icon) };

    case 'error':
      return { kind: 'error', path, title: str(raw.title), text: str(raw.text) };
  }
  return unsupported(path, nodeType);
}
