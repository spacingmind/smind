import { describe, expect, it } from 'vitest';
import { Block, Inline, parseInlines, parseMarkdown } from '../transcript/markdown';

function plainInline(inl: Inline[]): string {
  return inl
    .map((i) => {
      switch (i.type) {
        case 'text':
        case 'code':
          return i.text;
        case 'link':
          return i.text;
        default:
          return plainInline(i.children);
      }
    })
    .join('');
}
function plain(blocks: Block[]): string {
  return blocks
    .map((b) => {
      switch (b.type) {
        case 'list':
          return b.items.map(plainInline).join('\n');
        case 'code':
          return `${b.lang ?? ''}${b.text}`;
        default:
          return plainInline(b.inlines);
      }
    })
    .join('\n');
}

describe('parseMarkdown blocks', () => {
  it('paragraphs split on blank lines', () => {
    const b = parseMarkdown('one\n\ntwo');
    expect(b.map((x) => x.type)).toEqual(['paragraph', 'paragraph']);
  });
  it('headings 1-3', () => {
    const b = parseMarkdown('# A\n## B\n### C');
    expect(b.map((x) => (x.type === 'heading' ? x.level : 0))).toEqual([1, 2, 3]);
  });
  it('unordered and ordered lists', () => {
    const b = parseMarkdown('- a\n* b\n\n1. x\n2. y');
    expect(b).toHaveLength(2);
    expect(b[0]).toMatchObject({ type: 'list', ordered: false });
    expect(b[1]).toMatchObject({ type: 'list', ordered: true });
    expect((b[1] as { items: Inline[][] }).items).toHaveLength(2);
  });
  it('an indented nested item stays in the list', () => {
    const b = parseMarkdown('- a\n  - b');
    expect(b).toHaveLength(1);
    expect((b[0] as { items: Inline[][] }).items).toHaveLength(2);
  });
  it('fenced code with a language', () => {
    expect(parseMarkdown('```go\nx := 1\n```')).toEqual([{ type: 'code', lang: 'go', text: 'x := 1' }]);
  });
  it('an unclosed fence is still a code block', () => {
    expect(parseMarkdown('hi\n```\nfunc main() {')).toEqual([
      { type: 'paragraph', inlines: [{ type: 'text', text: 'hi' }] },
      { type: 'code', lang: null, text: 'func main() {' },
    ]);
  });
  it('blockquote', () => {
    expect(parseMarkdown('> quoted\n> more')[0]).toMatchObject({ type: 'quote' });
  });
  it('empty string is no blocks', () => {
    expect(parseMarkdown('')).toEqual([]);
  });
  it('a table stays literal text', () => {
    expect(plain(parseMarkdown('| a | b |\n|---|---|'))).toContain('| a | b |');
  });
  it('html stays literal text', () => {
    expect(plain(parseMarkdown('<script>alert(1)</script>'))).toBe('<script>alert(1)</script>');
  });
});

describe('inlines', () => {
  it('bold, italic (* and _), code', () => {
    expect(parseInlines('**b** *i* _j_ `c`')).toEqual([
      { type: 'bold', children: [{ type: 'text', text: 'b' }] },
      { type: 'text', text: ' ' },
      { type: 'italic', children: [{ type: 'text', text: 'i' }] },
      { type: 'text', text: ' ' },
      { type: 'italic', children: [{ type: 'text', text: 'j' }] },
      { type: 'text', text: ' ' },
      { type: 'code', text: 'c' },
    ]);
  });
  it('bold nested in italic', () => {
    expect(parseInlines('*a **b** c*')).toEqual([
      {
        type: 'italic',
        children: [
          { type: 'text', text: 'a ' },
          { type: 'bold', children: [{ type: 'text', text: 'b' }] },
          { type: 'text', text: ' c' },
        ],
      },
    ]);
  });
  it('snake_case is not italic', () => {
    expect(parseInlines('some_var_name')).toEqual([{ type: 'text', text: 'some_var_name' }]);
  });
  it('link allow-list', () => {
    expect(parseInlines('[x](https://a.b/c)')).toEqual([{ type: 'link', text: 'x', url: 'https://a.b/c' }]);
    expect(parseInlines('[m](mailto:a@b.c)')).toEqual([{ type: 'link', text: 'm', url: 'mailto:a@b.c' }]);
    expect(parseInlines('[x](javascript:alert(1))')[0]).toEqual({ type: 'text', text: 'x (javascript:alert(1))' });
    expect(parseInlines('[x](data:text/html,hi)')).toEqual([{ type: 'text', text: 'x (data:text/html,hi)' }]);
    expect(parseInlines('[x](/rel)')).toEqual([{ type: 'text', text: 'x (/rel)' }]);
  });
  it('a javascript: link never becomes a link node', () => {
    const inl = parseInlines('[x](javascript:alert(1))');
    expect(inl.some((i) => i.type === 'link')).toBe(false);
  });
});

describe('malformed input never throws or loses text', () => {
  it.each(['**unbalanced', '`lone', '[a](', '[a', '*', '_', '**', '```', '', 'x'.repeat(100_000)])('%j', (src) => {
    expect(() => parseMarkdown(src)).not.toThrow();
    expect(plain(parseMarkdown(src)).replace(/[*_`[\]()\s]/g, '')).toContain(src.replace(/[*_`[\]()\s]/g, '').slice(0, 1000));
  });
  it('5000 seeded random characters keep every letter', () => {
    let seed = 12345;
    const rnd = () => (seed = (seed * 1664525 + 1013904223) % 4294967296) / 4294967296;
    const charset = 'abcxyz *_`[]()#->\n';
    let src = '';
    for (let i = 0; i < 5000; i++) src += charset[Math.floor(rnd() * charset.length)];
    const blocks = parseMarkdown(src);
    expect(parseMarkdown(src)).toEqual(blocks); // deterministic
    const letters = (s: string) => s.replace(/[^a-z]/g, '').split('').sort().join('');
    expect(letters(plain(blocks))).toBe(letters(src));
  });
});
