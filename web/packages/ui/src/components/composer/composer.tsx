import { useCallback, useEffect, useRef, useState, type FormEvent, type KeyboardEvent, type Ref } from "react";

import { Icons } from "@/lib/icons";

import { ComposerStatusChips } from "@/components/composer/composer-status-chips";
import { QueuedPrompts } from "@/components/composer/queued-prompts";
import { resolveNextPermissionMode } from "@/components/composer/permission-mode-cycle";
import { useComposerDraft } from "@/components/composer/use-composer-draft";
import { useProviderCatalog } from "@/hooks/use-provider-catalog";
import { useProviderHealth } from "@/hooks/use-provider-health";
import { useProfileList } from "@/hooks/use-profiles";
import type { SubmitResult } from "@/hooks/use-run-timeline";
import { PromptTextarea } from "@/components/composer/prompt-textarea";
import { RunConfigToolbar, type LiveRunControls, type RunConfigContextValue, type RunConfigState } from "@/components/composer/run-config-toolbar";
import { RunConfigOptions } from "@/components/run-config-options";
import { Button } from "@/components/ui/button";
import { useActionHandler, useKeyboard } from "@/keyboard/keyboard-provider";
import { formatCombo } from "@/keyboard/shortcut-string";
import { type DiffStat } from "@/lib/diff-stat";
import type {
  ChatQueueItem,
  ConfigOptionParams,
  PermissionSettings,
  Provider,
  ThinkingLevel,
} from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

/** Icon-only send cluster: 44px touch targets below md (Item 21), ZCode's 32px rounded squares from md up. */
const ICON_ACTION_BUTTON_CLASS = "size-11 rounded-xl md:size-8";

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
 * The task composer (ui-redesign-parity Item 10, reshaped to the input
 * card anatomy by web-ui-dogfood-polish Item 5, then to ZCode's card with
 * Paseo's chips and focus hint by docs/plans/active/composer-redesign.md):
 * one rounded card whose top is the autogrowing prompt textarea (two rows
 * at rest) and whose bottom edge is the toolbar -- icon+label run-config
 * pills on the left, the icon-only Send/Stop cluster on the right -- with
 * the status chips (Working, diff stat) above the card. The only
 * affordances shown are ones that work -- no attachment "+", mic or voice
 * buttons until those features exist.
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
  prefill,
  toolbarRef,
  onRunConfigChange,
  configOptions,
  otherRunningChat,
  onJumpToChat,
  liveRun,
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
    model?: string,
  ) => Promise<SubmitResult>;
  onStop: (runId: string) => Promise<void>;
  /** Exposes the prompt textarea's DOM node -- what lets a plan review's "Chat about it" (Item 11) move focus into the composer without resolving the pending request. */
  textareaRef?: Ref<HTMLTextAreaElement>;
  /** "Edit & resend": each new `nonce` loads `text` into the draft (appended after a blank line when the draft is non-empty), then focuses the textarea with the caret at the end. */
  prefill?: { text: string; nonce: number };
  /** Exposes the run-config toolbar row's DOM node -- what the task header's run-config pill (task-detail.tsx) focuses when clicked. */
  toolbarRef?: Ref<HTMLDivElement>;
  /** Fires whenever RunConfigToolbar's shared state changes -- lets task-detail.tsx mirror it for the header pill without owning the state itself (run-config IA: "display-only, derived from the same state the toolbar reads"). */
  onRunConfigChange?: (state: RunConfigState) => void;
  /** Another chat of this task with a live run in the shared worktree (ADR-0016 P3) -- a chip in the status row. */
  otherRunningChat?: { id: number; title: string } | null;
  /** Jumps to otherRunningChat's tab. */
  onJumpToChat?: (chatId: number) => void;
  /** The running run's mode controls (D1): while set, the toolbar's Mode/Auto-accept pills drive that run via run.setPermissionMode instead of the next run's config. Omit when there is no live-switch-capable run. */
  liveRun?: LiveRunControls | null;
  /** GLM/Kimi's live ACP config options for the currently-running run, if any -- rendered inside this row per run-config IA's "same row, not a separate control" requirement. */
  configOptions?: {
    options: ConfigOptionParams[];
    error: string | null;
    onSetOption: (configId: string, value: string) => Promise<void>;
  };
}) {
  const draft = useComposerDraft(taskId);
  // ADR-0014's Profiles picker: a named provider/permissionMode/
  // thinkingLevel bundle a user applies in one click. Empty when the
  // daemon has none (a fresh install, or client.call failing) -- the
  // control below renders nothing in that case, so the composer's
  // pre-profiles defaults are exactly what an empty-profiles daemon still
  // gets (AC15's regression requirement).
  // One provider.list / profile.list cache entry per connection (ADR-0032):
  // the picker's provider list and Profiles picker read the shared cache,
  // kept live by events and mutations -- no per-composer fetch effects.
  const { providersOrFallback: providers, settled: providersSettled } = useProviderCatalog();
  // The Profiles picker: empty when the daemon has none, so the composer's
  // pre-profiles defaults are exactly what an empty-profiles daemon gets.
  const profiles = useProfileList();
  // Lazy provider.test results for the combined picker: fetched when it first opens, not on render.
  const providerHealth = useProviderHealth();
  // The run config itself lives in RunConfigToolbar's shared context
  // (run-config IA); this mirror is what the composer's own submit and
  // Shift+Tab cycle read, updated by the toolbar's onChange.
  const [runConfig, setRunConfig] = useState<RunConfigContextValue | null>(null);
  const { provider, permissionMode, autoAccept, thinkingLevel, model } = runConfig?.state ?? {
    provider: "claude-native" as Provider,
    permissionMode: "",
    autoAccept: false,
    thinkingLevel: "" as ThinkingLevel,
    model: "",
  };
  const [submitting, setSubmitting] = useState(false);
  const [stopping, setStopping] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [focused, setFocused] = useState(false);
  const textareaNode = useRef<HTMLTextAreaElement | null>(null);
  const setTextareaNode = useCallback(
    (node: HTMLTextAreaElement | null) => {
      textareaNode.current = node;
      if (typeof textareaRef === "function") textareaRef(node);
      else if (textareaRef) textareaRef.current = node;
    },
    [textareaRef],
  );

  // Applies each new prefill nonce once. The ref starts at the mount-time
  // nonce so a remount does not re-apply an old prefill.
  const draftRef = useRef(draft);
  draftRef.current = draft;
  const appliedPrefill = useRef(prefill?.nonce);
  useEffect(() => {
    if (!prefill || prefill.nonce === appliedPrefill.current) return;
    appliedPrefill.current = prefill.nonce;
    const current = draftRef.current.value;
    const next = current.trim() === "" ? prefill.text : `${current}\n\n${prefill.text}`;
    draftRef.current.setValue(next);
    const node = textareaNode.current;
    if (!node) return;
    node.focus();
    // The controlled value lands on the next render; defer the caret move.
    requestAnimationFrame(() => node.setSelectionRange(next.length, next.length));
  }, [prefill]);

  // `composer.focus` is in the shortcut registry but nothing claimed it
  // until now. The hint shows the *effective* combo (a user rebinding is
  // honoured; an unbound action shows nothing), Paseo-style: only while the
  // box is unfocused and empty, and only where a keyboard exists (md+).
  useActionHandler("composer.focus", () => textareaNode.current?.focus());
  const { bindings, isMac } = useKeyboard();
  const focusCombo = bindings.find((b) => b.id === "composer-focus")?.effectiveCombo ?? null;

  const canSend = connected && taskId !== null;
  const running = runningRunId !== null;

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
          model || undefined,
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
    [onSubmit, provider, permissionMode, autoAccept, thinkingLevel, model, onQueued],
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
      const next = liveRun
        ? resolveNextPermissionMode(liveRun.modes, liveRun.mode)
        : resolveNextPermissionMode(runConfig?.meta.modes ?? [], runConfig?.meta.effectiveMode ?? "");
      if (next) {
        e.preventDefault();
        if (liveRun) liveRun.onChange({ modeId: next }).catch(() => {});
        else runConfig?.actions.setPermissionMode(next);
      }
    }
  }

  function handleFormSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    submit();
  }

  const inactive = !canSend || submitting;
  const hasText = draft.value.trim() !== "";
  const showFocusHint = focusCombo !== null && canSend && !focused && draft.value === "";

  return (
    <form onSubmit={handleFormSubmit} data-testid="composer" className="mx-auto flex w-full max-w-3xl shrink-0 flex-col gap-2 px-4 pb-3 pt-2">
      <QueuedPrompts items={queuedItems} onCancel={onCancelQueued} />

      {/* Paseo's chip row above the card: the live-run chip and the diff
          stat -- real data only, nothing when neither applies. */}
      <ComposerStatusChips
        running={running}
        diffStat={diffStat}
        onOpenDiff={onOpenDiff}
        otherRunningChat={otherRunningChat}
        onJumpToChat={onJumpToChat}
      />

      {/*
       * The input card (Item 5): one rounded surface whose top is the
       * textarea and whose bottom edge is the toolbar, with no divider
       * between them (ZCode). The card carries the border/focus ring; the
       * textarea and toolbar controls inside are chrome-less, which is why
       * the selects are aria-labelled rather than sitting under visible
       * <label> text.
       *
       * `rounded-2xl`/`bg-input`/`border-input-border` (zcode-visual-parity
       * P3): the main chat input shell is one of DESIGN.md's three
       * approved `rounded-2xl` exceptions, and a visible resting border
       * (not just on focus) is ZCode's own "calm and integrated, not
       * glowing by default" input philosophy.
       */}
      <div
        data-testid="composer-card"
        className="relative flex flex-col rounded-2xl border border-input-border bg-input transition-colors hover:border-input-border-hover focus-within:border-input-border-focused focus-within:bg-input-focused"
      >
        <PromptTextarea
          ref={setTextareaNode}
          label="Prompt"
          rows={2}
          value={draft.value}
          onChange={draft.setValue}
          onSubmit={submit}
          onKeyDown={handleKeyDown}
          onFocus={() => setFocused(true)}
          onBlur={() => setFocused(false)}
          placeholder={composerPlaceholder({ connected, hasTask: taskId !== null, running })}
          disabled={inactive}
          className="rounded-none border-0 bg-transparent px-4 pb-1 pt-3.5 focus-visible:border-transparent focus-visible:ring-0"
        />
        {showFocusHint && (
          <span
            data-testid="composer-focus-hint"
            aria-hidden
            className="pointer-events-none absolute right-4 top-3.5 text-ui-sm text-foreground-subtlest max-md:hidden"
          >
            {formatCombo(focusCombo, isMac)} to focus
          </span>
        )}

        {formError && (
          <p role="alert" className="px-4 pb-1 text-ui-sm text-destructive">
            {formError}
          </p>
        )}

        {/*
         * The run-config toolbar (run-config IA): one compound component
         * whose parts share a single context, replacing the four
         * independent Selects that each carried their own state here.
         */}
        <RunConfigToolbar
          taskId={taskId}
          chatId={chatId}
          isDefaultChat={isDefaultChat}
          boundProvider={boundProvider}
          profiles={profiles}
          providers={providers}
          providersLoading={!!client && !providersSettled}
          disabled={inactive}
          liveRun={liveRun}
          providerHealth={providerHealth.health}
          onPickerOpen={providerHealth.ensure}
          onChange={(value) => {
            setRunConfig(value);
            onRunConfigChange?.(value.state);
          }}
        >
          <div
            ref={toolbarRef}
            tabIndex={-1}
            data-testid="composer-toolbar-row"
            className="flex flex-wrap items-center px-2 pb-2 pt-1 focus-visible:ring-3 focus-visible:ring-ring/50 rounded-xl"
          >
            <RunConfigToolbar.ModelPicker />
            <RunConfigToolbar.Thinking />
            <RunConfigToolbar.Mode />
            {configOptions && (
              <RunConfigOptions
                options={configOptions.options}
                error={configOptions.error}
                onSetOption={configOptions.onSetOption}
                omitMode={liveRun != null && liveRun.modes.length > 1}
              />
            )}
            <div className="ml-auto flex items-center gap-1">
              {running && hasText && (
                <Button
                  type="button"
                  variant="outline"
                  className={ICON_ACTION_BUTTON_CLASS}
                  disabled={inactive}
                  aria-label="Send now"
                  data-testid="chat-send-now-button"
                  title="Interrupt the running turn and send now (⌘/Ctrl+Enter)"
                  onClick={sendNow}
                >
                  <Icons.sendNow aria-hidden />
                </Button>
              )}
              {running && (
                <Button
                  type="button"
                  variant="execute"
                  className={ICON_ACTION_BUTTON_CLASS}
                  disabled={stopping}
                  aria-label={stopping ? "Stopping…" : "Stop"}
                  title="Stop the running turn (Esc)"
                  data-testid="chat-stop-button"
                  onClick={handleStop}
                >
                  <Icons.stop aria-hidden className="fill-current" />
                </Button>
              )}
              <Button
                type="submit"
                variant={running ? "default" : "execute"}
                className={ICON_ACTION_BUTTON_CLASS}
                disabled={inactive || !hasText}
                aria-label={running ? "Queue" : "Send"}
                title={running ? "Queue this prompt — it sends when the run finishes (Enter)" : "Send (Enter)"}
                data-testid="chat-send-button"
              >
                <Icons.send aria-hidden />
              </Button>
            </div>
          </div>
        </RunConfigToolbar>
      </div>
    </form>
  );
}
