import { formatRelativeTime } from "@/lib/relative-time";
import { formatCooldownUntil } from "./format";
import type { ActiveCooldown } from "./model";
import type { AccountUsage } from "./types";

/** "45 s", "3 min", "2 h": the wait until `iso`, rounded up; null when it is not in the future. */
function waitUntil(iso: string | null | undefined, now: number): string | null {
  if (!iso) return null;
  const ms = new Date(iso).getTime() - now;
  if (!Number.isFinite(ms) || ms <= 0) return null;
  const seconds = Math.ceil(ms / 1000);
  if (seconds < 60) return `${seconds} s`;
  const minutes = Math.ceil(seconds / 60);
  if (minutes < 60) return `${minutes} min`;
  return `${Math.ceil(minutes / 60)} h`;
}

/** A model name is client-controlled: cap what one cooldown line can hold. */
export const COOLDOWN_MODEL_MAX_CHARS = 40;

function shortModel(model: string): string {
  const chars = Array.from(model);
  return chars.length > COOLDOWN_MODEL_MAX_CHARS ? `${chars.slice(0, COOLDOWN_MODEL_MAX_CHARS - 1).join("")}…` : model;
}

/** What the upstream rejected: "5-hour window", "weekly limit for <model>", "<model> only". */
function cooldownRejected(cooldown: ActiveCooldown): string | null {
  const model = cooldown.model ? shortModel(cooldown.model) : null;
  switch (cooldown.window) {
    case "five_hour":
      return "5-hour window";
    case "weekly":
      return "weekly window";
    case "weekly_model":
      return model ? `weekly limit for ${model}` : "weekly model limit";
    default:
      return cooldown.scope === "model" && model ? `${model} only` : null;
  }
}

function tail(prefix: string, wait: string | null): string {
  return wait ? `${prefix} ${wait}` : "";
}

function errorSentence(usage: AccountUsage, now: number): string {
  const reason = usage.report?.error || usage.lastError || "unknown error";
  const wait = waitUntil(usage.nextRefreshAt, now);
  return `Couldn't read usage: ${reason}${tail(", retry in", wait)}`;
}

// User-facing copy for the usage surfaces, kept in one file so localization is a
// single-file change. Every reader tolerates missing fields (AC39).
export const usageCopy = {
  /** Why an account shows no windows, or null when there is nothing to explain. */
  problem: (usage: AccountUsage, now: number = Date.now()): string | null => {
    const report = usage.report;
    if (!report) return usage.lastError ? errorSentence(usage, now) : null;
    const problem = report.problem;
    if (problem?.kind === "no_quota") return problem.detail || "No plan usage for this account.";
    if (problem?.kind === "rejected") {
      const status = problem.status ? ` (HTTP ${problem.status})` : "";
      return `Login rejected${status}. Reconnect it in Providers.`;
    }
    if (problem?.kind === "expired") {
      const expired =
        problem.expiresAt && Number.isFinite(new Date(problem.expiresAt).getTime())
          ? ` ${formatRelativeTime(problem.expiresAt, now)}`
          : "";
      return `Login expired${expired}. Reconnect it in Providers.`;
    }
    if (report.status === "error") return errorSentence(usage, now);
    return null;
  },
  /** The refresh button's tooltip and accessible name, and whether it is disabled. */
  refreshState: (
    usage: AccountUsage,
    now: number = Date.now(),
  ): { disabled: boolean; label: string } => {
    const wait = waitUntil(usage.nextRefreshAt, now);
    switch (usage.refreshBlocked) {
      case "backoff":
        return { disabled: true, label: `Rate limited${tail(", retry in", wait)}` };
      case "floor":
        return { disabled: true, label: "Refreshed just now" };
      case "budget":
        return { disabled: true, label: `Refresh limit reached${tail(", next in", wait)}` };
      case "rejected":
        return { disabled: true, label: "Reconnect this account to refresh" };
      default:
        return { disabled: false, label: "Refresh" };
    }
  },
  /**
   * The passive cooldown as copy, in parts so a card can lay them out: `label` "Rate limited",
   * `until` "until 2:05 PM", `rejected` what was rejected (and "reset time unknown" for a guessed
   * end), `note` where it was seen. `headline` is `label` and `until` as one sentence.
   */
  cooldown: (
    cooldown: ActiveCooldown,
    now: number = Date.now(),
    locale?: string,
  ): { label: string; until: string; headline: string; rejected: string | null; note: string } => {
    const label = "Rate limited";
    const until = `until ${formatCooldownUntil(cooldown.untilMs, now, locale)}`;
    const rejected = [cooldownRejected(cooldown), cooldown.reason === "fallback" ? "reset time unknown" : null]
      .filter((part): part is string => part !== null)
      .join(" · ");
    return { label, until, headline: `${label} ${until}`, rejected: rejected || null, note: "seen on a proxied request" };
  },
  /** A window with no percent says its status instead of an empty meter. */
  windowStatus: (status: string | undefined): string | null => {
    if (status === "rejected") return "Limit reached";
    if (status === "allowed_warning") return "Near limit";
    return null;
  },
  observedWindow: "from recent traffic",
  disabled: "Plan usage is turned off. Set accountUsage.enabled to true in config to see it.",
  daemonTooOld: "Update the daemon to see plan usage",
  refreshFailed: "Unable to refresh usage",
  updated: "Updated",
  loading: "Loading plan usage…",
  empty: "No accounts with plan usage yet",
  unavailableLabel: "Unavailable",
  errorLabel: "Error",
} as const;
