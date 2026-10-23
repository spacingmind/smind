import { useCallback, useMemo, useRef, useState } from "react";
import { Icons } from "@/lib/icons";
import { providerLabel } from "@/lib/provider-catalog";

import { TaskLineageChips } from "@/components/task-lineage";
import { resendRequests } from "@/lib/attachments/reuse";
import { AttachmentDropOverlay } from "@/components/composer/attachment-drop-overlay";
import { Composer } from "@/components/composer/composer";
import { useAttachmentDraft } from "@/components/composer/use-attachment-draft";
import { useAttachmentDrop } from "@/components/composer/use-attachment-drop";
import type { RunConfigState } from "@/components/composer/run-config-toolbar";
import { FindBar } from "@/components/find/find-bar";
import { ChatUsageTotal } from "@/components/chat-usage-total";
import { McpServersIndicator } from "@/components/mcp-servers-indicator";
import { PortChip, SetupBadge, SetupDetails } from "@/components/task-setup";
import { PermissionCard } from "@/components/permission/permission-card";
import { useProviderCatalog } from "@/hooks/use-provider-catalog";
import { useProfileList } from "@/hooks/use-profiles";
import { useDetailLevel } from "@/components/timeline/detail-level";
import { RunTimeline } from "@/components/timeline/run-timeline";
import { ChatOutlineRail } from "@/components/timeline/chat-outline-rail";
import { useAutoFollow } from "@/components/timeline/use-auto-follow";
import { useChatOutline } from "@/components/timeline/use-chat-outline";
import { useChatFind } from "@/components/timeline/use-chat-find";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { InlineSpinner } from "@/components/ui/inline-spinner";
import { PaneHeader } from "@/components/ui/pane-header";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { useLiveTask } from "@/hooks/use-live-task";
import { useTaskLineage } from "@/hooks/use-task-lineage";
import { useChatQueue } from "@/hooks/use-chat-queue";
import { useRunConfigOptions } from "@/hooks/use-run-config-options";
import { useRunTimeline, type RunEntry } from "@/hooks/use-run-timeline";
import { useTaskGit } from "@/hooks/use-task-git";
import { useTaskGitFocus } from "@/lib/task-git-focus";
import { effectiveMode, providerModes, supportsAutoAccept, supportsLiveModeSwitch } from "@/lib/permission-modes";
import type { ConnectionStatus } from "@/lib/reconnect";
import type { AgentProfile, Chat, Provider, Task } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

/**
 * What the chat's meta row says about the run config. The composer toolbar
 * right below already shows provider, model, mode and thinking, so repeating
 * them is chrome; the one thing it does not show is the saved agent profile
 * the config came from. Returns that name ("<Agent>", or "<Agent> · edited"
 * once hand-edits drifted from it), or null when no agent applies. Pure so it
 * can be unit-tested without mounting the pane.
 */
export function agentChipLabel(state: RunConfigState, profiles: AgentProfile[]): string | null {
  const agent = state.baseAgentId ? profiles.find((p) => String(p.ID) === state.baseAgentId) : undefined;
  if (!agent) return null;
  return state.custom ? `${agent.Name} · edited` : agent.Name;
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
  task: taskProp,
  chat,
  isDefaultChat,
  otherRunningChat,
  onJumpToChat,
  connectionStatus = "connected",
  events,
  onOpenFile,
  onOpenDiffTab,
  onJumpToTask,
}: {
  client: WsClientLike | null;
  task: Task;
  /** The chat this pane shows (ADR-0016 P3) -- one tab, one chat: the timeline, composer, and run-config toolbar below are all scoped to chat.ID, never the whole task. */
  chat: Chat;
  /** Whether chat is task's default (oldest) chat -- migrates the pre-ADR-0016 per-task-only run-config key onto it. */
  isDefaultChat: boolean;
  /** Another of this task's chats currently running a run, if any -- drives the composer's "Also running" chip (another chat is running in this worktree). */
  otherRunningChat?: { id: number; title: string } | null;
  /** Jumps to otherRunningChat's own tab -- the concurrency chip's click target. */
  onJumpToChat?: (chatId: number) => void;
  /** Real-time connection status from App.tsx -- lets an active run.attach subscription visibly reflect a break instead of silently freezing on stale "live" output. Defaults to "connected" so every existing caller/test not wired up to App.tsx's status keeps behaving exactly as before. */
  connectionStatus?: ConnectionStatus;
  /** The app's single-subscription event stream, optional so existing tests/mounts render unchanged: feeds both run.status adoption and the chat's queue view. */
  events?: DaemonEvents | null;
  /** Opens a worktree-relative path as a file tab. Optional: without it, a tool-call card naming a file simply isn't click-through. */
  onOpenFile?: (path: string) => void;
  /** Brings the task's Diff tab forward -- the composer's diff-stat pill's click target. Optional: without it (no tab strip above) the pill is omitted. */
  onOpenDiffTab?: () => void;
  /** Selects another task (the lineage strip's parent/child jump targets). Optional: without it the strip renders but isn't click-through. */
  onJumpToTask?: (taskId: number) => void;
}) {
  // Worktree setup state changes after the task was selected (ADR-0024), so
  // follow the task's own update events instead of the selection snapshot.
  const { task, apply: applyTask } = useLiveTask(taskProp, events);
  // Hierarchy strip (task-hierarchy-tiers PR-U2): parent breadcrumb, child
  // pills, subtree rollups. Never blocks the pane on load -- it renders
  // nothing until the lineage arrives.
  const lineage = useTaskLineage(client, task, events);
  const { runs, error, submitPrompt, stopRun, respondPermission, setPermissionMode, retryWithHigherEffort } = useRunTimeline(
    client,
    task.ID,
    chat.ID,
    events,
  );
  // The daemon's per-chat prompt queue (ADR-0021 §9) -- one owner here,
  // the composer only renders/cancels what it's handed.
  const queue = useChatQueue(client, events ?? null, chat.ID);
  // provider.list's id→label map for the run headers ("label ?? id",
  // run-config IA): run entries carry only the wire provider id, and the
  // header should say "Claude Code", not "claude-native". Empty until the
  // fetch resolves -- RunTimeline falls back to the raw id, its exact
  // pre-existing behavior.
  const providers = useProviderCatalog().providers;
  const providerLabels = useMemo(() => Object.fromEntries(providers.map((p) => [p.id, providerLabel(p.id, [p])])), [providers]);
  // The run footer's human model labels: every catalog entry plus the
  // provider's default model under a bare `${id}/` key (the footer's
  // fallback order lives in run-footer-model.ts).
  const modelLabels = useMemo(() => {
    const labels: Record<string, string> = {};
    for (const p of providers) {
      for (const m of p.models ?? []) {
        labels[`${p.id}/${m.id}`] = m.label;
        if (m.default) labels[`${p.id}/`] = m.label;
      }
    }
    return labels;
  }, [providers]);
  // The header run-config pill's agent-name lookup (run-config IA), from
  // the same shared profile.list cache the composer picker reads.
  const profiles = useProfileList();
  // Mirrors RunConfigToolbar's own shared state (run-config IA): the
  // header pill is "display-only, derived from the same state the toolbar
  // reads" -- this is that read, not a second source of truth. Null until
  // the composer's first onChange fires (its own mount effect, near-
  // immediate).
  const [runConfigState, setRunConfigState] = useState<RunConfigState | null>(null);
  // The composer's chips (diff stat, PR, conflicts) read the task's shared git state (ADR-0039); the Diff
  // pane keeps its own task.diff query. This is the one place the daemon is asked to keep the open task's
  // git state fresh (task.gitFocus): TaskDetail is mounted exactly while a task is open.
  const git = useTaskGit(task.ID);
  useTaskGitFocus(task.ID);

  // Every run currently holding an unanswered permission request -- in
  // practice at most one (a task has one active run at a time), but this
  // stays a list so nothing here assumes that. Rendered in a dock pinned
  // between the scrolling log and the prompt form (see below) rather than
  // inline per-run, so it can't be scrolled out of view while log chunks
  // keep streaming in above/below it.
  const pendingRuns = runs?.filter((run) => run.pendingPermission) ?? [];

  // The chat's most recent run only -- the header pill shows that run's
  // MCP servers, never an older run's (no fallback when it reported none).
  const latestRun = runs?.[runs.length - 1];

  // The task's live run, if any. A task has at most one at a time, so the
  // first match is the one the composer's Stop and queue drain act on.
  const runningRun = runs?.find((run) => run.status === "running") ?? null;
  const runningRunId = runningRun?.id ?? null;

  // The live-session-scoped config-option control (GLM/Kimi's thinking
  // level etc, see use-run-config-options; Claude's synthetic `model`
  // option, ADR-0025) -- Codex (no live option, its model applies from the
  // next run) and no-live-run pass null, which the hook treats as "nothing
  // to show". Keyed off the
  // running run's own item count so a fresh option list is fetched as the
  // session progresses (see the hook's own doc comment for why there's no
  // dedicated push notification for this instead).
  const configOptionsRunId = runningRun && (runningRun.provider === "glm" || runningRun.provider === "kimi" || runningRun.provider === "claude-native") ? runningRun.id : null;
  // D1: while a live-switch-capable run is active the composer's Mode/Auto-accept pills drive it.
  const liveRunId = runningRun?.id;
  const liveProvider = runningRun?.provider;
  const liveMode = runningRun?.permissionMode;
  const liveAutoAccept = runningRun?.autoAccept;
  const liveInFlight = useRef(false);
  const liveRun = useMemo(
    () =>
      liveRunId && liveProvider && supportsLiveModeSwitch(providers, liveProvider)
        ? {
            provider: liveProvider,
            runId: liveRunId,
            modes: providerModes(providers, liveProvider),
            mode: effectiveMode(providers, liveProvider, liveMode ?? ""),
            autoAccept: liveAutoAccept ?? false,
            showAutoAccept: supportsAutoAccept(providers, liveProvider),
            // One switch at a time: a second change (a rapid Shift+Tab, a click during the
            // RPC) would be computed from the not-yet-updated mode and double-fire.
            onChange: async (change: { modeId?: string; autoAccept?: boolean }) => {
              if (liveInFlight.current) return;
              liveInFlight.current = true;
              try {
                await setPermissionMode(liveRunId, change);
              } finally {
                liveInFlight.current = false;
              }
            },
          }
        : null,
    [liveRunId, liveProvider, liveMode, liveAutoAccept, providers, setPermissionMode],
  );
  const configOptions = useRunConfigOptions(client, configOptionsRunId, (runningRun?.items.length ?? 0) + (runningRun?.configRevision ?? 0));

  // Auto-follow keys off the total item count across every run: that is
  // the one number that changes whenever anything is appended anywhere in
  // the transcript, and it's O(runs) rather than O(items) to compute.
  const itemCount = runs?.reduce((total, run) => total + run.items.length, 0) ?? 0;
  const follow = useAutoFollow<HTMLDivElement>(itemCount);
  const outline = useChatOutline(follow.ref, runs);

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

  // Message hover actions. Pinned behind refs so RunTimeline's memo keeps
  // bailing out while these always call the latest submitPrompt.
  const [prefill, setPrefill] = useState<{ text: string; nonce: number } | undefined>();
  const [actionError, setActionError] = useState<string | null>(null);
  const submitPromptRef = useRef(submitPrompt);
  submitPromptRef.current = submitPrompt;
  const runConfigRef = useRef(runConfigState);
  runConfigRef.current = runConfigState;
  const queueRefreshRef = useRef(queue.refresh);
  queueRefreshRef.current = queue.refresh;
  const rerunsInFlight = useRef(new Set<string>());
  // The shared path under Re-run and the footer's Continue: submit `prompt`
  // on the run's own config. Provider, model and thinking come from the
  // run; the permission settings come from what the composer shows NOW
  // (when it is on the same provider), so a re-submit never silently
  // replays a looser mode the old run was live-switched to.
  const resubmit = useCallback((run: RunEntry, prompt: string) => {
    // One re-submit per run at a time: a double click must not start (or queue) the prompt twice.
    if (rerunsInFlight.current.has(run.id)) return;
    rerunsInFlight.current.add(run.id);
    setActionError(null);
    const composer = runConfigRef.current;
    const permission =
      composer && composer.provider === run.provider
        ? { permissionMode: composer.permissionMode, autoAccept: composer.autoAccept }
        : { permissionMode: run.permissionMode, autoAccept: run.autoAccept };
    // ADR-0038: the earlier run's attachments ride along as `reuse`/ref requests (the daemon copies the stored files).
    const reuse = resendRequests(run.id, run.attachments);
    submitPromptRef
      .current(
        run.provider,
        prompt,
        permission,
        run.thinkingLevel,
        "queue",
        run.model || undefined,
        reuse.length > 0 ? reuse : undefined,
        reuse.length > 0 ? run.attachments : undefined,
      )
      .then((result) => {
        // A queued prompt shows up in the composer's queue list straight away.
        if (result.kind === "queued") queueRefreshRef.current();
      })
      .catch((err: unknown) => setActionError(err instanceof Error ? err.message : String(err)))
      .finally(() => rerunsInFlight.current.delete(run.id));
  }, []);
  const rerun = useCallback((run: RunEntry) => resubmit(run, run.prompt), [resubmit]);
  const continueRun = useCallback((run: RunEntry) => resubmit(run, "Continue"), [resubmit]);
  const editPrompt = useCallback((run: RunEntry) => {
    setActionError(null);
    setPrefill((prev) => ({ text: run.prompt, nonce: (prev?.nonce ?? 0) + 1 }));
  }, []);

  const composerTextareaRef = useRef<HTMLTextAreaElement | null>(null);
  const focusComposer = useCallback(() => composerTextareaRef.current?.focus(), []);

  // The header pill's click target (AC: "clicking it moves focus to the
  // composer toolbar"). tabIndex=-1 on the row itself (composer.tsx) is
  // what makes a plain div a valid .focus() target.
  const toolbarRef = useRef<HTMLDivElement | null>(null);
  const focusToolbar = useCallback(() => toolbarRef.current?.focus(), []);

  // Dropping files anywhere on the pane attaches them to this chat's draft (ADR-0038 §7); inert without daemon support.
  const attachDraft = useAttachmentDraft(task.ID, chat.ID);
  const dropZone = useAttachmentDrop({ enabled: attachDraft.limits !== null, onFiles: attachDraft.add });

  const agentLabel = runConfigState ? agentChipLabel(runConfigState, profiles) : null;

  return (
    <div className="relative flex h-full flex-col" {...dropZone.handlers}>
      {attachDraft.limits && <AttachmentDropOverlay active={dropZone.dragging} />}
      {/*
       * The task's title, branch and status live in the app header (and its
       * hover card); this row keeps only what is specific to this chat.
       */}
      <PaneHeader
        testId="task-chat-meta"
        className="items-start border-b-0"
        subtitle={
          <>
            <SetupBadge status={task.SetupStatus} />
            <PortChip port={task.Port} />
            {agentLabel && (
              <button
                type="button"
                data-testid="task-run-config-pill"
                title="Agent profile -- click to edit the run config"
                onClick={focusToolbar}
                className="truncate rounded-full border px-2 py-0.5 text-ui-sm text-foreground-muted transition-colors hover:bg-hover hover:text-foreground"
              >
                {agentLabel}
              </button>
            )}
            <McpServersIndicator servers={latestRun?.mcpServers} />
            <ChatUsageTotal runs={runs} provider={chat.Provider ?? latestRun?.provider} />
            <TaskLineageChips lineage={lineage} onJumpToTask={onJumpToTask} />
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

      <SetupDetails client={client} task={task} onTask={applyTask} />

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
                    onRerun={rerun}
                    onEditPrompt={editPrompt}
                    onContinue={continueRun}
                    providerLabels={providerLabels}
                    modelLabels={modelLabels}
                    isLatest={run.id === runs[runs.length - 1]?.id}
                  />
                ))}
              </ul>
            )}
            {/* After the list, where the log follows the tail: a failed Re-run is visible where the user just clicked. */}
            {actionError && <Alert variant="error" description={actionError} testId="rerun-error" className="mt-3" />}
          </div>
        </div>

        {outline.visible && (
          <ChatOutlineRail
            prompts={outline.prompts}
            activePrompt={outline.activePrompt}
            onJump={outline.jump}
            onScrub={outline.scrub}
          />
        )}

        {!follow.following && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            data-testid="jump-to-latest"
            onClick={follow.jumpToLatest}
            className="absolute bottom-3 left-1/2 -translate-x-1/2 shadow-sm"
          >
            <Icons.down className="size-3" />
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
        <div data-testid="pending-permission-dock" className="mx-auto w-full max-w-3xl shrink-0 space-y-2 px-4 py-2">
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

      <Composer
        client={client}
        taskId={task.ID}
        chatId={chat.ID}
        isDefaultChat={isDefaultChat}
        boundProvider={chat.Provider as Provider | null}
        connected={client !== null && connectionStatus === "connected"}
        runningRunId={runningRunId}
        waitingOnUser={!!runningRun?.pendingPermission}
        git={git}
        onOpenDiff={onOpenDiffTab}
        queuedItems={queue.items}
        onCancelQueued={queue.cancel}
        onQueued={queue.refresh}
        onSubmit={submitPrompt}
        onStop={stopRun}
        textareaRef={composerTextareaRef}
        prefill={prefill}
        toolbarRef={toolbarRef}
        onRunConfigChange={setRunConfigState}
        liveRun={liveRun}
        dropActive={dropZone.dragging}
        otherRunningChat={otherRunningChat}
        onJumpToChat={onJumpToChat}
        configOptions={{ options: configOptions.options, error: configOptions.error, onSetOption: configOptions.setOption }}
      />
    </div>
  );
}
