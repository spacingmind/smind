import { useEffect, useRef } from "react";
import { EditorView, basicSetup } from "codemirror";
import { keymap } from "@codemirror/view";
import type { Extension } from "@codemirror/state";

/**
 * Maps a mounted CodeMirrorEditor's container element to its live
 * EditorView instance. This exists purely for tests: jsdom's contentEditable
 * support isn't reliable enough to trust a synthesized keystroke/input
 * event reaching CodeMirror's own DOM-mutation-observing input pipeline
 * (see codemirror's domobserver.ts), so component tests instead drive an
 * edit through the same public `view.dispatch` API a real keystroke
 * ultimately reaches -- exercising this component's reaction (the
 * updateListener -> onChange wiring below), not CodeMirror's own event
 * capture, which isn't this codebase's to test.
 */
export const editorViewRegistry = new WeakMap<HTMLElement, EditorView>();

/**
 * Chrome-level theming (background/foreground/gutter/cursor/selection) via
 * literal `var(...)` references into the app's own tokens, rather than a
 * fixed light or dark color set -- unlike xterm.js (see
 * terminal-pane.tsx/lib/terminal-theme.ts), CodeMirror's `EditorView.theme`
 * accepts arbitrary CSS values, so this needs no re-application when
 * `.dark` toggles: the browser's own cascade handles it for free, the same
 * way every other component's Tailwind classes do. This is chrome only --
 * syntax-highlighting colors (basicSetup's defaultHighlightStyle) are a
 * separate, static token set left alone here; ui-redesign-parity's Item 1
 * scope is "CodeMirror ... take[s] its palette from app tokens" for the
 * always-visible white-box-in-dark-mode bug, not full per-language syntax
 * theming.
 */
export const appChromeTheme = EditorView.theme({
  "&": {
    color: "var(--foreground)",
    backgroundColor: "var(--background)",
  },
  ".cm-content": {
    caretColor: "var(--foreground)",
  },
  ".cm-cursor, .cm-dropCursor": {
    borderLeftColor: "var(--foreground)",
  },
  "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection": {
    backgroundColor: "var(--accent)",
  },
  ".cm-activeLine": {
    backgroundColor: "var(--surface)",
  },
  ".cm-gutters": {
    backgroundColor: "var(--card)",
    color: "var(--foreground-muted)",
    border: "none",
  },
  ".cm-activeLineGutter": {
    backgroundColor: "var(--surface)",
  },
});

/**
 * A minimal CodeMirror 6 editor bound to `value`/`onChange` like a
 * controlled input, plus a Mod-s (Ctrl/Cmd-S) keybinding that calls
 * `onSave`. The EditorView is created once per mount and never recreated
 * on a `value` change from outside (that would drop cursor position,
 * selection, and undo history on every keystroke, since every keystroke
 * round-trips through onChange -> parent state -> back into `value`) --
 * external changes instead get synced in via a dispatched transaction, see
 * the second effect below.
 */
export function CodeMirrorEditor({
  value,
  onChange,
  onSave,
  testId,
  findExtension,
  onViewReady,
}: {
  value: string;
  onChange: (value: string) => void;
  onSave: () => void;
  testId?: string;
  /** Extra extension(s) folded into the view at creation time -- `file-editor-pane.tsx` passes `FileFindModel.extension` (AC2) here. Optional so every other caller/test is unaffected. */
  findExtension?: Extension;
  /** Called with the live `EditorView` once created, and with `null` on unmount -- how a Find model outside this component gets the view instance `openSearchPanel`/`findNext`/etc. need. */
  onViewReady?: (view: EditorView | null) => void;
}) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const viewRef = useRef<EditorView | null>(null);

  // Always-current callbacks, readable from the extensions below without
  // needing to tear down and recreate the EditorView whenever the parent
  // passes a new onChange/onSave function identity.
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;
  const onSaveRef = useRef(onSave);
  onSaveRef.current = onSave;
  const onViewReadyRef = useRef(onViewReady);
  onViewReadyRef.current = onViewReady;

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const view = new EditorView({
      doc: value,
      extensions: [
        basicSetup,
        appChromeTheme,
        ...(findExtension ? [findExtension] : []),
        keymap.of([
          {
            key: "Mod-s",
            preventDefault: true,
            run: () => {
              onSaveRef.current();
              return true;
            },
          },
        ]),
        EditorView.updateListener.of((update) => {
          if (update.docChanged) {
            onChangeRef.current(update.state.doc.toString());
          }
        }),
      ],
      parent: container,
    });
    viewRef.current = view;
    editorViewRegistry.set(container, view);
    onViewReadyRef.current?.(view);

    return () => {
      editorViewRegistry.delete(container);
      view.destroy();
      viewRef.current = null;
      onViewReadyRef.current?.(null);
    };
    // Intentionally mount-once: see the doc comment above for why `value`
    // changes are synced via the effect below instead of a dependency here.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Syncs an external `value` change (a different file selected, or a
  // fresh file.read landing) into the live document. Comparing against the
  // editor's own current doc before dispatching is what keeps this from
  // looping: a local edit already flows doc -> onChange -> parent state ->
  // back here as the very same string, so `current !== value` is false and
  // nothing is dispatched.
  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;
    const current = view.state.doc.toString();
    if (current !== value) {
      view.dispatch({ changes: { from: 0, to: current.length, insert: value } });
    }
  }, [value]);

  return <div ref={containerRef} data-testid={testId} className="h-full min-h-0 flex-1 overflow-auto text-sm [&_.cm-editor]:h-full" />;
}
