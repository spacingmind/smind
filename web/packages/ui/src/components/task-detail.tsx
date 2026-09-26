import { useCallback, useEffect, useRef, useState } from "react";
import { ArrowDown } from "lucide-react";

import { ApprovalPolicyControl } from "@/components/approval-policy-control";
import { Composer } from "@/components/composer/composer";
import type { RunConfigState } from "@/components/composer/run-config-toolbar";
import { FindBar } from "@/components/find/find-bar";
import { PermissionCard } from "@/components/permission/permission-card";
import { useDetailLevel } from "@/components/timeline/detail-level";
import { RunTimeline } from "@/components/timeline/run-timeline";
import { useAutoFollow } from "@/components/timeline/use-auto-follow";
import { useChatFind } from "@/components/timeline/use-chat-find";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { InlineSpinner } from "@/components/ui/inline-spinner";
import { PaneHeader } from "@/components/ui/pane-header";
import { useRunConfigOptions } from "@/hooks/use-run-config-options";
import { useRunTimeline } from "@/hooks/use-run-timeline";
import { useTaskDiff } from "@/hooks/use-task-diff";
import { approvalPolicyLabel } from "@/lib/approval-policies";
import type { ConnectionStatus } from "@/lib/reconnect";
import { thinkingLevelLabel } from "@/lib/thinking-levels";
import type { AgentProfile, Task, ProviderListResult } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

/** The task header's run-config pill text (AC's "<Agent> · <Provider> · <Approval> · <Thinking>" format) -- pure so it can be unit-tested without mounting the whole pane. */
export function runConfigPillLabel(
  state: RunConfigState,
  profiles: AgentProfile[],
  providerLabels: Record<string, string>,
): string {
  const agent = state.baseAgentId ? profiles.find((p) => String(p.ID) === state.baseAgentId) : undefined;
  const agentSegment = agent ? agent.Name : state.custom ? "Custom" : "No agent";
  const parts = [agentSegment, providerLabels[state.provider] ?? state.provider, approvalPolicyLabel(state.approvalPolicy)];
  if (state.provider === "claude-native") parts.push(thinkingLevelLabel(state.thinkingLevel));
  return parts.join(" · ");
}

/**
 * The main-content pane for a selected task: identity header, a chat-log
 * timeline of its runs (history plus any still-live streaming), and the
 * composer that starts new runs via run.start + run.attach (never
 * task.prompt -- see the hook this delegates to for why). All data comes
 * from useRunTimeline; this component is presentation plus the wiring
 * that tells the composer which run (if any) is currently live.
 */
export function TaskDetailPane({
  client,
  task,
  connectionStatus = "connected",
  onOpenFile,
  onOpenDiffTab,
}: {
  client: WsClientLike | null;
  task: Task;
  /** Real-time connection status from App.tsx -- lets an active run.attach subscription visibly reflect a break instead of silently freezing on stale "live" output. Defaults to "connected" so every existing caller/test not wired up to App.tsx's status keeps behaving exactly as before. */
  connectionStatus?: ConnectionStatus;
  /** Opens a worktree-relative path as a file tab. Optional: without it, a tool-call card naming a file simply isn't click-through. */
  onOpenFile?: (path: string) => void;
  /** Brings the task's Diff tab forward -- the composer's diff-stat pill's click target. Optional: without it (no tab strip above) the pill is omitted. */
  onOpenDiffTab?: () => void;
}) {
  const { runs, error, submitPrompt, stopRun, respondPermission, setApprovalPolicy, retryWithHigherEffort } = useRunTimeline(
    client,
    task.ID,
  );
  // provider.list's id→label map for the run headers ("label ?? id",
  // run-config IA): run entries carry only the wire provider id, and the
  // header should say "Claude Code", not "claude-native". Empty until the
  // fetch resolves -- RunTimeline falls back to the raw id, its exact
  // pre-existing behavior.
  const [providerLabels, setProviderLabels] = useState<Record<string, string>>({});
  useEffect(() => {
    if (!client) return;
    let cancelled = false;
    client
      .call<ProviderListResult>("provider.list")
      .then((result) => {
        if (cancelled) return;
        setProviderLabels(Object.fromEntries(result.providers.map((p) => [p.id, p.label ?? p.id])));
      })
      .catch((err) => console.error("provider.list failed, keeping raw provider ids in run headers", err));
    return () => {
      cancelled = true;
    };
  }, [client]);
  // Feeds the header run-config pill's agent-name lookup (run-config IA) --
  // the composer fetches its own copy too (its picker seeds on it); see
  // app-sidebar.tsx's identical duplicate-fetch rationale for why sharing
  // one fetch isn't worth coupling these two surfaces together.
  const [profiles, setProfiles] = useState<AgentProfile[]>([]);
  useEffect(() => {
    if (!client) return;
    let cancelled = false;
    client
      .call<AgentProfile[]>("profile.list")
      .then((result) => {
        if (!cancelled) setProfiles(result ?? []);
      })
      .catch((err) => console.error("profile.list failed, hiding the agent name in the header pill", err));
    return () => {
      cancelled = true;
    };
  }, [client]);
  // Mirrors RunConfigToolbar's own shared state (run-config IA): the
  // header pill is "display-only, derived from the same state the toolbar
  // reads" -- this is that read, not a second source of truth. Null until
  // the composer's first onChange fires (its own mount effect, near-
  // immediate).
  const [runConfigState, setRunConfigState] = useState<RunConfigState | null>(null);
  // The composer's diff-stat pill (web-ui-dogfood-polish Item 5) reads the
  // same task.diff this is -- the same hook the diff pane itself uses, so
  // the pill and the pane's header stat can't disagree and no extra RPC
  // is introduced. It stays mounted even when the Diff tab isn't (the
  // fetch is cheap and the refresh signal is identical).
  const wholeDiff = useTaskDiff(client, task, undefined);

  // Every run currently holding an unanswered permission request -- in
  // practice at most one (a task has one active run at a time), but this
  // stays a list so nothing here assumes that. Rendered in a dock pinned
  // between the scrolling log and the prompt form (see below) rather than
  // inline per-run, so it can't be scrolled out of view while log chunks
  // keep streaming in above/below it.
  const pendingRuns = runs?.filter((run) => run.pendingPermission) ?? [];

  // The task's live run, if any. A task has at most one at a time, so the
  // first match is the one the composer's Stop and queue drain act on.
  const runningRun = runs?.find((run) => run.status === "running") ?? null;
  const runningRunId = runningRun?.id ?? null;

  // GLM/Kimi's own live-session-scoped config-option control (thinking
  // level, etc, see use-run-config-options) -- Claude/Codex/no-live-run
  // pass null, which the hook treats as "nothing to show". Keyed off the
  // running run's own item count so a fresh option list is fetched as the
  // session progresses (see the hook's own doc comment for why there's no
  // dedicated push notification for this instead).
  const configOptionsRunId = runningRun && (runningRun.provider === "glm" || runningRun.provider === "kimi") ? runningRun.id : null;
  const configOptions = useRunConfigOptions(client, configOptionsRunId, runningRun?.items.length ?? 0);

  // Auto-follow keys off the total item count across every run: that is
  // the one number that changes whenever anything is appended anywhere in
  // the transcript, and it's O(runs) rather than O(items) to compute.
  const itemCount = runs?.reduce((total, run) => total + run.items.length, 0) ?? 0;
  const follow = useAutoFollow<HTMLDivElement>(itemCount);

  // Chat Find's revision signal: item count alone misses a streamed chunk
  // appended to the *last* item (no new item, just more text), so this
  // also sums text length -- the one other thing that changes whenever the
  // transcript's searchable content grows. See use-chat-find.ts's doc
  // comment for why a re-walk (not incremental patching) needs this.
  const textLength =
    runs?.reduce(
      (total, run) =>
        total +
        run.items.reduce(
          (n, item) => n + (item.kind === "assistant" || item.kind === "user" || item.kind === "thinking" ? item.text.length : 0),
          0,
        ),
      0,
    ) ?? 0;
  const chatFind = useChatFind(follow.ref, `${itemCount}:${textLength}`);

  const [detailLevel, setDetailLevel] = useDetailLevel();

  // App.tsx re-creates its openFileTab closure on every render, which
  // would defeat TimelineRow's memo if passed straight through. Pinning
  // it behind a ref gives every row a callback whose identity never
  // changes while still calling the current one.
  const openFileRef = useRef(onOpenFile);
  openFileRef.current = onOpenFile;
  const openFile = useCallback((path: string) => openFileRef.current?.(path), []);

  const composerTextareaRef = useRef<HTMLTextAreaElement | null>(null);
  const focusComposer = useCallback(() => composerTextareaRef.current?.focus(), []);

  // The header pill's click target (AC: "clicking it moves focus to the
  // composer toolbar"). tabIndex=-1 on the row itself (composer.tsx) is
  // what makes a plain div a valid .focus() target.
  const toolbarRef = useRef<HTMLDivElement | null>(null);
  const focusToolbar = useCallback(() => toolbarRef.current?.focus(), []);

  return (
    <div className="flex h-full flex-col">
      <PaneHeader
        title={task.Title}
        subtitle={
          <>
            <span className="uppercase">{task.Status}</span>
            {task.Branch && <span className="truncate">{task.Branch}</span>}
            {runConfigState && (
              <button
                type="button"
                data-testid="task-run-config-pill"
                onClick={focusToolbar}
                className="truncate rounded-full border px-2 py-0.5 text-ui-sm text-foreground-muted transition-colors hover:bg-hover hover:text-foreground"
              >
                {runConfigPillLabel(runConfigState, profiles, providerLabels)}
              </button>
            )}
          </>
        }
        actions={
          <Button
            type="button"
            variant="ghost"
            size="xs"
            data-testid="detail-level-toggle"
            aria-pressed={detailLevel === "overview"}
            onClick={() => setDetailLevel(detailLevel === "overview" ? "detailed" : "overview")}
          >
            {detailLevel === "overview" ? "Overview" : "Detailed"}
          </Button>
        }
      />

      {connectionStatus === "reconnecting" && (
        <Alert
          testId="connection-banner"
          variant="warning"
          className="rounded-none border-x-0 border-t-0"
          description="Connection lost -- reconnecting to daemon…"
        />
      )}

      <div className="relative flex-1 min-h-0">
        <div
          ref={follow.ref}
          onScroll={follow.onScroll}
          onFocus={chatFind.onFocus}
          onBlur={chatFind.onBlur}
          tabIndex={-1}
          data-testid="run-log-scroll"
          data-following={follow.following}
          className="h-full overflow-y-auto px-4 py-3 outline-none"
        >
          {/*
           * Dogfood Item 2: the chat timeline is a reading column, not a
           * pane -- cap it (and center it) on wide screens instead of
           * stretching line length edge-to-edge. The wrapper lives
           * *inside* the scroll container (so it scrolls with the log)
           * and only the chat tab gets it: files/diff/terminal render
           * their own full-width roots.
           */}
          <div data-testid="run-log-column" className="mx-auto max-w-3xl">
            {error && <Alert variant="error" description={error} />}
            {!error && runs === null && <InlineSpinner label="Loading runs…" />}
            {!error && runs !== null && runs.length === 0 && (
              <EmptyState title="No runs yet" description="Send a prompt to start one" />
            )}
            {runs !== null && runs.length > 0 && (
              <ul className="space-y-4">
                {runs.map((run) => (
                  <RunTimeline
                    key={run.id}
                    run={run}
                    detailLevel={detailLevel}
                    worktreePath={task.WorktreePath ?? undefined}
                    onOpenFile={openFile}
                    onRetry={retryWithHigherEffort}
                    providerLabels={providerLabels}
                  />
                ))}
              </ul>
            )}
          </div>
        </div>

        {!follow.following && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            data-testid="jump-to-latest"
            onClick={follow.jumpToLatest}
            className="absolute bottom-3 left-1/2 -translate-x-1/2 shadow-sm"
          >
            <ArrowDown className="size-3" />
            Jump to latest
          </Button>
        )}

        {chatFind.open && (
          <div className="absolute top-2 right-3 z-10">
            <FindBar
              ref={chatFind.barRef}
              query={chatFind.query}
              status={chatFind.status}
              canNavigate={chatFind.count > 0}
              onQueryChange={chatFind.setQuery}
              onNext={chatFind.next}
              onPrevious={chatFind.previous}
              onClose={chatFind.close}
            />
          </div>
        )}
      </div>

      {/*
       * The pending-permission dock: a sibling of the scrolling log above,
       * not a descendant of it, so it stays pinned in place (like the
       * prompt form right below it) no matter how far the log has
       * scrolled or how much new output streams in (see the plan's Item 3)
       * -- and aligned to the same reading column as that log (dogfood
       * Item 2): mx-auto with the same max-w-3xl keeps a permission card
       * sitting at the bottom of the log visually continuous with it on
       * wide screens.
       */}
      {pendingRuns.length > 0 && (
        <div data-testid="pending-permission-dock" className="mx-auto w-full max-w-3xl shrink-0 border-t bg-background px-4 py-2">
          {pendingRuns.map((run) => (
            <PermissionCard
              key={run.id}
              runId={run.id}
              pending={run.pendingPermission!}
              onRespond={respondPermission}
              onChat={focusComposer}
            />
          ))}
        </div>
      )}

      {runningRun && (runningRun.approvalPolicy === "manual" || runningRun.approvalPolicy === "auto-safe") && (
        <ApprovalPolicyControl
          policy={runningRun.approvalPolicy}
          onChange={(policy) => setApprovalPolicy(runningRun.id, policy)}
        />
      )}

      <Composer
        client={client}
        taskId={task.ID}
        connected={client !== null && connectionStatus === "connected"}
        runningRunId={runningRunId}
        diffStat={wholeDiff.stat}
        onOpenDiff={onOpenDiffTab}
        onSubmit={submitPrompt}
        onStop={stopRun}
        textareaRef={composerTextareaRef}
        toolbarRef={toolbarRef}
        onRunConfigChange={setRunConfigState}
        configOptions={{ options: configOptions.options, error: configOptions.error, onSetOption: configOptions.setOption }}
      />
    </div>
  );
}
