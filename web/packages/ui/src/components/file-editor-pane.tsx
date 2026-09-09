import { useCallback, useEffect, useRef, useState } from "react";
import { Eye, PenLine } from "lucide-react";

import { CodeMirrorEditor } from "@/components/code-mirror-editor";
import { FilePreview, previewKind } from "@/components/file-preview";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { WsClientLike } from "@/lib/ws-client";
import { RpcError } from "@/lib/ws-client";
import type { FileReadResult, FileWriteResult, RunStatusEventPayload, Task } from "@/lib/types";

/**
 * The editor half of the old combined file-explorer pane, now mounted per
 * open file tab: file.read on mount/path change, a local edit buffer with
 * dirty tracking, and file.write on Save (button or Mod-s via
 * CodeMirrorEditor's onSave). Same `{ client, task }` contract as every
 * other task-scoped pane.
 *
 * Saves are conditional: file.write echoes the read-time mtime as
 * expectedMtime, so an agent rewriting the file under an open dirty
 * buffer is met with an inline conflict banner (changed-on-disk or
 * deleted-on-disk -- see the file-conflict-detection plan) instead of a
 * silent clobber. Both banner actions are explicit clicks; nothing
 * auto-reloads. A terminal run.status for this task re-probes the file
 * (re-read, compare mtimes) while the buffer is dirty, so the banner shows
 * before a save is even attempted -- the agent just finishing is the most
 * likely conflict moment.
 *
 * Previewable files (.md/.svg/.html -- see file-preview.tsx's previewKind)
 * get an Edit/Preview toggle; the preview renders the current buffer
 * (unsaved changes visible, no auto-save), and CodeMirror stays mounted
 * and is merely hidden in preview mode so cursor/undo history survive the
 * toggle. Ported from the combined-pane editor view when the explorer
 * became tree-only (see the tab-registry plan's Decisions).
 */
export function FileEditorPane({
  client,
  task,
  path,
  events,
}: {
  client: WsClientLike | null;
  task: Task;
  path: string;
  /** The app's single-subscription event stream; optional so existing tests/mounts render unchanged. A terminal run.status for this task re-probes a dirty buffer. */
  events?: DaemonEvents | null;
}) {
  const [content, setContentState] = useState("");
  const [savedContent, setSavedContent] = useState("");
  // The file's mtime as of the last read or clean save -- what a
  // conditional save echoes as expectedMtime.
  const [mtime, setMtime] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  // The conflict banner's state: null = no conflict; "changed" = the file
  // on disk moved since our read; "deleted" = it's gone. Set by a rejected
  // conditional save or by the run.status probe, cleared only by an
  // explicit Reload/Overwrite click (or a clean re-read on path change).
  const [conflict, setConflict] = useState<"changed" | "deleted" | null>(null);

  // null for non-previewable files -- the Edit/Preview control doesn't
  // render at all in that case, never as a disabled dead button.
  const kind = previewKind(path);
  const [mode, setMode] = useState<"edit" | "preview">("edit");
  // mode can outlive kind (preview a .md, then open a .ts tab): a stale
  // "preview" must not blank the editor for a file with no preview.
  const previewing = kind !== null && mode === "preview";

  // The dirty check the probe effects need without re-subscribing on every
  // keystroke (content changes ~per keystroke; dirty depends on it).
  const dirtyRef = useRef(false);
  const dirty = content !== savedContent;
  dirtyRef.current = dirty;

  const applyRead = useCallback((result: FileReadResult) => {
    setContentState(result.content);
    setSavedContent(result.content);
    setMtime(result.mtime);
    setConflict(null);
    setLoading(false);
  }, []);

  useEffect(() => {
    if (!client) return;
    let cancelled = false;

    setContentState("");
    setSavedContent("");
    setMtime(null);
    setLoading(true);
    setError(null);
    setSaveError(null);
    setConflict(null);

    client
      .call<FileReadResult>("file.read", { taskId: task.ID, path })
      .then((result) => {
        if (cancelled) return;
        applyRead(result);
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
  }, [client, task.ID, path, applyRead]);

  const setContent = useCallback((next: string) => {
    setContentState(next);
  }, []);

  // One file.write against the buffer. `expected` false is the Overwrite
  // action's unconditional force save (and last-write-wins legacy
  // behavior); a rejected conditional save maps the typed error's code to
  // the banner's state rather than the generic save-failed line.
  const save = useCallback(
    async (expected: boolean) => {
      if (!client) throw new Error("not connected");

      setSaving(true);
      setSaveError(null);
      try {
        const params: Record<string, unknown> = { taskId: task.ID, path, content };
        if (expected && mtime !== null) params.expectedMtime = mtime;
        const result = await client.call<FileWriteResult>("file.write", params);
        setSavedContent(content);
        setMtime(result.mtime);
        setConflict(null);
      } catch (err) {
        if (err instanceof RpcError && (err.code === "conflict" || err.code === "conflict_deleted")) {
          setConflict(err.code === "conflict_deleted" ? "deleted" : "changed");
        } else {
          setSaveError(err instanceof Error ? err.message : String(err));
        }
        throw err;
      } finally {
        setSaving(false);
      }
      // `content` must be read fresh at call time, so it's a real dependency.
      // eslint-disable-next-line react-hooks/exhaustive-deps
    },
    [client, task.ID, path, content, mtime],
  );

  function handleSave() {
    save(true).catch(() => {
      // conflict/saveError are already surfaced via state; nothing more to do here.
    });
  }

  // Reload: discard the buffer, re-read from disk (both banner states).
  function handleReload() {
    if (!client) return;
    setLoading(true);
    setError(null);
    setSaveError(null);
    client
      .call<FileReadResult>("file.read", { taskId: task.ID, path })
      .then(applyRead)
      .catch((err: unknown) => {
        setLoading(false);
        // Deleted-on-disk Reload has nothing to re-read: keep the banner
        // down (the read error below is the honest state) and show the
        // error line instead of the editor.
        setConflict(null);
        setError(err instanceof Error ? err.message : String(err));
      });
  }

  // Overwrite: force the buffer to disk unconditionally, banner down.
  function handleOverwrite() {
    save(false).catch(() => {
      // surfaced via state
    });
  }

  // Conflict probe: a terminal run.status for this task (the agent just
  // stopped touching files) re-reads the file while the buffer is dirty
  // and compares mtimes -- drift or a vanished file shows the banner
  // before any save attempt. Clean buffers need nothing (the mount read
  // already holds the disk state). Purely advisory: a lost race just
  // means the banner appears on save instead.
  const probe = useCallback(() => {
    if (!client || !dirtyRef.current || mtime === null) return;
    client
      .call<FileReadResult>("file.read", { taskId: task.ID, path })
      .then((result) => {
        if (result.mtime !== mtime) setConflict("changed");
      })
      .catch(() => {
        // The read failing on a probe means the file is most likely gone
        // (its error path); either way, a stale banner would be worse than
        // none -- the conditional save still catches it authoritatively.
        setConflict("deleted");
      });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [client, task.ID, path, mtime]);

  useEffect(() => {
    if (!events) return;
    return events.subscribe("run.status", (payload) => {
      const p = payload as Partial<RunStatusEventPayload>;
      if (p.taskId !== task.ID) return;
      if (p.status !== "done" && p.status !== "error" && p.status !== "stopped") return;
      probe();
    });
  }, [events, task.ID, probe]);

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
      {conflict && (
        <div
          className="flex flex-wrap items-center justify-between gap-2 border-b bg-amber-500/10 px-3 py-2 text-sm"
          data-testid="file-conflict-banner"
          data-conflict={conflict}
          role="alert"
        >
          <span className="text-amber-900">
            {conflict === "deleted"
              ? "This file was deleted on disk (unsaved edits are still in the editor)."
              : "This file changed on disk while you had unsaved edits."}
          </span>
          <div className="flex shrink-0 items-center gap-2">
            <button
              type="button"
              onClick={handleReload}
              className="h-7 rounded-lg border border-input bg-background px-2.5 text-xs font-medium hover:bg-muted"
              data-testid="conflict-reload"
            >
              Reload
            </button>
            <button
              type="button"
              onClick={handleOverwrite}
              className="h-7 rounded-lg border border-input bg-background px-2.5 text-xs font-medium hover:bg-muted"
              data-testid="conflict-overwrite"
            >
              Overwrite
            </button>
          </div>
        </div>
      )}
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
