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
  models?: string[];
  attempt?: number;
  max?: number;
  nextAt?: string;
}

export const AUTH_EXPIRED = "auth_expired";
export const MODEL_NOT_IN_PLAN = "model_not_in_plan";
export const TRANSIENT_429 = "transient_429";

const ACTIONABLE = new Set([AUTH_EXPIRED, MODEL_NOT_IN_PLAN, TRANSIENT_429, "plan_window_exhausted", "rate_limited"]);

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
  /** The control the card offers: Sign in (settings deep link), Retry, or none. */
  control: "sign_in" | "retry" | null;
  controlLabel: string | null;
}

/** The card view of a failure class that has its own card; null for plan windows, the residual, cli_exited and no failure. */
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
        control: countdown ? null : "retry",
        controlLabel: countdown ? null : "Retry",
      };
    }
  }
  return null;
}
