// markdown.ts is a small, total markdown parser for assistant messages (the
// supported subset only): paragraphs, #/##/### headings, -/*/1. lists,
// fenced code (an UNCLOSED fence is still code: the message may be
// streaming), > quotes; inline **bold**, *italic*/_italic_, `code`,
// [text](url). No HTML, images or tables (their source stays literal text).
// Security: a link is a link ONLY for http:/https:/mailto: URLs; anything
// else renders as plain text `text (url)`. parseMarkdown never throws and is
// deterministic. Pure: no react-native import.

export type Inline =
  | { type: 'text'; text: string }
  | { type: 'bold'; children: Inline[] }
  | { type: 'italic'; children: Inline[] }
  | { type: 'code'; text: string }
  | { type: 'link'; text: string; url: string };

export type Block =
  | { type: 'paragraph'; inlines: Inline[] }
  | { type: 'heading'; level: 1 | 2 | 3; inlines: Inline[] }
  | { type: 'list'; ordered: boolean; items: Inline[][] }
  | { type: 'code'; lang: string | null; text: string }
  | { type: 'quote'; inlines: Inline[] };

const MAX_DEPTH = 4;
const SAFE_URL = /^(https?:|mailto:)/i;
const FENCE = /^\s*```\s*([^\s`]*)/;
const HEADING = /^\s{0,3}(#{1,3})\s+(.*)$/;
const LIST_ITEM = /^(\s*)([-*]|\d+\.)\s+(.*)$/;
const QUOTE = /^\s{0,3}>\s?(.*)$/;

function pushText(out: Inline[], text: string): void {
  if (text === '') return;
  const last = out[out.length - 1];
  if (last && last.type === 'text') last.text += text;
  else out.push({ type: 'text', text });
}

/** Index of the closing single `ch` for an emphasis opened before `from`, skipping `**` pairs; -1 when none. */
function findEmphasisClose(s: string, ch: string, from: number): number {
  let j = from;
  while (j < s.length) {
    if (s[j] === ch) {
      if (ch === '*' && s[j + 1] === '*') {
        j += 2;
        continue;
      }
      if (j > from && !/\s/.test(s[j - 1])) {
        if (ch === '_' && /[A-Za-z0-9]/.test(s[j + 1] ?? '')) {
          j++;
          continue;
        }
        return j;
      }
    }
    j++;
  }
  return -1;
}

export function parseInlines(s: string, depth = 0): Inline[] {
  const out: Inline[] = [];
  if (depth > MAX_DEPTH) {
    pushText(out, s);
    return out;
  }
  let i = 0;
  let buf = '';
  const flush = () => {
    pushText(out, buf);
    buf = '';
  };
  while (i < s.length) {
    const c = s[i];
    if (c === '`') {
      const end = s.indexOf('`', i + 1);
      if (end > i + 1) {
        flush();
        out.push({ type: 'code', text: s.slice(i + 1, end) });
        i = end + 1;
        continue;
      }
    } else if (c === '*' && s[i + 1] === '*') {
      const end = s.indexOf('**', i + 2);
      if (end > i + 2) {
        flush();
        out.push({ type: 'bold', children: parseInlines(s.slice(i + 2, end), depth + 1) });
        i = end + 2;
        continue;
      }
      buf += '**';
      i += 2;
      continue;
    } else if ((c === '*' || c === '_') && s[i + 1] !== undefined && !/\s/.test(s[i + 1]) && s[i + 1] !== c) {
      const boundaryOk = c === '*' || i === 0 || !/[A-Za-z0-9]/.test(s[i - 1]);
      const end = boundaryOk ? findEmphasisClose(s, c, i + 1) : -1;
      if (end > i + 1) {
        flush();
        out.push({ type: 'italic', children: parseInlines(s.slice(i + 1, end), depth + 1) });
        i = end + 1;
        continue;
      }
    } else if (c === '[') {
      const close = s.indexOf(']', i + 1);
      if (close > i && s[close + 1] === '(') {
        const paren = s.indexOf(')', close + 2);
        if (paren > close + 1) {
          const text = s.slice(i + 1, close);
          const url = s.slice(close + 2, paren).trim();
          flush();
          if (SAFE_URL.test(url) && text !== '') out.push({ type: 'link', text, url });
          else pushText(out, `${text} (${url})`);
          i = paren + 1;
          continue;
        }
      }
    }
    buf += c;
    i++;
  }
  flush();
  return out;
}

export function parseMarkdown(src: string): Block[] {
  const lines = src.replace(/\r\n?/g, '\n').split('\n');
  const blocks: Block[] = [];
  let para: string[] = [];
  const flushPara = () => {
    if (para.length > 0) blocks.push({ type: 'paragraph', inlines: parseInlines(para.join('\n')) });
    para = [];
  };
  const startsBlock = (line: string) => FENCE.test(line) || HEADING.test(line) || LIST_ITEM.test(line) || QUOTE.test(line);

  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (line.trim() === '') {
      flushPara();
      i++;
      continue;
    }
    const fence = FENCE.exec(line);
    if (fence) {
      flushPara();
      const body: string[] = [];
      i++;
      while (i < lines.length && !/^\s*```\s*$/.test(lines[i])) body.push(lines[i++]);
      i++; // the closing fence, or past the end for an unclosed one
      blocks.push({ type: 'code', lang: fence[1] === '' ? null : fence[1], text: body.join('\n') });
      continue;
    }
    const heading = HEADING.exec(line);
    if (heading) {
      flushPara();
      blocks.push({ type: 'heading', level: heading[1].length as 1 | 2 | 3, inlines: parseInlines(heading[2].trim()) });
      i++;
      continue;
    }
    const quote = QUOTE.exec(line);
    if (quote) {
      flushPara();
      const parts = [quote[1]];
      i++;
      while (i < lines.length) {
        const q = QUOTE.exec(lines[i]);
        if (!q) break;
        parts.push(q[1]);
        i++;
      }
      blocks.push({ type: 'quote', inlines: parseInlines(parts.join('\n')) });
      continue;
    }
    const item = LIST_ITEM.exec(line);
    if (item) {
      flushPara();
      const ordered = /\d/.test(item[2]);
      const texts = [item[3]];
      i++;
      while (i < lines.length) {
        const next = lines[i];
        const m = LIST_ITEM.exec(next);
        if (m) {
          if (/\d/.test(m[2]) !== ordered) break;
          texts.push(m[3]);
        } else if (next.trim() !== '' && /^\s/.test(next) && !startsBlock(next)) {
          texts[texts.length - 1] += `\n${next.trim()}`; // an indented continuation line
        } else break;
        i++;
      }
      blocks.push({ type: 'list', ordered, items: texts.map((t) => parseInlines(t)) });
      continue;
    }
    para.push(line);
    i++;
  }
  flushPara();
  return blocks;
}
