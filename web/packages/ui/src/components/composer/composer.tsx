import { useCallback, useEffect, useState, type FormEvent, type KeyboardEvent, type Ref } from "react";

import { GitCompare } from "lucide-react";

import { QueuedPrompts } from "@/components/composer/queued-prompts";
import { resolveNextPermissionMode } from "@/components/composer/permission-mode-cycle";
import { useComposerDraft } from "@/components/composer/use-composer-draft";
import type { SubmitResult } from "@/hooks/use-run-timeline";
import { PromptTextarea } from "@/components/composer/prompt-textarea";
import { RunConfigToolbar, type RunConfigContextValue, type RunConfigState } from "@/components/composer/run-config-toolbar";
import { RunConfigOptions } from "@/components/run-config-options";
import { Button } from "@/components/ui/button";
import { type DiffStat } from "@/lib/diff-stat";
import type {
  AgentProfile,
  ChatQueueItem,
  ConfigOptionParams,
  PermissionSettings,
  Provider,
  ProviderInfo,
  ProviderListResult,
  ThinkingLevel,
} from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

/** Used until provider.list answers (and kept if it fails) so the composer is never unusable because one fetch lost. */
const FALLBACK_PROVIDERS: ProviderInfo[] = [{ id: "claude-native" }, { id: "glm" }];

const COMPACT_TOUCH_ACTION_BUTTON_CLASS = "h-11 px-4 text-ui-base md:h-7 md:px-2.5 md:text-ui-sm";

/**
 * Why the composer can't send right now, phrased for the placeholder. A
 * disabled composer must say *why* rather than silently greying out
 * (ui-redesign-parity Item 10, from audit-deepseek-harness.md §2's block
 * contract) -- and a run being in flight is deliberately not a block at
 * all here: that case queues instead (see Composer's doc comment).
 */
export function composerPlaceholder({
  connected,
  hasTask,
  running,
}: {
  connected: boolean;
  hasTask: boolean;
  running: boolean;
}): string {
  if (!connected) return "Not connected — reconnecting to the daemon…";
  if (!hasTask) return "Select a task to send a prompt";
  if (running) return "Queue a follow-up — it sends when this run finishes";
  return "Message the agent…";
}

/**
 * The task's diff stat as a compact pill above the input card
 * (web-ui-dogfood-polish Item 5): additions green, deletions red, and a
 * click jumps to the task's Diff tab. The numbers are the same stat the
 * diff pane renders (hooks/use-task-diff.ts), so nothing new is fetched
 * here -- the parent hands the already-derived stat in.
 */
function DiffStatPill({ stat, onOpenDiff }: { stat: DiffStat; onOpenDiff: () => void }) {
  return (
    <button
      type="button"
      data-testid="composer-diff-stat"
      aria-label={`Open diff: ${stat.additions} additions, ${stat.deletions} deletions`}
      onClick={onOpenDiff}
      className="flex shrink-0 items-center gap-1.5 rounded-full border border-input bg-surface px-2.5 py-0.5 text-ui-sm text-foreground-muted transition-colors hover:bg-hover hover:text-foreground"
    >
      <GitCompare aria-hidden className="size-3 opacity-70" />
      <span className="font-medium text-success">+{stat.additions}</span>
      <span className="font-medium text-destructive">−{stat.deletions}</span>
    </button>
  );
}

/**
 * The task composer (ui-redesign-parity Item 10, reshaped to the input
 * card anatomy by web-ui-dogfood-polish Item 5): one rounded card whose
 * top is the autogrowing prompt textarea and whose bottom edge is the
 * toolbar (compact provider/permission-mode selects, Stop/Send), with the
 * task's diff-stat pill sitting above the card. The only affordances
 * shown are ones that work -- no attachment "+" placeholder until
 * attachments exist.
 *
 * **Queue, not steer.** The daemon exposes no "send more input to a run
 * already in flight" RPC, so submitting while a run is live hands the
 * prompt to the daemon's per-chat queue via run.start whenBusy:"queue"
 * (ADR-0021 §9) -- the queued rows rendered above the card come straight
 * from that server state (useChatQueue, owned by task-detail.tsx), never
 * local. Cmd/Ctrl+Enter or "Send now" while running sends with
 * whenBusy:"interrupt" instead (Paseo's alternate-send shortcut).
 */
export function Composer({
  client,
  taskId,
  chatId,
  isDefaultChat,
  boundProvider,
  connected,
  runningRunId,
  diffStat,
  onOpenDiff,
  queuedItems,
  onCancelQueued,
  onQueued,
  onSubmit,
  onStop,
  textareaRef,
  toolbarRef,
  onRunConfigChange,
  configOptions,
}: {
  client: WsClientLike | null;
  /** null when no task is selected -- the composer renders, disabled, and says so. */
  taskId: number | null;
  /** The open chat tab's id -- keys RunConfigToolbar's per-chat state (ADR-0016 P3). null alongside taskId when no chat is selected. */
  chatId: number | null;
  /** Whether chatId is taskId's default (oldest) chat -- migrates the pre-ADR-0016 per-task-only run-config key onto it. */
  isDefaultChat: boolean;
  /** The chat's bound provider (chats.Provider), once set -- makes RunConfigToolbar's Provider select read-only. */
  boundProvider: Provider | null;
  connected: boolean;
  /** The task's currently-running run, or null. Drives Stop, "Send now", and the queue placeholder. */
  runningRunId: string | null;
  /** The task's diff stat (use-task-diff) for the pill above the card -- pill omitted when null/unchanged. */
  diffStat?: DiffStat | null;
  /** Brings the task's Diff tab forward when the pill is clicked; without it (no tab strip above) the pill is omitted entirely. */
  onOpenDiff?: () => void;
  /** The chat's daemon-side queue rows (useChatQueue in task-detail.tsx) -- the only queue this composer shows. */
  queuedItems: ChatQueueItem[];
  /** Cancels a queued row server-side (chat.queueCancel); removal is the snapshot's job. */
  onCancelQueued: (itemId: number) => void;
  /** Called after a prompt was queued, so the queue view refetches even if the live snapshot missed it. */
  onQueued?: () => void;
  onSubmit: (
    provider: Provider,
    prompt: string,
    permission: PermissionSettings,
    thinkingLevel?: ThinkingLevel,
    whenBusy?: "queue" | "interrupt",
  ) => Promise<SubmitResult>;
  onStop: (runId: string) => Promise<void>;
  /** Exposes the prompt textarea's DOM node -- what lets a plan review's "Chat about it" (Item 11) move focus into the composer without resolving the pending request. */
  textareaRef?: Ref<HTMLTextAreaElement>;
  /** Exposes the run-config toolbar row's DOM node -- what the task header's run-config pill (task-detail.tsx) focuses when clicked. */
  toolbarRef?: Ref<HTMLDivElement>;
  /** Fires whenever RunConfigToolbar's shared state changes -- lets task-detail.tsx mirror it for the header pill without owning the state itself (run-config IA: "display-only, derived from the same state the toolbar reads"). */
  onRunConfigChange?: (state: RunConfigState) => void;
  /** GLM/Kimi's live ACP config options for the currently-running run, if any -- rendered inside this row per run-config IA's "same row, not a separate control" requirement. */
  configOptions?: {
    options: ConfigOptionParams[];
    error: string | null;
    onSetOption: (configId: string, value: string) => Promise<void>;
  };
}) {
  const draft = useComposerDraft(taskId);
  const [providers, setProviders] = useState<ProviderInfo[]>(FALLBACK_PROVIDERS);
  // ADR-0014's Profiles picker: a named provider/permissionMode/
  // thinkingLevel bundle a user applies in one click. Empty when the
  // daemon has none (a fresh install, or client.call failing) -- the
  // control below renders nothing in that case, so the composer's
  // pre-profiles defaults are exactly what an empty-profiles daemon still
  // gets (AC15's regression requirement).
  const [profiles, setProfiles] = useState<AgentProfile[]>([]);
  // The run config itself lives in RunConfigToolbar's shared context
  // (run-config IA); this mirror is what the composer's own submit and
  // Shift+Tab cycle read, updated by the toolbar's onChange.
  const [runConfig, setRunConfig] = useState<RunConfigContextValue | null>(null);
  const { provider, permissionMode, autoAccept, thinkingLevel } = runConfig?.state ?? {
    provider: "claude-native" as Provider,
    permissionMode: "",
    autoAccept: false,
    thinkingLevel: "" as ThinkingLevel,
  };
  const [submitting, setSubmitting] = useState(false);
  const [stopping, setStopping] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  useEffect(() => {
    setProviders(FALLBACK_PROVIDERS);
    if (!client) return;
    let cancelled = false;
    client
      .call<ProviderListResult>("provider.list")
      .then((result) => {
        if (!cancelled && result.providers.length > 0) setProviders(result.providers);
      })
      .catch((err) => console.error("provider.list failed, using fallback provider list", err));
    return () => {
      cancelled = true;
    };
  }, [client]);

  useEffect(() => {
    setProfiles([]);
    if (!client) return;
    let cancelled = false;
    client
      .call<AgentProfile[]>("profile.list")
      .then((result) => {
        if (!cancelled) setProfiles(result ?? []);
      })
      .catch((err) => console.error("profile.list failed, hiding the Profiles picker", err));
    return () => {
      cancelled = true;
    };
  }, [client]);

  const canSend = connected && taskId !== null;
  const running = runningRunId !== null;
  const hasChanges =
    diffStat !== null &&
    diffStat !== undefined &&
    (diffStat.files > 0 || diffStat.additions > 0 || diffStat.deletions > 0);

  const send = useCallback(
    async (text: string, whenBusy: "queue" | "interrupt") => {
      setSubmitting(true);
      setFormError(null);
      try {
        const result = await onSubmit(
          provider,
          text,
          { permissionMode, autoAccept },
          provider === "claude-native" && thinkingLevel ? thinkingLevel : undefined,
          whenBusy,
        );
        if (result.kind === "queued") onQueued?.();
        return true;
      } catch (err) {
        setFormError(err instanceof Error ? err.message : String(err));
        return false;
      } finally {
        setSubmitting(false);
      }
    },
    [onSubmit, provider, permissionMode, autoAccept, thinkingLevel, onQueued],
  );

  // A plain submit always hands the prompt to the daemon's queue whenBusy
  // (ADR-0021 §9) -- an idle chat starts at once under every mode, and a
  // busy one queues instead of failing on a stale "running" flag.
  const submit = useCallback(() => {
    const trimmed = draft.value.trim();
    if (!trimmed || !canSend || submitting) return;
    draft.clear();
    void send(trimmed, "queue").then((ok) => {
      // Restore the text if the prompt never landed, so it isn't lost.
      if (!ok) draft.setValue(trimmed);
    });
  }, [draft, canSend, submitting, send]);

  // Paseo's alternate-send shortcut: while a run is live, Cmd/Ctrl+Enter
  // interrupts it rather than queueing behind it.
  const sendNow = useCallback(() => {
    const trimmed = draft.value.trim();
    if (!trimmed || !canSend || submitting) return;
    draft.clear();
    void send(trimmed, "interrupt").then((ok) => {
      if (!ok) draft.setValue(trimmed);
    });
  }, [draft, canSend, submitting, send]);

  async function handleStop() {
    if (!runningRunId) return;
    setStopping(true);
    try {
      await onStop(runningRunId);
    } catch (err) {
      setFormError(err instanceof Error ? err.message : String(err));
    } finally {
      // Unlike the old run-card button this control outlives the run, so
      // it has to re-enable itself rather than counting on unmounting.
      setStopping(false);
    }
  }

  function handleKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && !e.shiftKey) {
      // isSubmitKey ignores modifier-held Enters, so this is the one
      // handler for them -- interrupt while running, plain submit idle.
      e.preventDefault();
      if (running) sendNow();
      else submit();
      return;
    }
    if (e.key === "Escape" && running) {
      e.preventDefault();
      void handleStop();
      return;
    }
    // AC7 of docs/plans/active/web-keyboard-tabs.md, ported from Paseo's
    // Shift+Tab mode cycle (composer/agent-controls/mode.ts): cycles the
    // same permission mode the toolbar's own Select controls, so the
    // Select re-rendering with the new value *is* the visible feedback --
    // no separate indicator to keep in sync.
    if (e.key === "Tab" && e.shiftKey && !inactive) {
      const next = resolveNextPermissionMode(runConfig?.meta.modes ?? [], runConfig?.meta.effectiveMode ?? "");
      if (next) {
        e.preventDefault();
        runConfig?.actions.setPermissionMode(next);
      }
    }
  }

  function handleFormSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    submit();
  }

  const inactive = !canSend || submitting;

  return (
    <form onSubmit={handleFormSubmit} data-testid="composer" className="mx-auto w-full max-w-3xl shrink-0 flex-col gap-2 border-t px-4 py-3 flex">
      <QueuedPrompts items={queuedItems} onCancel={onCancelQueued} />

      {/* The pill row above the card (Item 5): only real data -- the diff
          stat, only when there are changes and there's a Diff tab to jump
          to. No invented run/subagent pills. */}
      {hasChanges && onOpenDiff && (
        <div className="flex items-center gap-2">
          <DiffStatPill stat={diffStat} onOpenDiff={onOpenDiff} />
        </div>
      )}

      {/*
       * The input card (Item 5): one rounded surface whose top is the
       * textarea and whose bottom edge is the toolbar. The card carries
       * the border/focus ring; the textarea and toolbar controls inside
       * are chrome-less, which is why the selects are aria-labelled
       * rather than sitting under visible <label> text anymore.
       *
       * `rounded-2xl`/`bg-input`/`border-input-border` (zcode-visual-parity
       * P3): the main chat input shell is one of DESIGN.md's three
       * approved `rounded-2xl` exceptions, and a visible resting border
       * (not just on focus) is ZCode's own "calm and integrated, not
       * glowing by default" input philosophy.
       */}
      <div
        data-testid="composer-card"
        className="flex flex-col rounded-2xl border border-input-border bg-input transition-colors hover:border-input-border-hover focus-within:border-input-border-focused focus-within:bg-input-focused"
      >
        <PromptTextarea
          ref={textareaRef}
          label="Prompt"
          value={draft.value}
          onChange={draft.setValue}
          onSubmit={submit}
          onKeyDown={handleKeyDown}
          placeholder={composerPlaceholder({ connected, hasTask: taskId !== null, running })}
          disabled={inactive}
          className="rounded-none border-0 bg-transparent px-3 py-2.5 focus-visible:border-transparent focus-visible:ring-0"
        />

        {/*
         * The run-config toolbar (run-config IA): one compound component
         * whose four parts share a single context, replacing the four
         * independent Selects that each carried their own state here.
         */}
        <RunConfigToolbar
          taskId={taskId}
          chatId={chatId}
          isDefaultChat={isDefaultChat}
          boundProvider={boundProvider}
          profiles={profiles}
          providers={providers}
          disabled={inactive}
          onChange={(value) => {
            setRunConfig(value);
            onRunConfigChange?.(value.state);
          }}
        >
          <div
            ref={toolbarRef}
            tabIndex={-1}
            data-testid="composer-toolbar-row"
            className="flex flex-wrap items-center gap-1.5 rounded-md border-t border-border/60 px-2 py-1.5 focus-visible:ring-3 focus-visible:ring-ring/50"
          >
            <RunConfigToolbar.Agent />
            <RunConfigToolbar.Provider />
            <RunConfigToolbar.Mode />
            <RunConfigToolbar.Thinking />
            {configOptions && (
              <RunConfigOptions
                options={configOptions.options}
                error={configOptions.error}
                onSetOption={configOptions.onSetOption}
              />
            )}
          <div className="ml-auto flex items-center gap-2">
            {formError && <span className="text-ui-sm text-destructive">{formError}</span>}
            {running && (
              <>
              <Button
                type="button"
                variant="execute"
                size="sm"
                className={COMPACT_TOUCH_ACTION_BUTTON_CLASS}
                disabled={stopping}
                data-testid="chat-stop-button"
                onClick={handleStop}
              >
                {stopping ? "Stopping…" : "Stop"}
              </Button>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className={COMPACT_TOUCH_ACTION_BUTTON_CLASS}
                disabled={inactive || !draft.value.trim()}
                data-testid="chat-send-now-button"
                title="Interrupt the running turn and send now (⌘/Ctrl+Enter)"
                onClick={sendNow}
              >
                Send now
              </Button>
              </>
            )}
            <Button
              type="submit"
              variant={running ? "default" : "execute"}
              size="sm"
              className={COMPACT_TOUCH_ACTION_BUTTON_CLASS}
              disabled={inactive || !draft.value.trim()}
              data-testid="chat-send-button"
            >
              {running ? "Queue" : "Send"}
            </Button>
          </div>
          </div>
        </RunConfigToolbar>
      </div>
    </form>
  );
}
