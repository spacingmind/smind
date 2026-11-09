import { failureSentence } from "@/lib/failure-text";
import { selectProvider } from "@/stores/providers-view-store";

/**
 * The run failure classes (docs/plans/active/run-failure-classes.md, ADR-0050)
 * as the UI reads them: the daemon sends `failureKind` and a derived `action`.
 * Plan windows and the residual rate limit keep their existing rate-limit view
 * (lib/rate-limit.ts); this file owns the other classes and the next-action
 * control. Pure: callers pass `now`.
 */

/** The daemon's `action` object; an unknown `kind` still renders its `label`. */
export interface FailureAction {
  kind: string;
  /** `settings:providers?account=<id>` / `?provider=<id>`, `profile:<id>`, or an RFC 3339 time. */
  target?: string;
  label: string;
  /** The daemon's one plain sentence of what happened (R3a); absent on an older daemon. */
  summary?: string;
  models?: string[];
  attempt?: number;
  max?: number;
  nextAt?: string;
}

export const AUTH_EXPIRED = "auth_expired";
export const MODEL_NOT_IN_PLAN = "model_not_in_plan";
export const TRANSIENT_429 = "transient_429";
export const CONTEXT_WINDOW_EXCEEDED = "context_window_exceeded";
export const SESSION_BUDGET_EXCEEDED = "session_budget_exceeded";
export const TOO_MANY_DENIALS = "too_many_denials";

/** The human-action classes (R3a): a person must start a fresh chat, raise the provider's session budget or review the permission policy; never retried, never failed over. */
export const HUMAN_ACTION_KINDS = new Set([CONTEXT_WINDOW_EXCEEDED, SESSION_BUDGET_EXCEEDED, TOO_MANY_DENIALS]);

const ACTIONABLE = new Set([
  AUTH_EXPIRED,
  MODEL_NOT_IN_PLAN,
  TRANSIENT_429,
  "plan_window_exhausted",
  "rate_limited",
  CONTEXT_WINDOW_EXCEEDED,
  SESSION_BUDGET_EXCEEDED,
  TOO_MANY_DENIALS,
]);

/** True for a class that has a next action to show (cli_exited keeps the footer's Continue). */
export function isActionableFailure(kind: string | undefined | null): boolean {
  return kind != null && ACTIONABLE.has(kind);
}

export interface SettingsTarget {
  sectionId: string;
  accountId?: number;
  provider?: string;
}

/** Reads `settings:<section>[?account=<id>|?provider=<id>]`; null for anything else. */
export function parseSettingsTarget(target: string | undefined | null): SettingsTarget | null {
  if (!target) return null;
  const m = /^settings:([a-z][a-z0-9-]*)(?:\?(account|provider)=([A-Za-z0-9._:-]+))?$/.exec(target);
  if (!m) return null;
  const out: SettingsTarget = { sectionId: m[1] };
  if (m[2] === "account") {
    const id = Number(m[3]);
    if (Number.isInteger(id) && id > 0) out.accountId = id;
  } else if (m[2] === "provider") {
    out.provider = m[3];
  }
  return out;
}

/** Opens the settings page a sign_in action points at through the app's one deep-link event (App.tsx listens). */
export function openFailureSettings(action: FailureAction | undefined): boolean {
  const target = parseSettingsTarget(action?.target);
  if (!target) return false;
  window.dispatchEvent(new CustomEvent("smind:open-settings", { detail: target }));
  return true;
}

/** `28s`, `1m 30s`, `1h 5m`: whole seconds, no sub-second noise (the retry gaps are 5 s to 5 min). */
function shortDuration(ms: number): string {
  const total = Math.max(1, Math.ceil(ms / 1000));
  if (total < 60) return `${total}s`;
  const m = Math.floor(total / 60);
  const sec = total % 60;
  if (m < 60) return sec === 0 ? `${m}m` : `${m}m ${sec}s`;
  const h = Math.floor(m / 60);
  return m % 60 === 0 ? `${h}h` : `${h}h ${m % 60}m`;
}

/** Applies what a settings deep link names beyond the section: on Providers, the provider to select. */
export function focusSettingsTarget(target: Partial<SettingsTarget> | undefined): void {
  if (target?.sectionId === "providers" && target.provider) selectProvider(target.provider);
}

/** "Retrying in 28s (1 of 2)" for a queued automatic retry; null for any other action. */
export function failureCountdown(action: FailureAction | undefined, now: Date): string | null {
  if (action?.kind !== "auto_retry" || !action.nextAt) return null;
  const at = Date.parse(action.nextAt);
  if (Number.isNaN(at)) return null;
  const of = action.attempt && action.max ? ` (${action.attempt} of ${action.max})` : "";
  const left = at - now.getTime();
  return left > 0 ? `Retrying in ${shortDuration(left)}${of}` : `Retrying now${of}`;
}

export interface FailureCardView {
  tone: "danger" | "warning";
  /** The plain sentence of the error card and the footer. */
  message: string;
  /** A second quiet line (allowed models), or null. */
  hint: string | null;
  /** The live countdown of a queued retry, or null. */
  countdown: string | null;
  /** The control the card offers: Sign in / Review the permission policy (settings deep links) or Start a fresh chat. */
  control: "sign_in" | "review_policy" | "new_chat" | null;
  controlLabel: string | null;
}

/** The card view of a failure class that has its own card; null for plan windows, the residual and no failure (cli_exited's card carries no control; the footer keeps its Continue). */
export function describeFailureCard(
  run: { failureKind?: string; failureAction?: FailureAction },
  now: Date,
): FailureCardView | null {
  const action = run.failureAction;
  switch (run.failureKind) {
    case AUTH_EXPIRED:
      return { tone: "danger", message: "You are signed out of this account: the provider refused the login.", hint: null, countdown: null, control: "sign_in", controlLabel: "Sign in" };
    case MODEL_NOT_IN_PLAN:
      return {
        tone: "danger",
        message: "This model is not in your plan.",
        hint: action?.models && action.models.length > 0 ? `Your plan allows: ${action.models.join(", ")}` : "Pick another model and send again.",
        countdown: null,
        control: null,
        controlLabel: null,
      };
    case TRANSIENT_429: {
      const countdown = failureCountdown(action, now);
      return {
        tone: "warning",
        message: "The provider is busy for a moment.",
        hint: "A short throttle, not a usage limit.",
        countdown,
        // Retry lives in the footer (latest run only); the card never carries a second one.
        control: null,
        controlLabel: null,
      };
    }
    // The human-action classes (R3a): the daemon's one sentence says what happened, the
    // control says what a person does about it -- a hint never repeats the control's own
    // words. Never a Retry, never a Continue.
    case CONTEXT_WINDOW_EXCEEDED:
      return { tone: "danger", message: failureSentence(CONTEXT_WINDOW_EXCEEDED, action), hint: "Earlier turns stay in this chat.", countdown: null, control: "new_chat", controlLabel: "Start a fresh chat" };
    case SESSION_BUDGET_EXCEEDED:
      // No control: whose budget it is and where to raise it differ per provider, so the label is the hint.
      return { tone: "danger", message: failureSentence(SESSION_BUDGET_EXCEEDED, action), hint: action?.label ?? null, countdown: null, control: null, controlLabel: null };
    case TOO_MANY_DENIALS:
      return { tone: "danger", message: failureSentence(TOO_MANY_DENIALS, action), hint: null, countdown: null, control: "review_policy", controlLabel: "Review the permission policy" };
    case "cli_exited":
      return { tone: "warning", message: failureSentence("cli_exited", action), hint: null, countdown: null, control: null, controlLabel: null };
  }
  return null;
}
