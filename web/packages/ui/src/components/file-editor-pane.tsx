import { useCallback, useEffect, useState } from "react";
import { Eye, PenLine } from "lucide-react";

import { CodeMirrorEditor } from "@/components/code-mirror-editor";
import { FilePreview, previewKind } from "@/components/file-preview";
import type { WsClientLike } from "@/lib/ws-client";
import type { FileReadResult, Task } from "@/lib/types";

/**
 * The editor half of the old combined file-explorer pane, now mounted per
 * open file tab: file.read on mount/path change, a local edit buffer with
 * dirty tracking, and file.write on Save (button or Mod-s via
 * CodeMirrorEditor's onSave). Same `{ client, task }` contract as every
 * other task-scoped pane.
 *
 * Previewable files (.md/.svg/.html -- see file-preview.tsx's previewKind)
 * get an Edit/Preview toggle; the preview renders the current buffer
 * (unsaved changes visible, no auto-save), and CodeMirror stays mounted
 * and is merely hidden in preview mode so cursor/undo history survive the
 * toggle. Ported from the combined-pane editor view when the explorer
 * became tree-only (see the tab-registry plan's Decisions).
 */
export function FileEditorPane({ client, task, path }: { client: WsClientLike | null; task: Task; path: string }) {
  const [content, setContentState] = useState("");
  const [savedContent, setSavedContent] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  // null for non-previewable files -- the Edit/Preview control doesn't
  // render at all in that case, never as a disabled dead button.
  const kind = previewKind(path);
  const [mode, setMode] = useState<"edit" | "preview">("edit");
  // mode can outlive kind (preview a .md, then open a .ts tab): a stale
  // "preview" must not blank the editor for a file with no preview.
  const previewing = kind !== null && mode === "preview";

  useEffect(() => {
    if (!client) return;
    let cancelled = false;

    setContentState("");
    setSavedContent("");
    setLoading(true);
    setError(null);
    setSaveError(null);

    client
      .call<FileReadResult>("file.read", { taskId: task.ID, path })
      .then((result) => {
        if (cancelled) return;
        setContentState(result.content);
        setSavedContent(result.content);
        setLoading(false);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setLoading(false);
        setError(err instanceof Error ? err.message : String(err));
      });

    return () => {
      cancelled = true;
    };
    // task.ID (not task) is the intended dependency: a fresh Task object
    // with the same ID shouldn't discard the user's in-progress edits.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [client, task.ID, path]);

  const setContent = useCallback((next: string) => {
    setContentState(next);
  }, []);

  const save = useCallback(async () => {
    if (!client) throw new Error("not connected");

    setSaving(true);
    setSaveError(null);
    try {
      await client.call("file.write", { taskId: task.ID, path, content });
      setSavedContent(content);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err));
      throw err;
    } finally {
      setSaving(false);
    }
    // `content` must be read fresh at call time, so it's a real dependency.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [client, task.ID, path, content]);

  function handleSave() {
    save().catch(() => {
      // saveError is already surfaced via state; nothing more to do here.
    });
  }

  const dirty = content !== savedContent;

  return (
    <div className="flex h-full min-h-0 flex-col" data-testid="file-editor-pane">
      <div className="flex items-center justify-between gap-2 border-b px-3 py-2">
        <span className="truncate text-sm font-medium" data-testid="file-editor-path">
          {path}
          {dirty && <span aria-label="unsaved changes"> *</span>}
        </span>
        <div className="flex shrink-0 items-center gap-2">
          {kind && (
            <div className="flex h-7 items-center rounded-lg border border-input p-0.5" data-testid="preview-toggle">
              <button
                type="button"
                onClick={() => setMode("edit")}
                disabled={mode === "edit"}
                aria-pressed={mode === "edit"}
                className="flex h-6 items-center gap-1 rounded-md px-2 text-xs font-medium disabled:pointer-events-none disabled:bg-muted"
              >
                <PenLine className="size-3" />
                Edit
              </button>
              <button
                type="button"
                onClick={() => setMode("preview")}
                disabled={mode === "preview"}
                aria-pressed={mode === "preview"}
                className="flex h-6 items-center gap-1 rounded-md px-2 text-xs font-medium disabled:pointer-events-none disabled:bg-muted"
              >
                <Eye className="size-3" />
                Preview
              </button>
            </div>
          )}
          <button
            type="button"
            onClick={handleSave}
            disabled={saving || !dirty}
            className="h-7 shrink-0 rounded-lg border border-input bg-background px-2.5 text-xs font-medium hover:bg-muted disabled:pointer-events-none disabled:opacity-50"
          >
            {saving ? "Saving…" : "Save"}
          </button>
        </div>
      </div>
      {error && <p className="px-3 py-2 text-sm text-destructive">{error}</p>}
      {saveError && <p className="px-3 py-2 text-sm text-destructive">save failed: {saveError}</p>}
      {loading ? (
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">Loading…</div>
      ) : (
        !error && (
          <>
            {/*
              The editor stays mounted in preview mode, hidden via CSS:
              unmounting would destroy the EditorView and its cursor/
              selection/undo history (and the tests rely on
              editorViewRegistry lookups staying valid across the toggle).
              The preview only ever renders the buffer react has, which
              onChange keeps equal to the editor's live document -- unsaved
              edits included. Preview does not auto-save.
            */}
            <div className={previewing ? "hidden" : "flex h-full min-h-0 flex-col"}>
              <CodeMirrorEditor value={content} onChange={setContent} onSave={handleSave} testId="file-editor" />
            </div>
            {previewing && kind && <FilePreview kind={kind} content={content} path={path} />}
          </>
        )
      )}
    </div>
  );
}
