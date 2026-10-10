import { useEffect, useMemo, useState } from "react";

import { toast } from "@/components/ui/toast";
import { CONNECTION_CHANGED_EVENT, FILE_MANAGER_EDITOR_ID } from "@/lib/local-paths";
import { desktop, isDesktop, type EditorInfo } from "@/lib/platform";

/** What the Reveal / Open-in-editor menu items need (desktop-native-feel D4.1). */
export interface LocalPathActions {
  /** "Reveal in Finder" / "Reveal in File Explorer" / ... for the platform's file manager. */
  revealLabel: string;
  /** Installed editors only -- the file manager is `reveal`'s, not listed here. */
  editors: EditorInfo[];
  /** Shows `path` in the platform file manager. `path` must be absolute. */
  reveal(path: string): void;
  /** Opens `absolutePath` in the editor with `editorId` (an id from `editors`). */
  open(editorId: string, absolutePath: string): void;
}

/**
 * Local-path actions, or `null` when they don't apply: a browser build,
 * or a desktop build whose current connection isn't the local daemon (a
 * url/relay daemon's paths aren't on this machine, so the items must be
 * absent rather than disabled). Read on mount and again whenever the connection changes; best-effort --
 * a failure to learn the connection or the editor list just means no
 * items, the same posture as the daemon-version banner. Call it once per
 * surface and pass the result down, not per row.
 */
export function useLocalPathActions(): LocalPathActions | null {
  const [state, setState] = useState<{ fileManager: EditorInfo; editors: EditorInfo[] } | null>(null);

  useEffect(() => {
    if (!isDesktop) return;
    let cancelled = false;
    const load = () => {
      (async () => {
        const current = await desktop.getCurrentConnection();
        if (cancelled) return;
        if (current.kind !== "local") {
          setState(null);
          return;
        }
        const list = await desktop.editorsList();
        const fileManager =
          list.find((e) => e.id === FILE_MANAGER_EDITOR_ID) ?? list.find((e) => e.kind === "fileManager");
        if (cancelled) return;
        setState(fileManager ? { fileManager, editors: list.filter((e) => e.kind === "editor") } : null);
      })().catch(() => {
        // Best-effort: these items are a convenience, never load-bearing.
        if (!cancelled) setState(null);
      });
    };
    load();
    // Switching connections doesn't reload the page, so re-read the kind.
    window.addEventListener(CONNECTION_CHANGED_EVENT, load);
    return () => {
      cancelled = true;
      window.removeEventListener(CONNECTION_CHANGED_EVENT, load);
    };
  }, []);

  return useMemo(() => {
    if (!state) return null;
    const run = (editor: EditorInfo, path: string) => {
      desktop.openInEditor(editor.id, path).catch((err: unknown) => {
        toast({
          variant: "error",
          title: editor.kind === "fileManager" ? `Couldn't reveal in ${editor.label}` : `Couldn't open in ${editor.label}`,
          description: err instanceof Error ? err.message : String(err),
        });
      });
    };
    return {
      revealLabel: `Reveal in ${state.fileManager.label}`,
      editors: state.editors,
      reveal: (path) => run(state.fileManager, path),
      open: (editorId, absolutePath) => {
        const editor = state.editors.find((e) => e.id === editorId);
        if (editor) run(editor, absolutePath);
      },
    };
  }, [state]);
}
