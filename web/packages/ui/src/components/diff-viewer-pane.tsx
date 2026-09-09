import { useCallback, useEffect, useRef, useState } from "react";

import { DiffRender, type DiffOutputFormat } from "@/components/diff-render";
import { ReviewComments, type PendingComment } from "@/components/review-comments";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { WsClientLike } from "@/lib/ws-client";
import type { RunStatusEventPayload, Task, TaskDiffResult } from "@/lib/types";

/**
 * A per-file review-and-commit surface for a task (ADR 0006): the task's
 * changed files (task.files) as a collapsible list, each rendering its own
 * task.fileDiff via diff2html exactly as the old single-blob view did, a
 * per-file stage/unstage checkbox (task.stage -- per-file, never per-hunk),
 * a local "viewed" indicator, and a commit bar. Commit sends task.commit
 * with the human-written message only -- this is deliberately a human-only
 * surface; the agent author/trailer path exists at the RPC layer but no UI
 * exposes it.
 *
 * Item 19 of the ui-redesign-parity plan adds the review layer on top: a
 * whole-diff view beside the per-file list, a unified/side-by-side
 * toggle, per-line draft comments submitted back to the agent as one
 * prompt, and a files/+/- stat. Per-*hunk* staging stays out of scope --
 * ADR 0006 deliberately collapses staged/unstaged/untracked into one
 * base->worktree diff, and Item 19 restates that.
 */
export function DiffViewerPane({
  client,
  task,
  events,
}: {
  client: WsClientLike | null;
  task: Task;
  /** The app's single-subscription event stream; optional so existing tests/mounts render unchanged. A terminal run.status for this task refetches the diff. */
  events?: DaemonEvents | null;
}) {
  const [diff, setDiff] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  // Per-file task.stage in-flight tracking -- the checkbox for a given
  // path disables only while *its own* call is outstanding (Item 2's
  // "Stage... gain in-flight labels/disabled states"), not the whole list.
  const [stagingPaths, setStagingPaths] = useState<Set<string>>(new Set());
  const [committing, setCommitting] = useState(false);
  const [commitError, setCommitError] = useState<string | null>(null);
  const [message, setMessage] = useState("");
  const [lastCommit, setLastCommit] = useState<TaskCommitResult | null>(null);
  const [creatingPR, setCreatingPR] = useState(false);
  const [prError, setPrError] = useState<string | null>(null);
  const [prUrl, setPrUrl] = useState<string | null>(null);
  // The path a pending "Reveal in diff" wants scrolled to, cleared as soon
  // as the scroll lands (it's a one-shot action, not a selection).
  const [revealPath, setRevealPath] = useState<string | null>(null);
  const listRef = useRef<HTMLDivElement | null>(null);

  // Item 19: the two view toggles, seeded from (and written back to)
  // localStorage so they survive this pane unmounting on a tab switch.
  const [prefs, setPrefs] = useState(loadDiffPrefs);
  function updatePrefs(patch: Partial<typeof prefs>): void {
    setPrefs((prev) => {
      const next = { ...prev, ...patch };
      saveDiffPrefs(next);
      return next;
    });
  }

  // The whole-diff render and the files/+/- stat both come off one
  // task.diff fetch -- see hooks/use-task-diff.ts.
  const wholeDiff = useTaskDiff(client, task, events);

  // Review drafts live outside React (lib/review-drafts.ts) so they
  // survive collapsing a file and switching tabs; `pending` is the
  // not-yet-written comment whose composer is open, which deliberately
  // does not.
  const drafts = useReviewDrafts(task.ID);
  const [pending, setPending] = useState<PendingComment | null>(null);
  const [submittingReview, setSubmittingReview] = useState(false);
  const [reviewError, setReviewError] = useState<string | null>(null);

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
  // (No per-write diff events exist; terminal run status is the right
  // trigger.) Registered on `events`, keyed on fetchDiff's own deps
  // (client, task.ID), so a task switch re-registers cleanly.
  useEffect(() => {
    if (!events) return;
    return events.subscribe("run.status", (payload) => {
      const p = payload as Partial<RunStatusEventPayload>;
      if (p.taskId !== task.ID) return;
      if (p.status !== "done" && p.status !== "error" && p.status !== "stopped") return;
      fetchDiff();
    });
  }, [events, fetchDiff, task.ID]);

  // Renders `diff` into containerRef via diff2html's DOM-based UI (rather
  // than dangerouslySetInnerHTML) so its own highlightCode() pass can run
  // against real DOM nodes afterward.
  useEffect(() => {
    if (!events) return;
    return events.subscribe("run.status", (payload) => {
      const p = payload as Partial<RunStatusEventPayload>;
      if (p.taskId !== task.ID) return;
      if (p.status !== "done" && p.status !== "error" && p.status !== "stopped") return;
      fetchFiles();
    });
  }, [events, fetchFiles, task.ID]);

  // "Reveal in diff" from the explorer's row menu (Item 17): expand the
  // requested file (even if the user had collapsed it) and scroll it into
  // view. The request is usually *latched* rather than live -- this pane
  // is unmounted while the Files tab is in front -- so the mount path
  // consumes takeDiffReveal(), and the subscription only covers the case
  // where this tab was already open.
  const reveal = useCallback((path: string) => {
    setCollapsed((prev) => (prev[path] ? { ...prev, [path]: false } : prev));
    setRevealPath(path);
  }, []);

  useEffect(() => {
    const latched = takeDiffReveal(task.ID);
    if (latched) reveal(latched.path);
    return subscribeDiffReveal((request) => {
      if (request.taskId === task.ID) reveal(request.path);
    });
  }, [reveal, task.ID]);

  // Scrolling has to wait for the row to exist: a reveal raised while
  // task.files is still in flight has nothing to scroll to yet, so the
  // request stays pending (rather than being dropped) until `files`
  // arrives and this effect re-runs. The lookup is scoped to this pane's
  // own list, not the document, so a second diff pane (Item 6's side
  // dock) can't have its row scrolled by the other one's request.
  useEffect(() => {
    if (!revealPath || !files) return;
    const row = listRef.current?.querySelector(`[data-testid="diff-file-${CSS.escape(revealPath)}"]`);
    row?.scrollIntoView?.({ block: "start" });
    setRevealPath(null);
  }, [revealPath, files]);

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
    setStagingPaths((prev) => new Set(prev).add(file.path));
    client
      .call("task.stage", { taskId: task.ID, path: file.path, staged })
      .then(() => {
        setFiles((prev) => prev?.map((f) => (f.path === file.path ? { ...f, staged } : f)) ?? null);
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        setStagingPaths((prev) => {
          const next = new Set(prev);
          next.delete(file.path);
          return next;
        });
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

  function selectLine(path: string, ref: DiffLineRef): void {
    setPending({ path, line: ref.line, side: ref.side, snippet: ref.text });
  }

  function addDraftFromPending(body: string): void {
    if (!pending) return;
    addReviewDraft(task.ID, { path: pending.path, line: pending.line, side: pending.side, snippet: pending.snippet, body });
    setPending(null);
  }

  /**
   * Sends every draft as one prompt (Item 19: "submitted as a single
   * prompt back to the agent"), then clears them so the same review can't
   * be sent twice.
   *
   * The provider is the daemon's first reported one (the same
   * daemon-derived list the composer uses -- never a hardcoded client-side
   * list, per audit-smind-current.md §10's guarantees). Choosing a
   * provider *per review* belongs to Item 10's composer toolbar; this
   * surface deliberately doesn't grow a second provider control.
   */
  const submitReview = async () => {
    if (!client || drafts.length === 0) return;
    setSubmittingReview(true);
    setReviewError(null);
    try {
      let provider: Provider = "claude-native";
      try {
        const result = await client.call<ProviderListResult>("provider.list");
        if (result?.providers?.[0]) provider = result.providers[0].id;
      } catch {
        // provider.list failing shouldn't block the review -- the daemon's
        // own default is the same fallback the composer uses.
      }
      await client.call<RunStartResult>("run.start", {
        taskId: task.ID,
        provider,
        prompt: buildReviewPrompt(drafts),
      });
      clearReviewDrafts(task.ID);
      setPending(null);
    } catch (err: unknown) {
      setReviewError(err instanceof Error ? err.message : String(err));
    } finally {
      setSubmittingReview(false);
    }
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
      <PaneHeader
        title="Diff"
        subtitle={
          <span data-testid="diff-stat" data-files={wholeDiff.stat.files} data-additions={wholeDiff.stat.additions} data-deletions={wholeDiff.stat.deletions}>
            {formatDiffStat(wholeDiff.stat)}
          </span>
        }
        actions={
          <>
            <SegmentedToggle
              testId="diff-view-toggle"
              label="Diff view"
              value={prefs.view}
              onChange={(view) => updatePrefs({ view })}
              options={[
                { value: "by-file", label: "By file" },
                { value: "whole", label: "Whole diff" },
              ]}
            />
            <SegmentedToggle
              testId="diff-format-toggle"
              label="Diff layout"
              value={prefs.format}
              onChange={(format) => updatePrefs({ format })}
              options={[
                { value: "line-by-line", label: "Unified" },
                { value: "side-by-side", label: "Side by side" },
              ]}
            />
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={!client || loading}
              onClick={() => {
                fetchFiles();
                wholeDiff.refresh();
              }}
            >
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
          </>
        }
      />

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

      <div ref={listRef} className="flex-1 overflow-auto p-4" data-testid="diff-file-list">
        {error && (
          <p className="text-sm text-destructive" data-testid="diff-error">
            {error}
          </p>
        )}
        {loading && files === null && <InlineSpinner label="Loading diff…" />}
        {!error && isEmpty && <EmptyState testId="diff-empty" title="No changes" />}

        {/*
          Whole-diff view: one render of task.diff, so a review can be read
          top to bottom without expanding files one at a time. It shares
          the per-file view's line-click-to-comment wiring -- the path
          comes from the clicked row's own file wrapper rather than from a
          React parent, since one render covers every file.
        */}
        {prefs.view === "whole" && !isEmpty && !error && (
          <div data-testid="whole-diff">
            {wholeDiff.diff === null ? (
              <InlineSpinner label="Loading diff…" />
            ) : (
              <DiffRender
                diff={wholeDiff.diff}
                outputFormat={prefs.format}
                testId="whole-diff-container"
                onSelectLine={(ref, element) => {
                  const path = filePathForElement(element);
                  if (path) selectLine(path, ref);
                }}
              />
            )}
            {wholeDiff.error && (
              <p className="text-sm text-destructive" data-testid="whole-diff-error">
                {wholeDiff.error}
              </p>
            )}
            <ReviewComments
              taskId={task.ID}
              drafts={drafts}
              pending={pending}
              onCancelPending={() => setPending(null)}
              onAddDraft={addDraftFromPending}
            />
          </div>
        )}

        {prefs.view === "by-file" &&
          files?.map((file) => (
            <FileRow
              key={file.path}
              taskId={task.ID}
              file={file}
              state={fileStates[file.path]}
              collapsed={collapsed[file.path] ?? false}
              outputFormat={prefs.format}
              drafts={drafts.filter((d) => d.path === file.path)}
              pending={pending?.path === file.path ? pending : null}
              onSelectLine={(ref) => selectLine(file.path, ref)}
              onCancelPending={() => setPending(null)}
              onAddDraft={addDraftFromPending}
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
              disabled={!client || stagingPaths.has(file.path)}
            />
          ))}
      </div>

      {(drafts.length > 0 || reviewError) && (
        <div className="border-t px-4 py-2" data-testid="review-bar">
          {reviewError && (
            <Alert variant="error" description={`Submit failed: ${reviewError}`} className="mb-2" testId="review-error" />
          )}
          <div className="flex items-center gap-2">
            <Button
              type="button"
              size="sm"
              disabled={!client || drafts.length === 0 || submittingReview}
              onClick={() => void submitReview()}
              data-testid="submit-review-button"
            >
              {submittingReview ? "Submitting…" : `Submit review (${drafts.length})`}
            </Button>
            <span className="text-xs text-foreground-muted">
              Sent as one prompt to the agent, then cleared
            </span>
          </div>
        </div>
      )}

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
          {committing ? "Committing…" : `Commit (${stagedCount} staged)`}
        </Button>
      </div>
    </div>
  );
}

/**
 * One collapsible file entry: header (stage checkbox, viewed toggle,
 * status, path), its diff2html render when expanded, and its review
 * drafts. Drafts render even while the file is *collapsed* -- Item 19's
 * scenario is that a draft survives collapsing, and hiding it would make
 * a surviving draft look lost.
 */
function FileRow({
  taskId,
  file,
  state,
  collapsed,
  outputFormat,
  drafts,
  pending,
  onSelectLine,
  onCancelPending,
  onAddDraft,
  onToggleCollapse,
  onStage,
  onMarkViewed,
  disabled,
}: {
  taskId: number;
  file: TaskFile;
  state: FileState | undefined;
  collapsed: boolean;
  outputFormat: DiffOutputFormat;
  drafts: ReviewDraft[];
  pending: PendingComment | null;
  onSelectLine: (line: DiffLineRef) => void;
  onCancelPending: () => void;
  onAddDraft: (body: string) => void;
  onToggleCollapse: () => void;
  onStage: (staged: boolean) => void;
  onMarkViewed: () => void;
  disabled: boolean;
}) {
  const diff = state?.diff ?? null;

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
            <p className="text-sm text-muted-foreground">No changes</p>
          ) : (
            <DiffRender
              diff={diff}
              outputFormat={outputFormat}
              onSelectLine={onSelectLine}
              testId={`diff-container-${file.path}`}
            />
          )}
        </div>
      )}
      <ReviewComments
        taskId={taskId}
        drafts={drafts}
        pending={pending}
        onCancelPending={onCancelPending}
        onAddDraft={onAddDraft}
      />
    </div>
  );
}

/**
 * A two-or-more-option toggle rendered as one bordered group of pressed
 * buttons -- the shape file-editor-pane.tsx's Edit/Preview control
 * already uses, generalized here for Item 19's two toggles rather than
 * hand-rolled a third and fourth time. `aria-pressed` plus the disabled
 * active option is what makes the current value readable both to a screen
 * reader and to a test.
 */
function SegmentedToggle<T extends string>({
  value,
  options,
  onChange,
  label,
  testId,
}: {
  value: T;
  options: { value: T; label: string }[];
  onChange: (value: T) => void;
  label: string;
  testId: string;
}) {
  return (
    <div
      role="group"
      aria-label={label}
      data-testid={testId}
      data-value={value}
      className="flex h-7 items-center rounded-lg border border-input p-0.5"
    >
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          onClick={() => onChange(option.value)}
          disabled={value === option.value}
          aria-pressed={value === option.value}
          data-testid={`${testId}-${option.value}`}
          className="flex h-6 items-center rounded-md px-2 text-xs font-medium disabled:pointer-events-none disabled:bg-muted"
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}
