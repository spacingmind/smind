import { useCallback, useEffect, useMemo, useRef, useSyncExternalStore, type RefObject } from "react";
import type { EditorView } from "@codemirror/view";

import { FindBar, type FindBarHandle } from "@/components/find/find-bar";
import { FileFindModel } from "@/components/file-editor-find";
import { useActionHandler } from "@/keyboard/keyboard-provider";
import { cn } from "@/lib/utils";

export { FileFindModel } from "@/components/file-editor-find";

/**
 * File Find's React half (AC2): claims `pane.find` while the editor pane is
 * focused, drives `FileFindModel` from the shared `FindBar`, and floats the
 * bar over the editor -- flipping corners when the model says the active
 * match would otherwise sit underneath it. Ported from Paseo's
 * `file-pane/find/index.web.tsx`, rewritten for Tailwind instead of
 * `react-native-unistyles`.
 */
export function FileEditorFindBar({
  model,
  editor,
  focused,
}: {
  model: FileFindModel;
  /** The live CodeMirror view, set by `CodeMirrorEditor`'s `onViewReady` -- null until the editor mounts, and while a save/read is still loading. */
  editor: RefObject<EditorView | null>;
  /** Whether this pane currently holds focus -- gates the `pane.find` claim, per `use-pane-focus-within.ts`. */
  focused: boolean;
}) {
  const state = useSyncExternalStore(model.subscribe, model.getSnapshot, model.getSnapshot);
  const barRef = useRef<FindBarHandle>(null);

  const openAndFocus = useCallback(() => {
    model.open(editor.current);
    barRef.current?.focus();
  }, [model, editor]);
  useActionHandler("pane.find", openAndFocus, { enabled: focused });

  useEffect(() => {
    if (state.open) barRef.current?.focus();
  }, [state.open]);

  const replace = useMemo(
    () =>
      state.readOnly
        ? undefined
        : {
            value: state.replacement,
            onChange: model.setReplacement,
            onReplace: model.replace,
            onReplaceAll: model.replaceAll,
          },
    [model, state.readOnly, state.replacement],
  );

  if (!state.open) return null;

  const total = `${state.total}${state.limited ? "+" : ""}`;
  let status = "";
  if (state.query) {
    if (state.total === 0) status = "No matches";
    else if (state.current) status = `${state.current}/${total}`;
    else status = total;
  }

  return (
    <div
      ref={model.setWidgetNode}
      data-testid="file-find-bar"
      className={cn(
        "absolute right-2 left-2 z-10 flex justify-end",
        state.placement === "top" ? "top-2" : "bottom-2",
      )}
    >
      <FindBar
        ref={barRef}
        query={state.query}
        status={status}
        canNavigate={state.total > 0}
        onQueryChange={model.setSearch}
        onNext={model.next}
        onPrevious={model.previous}
        onClose={model.close}
        replace={replace}
      />
    </div>
  );
}
