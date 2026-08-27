import { useEffect, useRef } from "react";
import { EditorView, basicSetup } from "codemirror";
import { keymap } from "@codemirror/view";

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
}: {
  value: string;
  onChange: (value: string) => void;
  onSave: () => void;
  testId?: string;
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

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const view = new EditorView({
      doc: value,
      extensions: [
        basicSetup,
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

    return () => {
      editorViewRegistry.delete(container);
      view.destroy();
      viewRef.current = null;
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
