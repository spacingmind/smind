import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import { Icons } from "@/lib/icons";

import { useObservedModel } from "@/hooks/use-observed-model";
import { ProviderModelPicker, type PickerProfileRow } from "@/components/pickers/provider-model-picker";
import { readRunConfigPreference, writeRunConfigPreference } from "@/components/composer/run-config-preference";
import { setSettingsIntent } from "@/lib/settings-intent";
import { buildBrowserProviders, type ProviderHealth } from "@/lib/model-browser";
import { useDefaultAgentStore } from "@/stores/default-agent-store";
import { selectProvider } from "@/stores/providers-view-store";
import {
  effectiveMode,
  providerModes,
  supportsAutoAccept,
} from "@/lib/permission-modes";
import { supportsThinking } from "@/lib/thinking-levels";
import { ModeSelect } from "@/components/pickers/mode-select";
import { ThinkingSelect } from "@/components/pickers/thinking-select";
import { AutoAcceptToggle } from "@/components/pickers/auto-accept-toggle";
import { describeProfile } from "@/lib/describe-profile";
import { Button } from "@/components/ui/button";
import type { AgentProfile, ModeInfo, Provider, ProviderInfo, ThinkingLevel } from "@/lib/types";

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
  /** ADR-0025: the requested model id for the selected provider; "" means the provider's default (nothing is sent). Provider-scoped like permissionMode, so a provider switch resets it. */
  model: string;
}

export interface RunConfigContextValue {
  state: RunConfigState;
  actions: {
    /** The combined picker's one action: picking a model under another provider switches provider and model together (mode/autoAccept reset to the new provider's defaults, ADR-0019); under the current provider only the model changes. "" = the provider's default model (ADR-0025). */
    setProviderModel: (provider: Provider, model: string) => void;
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
    /** provider.list has not settled yet: the combined picker shows loading states instead of the fallback catalog. */
    providersLoading: boolean;
    /** The selected provider's own permission modes (provider.list's catalog). */
    modes: ModeInfo[];
    /** provider.test results by provider id (lazy, see use-provider-health.ts) -- an unhealthy provider is shown disabled with its reason in the combined picker. */
    health: Record<string, ProviderHealth | undefined>;
    /** Fired each time the combined picker opens (the owner fetches provider health lazily). */
    onPickerOpen: (() => void) | undefined;
    /** state.permissionMode resolved against the provider default -- what the run will actually use. */
    effectiveMode: string;
    /** Whether the selected provider takes autoAccept (ACP). */
    supportsAutoAccept: boolean;
    disabled: boolean;
    /** ADR-0016 P3: the chat's own bound provider (chats.Provider, set on its first run and immutable after) -- non-null makes the combined picker offer only that provider, since a stored agent session is provider-native. */
    boundProvider: Provider | null;
    /** The chat this config belongs to (null before one exists): where the chip looks for the model a default resolved to. */
    chatId: number | null;
    /** Set while a live-switch-capable run is active: the Mode/Auto-accept pills drive it instead of the next run's config. */
    liveRun: LiveRunControls | null;
  };
}

/**
 * D1 of live-run-composer-and-noise: while a run is live on a provider that
 * supports a live switch (ADR-0019) the Mode and Auto-accept pills show and
 * drive the RUNNING run instead of the next run's config. Absent otherwise.
 */
export interface LiveRunControls {
  /** The running run's provider and id (a live change only syncs the next-run config for the same provider). */
  provider: Provider;
  runId: string;
  modes: ModeInfo[];
  /** The running run's effective mode id. */
  mode: string;
  autoAccept: boolean;
  showAutoAccept: boolean;
  /** run.setPermissionMode; rejects when the switch fails (the pill surfaces the message). */
  onChange: (change: { modeId?: string; autoAccept?: boolean }) => Promise<void>;
}

const RunConfigContext = createContext<RunConfigContextValue | null>(null);

/** Reads the composer toolbar's shared run config. Only valid inside RunConfigToolbar. */
export function useRunConfig(): RunConfigContextValue {
  const ctx = useContext(RunConfigContext);
  if (!ctx) throw new Error("useRunConfig must be used inside <RunConfigToolbar>");
  return ctx;
}

const EMPTY_HEALTH: Record<string, ProviderHealth | undefined> = {};

const EMPTY_STATE: RunConfigState = { baseAgentId: null, custom: false, provider: "claude-native", permissionMode: "", autoAccept: false, thinkingLevel: "", model: "" };

/** Applies profile p's stored fields on top of prev -- shared by applyProfile and resetToAgent so "reset" really means "re-apply the same profile". A mode is provider-scoped, so it always comes from the profile ("" = that provider's default) rather than carrying prev's (possibly another provider's) mode over; an empty ThinkingLevel leaves the current value alone. */
function applyProfileFields(prev: RunConfigState, p: AgentProfile): RunConfigState {
  return {
    baseAgentId: String(p.ID),
    custom: false,
    provider: p.Provider as Provider,
    permissionMode: p.PermissionMode,
    autoAccept: p.AutoAccept,
    thinkingLevel: (p.ThinkingLevel || prev.thinkingLevel) as ThinkingLevel,
    model: p.Model ?? "",
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
  providersLoading = false,
  disabled = false,
  providerHealth = EMPTY_HEALTH,
  onPickerOpen,
  onChange,
  liveRun = null,
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
  /** provider.list has not settled yet (see meta.providersLoading). */
  providersLoading?: boolean;
  disabled?: boolean;
  /** provider.test results by provider id; absent/untested providers are enabled. */
  providerHealth?: Record<string, ProviderHealth | undefined>;
  /** Fired each time the combined model picker opens. */
  onPickerOpen?: () => void;
  /**
   * Fires whenever the shared state changes, with the full context value
   * (state + actions) -- the composer submits a run from `state` and lets
   * its textarea's Shift+Tab approval cycle write back through `actions`,
   * without re-implementing either where the keydown lives.
   */
  onChange?: (value: RunConfigContextValue) => void;
  /** The running run's mode controls (see LiveRunControls); null/absent edits the next run's config. */
  liveRun?: LiveRunControls | null;
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
    setState((prev) => (prev.provider === boundProvider ? prev : { ...prev, provider: boundProvider, permissionMode: "", autoAccept: false, model: "" }));
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
    const defaultAgentId = useDefaultAgentStore.getState().value;
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
    (patch: Partial<Pick<RunConfigState, "provider" | "permissionMode" | "autoAccept" | "thinkingLevel" | "model">>) => {
      setState((prev) => ({ ...prev, ...patch, custom: prev.baseAgentId !== null ? true : prev.custom }));
    },
    [],
  );
  // Modes (W1) and models (ADR-0025) are provider-scoped: switching provider
  // resets to the new provider's own default mode and turns autoAccept off,
  // and the model is the one picked alongside it, so a model id never leaks
  // into a provider that lacks it. Same-provider picks keep the mode.
  const setProviderModel = useCallback((provider: Provider, model: string) => {
    setState((prev) => {
      const patch =
        provider === prev.provider ? { model } : { provider, permissionMode: "", autoAccept: false, model };
      return { ...prev, ...patch, custom: prev.baseAgentId !== null ? true : prev.custom };
    });
  }, []);
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
      actions: { setProviderModel, setPermissionMode, setAutoAccept, setThinkingLevel, applyProfile, clearAgent, resetToAgent },
      meta: {
        profiles,
        providers,
        providersLoading,
        modes: providerModes(providers, state.provider),
        health: providerHealth,
        onPickerOpen,
        effectiveMode: effectiveMode(providers, state.provider, state.permissionMode),
        supportsAutoAccept: supportsAutoAccept(providers, state.provider),
        disabled,
        boundProvider,
        chatId,
        liveRun,
      },
    };
  }, [chatId, state, setProviderModel, setPermissionMode, setAutoAccept, setThinkingLevel, applyProfile, clearAgent, resetToAgent, profiles, providers, providersLoading, disabled, boundProvider, providerHealth, onPickerOpen, liveRun]);

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

/**
 * The one combined provider + model selector (Paseo's combined-model-selector;
 * docs/plans/completed/composer-combined-model-picker.md) -- it replaced the
 * separate Agent, Provider and Model controls. Profiles ("agents") are the
 * pinned root section of its popover, so applying, detaching and managing them
 * works as before; the reset-to-agent button stays beside the trigger while
 * the run config has drifted from the applied profile.
 */
function RunConfigToolbarModelPicker() {
  const { state, actions, meta } = useRunConfig();
  const browserProviders = useMemo(() => buildBrowserProviders(meta.providers, meta.health, { loading: meta.providersLoading }), [meta.providers, meta.health, meta.providersLoading]);
  const profileRows = useMemo<PickerProfileRow[]>(
    () =>
      meta.profiles.map((p) => ({
        id: String(p.ID),
        name: p.Name,
        summary: describeProfile(p, meta.providers),
        provider: p.Provider,
        model: p.Model ?? "",
        notes: p.Notes || undefined,
      })),
    [meta.profiles, meta.providers],
  );
  const observedModel = useObservedModel(meta.chatId, state.provider, state.model === "");
  const baseAgent = state.baseAgentId ? meta.profiles.find((p) => String(p.ID) === state.baseAgentId) : undefined;
  const openSettings = (sectionId: string) => window.dispatchEvent(new CustomEvent("smind:open-settings", { detail: { sectionId } }));

  return (
    <div className="flex shrink-0 items-center">
      <ProviderModelPicker
        providers={browserProviders}
        selectedProvider={state.provider}
        selectedModel={state.model}
        observedModel={observedModel}
        boundProvider={meta.boundProvider}
        disabled={meta.disabled}
        profileRows={profileRows}
        baseAgentId={state.baseAgentId}
        custom={state.custom}
        onSelect={(provider, model) => actions.setProviderModel(provider as Provider, model)}
        onApplyProfile={actions.applyProfile}
        onClearAgent={actions.clearAgent}
        currentConfig={{ permissionMode: state.permissionMode, autoAccept: state.autoAccept, thinkingLevel: supportsThinking(state.provider) ? state.thinkingLevel : "" }}
        onManageAgents={() => openSettings("agents")}
        onCreateProfile={(seed) => {
          setSettingsIntent({ kind: "newProfile", seed });
          openSettings("agents");
        }}
        onEditProfile={(profileId) => {
          setSettingsIntent({ kind: "editProfile", id: Number(profileId) });
          openSettings("agents");
        }}
        onOpenProviderSettings={(providerId) => {
          selectProvider(providerId);
          openSettings("providers");
        }}
        onOpen={meta.onPickerOpen}
      />
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
          <Icons.reset aria-hidden />
        </Button>
      )}
    </div>
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
  const live = meta.liveRun;
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const modes = live ? live.modes : meta.modes;
  const modeId = live ? live.mode : meta.effectiveMode;
  const autoAccept = live ? live.autoAccept : state.autoAccept;
  const showAutoAccept = live ? live.showAutoAccept : meta.supportsAutoAccept;
  const current = modes.find((m) => m.id === modeId);
  // The mode auto-approves (or ACP Auto-accept is on) -- the pill warns
  // (ShieldAlert + warning colour) without changing any mode semantics.
  const dangerous = current?.autoApproves === true || autoAccept;
  const disabled = meta.disabled;

  // The run ended (or another one started) with a switch still failing: its message is stale.
  const liveRunId = live?.runId;
  useEffect(() => setError(null), [liveRunId]);

  // Live: the RPC switches the running session; a failure is shown inline.
  async function changeLive(liveRun: LiveRunControls, change: { modeId?: string; autoAccept?: boolean }) {
    if (pending) return;
    setPending(true);
    try {
      await liveRun.onChange(change);
      setError(null);
      // The session mode sticks: the next prompt (a queued follow-up, or the run after this
      // one) must go out with what the pill shows now, not the config this run started with.
      if (liveRun.provider === state.provider) {
        if (change.modeId !== undefined) actions.setPermissionMode(change.modeId);
        if (change.autoAccept !== undefined) actions.setAutoAccept(change.autoAccept);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setPending(false);
    }
  }
  const pickMode = (v: string) => {
    if (!v || v === modeId) return;
    if (live) void changeLive(live, { modeId: v });
    else actions.setPermissionMode(v);
  };
  const toggleAutoAccept = () => {
    if (live) void changeLive(live, { autoAccept: !autoAccept });
    else actions.setAutoAccept(!autoAccept);
  };
  return (
    <>
      <ModeSelect modes={modes} value={modeId} onChange={pickMode} disabled={disabled} dangerous={dangerous} variant="pill" />
      {showAutoAccept && (
        <AutoAcceptToggle checked={autoAccept} onChange={toggleAutoAccept} disabled={disabled} pending={pending} variant="pill" />
      )}
      {error && (
        <span data-testid="permission-mode-error" role="alert" className="px-1 text-ui-sm text-destructive">
          {error}
        </span>
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
  if (!supportsThinking(state.provider)) return null;
  return (
    <ThinkingSelect
      value={state.thinkingLevel || "standard"}
      onChange={(v) => actions.setThinkingLevel(v as ThinkingLevel)}
      disabled={meta.disabled}
      variant="pill"
      placeholder="Select thinking level"
    />
  );
}

RunConfigToolbar.ModelPicker = RunConfigToolbarModelPicker;
RunConfigToolbar.Mode = RunConfigToolbarMode;

RunConfigToolbar.Thinking = RunConfigToolbarThinking;
