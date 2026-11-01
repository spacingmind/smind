// markdown.ts adapts the transcript parser to plugin documents: images are
// never fetched (the alt text stands in, even nested in link text), raw HTML
// is stripped before parsing, and after parsing any non-http(s) link
// (mailto:, ...) is flattened to plain text -- so the only tappable thing a
// plugin markdown block can produce is a link openUrlHost accepts. Pure TS,
// no react-native import.
import { parseMarkdown, type Block, type Inline } from '../../transcript/markdown';
import { openUrlHost } from './actions';
import type { ViewAction } from './types';

export type { Block };

const IMAGE = /!\[([^\]]*)\]\([^)]*\)/g;
const HTML_COMMENT = /<!--[\s\S]*?-->/g;
const HTML_TAG = /<[A-Za-z/!][^>]*>/g;

function inlineText(inlines: Inline[]): Inline[] {
  return inlines.map((i) => {
    if (i.type === 'link') return openUrlHost(i.url) === null ? { type: 'text', text: i.text } : i;
    if (i.type === 'bold' || i.type === 'italic') return { ...i, children: inlineText(i.children) };
    return i;
  });
}

function sanitizeBlocks(blocks: Block[]): Block[] {
  return blocks.map((b) => {
    if (b.type === 'list') return { ...b, items: b.items.map(inlineText) };
    if (b.type === 'code') return b;
    return { ...b, inlines: inlineText(b.inlines) };
  });
}

export function pluginMarkdown(text: string): Block[] {
  const prepped = text
    .replace(IMAGE, '$1')
    .replace(HTML_COMMENT, '')
    .replace(HTML_TAG, '');
  return sanitizeBlocks(parseMarkdown(prepped));
}

/** The action tapping a safe markdown link produces; unsafe urls stay plain text. */
export function mdLinkAction(url: string): ViewAction | null {
  const host = openUrlHost(url);
  return host === null ? null : { kind: 'openUrl', url, host };
}
