import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import { RotateCcw } from "lucide-react";

import { readRunConfigPreference, writeRunConfigPreference } from "@/components/composer/run-config-preference";
import { readStoredDefaultAgentId } from "@/lib/settings-preferences";
import { cn } from "@/lib/utils";
import {
  AUTO_ACCEPT_HELP,
  AUTO_ACCEPT_LABEL,
  describePermission,
  effectiveMode,
  providerModes,
  supportsAutoAccept,
} from "@/lib/permission-modes";
import { THINKING_LEVELS, thinkingLevelLabel } from "@/lib/thinking-levels";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { AgentProfile, ModeInfo, Provider, ProviderInfo, ThinkingLevel } from "@/lib/types";

// Item 21: 44px (WCAG 2.5.5 AAA / Apple HIG) below the compact breakpoint,
// the original dense sizing at `md:` and above -- plain responsive Tailwind
// rather than a threaded `isMobile` prop (see composer.tsx's
// COMPACT_TOUCH_BUTTON_CLASS for the same call).
//
// Label-less, per web-ui-dogfood-polish Item 5: the selects sit inside the
// input card's bottom toolbar, so their visible chrome is nothing -- the
// card is the border, and cn()'s tailwind-merge strips SelectTrigger's own
// border/background halves in favour of these.
const SELECT_TRIGGER_CLASS =
  "h-11 shrink-0 border-0 bg-transparent px-2 text-ui-base hover:bg-hover md:h-7 md:px-1.5 md:text-ui-sm";

/** The Agent trigger button matches SELECT_TRIGGER_CLASS's sizing/touch-target rules, but Button's own variant system needs its background/border zeroed the same way SelectTrigger's are (see that class's own doc comment). */
const AGENT_TRIGGER_CLASS =
  "h-11 shrink-0 gap-1 rounded-lg border-0 bg-transparent px-2 text-ui-base font-normal hover:bg-hover md:h-7 md:px-1.5 md:text-ui-sm";

/**
 * The per-run configuration the composer submits with (run-config IA
 * plan): which agent (profile) was picked, if any, and the three fields a
 * pick seeds.
 *
 * `baseAgentId` is sticky once an agent is picked -- it stays set even
 * after a field is hand-edited, so `custom` can mean "these fields have
 * drifted from `baseAgentId`'s stored config" rather than "no agent is
 * associated with this run at all". `custom` is a per-run override only:
 * hand-editing a field never writes back to the stored profile, and
 * `resetToAgent` is what restores `baseAgentId`'s own values.
 */
export interface RunConfigState {
  /** The last-applied agent profile's id, or null when no agent has ever been picked (or "No agent" was explicitly chosen). */
  baseAgentId: string | null;
  /** True once any field has been hand-edited since baseAgentId was applied. */
  custom: boolean;
  provider: Provider;
  /** One of the provider's own permission mode ids (ADR-0019); "" means the provider's default. */
  permissionMode: string;
  /** ACP providers only: approve every permission prompt. */
  autoAccept: boolean;
  thinkingLevel: ThinkingLevel;
}

export interface RunConfigContextValue {
  state: RunConfigState;
  actions: {
    setProvider: (provider: Provider) => void;
    setPermissionMode: (mode: string) => void;
    setAutoAccept: (autoAccept: boolean) => void;
    setThinkingLevel: (level: ThinkingLevel) => void;
    /** Applies a profile's stored config to the three fields (ADR-0014's client-side seed) and marks it as the base agent. */
    applyProfile: (profileId: string) => void;
    /** The agent menu's "No agent" entry: detaches from baseAgentId without touching the current field values. */
    clearAgent: () => void;
    /** The ↺ control: re-applies baseAgentId's own stored config, discarding any hand-edits. No-op when no agent is applied. */
    resetToAgent: () => void;
  };
  meta: {
    profiles: AgentProfile[];
    providers: ProviderInfo[];
    /** The selected provider's own permission modes (provider.list's catalog). */
    modes: ModeInfo[];
    /** state.permissionMode resolved against the provider default -- what the run will actually use. */
    effectiveMode: string;
    /** Whether the selected provider takes autoAccept (ACP). */
    supportsAutoAccept: boolean;
    disabled: boolean;
    /** ADR-0016 P3: the chat's own bound provider (chats.Provider, set on its first run and immutable after) -- non-null makes the Provider select read-only, since a stored agent session is provider-native. */
    boundProvider: Provider | null;
  };
}

const RunConfigContext = createContext<RunConfigContextValue | null>(null);

/** Reads the composer toolbar's shared run config. Only valid inside RunConfigToolbar. */
export function useRunConfig(): RunConfigContextValue {
  const ctx = useContext(RunConfigContext);
  if (!ctx) throw new Error("useRunConfig must be used inside <RunConfigToolbar>");
  return ctx;
}

const EMPTY_STATE: RunConfigState = { baseAgentId: null, custom: false, provider: "claude-native", permissionMode: "", autoAccept: false, thinkingLevel: "" };

/** Applies profile p's stored fields on top of prev -- shared by applyProfile and resetToAgent so "reset" really means "re-apply the same profile". A mode is provider-scoped, so it always comes from the profile ("" = that provider's default) rather than carrying prev's (possibly another provider's) mode over; an empty ThinkingLevel leaves the current value alone. */
function applyProfileFields(prev: RunConfigState, p: AgentProfile): RunConfigState {
  return {
    baseAgentId: String(p.ID),
    custom: false,
    provider: p.Provider as Provider,
    permissionMode: p.PermissionMode,
    autoAccept: p.AutoAccept,
    thinkingLevel: (p.ThinkingLevel || prev.thinkingLevel) as ThinkingLevel,
  };
}

/** The initial state for chatId: its own persisted run-config if there is one (falling back to the task's legacy pre-ADR-0016 key for its default chat), else EMPTY_STATE -- the ★ default agent (if any) is seeded separately, once profiles have loaded (see the effect below), since it can't be looked up before profile.list answers. */
function initialStateFor(taskId: number | null, chatId: number | null, isDefaultChat: boolean): RunConfigState {
  return readRunConfigPreference(taskId, chatId, isDefaultChat) ?? EMPTY_STATE;
}

/**
 * The composer's run-config toolbar (run-config IA plan): one compound
 * component -- `RunConfigToolbar.Agent`, `.Provider`, `.Mode`,
 * `.Thinking` -- sharing a single context that holds
 * `{baseAgentId, custom, provider, permissionMode, autoAccept, thinkingLevel}`. The
 * provider is the only place state lives; the parts are pure readers
 * (vercel composition-patterns' state-context-interface / state-decouple-
 * implementation).
 *
 * The state is persisted per task (docs/design.md §9's mechanism, same
 * per-task-key shape as use-composer-draft.ts) so the task header's
 * run-config pill (task-detail.tsx) reflects durable config, not just
 * whatever happens to be in memory -- reloading the page or reopening a
 * closed tab keeps the same picked agent / hand-edits.
 *
 * GLM/Kimi's live ACP run-config options render in this same row (they
 * arrive only once a session is live; the composer mounts them as extra
 * children between the Thinking part and the send controls).
 */
export function RunConfigToolbar({
  taskId,
  chatId,
  isDefaultChat = false,
  boundProvider = null,
  profiles,
  providers,
  disabled = false,
  onChange,
  children,
}: {
  /** Keys the per-task legacy-migration lookup only now (ADR-0016 P3 moved the actual persisted state to the chat) -- still needed alongside chatId to form the storage key. null renders with EMPTY_STATE and persists nothing. */
  taskId: number | null;
  /** Keys the per-chat persisted state and the ★ default-agent seed (a chat with no persisted state yet starts from it). null renders with EMPTY_STATE and persists nothing. */
  chatId: number | null;
  /** Whether chatId is taskId's default (oldest) chat -- the only one that migrates the pre-ADR-0016 per-task-only persisted key (see readRunConfigPreference). */
  isDefaultChat?: boolean;
  /** The chat's own bound provider (chats.Provider), once its first run has set one -- forces state.provider to match and makes the Provider select read-only (ADR-0016 P3). null (not yet bound) leaves the provider freely selectable, as today. */
  boundProvider?: Provider | null;
  profiles: AgentProfile[];
  providers: ProviderInfo[];
  disabled?: boolean;
  /**
   * Fires whenever the shared state changes, with the full context value
   * (state + actions) -- the composer submits a run from `state` and lets
   * its textarea's Shift+Tab approval cycle write back through `actions`,
   * without re-implementing either where the keydown lives.
   */
  onChange?: (value: RunConfigContextValue) => void;
  /** The row's parts in layout order, plus any live ACP option controls. */
  children?: ReactNode;
}) {
  const [state, setState] = useState<RunConfigState>(() => initialStateFor(taskId, chatId, isDefaultChat));

  // Render-phase "adjust state when a prop changes" (same pattern as
  // use-composer-draft.ts's taskId handling): reads the new chat's
  // persisted run-config during the same render that saw the new id, so a
  // chat switch never paints one frame of the previous chat's config.
  // The storage key is the (taskId, chatId) pair (run-config-preference.ts),
  // so "did the selection change" must compare both -- chatId alone would
  // miss a same-chatId-different-task switch (the toolbar's own chatId prop
  // is never reused across tasks in production, since chat ids are globally
  // unique, but standalone tests that pin a fixed chatId across a taskId
  // rerender depend on this being right too).
  const lastSelection = useRef<{ taskId: number | null; chatId: number | null }>({ taskId, chatId });
  const hadPersisted = useRef<boolean>(readRunConfigPreference(taskId, chatId, isDefaultChat) !== null);
  const seededFromDefault = useRef(false);
  if (lastSelection.current.taskId !== taskId || lastSelection.current.chatId !== chatId) {
    lastSelection.current = { taskId, chatId };
    hadPersisted.current = readRunConfigPreference(taskId, chatId, isDefaultChat) !== null;
    seededFromDefault.current = false;
    setState(initialStateFor(taskId, chatId, isDefaultChat));
  }

  // A bound provider overrides whatever the persisted/hand-picked state
  // says the moment it's known (or changes) -- a chat's provider is
  // immutable after its first run, so the toolbar must never show (or let
  // the user pick) anything else, regardless of what was seeded above.
  useEffect(() => {
    if (!boundProvider) return;
    setState((prev) => (prev.provider === boundProvider ? prev : { ...prev, provider: boundProvider, permissionMode: "", autoAccept: false }));
  }, [boundProvider]);

  // The ★ default agent (Settings -> Agents): a task that has never had a
  // run-config persisted for it starts from whichever profile is marked
  // default, once profiles have actually loaded (profile.list is async --
  // an empty `profiles` on the first render just means "not answered yet",
  // not "no default"). Fires at most once per task: seededFromDefault
  // guards against re-seeding after the user has since hand-picked "No
  // agent" or a different agent.
  useEffect(() => {
    if (hadPersisted.current || seededFromDefault.current) return;
    const defaultAgentId = readStoredDefaultAgentId();
    if (!defaultAgentId) return;
    const p = profiles.find((candidate) => String(candidate.ID) === defaultAgentId);
    if (!p) return;
    seededFromDefault.current = true;
    setState((prev) => applyProfileFields(prev, p));
  }, [profiles]);

  const applyProfile = useCallback(
    (profileId: string) => {
      const p = profiles.find((candidate) => String(candidate.ID) === profileId);
      if (!p) return;
      seededFromDefault.current = true;
      setState((prev) => applyProfileFields(prev, p));
    },
    [profiles],
  );

  const clearAgent = useCallback(() => {
    seededFromDefault.current = true;
    setState((prev) => ({ ...prev, baseAgentId: null, custom: false }));
  }, []);

  const resetToAgent = useCallback(() => {
    setState((prev) => {
      if (!prev.baseAgentId) return prev;
      const p = profiles.find((candidate) => String(candidate.ID) === prev.baseAgentId);
      if (!p) return prev;
      return applyProfileFields(prev, p);
    });
  }, [profiles]);

  const setField = useCallback(
    (patch: Partial<Pick<RunConfigState, "provider" | "permissionMode" | "autoAccept" | "thinkingLevel">>) => {
      setState((prev) => ({ ...prev, ...patch, custom: prev.baseAgentId !== null ? true : prev.custom }));
    },
    [],
  );
  // Modes are provider-scoped (W1): switching provider resets to the new
  // provider's own default mode and turns autoAccept off.
  const setProvider = useCallback(
    (provider: Provider) => setField({ provider, permissionMode: "", autoAccept: false }),
    [setField],
  );
  const setPermissionMode = useCallback((permissionMode: string) => setField({ permissionMode }), [setField]);
  const setAutoAccept = useCallback((autoAccept: boolean) => setField({ autoAccept }), [setField]);
  const setThinkingLevel = useCallback((thinkingLevel: ThinkingLevel) => setField({ thinkingLevel }), [setField]);

  // The command palette's "Use agent: <name>" entry lands here (the
  // sidebar owns the palette entry; this toolbar owns the state). The
  // event carries the full AgentProfile the sidebar fetched -- profiles
  // may differ between the two fetches, so it is matched by ID and
  // re-looked-up in this toolbar's own list, falling back to the
  // dispatched copy so a just-created profile still applies.
  useEffect(() => {
    const onUseAgent = (e: Event) => {
      const dispatched = (e as CustomEvent<AgentProfile>).detail;
      if (!dispatched) return;
      const p = profiles.find((candidate) => candidate.ID === dispatched.ID) ?? dispatched;
      seededFromDefault.current = true;
      setState((prev) => applyProfileFields(prev, p));
    };
    window.addEventListener("smind:use-agent", onUseAgent);
    return () => window.removeEventListener("smind:use-agent", onUseAgent);
  }, [profiles]);

  const value = useMemo<RunConfigContextValue>(() => {
    return {
      state,
      actions: { setProvider, setPermissionMode, setAutoAccept, setThinkingLevel, applyProfile, clearAgent, resetToAgent },
      meta: {
        profiles,
        providers,
        modes: providerModes(providers, state.provider),
        effectiveMode: effectiveMode(providers, state.provider, state.permissionMode),
        supportsAutoAccept: supportsAutoAccept(providers, state.provider),
        disabled,
        boundProvider,
      },
    };
  }, [state, setProvider, setPermissionMode, setAutoAccept, setThinkingLevel, applyProfile, clearAgent, resetToAgent, profiles, providers, disabled, boundProvider]);

  useEffect(() => {
    onChange?.(value);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [onChange, value]);

  // Persists on every state change (docs/design.md §9) -- taskId or chatId
  // === null (no chat selected) writes nothing, matching
  // use-composer-draft.ts's own no-op-when-null contract.
  useEffect(() => {
    writeRunConfigPreference(taskId, chatId, state);
  }, [taskId, chatId, state]);

  return <RunConfigContext.Provider value={value}>{children}</RunConfigContext.Provider>;
}

/** "<provider> · <mode> · <thinking>" -- the agent menu's per-row metadata and the header pill share this. The mode is the provider's own label (ADR-0019). Thinking is omitted for a non-Claude profile, mirroring the Thinking control's own visibility rule. */
export function describeProfile(p: AgentProfile, providers: ProviderInfo[]): string {
  const providerLabel = providers.find((candidate) => candidate.id === p.Provider)?.label ?? p.Provider;
  const parts = [providerLabel, describePermission(providers, p.Provider, p.PermissionMode, p.AutoAccept)];
  if (p.Provider === "claude-native") parts.push(thinkingLevelLabel(p.ThinkingLevel as ThinkingLevel));
  return parts.join(" · ");
}

/**
 * shadcn/Radix DropdownMenu rather than Select: the AC's menu shape --
 * each profile's row carrying its own provider/approval/thinking
 * metadata, a "No agent" entry, a separator, and a "Manage agents…"
 * entry -- is a richer list than Select's single-line SelectItem/
 * SelectValue pairing supports cleanly. The trigger's own accessible name
 * stays the static "Agents" (aria-label) regardless of what it displays,
 * matching every other label-less toolbar control here.
 */
function RunConfigToolbarAgent() {
  const { state, actions, meta } = useRunConfig();
  if (meta.profiles.length === 0) return null;

  const baseAgent = state.baseAgentId ? meta.profiles.find((p) => String(p.ID) === state.baseAgentId) : undefined;
  const noAgent = !state.baseAgentId;
  // "No agent" (muted) matches the header pill's and the menu's own "No
  // agent" wording -- it used to just say "Agents" here, reading like a
  // placeholder rather than a real state.
  const triggerLabel = noAgent ? "No agent" : state.custom ? `Custom · from ${baseAgent?.Name ?? "agent"}` : (baseAgent?.Name ?? "No agent");

  return (
    <div className="flex shrink-0 items-center">
      <DropdownMenu>
        <DropdownMenuTrigger asChild disabled={meta.disabled}>
          <Button
            type="button"
            variant="ghost"
            aria-label="Agents"
            data-testid="composer-agent-select"
            disabled={meta.disabled}
            className={cn(AGENT_TRIGGER_CLASS, noAgent && "text-foreground-muted")}
          >
            {triggerLabel}
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="max-w-sm">
          <DropdownMenuItem data-testid="agent-menu-no-agent" onSelect={() => actions.clearAgent()}>
            No agent
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          {meta.profiles.map((p) => (
            <DropdownMenuItem
              key={p.ID}
              data-testid={`agent-menu-item-${p.ID}`}
              title={p.Notes || undefined}
              onSelect={() => actions.applyProfile(String(p.ID))}
            >
              <span className="min-w-0 truncate">
                {p.Name} <span className="text-foreground-muted">({describeProfile(p, meta.providers)})</span>
              </span>
            </DropdownMenuItem>
          ))}
          <DropdownMenuSeparator />
          <DropdownMenuItem
            data-testid="agent-menu-manage"
            onSelect={() => window.dispatchEvent(new CustomEvent("smind:open-settings", { detail: { sectionId: "agents" } }))}
          >
            Manage agents…
            <span className="ml-auto text-foreground-subtlest">⌘,</span>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      {state.custom && (
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          aria-label={`Reset to ${baseAgent?.Name ?? "agent"} defaults`}
          title={`Reset to ${baseAgent?.Name ?? "agent"}'s saved config`}
          data-testid="composer-agent-reset"
          disabled={meta.disabled}
          onClick={() => actions.resetToAgent()}
        >
          <RotateCcw aria-hidden />
        </Button>
      )}
    </div>
  );
}

function RunConfigToolbarProvider() {
  const { state, actions, meta } = useRunConfig();
  // Once a chat's provider is bound (its first run), the selector goes
  // read-only (ADR-0016 P3: switching providers mid-chat would silently
  // drop the stored agent session, which is provider-native) -- a tooltip
  // on the trigger explains why, the same "disabled says why" convention
  // composerPlaceholder already follows for the composer itself.
  const boundLabel = meta.boundProvider ? (meta.providers.find((p) => p.id === meta.boundProvider)?.label ?? meta.boundProvider) : null;
  return (
    <Select
      value={state.provider}
      // Radix's Select fires a spurious onValueChange("") of its own --
      // not from any user interaction -- the moment a `disabled` select's
      // value settles (observed right after a chat's provider gets bound
      // and this select flips to read-only). Ignoring the empty string is
      // what stops that from corrupting `state.provider` to "" -- a real
      // pick from the list is never itself an empty id.
      onValueChange={(v) => {
        if (v) actions.setProvider(v as Provider);
      }}
      disabled={meta.disabled || Boolean(meta.boundProvider)}
    >
      <SelectTrigger
        aria-label="Provider"
        title={boundLabel ? `This chat is bound to ${boundLabel}; start a new chat to switch` : undefined}
        data-testid="composer-provider-bound"
        data-bound={boundLabel ? "true" : undefined}
        className={SELECT_TRIGGER_CLASS}
      >
        <SelectValue placeholder="Select provider" />
      </SelectTrigger>
      <SelectContent>
        {meta.providers.map((p) => (
          <SelectItem key={p.id} value={p.id}>
            {p.label ?? p.id}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

/**
 * The provider's own permission modes (ADR-0019), straight from
 * provider.list -- labels and help text are the provider's words. For an
 * ACP provider an Auto-accept toggle sits beside it (Paseo's auto_accept:
 * approve every prompt), never switched on by default.
 */
function RunConfigToolbarMode() {
  const { state, actions, meta } = useRunConfig();
  const current = meta.modes.find((m) => m.id === meta.effectiveMode);
  return (
    <>
      <Select value={meta.effectiveMode} onValueChange={(v) => v && actions.setPermissionMode(v)} disabled={meta.disabled}>
        <SelectTrigger aria-label="Permission mode" title={current?.description} className={SELECT_TRIGGER_CLASS}>
          <SelectValue placeholder="Select mode" />
        </SelectTrigger>
        <SelectContent>
          {meta.modes.map((m) => (
            <SelectItem key={m.id} value={m.id} title={m.description}>
              {m.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {meta.supportsAutoAccept && (
        <Button
          type="button"
          variant="ghost"
          aria-pressed={state.autoAccept}
          title={AUTO_ACCEPT_HELP}
          data-testid="composer-auto-accept"
          disabled={meta.disabled}
          onClick={() => actions.setAutoAccept(!state.autoAccept)}
          className={cn(AGENT_TRIGGER_CLASS, state.autoAccept ? "text-foreground" : "text-foreground-muted")}
        >
          {AUTO_ACCEPT_LABEL}
          {state.autoAccept ? ": on" : ": off"}
        </Button>
      )}
    </>
  );
}

function RunConfigToolbarThinking() {
  const { state, actions, meta } = useRunConfig();
  // Claude-only, pre-run control (see thinking-levels.ts's doc comment) --
  // omitted entirely, not just disabled, for every other provider: no
  // dead control sitting in the toolbar for a provider that can't act on
  // it. GLM/Kimi's own thinking-level control lives in this same row
  // instead, once a live session exists (see run-config-options in
  // task-detail), since that option list is only ever known after ACP's
  // NewSession responds.
  if (state.provider !== "claude-native") return null;
  return (
    <Select
      value={state.thinkingLevel || "standard"}
      onValueChange={(v) => actions.setThinkingLevel(v as ThinkingLevel)}
      disabled={meta.disabled}
    >
      <SelectTrigger
        aria-label="Thinking level"
        title={THINKING_LEVELS.find((t) => t.id === (state.thinkingLevel || "standard"))?.help}
        className={SELECT_TRIGGER_CLASS}
      >
        <SelectValue placeholder="Select thinking level" />
      </SelectTrigger>
      <SelectContent>
        {THINKING_LEVELS.map((t) => (
          <SelectItem key={t.id} value={t.id} title={t.help}>
            {t.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

RunConfigToolbar.Agent = RunConfigToolbarAgent;
RunConfigToolbar.Provider = RunConfigToolbarProvider;
RunConfigToolbar.Mode = RunConfigToolbarMode;
RunConfigToolbar.Thinking = RunConfigToolbarThinking;
