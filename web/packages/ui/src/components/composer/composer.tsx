import { useCallback, useEffect, useRef, useState, type FormEvent, type KeyboardEvent, type Ref } from "react";

import { GitCompare } from "lucide-react";

import { useComposerDraft } from "@/components/composer/use-composer-draft";
import { PromptTextarea } from "@/components/composer/prompt-textarea";
import { Button } from "@/components/ui/button";
import { type DiffStat } from "@/lib/diff-stat";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { ApprovalPolicy, Provider, ProviderInfo, ProviderListResult } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

/** Used until provider.list answers (and kept if it fails) so the composer is never unusable because one fetch lost. */
const FALLBACK_PROVIDERS: ProviderInfo[] = [{ id: "claude-native" }, { id: "glm" }];

// internal/taskrunner.ApprovalPolicy's two values. "manual" is first since
// it's the daemon's default when run.start omits the field entirely.
const APPROVAL_POLICIES: { id: ApprovalPolicy; label: string }[] = [
  { id: "manual", label: "Manual approval" },
  { id: "auto-safe", label: "Auto-safe" },
];

const APPROVAL_POLICY_HELP =
  "Auto-safe auto-approves allowlisted read-only verification commands (e.g. gofmt, go vet, go test); everything else still needs human approval.";

// Item 21: 44px (WCAG 2.5.5 AAA / Apple HIG) below the compact breakpoint,
// the original dense sizing at `md:` and above -- see
// COMPACT_TOUCH_BUTTON_CLASS's doc comment for why this is plain
// responsive Tailwind rather than a threaded `isMobile` prop.
//
// Label-less, per web-ui-dogfood-polish Item 5: the selects sit inside the
// input card's bottom toolbar, so their visible chrome is nothing -- the
// card is the border, and cn()'s tailwind-merge strips SelectTrigger's
// own border/background halves in favour of these.
const SELECT_TRIGGER_CLASS =
  "h-11 shrink-0 border-0 bg-transparent px-2 text-sm hover:bg-accent md:h-7 md:px-1.5 md:text-xs";

const COMPACT_TOUCH_ACTION_BUTTON_CLASS = "h-11 px-4 text-sm md:h-7 md:px-2.5 md:text-[0.8rem]";

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
      className="flex shrink-0 items-center gap-1.5 rounded-full border border-input bg-surface-2 px-2.5 py-0.5 text-xs text-foreground-muted transition-colors hover:bg-accent hover:text-foreground"
    >
      <GitCompare aria-hidden className="size-3 opacity-70" />
      <span className="font-medium text-status-success">+{stat.additions}</span>
      <span className="font-medium text-status-danger">−{stat.deletions}</span>
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
  onSubmit: (provider: Provider, prompt: string, approvalPolicy: ApprovalPolicy) => Promise<void>;
  onStop: (runId: string) => Promise<void>;
  /** Exposes the prompt textarea's DOM node -- what lets a plan review's "Chat about it" (Item 11) move focus into the composer without resolving the pending request. */
  textareaRef?: Ref<HTMLTextAreaElement>;
}) {
  const draft = useComposerDraft(taskId);
  const [providers, setProviders] = useState<ProviderInfo[]>(FALLBACK_PROVIDERS);
  const [provider, setProvider] = useState<Provider>("claude-native");
  const [approvalPolicy, setApprovalPolicy] = useState<ApprovalPolicy>("manual");
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
        await onSubmit(provider, text, approvalPolicy);
        return true;
      } catch (err) {
        setFormError(err instanceof Error ? err.message : String(err));
        return false;
      } finally {
        setSubmitting(false);
      }
    },
    [onSubmit, provider, approvalPolicy],
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
              className="flex items-center gap-2 rounded-md bg-surface-2 px-2 py-1 text-xs text-foreground-muted"
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
       */}
      <div
        data-testid="composer-card"
        className="flex flex-col rounded-xl border border-input bg-surface-2 focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50"
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

        <div className="flex items-center gap-1.5 border-t border-border/60 px-2 py-1.5">
          {/*
           * shadcn/Radix rather than a native <select>: the closed state
           * was already styled to match, but the *open* list was OS chrome
           * -- square, light-mode-only, ignoring bg-popover/text-popover-
           * foreground like every other menu in the app. The accessible
           * name survives the move into the toolbar via aria-label, since
           * there is no visible <label> beside it anymore.
           */}
          <Select
            value={provider}
            onValueChange={(value) => setProvider(value as Provider)}
            disabled={inactive}
          >
            <SelectTrigger aria-label="Provider" className={SELECT_TRIGGER_CLASS}>
              <SelectValue placeholder="Select provider" />
            </SelectTrigger>
            <SelectContent>
              {providers.map((p) => (
                <SelectItem key={p.id} value={p.id}>
                  {p.label ?? p.id}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          <Select
            value={approvalPolicy}
            onValueChange={(value) => setApprovalPolicy(value as ApprovalPolicy)}
            disabled={inactive}
          >
            {/* The help text stays a plain `title` -- a hover tooltip on the
                trigger, exactly where it was on the native select. */}
            <SelectTrigger aria-label="Approval policy" title={APPROVAL_POLICY_HELP} className={SELECT_TRIGGER_CLASS}>
              <SelectValue placeholder="Select policy" />
            </SelectTrigger>
            <SelectContent>
              {APPROVAL_POLICIES.map((p) => (
                <SelectItem key={p.id} value={p.id}>
                  {p.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          <div className="ml-auto flex items-center gap-2">
            {formError && <span className="text-xs text-destructive">{formError}</span>}
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
      </div>
    </form>
  );
}
