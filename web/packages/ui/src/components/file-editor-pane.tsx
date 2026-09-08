import { useCallback, useEffect, useState } from "react";

import { CodeMirrorEditor } from "@/components/code-mirror-editor";
import type { WsClientLike } from "@/lib/ws-client";
import type { FileReadResult, Task } from "@/lib/types";

/**
 * The editor half of the old combined file-explorer pane, now mounted per
 * open file tab: file.read on mount/path change, a local edit buffer with
 * dirty tracking, and file.write on Save (button or Mod-s via
 * CodeMirrorEditor's onSave). Same `{ client, task }` contract as every
 * other task-scoped pane.
 */
export function FileEditorPane({ client, task, path }: { client: WsClientLike | null; task: Task; path: string }) {
  const [content, setContentState] = useState("");
  const [savedContent, setSavedContent] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

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
        <button
          type="button"
          onClick={handleSave}
          disabled={saving || !dirty}
          className="h-7 shrink-0 rounded-lg border border-input bg-background px-2.5 text-xs font-medium hover:bg-muted disabled:pointer-events-none disabled:opacity-50"
        >
          {saving ? "Saving…" : "Save"}
        </button>
      </div>
      {error && <p className="px-3 py-2 text-sm text-destructive">{error}</p>}
      {saveError && <p className="px-3 py-2 text-sm text-destructive">save failed: {saveError}</p>}
      {loading ? (
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">Loading…</div>
      ) : (
        !error && <CodeMirrorEditor value={content} onChange={setContent} onSave={handleSave} testId="file-editor" />
      )}
    </div>
  );
}
