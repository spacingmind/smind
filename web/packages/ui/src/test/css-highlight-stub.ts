/**
 * jsdom doesn't implement the CSS Custom Highlight API
 * (`CSS.highlights`/`Highlight`) that `components/timeline/chat-find-dom.ts`
 * uses to paint chat Find's matches. This installs a minimal, inspectable
 * stand-in so component tests can exercise the real paint path -- through
 * that file's own feature-detection (`typeof Highlight`, `CSS.highlights`)
 * -- and assert which ranges got registered as "every match" vs "the
 * active one", instead of mocking `chat-find-dom.ts` itself.
 */

export class FakeHighlight {
  readonly ranges: readonly Range[];
  constructor(...ranges: Range[]) {
    this.ranges = ranges;
  }
}

export class FakeHighlightRegistry {
  private readonly entries = new Map<string, FakeHighlight>();
  set(name: string, highlight: FakeHighlight): this {
    this.entries.set(name, highlight);
    return this;
  }
  delete(name: string): boolean {
    return this.entries.delete(name);
  }
  get(name: string): FakeHighlight | undefined {
    return this.entries.get(name);
  }
  has(name: string): boolean {
    return this.entries.has(name);
  }
  clear(): void {
    this.entries.clear();
  }
  get size(): number {
    return this.entries.size;
  }
}

/**
 * Installs the stub onto `globalThis.Highlight`/`CSS.highlights` and
 * returns the registry to assert against plus a `restore()` -- call that
 * in `afterEach` so the stub never leaks into an unrelated test file.
 */
export function installCssHighlightStub(): { registry: FakeHighlightRegistry; restore(): void } {
  const registry = new FakeHighlightRegistry();
  const previousHighlight = (globalThis as { Highlight?: unknown }).Highlight;
  const previousRegistry = Object.getOwnPropertyDescriptor(CSS, "highlights");

  (globalThis as { Highlight?: unknown }).Highlight = FakeHighlight;
  Object.defineProperty(CSS, "highlights", { value: registry, configurable: true });

  return {
    registry,
    restore() {
      (globalThis as { Highlight?: unknown }).Highlight = previousHighlight;
      if (previousRegistry) Object.defineProperty(CSS, "highlights", previousRegistry);
      else delete (CSS as { highlights?: unknown }).highlights;
    },
  };
}
