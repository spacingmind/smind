export {};

/**
 * This project's TypeScript lib snapshot models `CSS.highlights` as a
 * `HighlightRegistry` with only `.forEach` -- missing the `Map`-shaped
 * `set`/`delete`/`get`/`has`/`clear` the CSS Custom Highlight API spec
 * actually gives it (https://drafts.csswg.org/css-highlight-api-1/), which
 * `chat-find-dom.ts` needs. Declaration-merged onto the existing global
 * interface rather than redefined, so this only adds members.
 */
declare global {
  interface HighlightRegistry {
    set(name: string, highlight: Highlight): HighlightRegistry;
    delete(name: string): boolean;
    get(name: string): Highlight | undefined;
    has(name: string): boolean;
    clear(): void;
    readonly size: number;
  }
}
