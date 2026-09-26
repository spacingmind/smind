import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

import { approvalPolicies as allApprovalPolicies, type ApprovalPolicyInfo } from "@/lib/approval-policies";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { AgentProfile, ApprovalPolicy, Provider, ProviderInfo, ThinkingLevel } from "@/lib/types";

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

/**
 * Claude's own thinking-level tiers (internal/taskrunner.ThinkingLevel;
 * "" is the unset default, deliberately not offered as its own option --
 * choosing a tier is opt-in, and "Standard" already IS today's ordinary
 * behavior in effect, just via the adaptive Option instead of no Option at
 * all). Only ever shown when provider === "claude-native" -- GLM/Kimi's
 * thinking-level control is a different, live-session-scoped mechanism
 * (see run-config-options in task-detail), and Codex has none.
 */
const THINKING_LEVELS: { id: ThinkingLevel; label: string; help: string }[] = [
  { id: "off", label: "Off", help: "No extended thinking -- responds immediately." },
  { id: "standard", label: "Standard", help: "The model adapts how much it thinks to the turn." },
  { id: "extended", label: "Extended", help: "A large fixed thinking budget, for turns that need to reason at length before acting." },
];

/**
 * The per-run configuration the composer submits with: which agent
 * (profile) was picked, if any, and the three fields a pick seeds
 * (run-config IA plan). One shared context rather than four independent
 * Selects each with their own state, so "picking an agent fills the other
 * fields" and "hand-editing one flips the agent to Custom" are ordinary
 * state transitions in one place instead of coordination between siblings.
 */
export interface RunConfigState {
  /** The picked agent profile's id, or null when no agent is active (Custom included -- see baseAgent). */
  agent: string | null;
  provider: Provider;
  approvalPolicy: ApprovalPolicy;
  thinkingLevel: ThinkingLevel;
}

export interface RunConfigContextValue {
  state: RunConfigState;
  actions: {
    setProvider: (provider: Provider) => void;
    setApprovalPolicy: (policy: ApprovalPolicy) => void;
    setThinkingLevel: (level: ThinkingLevel) => void;
    /** Applies a profile's stored config to the three fields (ADR-0014's client-side seed). */
    applyProfile: (profileId: string) => void;
  };
  meta: {
    profiles: AgentProfile[];
    providers: ProviderInfo[];
    /** The three tiers for the currently selected provider (full-access's label is provider-specific). */
    approvalPolicies: ApprovalPolicyInfo[];
    disabled: boolean;
  };
}

const RunConfigContext = createContext<RunConfigContextValue | null>(null);

/** Reads the composer toolbar's shared run config. Only valid inside RunConfigToolbar. */
export function useRunConfig(): RunConfigContextValue {
  const ctx = useContext(RunConfigContext);
  if (!ctx) throw new Error("useRunConfig must be used inside <RunConfigToolbar>");
  return ctx;
}

/**
 * The composer's run-config toolbar (run-config IA plan): one compound
 * component -- `RunConfigToolbar.Agent`, `.Provider`, `.Approval`,
 * `.Thinking` -- sharing a single context that holds
 * `{agent, provider, approvalPolicy, thinkingLevel}`, replacing the four
 * independent Selects that each carried their own state. The provider is
 * the only place state lives; the parts are pure readers (vercel
 * composition-patterns' state-context-interface / state-decouple-
 * implementation).
 *
 * GLM/Kimi's live ACP run-config options render in this same row (they
 * arrive only once a session is live; the composer mounts them as extra
 * children between the Thinking part and the send controls).
 */
export function RunConfigToolbar({
  profiles,
  providers,
  disabled = false,
  onChange,
  children,
}: {
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
  const [agent, setAgent] = useState<string | null>(null);
  const [provider, setProvider] = useState<Provider>("claude-native");
  const [approvalPolicy, setApprovalPolicy] = useState<ApprovalPolicy>("manual");
  const [thinkingLevel, setThinkingLevel] = useState<ThinkingLevel>("");

  // Applies profileId's provider/approvalPolicy/thinkingLevel to the
  // shared state, per ADR-0014's "client copies the fields" apply
  // mechanism -- a plain client-side seed, no task.prompt wire change.
  const applyProfile = useCallback(
    (profileId: string) => {
      const p = profiles.find((candidate) => String(candidate.ID) === profileId);
      if (!p) return;
      setAgent(String(p.ID));
      setProvider(p.Provider as Provider);
      if (p.ApprovalPolicy) setApprovalPolicy(p.ApprovalPolicy as ApprovalPolicy);
      if (p.ThinkingLevel) setThinkingLevel(p.ThinkingLevel as ThinkingLevel);
    },
    [profiles],
  );

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
      setAgent(String(p.ID));
      setProvider(p.Provider as Provider);
      if (p.ApprovalPolicy) setApprovalPolicy(p.ApprovalPolicy as ApprovalPolicy);
      if (p.ThinkingLevel) setThinkingLevel(p.ThinkingLevel as ThinkingLevel);
    };
    window.addEventListener("smind:use-agent", onUseAgent);
    return () => window.removeEventListener("smind:use-agent", onUseAgent);
  }, [profiles]);

  const value = useMemo<RunConfigContextValue>(() => {
    const policies = allApprovalPolicies(provider);
    return {
      state: { agent, provider, approvalPolicy, thinkingLevel },
      actions: {
        setProvider,
        setApprovalPolicy,
        setThinkingLevel,
        applyProfile,
      },
      meta: { profiles, providers, approvalPolicies: policies, disabled },
    };
  }, [agent, provider, approvalPolicy, thinkingLevel, applyProfile, profiles, providers, disabled]);

  useEffect(() => {
    onChange?.(value);
  }, [onChange, value]);

  return <RunConfigContext.Provider value={value}>{children}</RunConfigContext.Provider>;
}

/**
 * shadcn/Radix rather than a native <select>: the closed state was already
 * styled to match, but the *open* list was OS chrome -- square,
 * light-mode-only, ignoring bg-popover/text-popover-foreground like every
 * other menu in the app. The accessible name survives the move into the
 * toolbar via aria-label, since there is no visible <label> beside it.
 */
function RunConfigToolbarAgent() {
  const { actions, meta } = useRunConfig();
  if (meta.profiles.length === 0) return null;
  return (
    <Select value="" onValueChange={actions.applyProfile} disabled={meta.disabled}>
      <SelectTrigger aria-label="Agents" data-testid="composer-agent-select" className={SELECT_TRIGGER_CLASS}>
        <SelectValue placeholder="Agents" />
      </SelectTrigger>
      <SelectContent>
        {meta.profiles.map((p) => (
          <SelectItem key={p.ID} value={String(p.ID)} title={p.Notes || undefined}>
            {p.Name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function RunConfigToolbarProvider() {
  const { state, actions, meta } = useRunConfig();
  return (
    <Select value={state.provider} onValueChange={(v) => actions.setProvider(v as Provider)} disabled={meta.disabled}>
      <SelectTrigger aria-label="Provider" className={SELECT_TRIGGER_CLASS}>
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

function RunConfigToolbarApproval() {
  const { state, actions, meta } = useRunConfig();
  return (
    <Select
      value={state.approvalPolicy}
      onValueChange={(v) => actions.setApprovalPolicy(v as ApprovalPolicy)}
      disabled={meta.disabled}
    >
      {/* The help text stays a plain `title` -- a hover tooltip on the
          trigger, exactly where it was on the native select. Reflects the
          *current* selection's own help (each option gets its own too, in
          the open list below), since the three tiers no longer share one
          description now that full-access differs per provider. */}
      <SelectTrigger
        aria-label="Approval policy"
        title={meta.approvalPolicies.find((p) => p.id === state.approvalPolicy)?.help}
        className={SELECT_TRIGGER_CLASS}
      >
        <SelectValue placeholder="Select policy" />
      </SelectTrigger>
      <SelectContent>
        {meta.approvalPolicies.map((p) => (
          <SelectItem key={p.id} value={p.id} title={p.help}>
            {p.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function RunConfigToolbarThinking() {
  const { state, actions, meta } = useRunConfig();
  // Claude-only, pre-run control (see THINKING_LEVELS' doc comment) --
  // omitted entirely, not just disabled, for every other provider: no
  // dead control sitting in the toolbar for a provider that can't act on
  // it. GLM/Kimi's own thinking-level control lives in the chat view
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
RunConfigToolbar.Approval = RunConfigToolbarApproval;
RunConfigToolbar.Thinking = RunConfigToolbarThinking;
