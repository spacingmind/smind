import { useCallback, useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { providerListQueryOptions } from "@/hooks/use-provider-catalog";
import { Icons } from "@/lib/icons";

import { DiffHeaderMenu } from "@/components/diff-header-menu";
import { DiffRender, type DiffAnnotation, type DiffOutputFormat } from "@/components/diff-render";
import { FileStatusMarker } from "@/components/file-status-marker";
import { PrAction } from "@/components/git/pr-action";
import { CommentComposer, ReviewAnnotationFrame, ReviewComments, ReviewDraftCard, type PendingComment } from "@/components/review-comments";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { InlineSpinner } from "@/components/ui/inline-spinner";
import { PaneHeader } from "@/components/ui/pane-header";
import { cn } from "@/lib/utils";
import { useIsMobile } from "@/hooks/use-mobile";
import { useTaskDiff } from "@/hooks/use-task-diff";
import { toast } from "@/components/ui/toast";
import { useFocusedChatId, useFocusedChatStore } from "@/stores/focused-chat-store";
import { AddToChatButton } from "@/components/add-to-chat-button";
import { useAddToChat } from "@/hooks/use-add-to-chat";
import type { MentionRef } from "@/lib/file-mention";
import { suggestCommitMessage } from "@/lib/commit-suggestion";
import { formatDiffStat } from "@/lib/diff-stat";
import { subscribeDiffReveal, takeDiffReveal } from "@/lib/diff-reveal";
import { filePathForElement, type DiffLineRef } from "@/lib/diff-lines";
import {
  addReviewDraft,
  buildReviewPrompt,
  clearReviewDrafts,
  useReviewDrafts,
  type ReviewDraft,
} from "@/lib/review-drafts";
import { loadDiffPrefs, saveDiffPrefs } from "@/lib/diff-prefs";
import { recipientRunParams, resolveReviewRecipient } from "@/lib/review-recipient";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { WsClientLike } from "@/lib/ws-client";
import type {
  Chat,
  Provider,
  RunStartResult,
  RunStatusEventPayload,
  Task,
  TaskCommitResult,
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
  chats,
}: {
  client: WsClientLike | null;
  task: Task;
  /** The app's single-subscription event stream; optional so existing tests/mounts render unchanged. A terminal run.status for this task refetches the files list. */
  events?: DaemonEvents | null;
  /** The task's active chats (App.tsx's chat.list) -- a review is addressed to one of them. Without any, "Submit review" is disabled. */
  chats?: Chat[] | null;
}) {
  const queryClient = useQueryClient();
  const isMobile = useIsMobile();
  const focusedChatId = useFocusedChatId(task.ID);
  const addToChat = useAddToChat(task.ID);
  const [files, setFiles] = useState<TaskFile[] | null>(null);
  const [fileStates, setFileStates] = useState<Record<string, FileState>>({});
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
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

  // Who the review goes to: the focused chat, with that chat's own run config (lib/review-recipient.ts).
  const recipientResult = resolveReviewRecipient({ taskId: task.ID, chats, focusedChatId, connected: client !== null });
  const recipient = recipientResult.kind === "ready" ? recipientResult.recipient : null;

  /**
   * Sends every draft as one prompt (Item 19: "submitted as a single
   * prompt back to the agent"), then clears them so the same review can't
   * be sent twice.
   *
   * The prompt goes to the *focused chat* (`chatId`), queued when that chat
   * is mid-run (`whenBusy: "queue"`, the composer's own default), under the
   * provider/model/permission settings that chat is using -- not "the
   * first provider the daemon lists" into whichever chat was oldest. Only
   * a chat that has never run and has no saved run config falls back to
   * the shared provider catalog's first entry, the composer's default.
   */
  const submitReview = async () => {
    if (!client || !recipient || drafts.length === 0) return;
    setSubmittingReview(true);
    setReviewError(null);
    try {
      let provider: Provider | null = recipient.provider;
      if (!provider) {
        provider = "claude-native";
        try {
          // The shared catalog's cache entry: served from the cache while it is fresh, re-read only if
          // it was never fetched or was invalidated -- never a second one-off fetch.
          const result = await queryClient.fetchQuery(providerListQueryOptions(client));
          if (result?.providers?.[0]) provider = result.providers[0].id;
        } catch {
          // provider.list failing shouldn't block the review -- the daemon's
          // own default is the same fallback the composer uses.
        }
      }
      const started = await client.call<RunStartResult>("run.start", {
        taskId: task.ID,
        chatId: recipient.chatId,
        provider,
        prompt: buildReviewPrompt(drafts),
        whenBusy: "queue",
        ...recipientRunParams(recipient),
      });
      const count = drafts.length;
      const chatName = recipient.title || "the chat";
      clearReviewDrafts(task.ID);
      setPending(null);
      // The drafts vanish from this tab and the run starts in another one: say so, and take the reviewer to the chat
      // that received it (a no-op when that chat is already in front).
      toast({
        variant: "success",
        title: "queued" in started ? `Review queued in ${chatName}` : `Review sent to ${chatName}`,
        description:
          `${count} comment${count === 1 ? "" : "s"}` + ("queued" in started ? " · it sends when the current run finishes" : ""),
      });
      useFocusedChatStore.getState().requestRevealChat(task.ID, recipient.chatId);
    } catch (err: unknown) {
      setReviewError(err instanceof Error ? err.message : String(err));
    } finally {
      setSubmittingReview(false);
    }
  };

  const refreshAll = () => {
    fetchFiles();
    wholeDiff.refresh();
  };

  const stagedCount = files?.filter((f) => f.staged).length ?? 0;
  // Item 8: a suggested message derived from the staged set itself
  // (lib/commit-suggestion.ts). Offered as a one-click fill rather than
  // placeholder text -- a placeholder vanishes on focus and can't be
  // acted on -- and only while the message is still empty, so it can
  // never overwrite something the user typed.
  const suggestion = suggestCommitMessage(files ?? []);
  const canCommit = stagedCount > 0 && message.trim() !== "" && !committing;
  const isEmpty = files !== null && files.length === 0;

  return (
    <div className="flex h-full flex-col" data-testid="diff-viewer-pane">
      <PaneHeader
        title="Diff"
        subtitle={
          <span
            data-testid="diff-stat"
            data-files={wholeDiff.stat.files}
            data-additions={wholeDiff.stat.additions}
            data-deletions={wholeDiff.stat.deletions}
            title={formatDiffStat(wholeDiff.stat)}
          >
            {wholeDiff.stat.files} file{wholeDiff.stat.files === 1 ? "" : "s"}{" "}
            <span className="text-diff-added">+{wholeDiff.stat.additions}</span>{" "}
            <span className="text-diff-removed">−{wholeDiff.stat.deletions}</span>
          </span>
        }
        actions={
          isMobile ? (
            <>
              <PrAction taskId={task.ID} workspaceId={task.WorkspaceID} title={task.Title} />
              <DiffHeaderMenu
                prefs={prefs}
                onChangePrefs={updatePrefs}
                onRefresh={refreshAll}
                refreshDisabled={!client || loading}
              />
            </>
          ) : (
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
              <Button type="button" variant="outline" size="sm" disabled={!client || loading} onClick={refreshAll}>
                Refresh
              </Button>
              <PrAction taskId={task.ID} workspaceId={task.WorkspaceID} title={task.Title} />
            </>
          )
        }
      />

      <div ref={listRef} className="flex-1 overflow-auto p-4" data-testid="diff-file-list">
        {error && (
          <p className="text-ui-base text-destructive" data-testid="diff-error">
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
                annotations={reviewAnnotations({
                  taskId: task.ID,
                  drafts,
                  pending,
                  withPath: true,
                  onCancelPending: () => setPending(null),
                  onAddDraft: addDraftFromPending,
                })}
                onSelectLine={(ref, element) => {
                  const path = filePathForElement(element);
                  if (path) selectLine(path, ref);
                }}
              />
            )}
            {wholeDiff.error && (
              <p className="text-ui-base text-destructive" data-testid="whole-diff-error">
                {wholeDiff.error}
              </p>
            )}
            {/* Until the diff is on screen the comments have no line to sit under: list them (DiffRender does the same for one it cannot place). */}
            {wholeDiff.diff === null && (
              <ReviewComments
                taskId={task.ID}
                drafts={drafts}
                pending={pending}
                onCancelPending={() => setPending(null)}
                onAddDraft={addDraftFromPending}
              />
            )}
          </div>
        )}

        {prefs.view === "by-file" &&
          files?.map((file) => (
            <FileRow
              onAddToChat={addToChat.canAdd ? addToChat.add : undefined}
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
        <div className="relative z-10 border-t bg-background px-4 py-2" data-testid="review-bar">
          {reviewError && (
            <Alert variant="error" description={`Submit failed: ${reviewError}`} className="mb-2" testId="review-error" />
          )}
          <div className="flex items-center gap-2">
            <Button
              type="button"
              size="sm"
              disabled={!client || !recipient || drafts.length === 0 || submittingReview}
              onClick={() => void submitReview()}
              data-testid="submit-review-button"
            >
              {submittingReview ? "Submitting…" : `Submit review (${drafts.length})`}
            </Button>
            <span className="min-w-0 truncate text-ui-sm text-foreground-muted" data-testid="review-recipient">
              {recipient
                ? `Sent as one prompt to ${recipient.title || "the chat"}, then cleared`
                : recipientResult.kind === "unavailable" && recipientResult.reason === "no-chats"
                  ? "No chat to send to"
                  : "Not connected"}
            </span>
          </div>
        </div>
      )}

      {/*
        Staging is per file (the checkboxes live on the By-file rows), so the commit bar belongs to that view: in the
        whole-diff view it had no checkboxes next to it to explain "Commit (0 staged)".
      */}
      {prefs.view === "by-file" && (
      <div className="relative z-10 border-t bg-background px-4 py-3" data-testid="commit-bar">
        {lastCommit && (
          <p className="mb-2 text-ui-base text-foreground-subtle" data-testid="commit-success">
            Committed {lastCommit.subject} ({lastCommit.commit.slice(0, 8)}) — {lastCommit.files} file
            {lastCommit.files === 1 ? "" : "s"}
          </p>
        )}
        {commitError && (
          <p className="mb-2 text-ui-base text-destructive" data-testid="commit-error">
            {commitError}
          </p>
        )}
        <textarea
          className="mb-2 w-full rounded border p-2 text-ui-base"
          rows={2}
          placeholder="Commit message…"
          value={message}
          onChange={(e) => setMessage(e.target.value)}
          data-testid="commit-message"
        />
        <div className="flex items-center gap-2">
          <Button type="button" size="sm" disabled={!canCommit} onClick={commit} data-testid="commit-button">
            {committing ? "Committing…" : `Commit (${stagedCount} staged)`}
          </Button>
          {suggestion && message.trim() === "" && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setMessage(suggestion)}
              data-testid="commit-suggestion"
            >
              Use “{suggestion}”
            </Button>
          )}
        </div>
      </div>
      )}
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
  onAddToChat,
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
  /** Present when the daemon accepts `file_ref` and a chat can receive it. */
  onAddToChat?: (ref: MentionRef) => void;
  disabled: boolean;
}) {
  const diff = state?.diff ?? null;

  return (
    <div className="mb-1 rounded-lg" data-testid={`diff-file-${file.path}`}>
      <div
        className={cn(
          "group flex h-8 items-center gap-2 rounded-lg px-2 transition-colors hover:bg-surface-hover",
          !collapsed && "bg-surface-hover",
        )}
      >
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
          aria-expanded={!collapsed}
          className="flex min-w-0 flex-1 items-center gap-1.5 text-left text-ui-base"
          onClick={onToggleCollapse}
          data-testid={`diff-file-header-${file.path}`}
        >
          <Icons.chevronRight
            aria-hidden="true"
            className={cn("size-3.5 shrink-0 text-foreground-subtle transition-transform", !collapsed && "rotate-90")}
          />
          <span className="min-w-0 flex-1 truncate">{file.path}</span>
          <FileStatusMarker status={file.status} />
        </button>
        {onAddToChat && (
          <AddToChatButton
            label="Add to chat"
            disabledReason={file.status === "deleted" ? "Deleted in this task" : undefined}
            onAdd={() => onAddToChat({ path: file.path })}
            testId={`add-to-chat-${file.path}`}
          />
        )}
        <label className="flex shrink-0 items-center gap-1 text-ui-sm text-foreground-subtle">
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
            <p className="text-ui-base text-foreground-subtle">Loading…</p>
          ) : diff === "" ? (
            <p className="text-ui-base text-foreground-subtle">No changes</p>
          ) : (
            <DiffRender
              diff={diff}
              outputFormat={outputFormat}
              onSelectLine={onSelectLine}
              annotations={reviewAnnotations({ taskId, drafts, pending, withPath: false, onCancelPending, onAddDraft })}
              renderHunkAction={
                onAddToChat && file.status !== "deleted"
                  ? (hunk) => (
                      <AddToChatButton
                        label="Add hunk to chat"
                        className="size-5"
                        onAdd={() => onAddToChat({ path: file.path, ...hunk })}
                        testId="add-hunk-to-chat"
                      />
                    )
                  : undefined
              }
              hideFileHeader
              testId={`diff-container-${file.path}`}
            />
          )}
        </div>
      )}
      {/* The diff shows the comments under their lines; with no diff on screen (collapsed, loading) they are listed here so a draft never looks lost. */}
      {(collapsed || !diff) && (
        <ReviewComments
          taskId={taskId}
          drafts={drafts}
          pending={pending}
          onCancelPending={onCancelPending}
          onAddDraft={onAddDraft}
        />
      )}
    </div>
  );
}

/**
 * The review drafts and the open composer as inline annotations for DiffRender: each sits right under the line it is
 * about. `withPath` is for the whole-diff render, which holds many files and finds the right one by name.
 */
function reviewAnnotations({
  taskId,
  drafts,
  pending,
  withPath,
  onCancelPending,
  onAddDraft,
}: {
  taskId: number;
  drafts: ReviewDraft[];
  pending: PendingComment | null;
  withPath: boolean;
  onCancelPending: () => void;
  onAddDraft: (body: string) => void;
}): DiffAnnotation[] {
  const out: DiffAnnotation[] = drafts.map((draft) => ({
    id: draft.id,
    path: withPath ? draft.path : undefined,
    side: draft.side,
    line: draft.line,
    text: draft.snippet,
    node: (
      <ReviewAnnotationFrame>
        <ReviewDraftCard taskId={taskId} draft={draft} />
      </ReviewAnnotationFrame>
    ),
  }));
  if (pending) {
    out.push({
      id: "pending-comment",
      path: withPath ? pending.path : undefined,
      side: pending.side,
      line: pending.line,
      text: pending.snippet,
      node: (
        <ReviewAnnotationFrame>
          <CommentComposer pending={pending} onCancel={onCancelPending} onAdd={onAddDraft} />
        </ReviewAnnotationFrame>
      ),
    });
  }
  return out;
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
      className="flex h-7 items-center rounded-lg border border-input-border p-0.5"
    >
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          onClick={() => onChange(option.value)}
          disabled={value === option.value}
          aria-pressed={value === option.value}
          data-testid={`${testId}-${option.value}`}
          className="flex h-6 items-center rounded-md px-2 text-ui-sm font-medium disabled:pointer-events-none disabled:bg-muted"
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}
