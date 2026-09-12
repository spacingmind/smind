import { useCallback, useEffect, useRef, useState } from "react";
import { Diff2HtmlUI } from "diff2html/lib/ui/js/diff2html-ui.js";
import "diff2html/bundles/css/diff2html.min.css";
import "highlight.js/styles/github.css";

import { Button } from "@/components/ui/button";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { WsClientLike } from "@/lib/ws-client";
import type {
  RunStatusEventPayload,
  Task,
  TaskCommitResult,
  TaskCreatePrResult,
  TaskFile,
  TaskFileDiffResult,
  TaskFilesResult,
} from "@/lib/types";

/** One file row's view state: fetched diff text, viewed flag (local only this pass), open/collapsed. */
interface FileState {
  diff: string | null;
  viewed: boolean;
}

/**
 * A per-file review-and-commit surface for a task (ADR 0006): the task's
 * changed files (task.files) as a collapsible list, each rendering its own
 * task.fileDiff via diff2html exactly as the old single-blob view did, a
 * per-file stage/unstage checkbox (task.stage -- per-file, never per-hunk),
 * a local "viewed" indicator, and a commit bar. Commit sends task.commit
 * with the human-written message only -- this is deliberately a human-only
 * surface; the agent author/trailer path exists at the RPC layer but no UI
 * exposes it.
 */
export function DiffViewerPane({
  client,
  task,
  events,
}: {
  client: WsClientLike | null;
  task: Task;
  /** The app's single-subscription event stream; optional so existing tests/mounts render unchanged. A terminal run.status for this task refetches the files list. */
  events?: DaemonEvents | null;
}) {
  const [files, setFiles] = useState<TaskFile[] | null>(null);
  const [fileStates, setFileStates] = useState<Record<string, FileState>>({});
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [committing, setCommitting] = useState(false);
  const [commitError, setCommitError] = useState<string | null>(null);
  const [message, setMessage] = useState("");
  const [lastCommit, setLastCommit] = useState<TaskCommitResult | null>(null);
  const [creatingPR, setCreatingPR] = useState(false);
  const [prError, setPrError] = useState<string | null>(null);
  const [prUrl, setPrUrl] = useState<string | null>(null);

  const fetchFiles = useCallback(() => {
    if (!client) return;
    setLoading(true);
    setError(null);
    client
      .call<TaskFilesResult>("task.files", { taskId: task.ID })
      .then((result) => {
        setFiles(result.files ?? []);
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        setLoading(false);
      });
  }, [client, task.ID]);

  useEffect(() => {
    setFiles(null);
    setFileStates({});
    setCollapsed({});
    setError(null);
    setLastCommit(null);
    setPrError(null);
    setPrUrl(null);
    fetchFiles();
  }, [fetchFiles]);

  // Live refresh: a terminal run.status (done/error/stopped) for the
  // viewed task means the agent just stopped changing files -- refetch.
  useEffect(() => {
    if (!events) return;
    return events.subscribe("run.status", (payload) => {
      const p = payload as Partial<RunStatusEventPayload>;
      if (p.taskId !== task.ID) return;
      if (p.status !== "done" && p.status !== "error" && p.status !== "stopped") return;
      fetchFiles();
    });
  }, [events, fetchFiles, task.ID]);

  const fetchFileDiff = useCallback(
    (path: string) => {
      if (!client) return;
      client
        .call<TaskFileDiffResult>("task.fileDiff", { taskId: task.ID, path })
        .then((result) => {
          setFileStates((prev) => ({ ...prev, [path]: { diff: result.diff, viewed: prev[path]?.viewed ?? false } }));
        })
        .catch((err: unknown) => {
          setError(err instanceof Error ? err.message : String(err));
        });
    },
    [client, task.ID],
  );

  // Fetch the per-file diff of every expanded file that doesn't have one
  // yet (freshly listed, or re-listed after a commit/refresh -- diff text
  // is refetched by clearing fileStates whenever files change identity).
  useEffect(() => {
    if (!files) return;
    for (const f of files) {
      if (collapsed[f.path]) continue;
      if (fileStates[f.path]?.diff == null) fetchFileDiff(f.path);
    }
  }, [files, collapsed, fileStates, fetchFileDiff]);

  const toggleStage = (file: TaskFile, staged: boolean) => {
    if (!client) return;
    setError(null);
    client
      .call("task.stage", { taskId: task.ID, path: file.path, staged })
      .then(() => {
        setFiles((prev) => prev?.map((f) => (f.path === file.path ? { ...f, staged } : f)) ?? null);
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : String(err));
      });
  };

  const commit = () => {
    if (!client) return;
    setCommitting(true);
    setCommitError(null);
    client
      .call<TaskCommitResult>("task.commit", { taskId: task.ID, message, author: "human" })
      .then((result) => {
        setLastCommit(result);
        setMessage("");
        setFileStates({});
        fetchFiles();
      })
      .catch((err: unknown) => {
        setCommitError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        setCommitting(false);
      });
  };

  const createPR = () => {
    if (!client) return;
    setCreatingPR(true);
    setPrError(null);
    client
      .call<TaskCreatePrResult>("task.createPr", { taskId: task.ID })
      .then((result) => {
        setPrUrl(result.url);
      })
      .catch((err: unknown) => {
        setPrError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        setCreatingPR(false);
      });
  };

  const stagedCount = files?.filter((f) => f.staged).length ?? 0;
  const canCommit = stagedCount > 0 && message.trim() !== "" && !committing;
  const isEmpty = files !== null && files.length === 0;

  return (
    <div className="flex h-full flex-col" data-testid="diff-viewer-pane">
      <div className="flex items-center justify-between border-b px-4 py-3">
        <h2 className="text-sm font-semibold">Diff</h2>
        <div className="flex items-center gap-2">
          <Button type="button" variant="outline" size="sm" disabled={!client || loading} onClick={fetchFiles}>
            Refresh
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={!client || creatingPR}
            onClick={createPR}
            data-testid="create-pr-button"
          >
            {creatingPR ? "Creating PR…" : "Create PR"}
          </Button>
        </div>
      </div>

      {(prUrl || prError) && (
        <div className="border-b px-4 py-2">
          {prUrl && (
            <p className="text-sm" data-testid="pr-url">
              PR opened:{" "}
              <a href={prUrl} target="_blank" rel="noreferrer" className="underline" data-testid="pr-url-link">
                {prUrl}
              </a>
            </p>
          )}
          {prError && (
            <p className="text-sm text-destructive" data-testid="pr-error">
              {prError}
            </p>
          )}
        </div>
      )}

      <div className="flex-1 overflow-auto p-4" data-testid="diff-file-list">
        {error && (
          <p className="text-sm text-destructive" data-testid="diff-error">
            {error}
          </p>
        )}
        {loading && files === null && <p className="text-sm text-muted-foreground">Loading diff…</p>}
        {!error && isEmpty && (
          <p className="text-sm text-muted-foreground" data-testid="diff-empty">
            No changes.
          </p>
        )}
        {files?.map((file) => (
            <FileRow
              key={file.path}
              file={file}
              state={fileStates[file.path]}
              collapsed={collapsed[file.path] ?? false}
              onToggleCollapse={() =>
                setCollapsed((prev) => ({ ...prev, [file.path]: !(prev[file.path] ?? false) }))
              }
              onStage={(staged) => toggleStage(file, staged)}
              onMarkViewed={() =>
                setFileStates((prev) => ({
                  ...prev,
                  [file.path]: { diff: prev[file.path]?.diff ?? null, viewed: true },
                }))
              }
              disabled={!client}
            />
          ))}
      </div>

      <div className="border-t px-4 py-3" data-testid="commit-bar">
        {lastCommit && (
          <p className="mb-2 text-sm text-muted-foreground" data-testid="commit-success">
            Committed {lastCommit.subject} ({lastCommit.commit.slice(0, 8)}) — {lastCommit.files} file
            {lastCommit.files === 1 ? "" : "s"}
          </p>
        )}
        {commitError && (
          <p className="mb-2 text-sm text-destructive" data-testid="commit-error">
            {commitError}
          </p>
        )}
        <textarea
          className="mb-2 w-full rounded border p-2 text-sm"
          rows={2}
          placeholder="Commit message…"
          value={message}
          onChange={(e) => setMessage(e.target.value)}
          data-testid="commit-message"
        />
        <Button type="button" size="sm" disabled={!canCommit} onClick={commit} data-testid="commit-button">
          Commit ({stagedCount} staged)
        </Button>
      </div>
    </div>
  );
}

/** One collapsible file entry: header (stage checkbox, viewed toggle, status, path) plus its diff2html render when expanded. */
function FileRow({
  file,
  state,
  collapsed,
  onToggleCollapse,
  onStage,
  onMarkViewed,
  disabled,
}: {
  file: TaskFile;
  state: FileState | undefined;
  collapsed: boolean;
  onToggleCollapse: () => void;
  onStage: (staged: boolean) => void;
  onMarkViewed: () => void;
  disabled: boolean;
}) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const diff = state?.diff ?? null;

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    el.innerHTML = "";
    if (!diff) return;
    const ui = new Diff2HtmlUI(el, diff, {
      outputFormat: "side-by-side",
      drawFileList: false,
      matching: "lines",
      highlight: true,
    });
    ui.draw();
    ui.highlightCode();
  }, [diff]);

  return (
    <div className="mb-2" data-testid={`diff-file-${file.path}`}>
      <div className="flex items-center gap-2">
        <input
          type="checkbox"
          checked={file.staged}
          disabled={disabled}
          onChange={(e) => onStage(e.target.checked)}
          aria-label={`Stage ${file.path}`}
          data-testid={`stage-${file.path}`}
        />
        <button
          type="button"
          className="flex-1 text-left text-sm font-medium"
          onClick={onToggleCollapse}
          data-testid={`diff-file-header-${file.path}`}
        >
          {collapsed ? "▸" : "▾"} {file.path} <span className="text-muted-foreground">({file.status})</span>
        </button>
        <label className="flex items-center gap-1 text-xs text-muted-foreground">
          <input
            type="checkbox"
            checked={state?.viewed ?? false}
            onChange={onMarkViewed}
            data-testid={`viewed-${file.path}`}
          />
          viewed
        </label>
      </div>
      {!collapsed && (
        <div className="mt-1">
          {diff === null ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : diff === "" ? (
            <p className="text-sm text-muted-foreground">No changes.</p>
          ) : (
            <div ref={containerRef} data-testid={`diff-container-${file.path}`} />
          )}
        </div>
      )}
    </div>
  );
}
