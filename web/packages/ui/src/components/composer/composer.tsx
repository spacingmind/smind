import { useCallback, useEffect, useRef, useState, type FormEvent, type KeyboardEvent, type Ref } from "react";

import { GitCompare } from "lucide-react";

import { resolveNextApprovalPolicy } from "@/components/composer/approval-policy-cycle";
import { useComposerDraft } from "@/components/composer/use-composer-draft";
import { PromptTextarea } from "@/components/composer/prompt-textarea";
import { RunConfigToolbar, type RunConfigContextValue, type RunConfigState } from "@/components/composer/run-config-toolbar";
import { RunConfigOptions } from "@/components/run-config-options";
import { Button } from "@/components/ui/button";
import { type DiffStat } from "@/lib/diff-stat";
import type {
  AgentProfile,
  ApprovalPolicy,
  ConfigOptionParams,
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
 * toolbar (compact provider/approval-policy selects, Stop/Send), with the
 * task's diff-stat pill sitting above the card. The only affordances
 * shown are ones that work -- no attachment "+" placeholder until
 * attachments exist.
 *
 * **Queue, not steer.** The daemon exposes no "send more input to a run
 * already in flight" RPC (internal/wsapi/handlers.go's method table has
 * run.start/attach/stop/logs/respondPermission and nothing else), so
 * submitting while a run is live holds the text client-side and starts it
 * as its own run when the live one reaches a terminal state. The queue is
 * deliberately in-memory: an unsent *draft* is worth persisting, but a
 * queued follow-up whose whole meaning is "right after the run I was
 * watching" is not, once that session is gone.
 */
export function Composer({
  client,
  taskId,
  connected,
  runningRunId,
  diffStat,
  onOpenDiff,
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
  connected: boolean;
  /** The task's currently-running run, or null. Drives Stop and the queue drain. */
  runningRunId: string | null;
  /** The task's diff stat (use-task-diff) for the pill above the card -- pill omitted when null/unchanged. */
  diffStat?: DiffStat | null;
  /** Brings the task's Diff tab forward when the pill is clicked; without it (no tab strip above) the pill is omitted entirely. */
  onOpenDiff?: () => void;
  onSubmit: (
    provider: Provider,
    prompt: string,
    approvalPolicy: ApprovalPolicy,
    thinkingLevel?: ThinkingLevel,
  ) => Promise<void>;
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
  // ADR-0014's Profiles picker: a named provider/approvalPolicy/
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
  const { provider, approvalPolicy, thinkingLevel } = runConfig?.state ?? {
    provider: "claude-native" as Provider,
    approvalPolicy: "manual" as ApprovalPolicy,
    thinkingLevel: "" as ThinkingLevel,
  };
  const [submitting, setSubmitting] = useState(false);
  const [stopping, setStopping] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [queued, setQueued] = useState<string[]>([]);

  // A queued follow-up belongs to the task it was typed for; switching
  // tasks drops it rather than firing it at whatever is selected next.
  const lastTaskId = useRef<number | null>(taskId);
  if (lastTaskId.current !== taskId) {
    lastTaskId.current = taskId;
    if (queued.length > 0) setQueued([]);
  }

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
    async (text: string) => {
      setSubmitting(true);
      setFormError(null);
      try {
        await onSubmit(provider, text, approvalPolicy, provider === "claude-native" && thinkingLevel ? thinkingLevel : undefined);
        return true;
      } catch (err) {
        setFormError(err instanceof Error ? err.message : String(err));
        return false;
      } finally {
        setSubmitting(false);
      }
    },
    [onSubmit, provider, approvalPolicy, thinkingLevel],
  );

  // draft is a fresh object every render (it closes over the current
  // text), so the drain effect reads it through a ref instead of taking
  // it as a dependency -- otherwise every keystroke would re-run a drain.
  const draftRef = useRef(draft);
  draftRef.current = draft;

  // Drains one queued prompt per terminal run. draining guards against a
  // second drain being kicked off by the re-render that setQueued causes
  // before onSubmit has had a chance to flip runningRunId back on.
  const draining = useRef(false);
  useEffect(() => {
    if (running || draining.current || queued.length === 0 || !canSend) return;
    const [next, ...rest] = queued;
    draining.current = true;
    setQueued(rest);
    void send(next)
      .then((ok) => {
        // A failed send puts the text back in the composer rather than
        // back in the queue: re-queuing would retry against the same
        // failing condition forever, with no way for the user to see or
        // edit what's looping.
        if (!ok) {
          const current = draftRef.current;
          current.setValue(current.value ? `${current.value}\n${next}` : next);
        }
      })
      .finally(() => {
        draining.current = false;
      });
  }, [running, queued, canSend, send]);

  const submit = useCallback(() => {
    const trimmed = draft.value.trim();
    if (!trimmed || !canSend || submitting) return;
    if (running) {
      setQueued((prev) => [...prev, trimmed]);
      draft.clear();
      return;
    }
    draft.clear();
    void send(trimmed).then((ok) => {
      // Restore the text if the run never started, so it isn't lost.
      if (!ok) draft.setValue(trimmed);
    });
  }, [draft, canSend, submitting, running, send]);

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
    if (e.key === "Escape" && running) {
      e.preventDefault();
      void handleStop();
      return;
    }
    // AC7 of docs/plans/active/web-keyboard-tabs.md, ported from Paseo's
    // Shift+Tab mode cycle (composer/agent-controls/mode.ts): cycles the
    // same approvalPolicy the toolbar's own Select controls, so the
    // Select re-rendering with the new value *is* the visible feedback --
    // no separate indicator to keep in sync.
    if (e.key === "Tab" && e.shiftKey && !inactive) {
      const next = resolveNextApprovalPolicy(runConfig?.meta.approvalPolicies ?? [], approvalPolicy);
      if (next) {
        e.preventDefault();
        runConfig?.actions.setApprovalPolicy(next);
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
      {queued.length > 0 && (
        <ul data-testid="composer-queue" className="flex flex-col gap-1">
          {queued.map((text, index) => (
            <li
              key={`${index}-${text}`}
              data-testid="composer-queued-item"
              className="flex items-center gap-2 rounded-md bg-surface px-2 py-1 text-ui-sm text-foreground-muted"
            >
              <span className="shrink-0 uppercase">Queued</span>
              <span className="min-w-0 flex-1 truncate">{text}</span>
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                aria-label={`Remove queued prompt ${index + 1}`}
                onClick={() => setQueued((prev) => prev.filter((_, i) => i !== index))}
              >
                ×
              </Button>
            </li>
          ))}
        </ul>
      )}

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
            <RunConfigToolbar.Approval />
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
