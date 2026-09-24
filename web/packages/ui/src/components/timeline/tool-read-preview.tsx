import { useEffect, useRef } from "react";
import { EditorState } from "@codemirror/state";
import { defaultHighlightStyle, syntaxHighlighting } from "@codemirror/language";
import { EditorView, highlightActiveLineGutter, lineNumbers } from "@codemirror/view";

import { appChromeTheme, editorViewRegistry } from "@/components/code-mirror-editor";

/**
 * A read-only, line-numbered window onto a `read`-intent tool call's file
 * content -- reuses CodeMirror's own gutter and read-only mode plus the
 * app's chrome theme (`code-mirror-editor.tsx`) instead of a second
 * syntax-highlighter dependency or a hand-rolled `<pre>` with a synthetic
 * line-number column. Deliberately a smaller extension set than the full
 * editor's `basicSetup` -- history/autocomplete/bracket-matching don't
 * apply to a non-editable preview.
 *
 * `startLine` offsets the gutter to match the file's real line numbers
 * when the call only read a slice (`offset`/`start_line` in its input),
 * not the 1-based numbering of the slice itself.
 */
export function ToolReadPreview({ content, startLine = 1, testId }: { content: string; startLine?: number; testId?: string }) {
  const containerRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const view = new EditorView({
      doc: content,
      extensions: [
        lineNumbers({ formatNumber: (n) => String(n + startLine - 1) }),
        highlightActiveLineGutter(),
        syntaxHighlighting(defaultHighlightStyle),
        EditorView.lineWrapping,
        EditorState.readOnly.of(true),
        EditorView.editable.of(false),
        appChromeTheme,
      ],
      parent: container,
    });
    editorViewRegistry.set(container, view);

    return () => {
      editorViewRegistry.delete(container);
      view.destroy();
    };
    // Recreated whenever the content/range changes rather than synced via
    // a dispatched transaction (contrast CodeMirrorEditor): a read-only
    // preview has no cursor/selection/undo history worth preserving across
    // a different tool call's result landing.
  }, [content, startLine]);

  return (
    <div
      ref={containerRef}
      data-testid={testId}
      className="mt-0.5 max-h-64 overflow-auto rounded border text-ui-sm font-medium [&_.cm-editor]:h-full"
    />
  );
}
