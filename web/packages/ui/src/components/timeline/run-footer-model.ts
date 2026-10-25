import { formatCost } from "@/lib/usage-format";
import { formatRelativeTime } from "@/lib/relative-time";
import { describeRateLimit, isRateLimitedRun } from "@/lib/rate-limit";
import type { RunStatusValue, RunUsage } from "@/lib/types";

/**
 * The run footer's pure view model (readable run footers). Everything the
 * component renders -- sentences, tones, durations, ages -- is decided
 * here so the table in docs/plans/active/run-footer-readable.md is
 * testable without mounting anything.
 */

export type RunFooterTone = "neutral" | "warning" | "danger";
export type RunFooterKind = "running" | "done" | "abnormal";

export interface RunFooterAge {
  label: string;
  title: string;
}

export interface RunFooterView {
  kind: RunFooterKind;
  /** The abnormal-ending sentence ("Stopped by you", "Failed after 9s"); absent on a normal finish and while running. */
  sentence?: string;
  tone: RunFooterTone;
  action: "continue" | "retry" | null;
  duration: string | null;
  /** The precise duration ("9.2 s") for the segment's title. */
  durationTitle: string | null;
  model: string;
  age: RunFooterAge | null;
  ariaLabel: string;
}

/** The run shape describeRunFooter reads -- structurally what RunEntry carries. */
export interface RunFooterRun {
  id: string;
  provider: string;
  status: RunStatusValue;
  startedAt: string;
  finishedAt?: string;
  stopReason?: string;
  /** The daemon's derived disposition (ADR-0027 D13): "needs-continue" when the run ended done at a turn/token/budget cap, or failed resumably (J1: cli_exited, or rate_limited past its reset) with a stored session. */
  disposition?: string;
  /** ADR-0041: "rate_limited" when an errored run failed on a provider quota. */
  failureKind?: string;
  /** The quota window that was hit ("5h", "weekly", ...). */
  failureWindow?: string;
  /** When the limit lifts (RFC 3339 UTC). */
  failureResetAt?: string;
  failureResetInferred?: boolean;
  /** ADR-0041 F2: a rate-limited run whose profile names a fallback (run.logs). */
  fallbackProfileId?: number;
  err?: string;
  model?: string;
  usage?: RunUsage;
  baseSha?: string;
  headSha?: string;
  dirty?: boolean;
}

/** `modelLabels` keys: `${provider}/${modelId}` for a catalog entry, `${provider}/` for the provider's default-model label. */
export type RunModelLabels = Record<string, string>;

/** runs.DispositionNeedsContinue. */
const NEEDS_CONTINUE = "needs-continue";

/** How each cap a daemon reports is worded; an unlisted cap reads "Stopped at a limit". */
const CAP_SENTENCES: Record<string, string> = {
  max_tokens: "Stopped at the output limit",
  max_turn_requests: "Hit the tool-turn limit",
  budget_exceeded: "Stopped at the budget limit",
  // The daemon went down under the run (a restart): not a cap, but also not finished.
  daemon_restart: "Interrupted when the daemon restarted",
};

/** The prompt of a Continue on a run that ended done at a cap or was cut off by a restart. */
export const CONTINUE_PROMPT = "Continue";

/** How a resumable failure kind reads in the error run's Continue prompt. */
const FAILURE_LABELS: Record<string, string> = {
  cli_exited: "the agent CLI exited without a result",
  rate_limited: "a provider rate limit, which has since reset",
};

/**
 * The follow-up prompt the footer's Continue sends for `run`: the standard
 * "Continue" for a capped or restart-interrupted run, one line naming the
 * failure for an errored one (J1).
 */
export function continuePrompt(run: Pick<RunFooterRun, "status" | "failureKind">): string {
  if (run.status !== "error") return CONTINUE_PROMPT;
  const label = FAILURE_LABELS[run.failureKind ?? ""] ?? "an unexpected error";
  return `Continue from where you left off (the previous run ended unexpectedly: ${label}).`;
}

const MINUTE_MS = 60_000;

/**
 * A duration as Paseo words it (`9s`, `1m 42s`, `1h 5m`); under a second
 * shows `<1s`. Paseo's own formatDuration clamps sub-second runs to "0s",
 * which reads like a zero-length run -- the footer says `<1s` instead.
 */
export function formatDuration(durationMs: number): string {
  if (!Number.isFinite(durationMs) || durationMs < 0) return "<1s";
  if (durationMs < 1000) return "<1s";
  const totalSeconds = Math.floor(durationMs / 1000);
  if (totalSeconds < 60) return `${totalSeconds}s`;
  const totalMinutes = Math.floor(totalSeconds / 60);
  if (totalMinutes < 60) {
    const seconds = totalSeconds % 60;
    return seconds === 0 ? `${totalMinutes}m` : `${totalMinutes}m ${seconds}s`;
  }
  const hours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;
  return minutes === 0 ? `${hours}h` : `${hours}h ${minutes}m`;
}

/** The precise duration for a tooltip ("9.2 s"); never throws on a bad date. */
export function formatPreciseDuration(durationMs: number): string {
  if (!Number.isFinite(durationMs) || durationMs < 0) return "<1 s";
  if (durationMs < 1000) return `${(durationMs / 1000).toFixed(1)} s`;
  const totalSeconds = Math.floor(durationMs / 1000);
  if (totalSeconds < 60) return `${(durationMs / 1000).toFixed(1)} s`;
  const totalMinutes = Math.floor(totalSeconds / 60);
  if (totalMinutes < 60) {
    const seconds = totalSeconds % 60;
    return seconds === 0 ? `${totalMinutes}m` : `${totalMinutes}m ${seconds}s`;
  }
  const hours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;
  return minutes === 0 ? `${hours}h` : `${hours}h ${minutes}m`;
}

/** The absolute local timestamp a relative age's title carries. */
export function formatAbsoluteTime(date: Date): string {
  return date.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

/**
 * Humanises an uncatalogued Claude model id: strip the leading `claude-`,
 * join adjacent numeric parts with a dot, capitalise each word
 * (`claude-opus-5-5` → `Opus 5.5`). Anything else is returned raw.
 */
export function humaniseModelId(modelId: string): string {
  if (!modelId.startsWith("claude-")) return modelId;
  // A `[1m]`-style context suffix and a trailing 8-digit date are noise to a reader.
  const bare = modelId.replace(/\[[^\]]*\]$/, "").replace(/-\d{8}$/, "");
  const parts = bare.slice("claude-".length).split("-");
  const joined: string[] = [];
  for (const part of parts) {
    const last = joined[joined.length - 1];
    if (/^\d+$/.test(part) && last !== undefined && /^\d+$/.test(last)) {
      joined[joined.length - 1] = `${last}.${part}`;
    } else {
      joined.push(part);
    }
  }
  return joined.map((part) => (/^\d+(\.\d+)?$/.test(part) ? part : part.charAt(0).toUpperCase() + part.slice(1))).join(" ");
}

/**
 * The footer's model label: the requested model's catalog label, else the
 * reported model's (`usage.model`), else the provider's default-model
 * label, else the provider label. Raw ids fall through humaniseModelId.
 */
export function resolveRunModelLabel(
  run: RunFooterRun,
  modelLabels?: RunModelLabels,
  providerLabel?: string,
): string {
  const requested = run.model && run.model !== "" ? run.model : undefined;
  const reported = run.usage?.model ?? undefined;
  const catalog = (id: string) => modelLabels?.[`${run.provider}/${id}`] ?? humaniseModelId(id);
  if (requested) return catalog(requested);
  if (reported) return catalog(reported);
  const providerDefault = modelLabels?.[`${run.provider}/`];
  if (providerDefault) return providerDefault;
  return providerLabel ?? run.provider;
}

function durationMs(run: RunFooterRun, now: Date): number | null {
  const start = Date.parse(run.startedAt);
  if (Number.isNaN(start)) return null;
  const end = run.finishedAt ? Date.parse(run.finishedAt) : now.getTime();
  if (Number.isNaN(end)) return null;
  return Math.max(0, end - start);
}

function proseDuration(ms: number | null): string | null {
  if (ms === null) return null;
  return formatPreciseDuration(ms);
}

/**
 * The footer's whole wording for one run, per the spec's table. Never
 * throws: a bad `startedAt` yields a null duration and everything else
 * still renders.
 */
export function describeRunFooter(
  run: RunFooterRun,
  opts: { now: Date; modelLabel: string },
): RunFooterView {
  const now = opts.now;
  const ms = durationMs(run, now);
  const duration = ms === null ? null : formatDuration(ms);

  if (run.status === "running") {
    return {
      kind: "running",
      tone: "neutral",
      action: null,
      duration,
      durationTitle: null,
      model: "",
      age: null,
      // Static: the group's label must not change every second (the ticking time is its own timer element).
      ariaLabel: "Working",
    };
  }

  const finishedAt = run.finishedAt ? new Date(run.finishedAt) : null;
  const validFinishedAt = finishedAt !== null && !Number.isNaN(finishedAt.getTime()) ? finishedAt : null;
  const age =
    validFinishedAt !== null && now.getTime() - validFinishedAt.getTime() >= MINUTE_MS
      ? { label: formatRelativeTime(validFinishedAt, now, { shortDateAfterDays: 7 }), title: formatAbsoluteTime(validFinishedAt) }
      : null;

  const detail = (view: Omit<RunFooterView, "ariaLabel">): RunFooterView => ({
    ...view,
    ariaLabel: ariaLabel(run, view, ms, age),
  });

  // The daemon says an errored run is resumable (J1: its CLI died without a
  // result, or its quota has reset, and its chat has a stored session): the
  // disposition, never the failure kind alone, decides Continue, so a client
  // that cannot see the session and a daemon that can never disagree.
  const resumable = run.status === "error" && run.disposition === NEEDS_CONTINUE;

  // A provider quota (ADR-0041) is not a code failure: warning tone; Retry, or Continue once the daemon says it is resumable.
  if (isRateLimitedRun(run)) {
    return detail({
      kind: "abnormal",
      sentence: describeRateLimit({ window: run.failureWindow, resetAt: run.failureResetAt, inferred: run.failureResetInferred }, now),
      tone: "warning",
      action: resumable ? "continue" : "retry",
      duration,
      durationTitle: proseDuration(ms),
      model: opts.modelLabel,
      age,
    });
  }

  if (run.status === "error") {
    return detail({
      kind: "abnormal",
      sentence: duration === null ? "Failed" : `Failed after ${duration}`,
      tone: "danger",
      action: resumable ? "continue" : "retry",
      duration: null,
      durationTitle: null,
      model: opts.modelLabel,
      age,
    });
  }

  // The daemon says this run was cut off at a cap (ADR-0027 D13): it is not
  // finished, whatever its "done" status. The disposition, not a list of stop
  // reasons here, decides, so a cap this client has never heard of (a budget
  // stop) still reads as needing a continue; an older daemon sends none and
  // the stop-reason cases below cover the two caps it can report.
  if (run.disposition === NEEDS_CONTINUE) {
    return detail({
      kind: "abnormal",
      sentence: `${CAP_SENTENCES[run.stopReason ?? ""] ?? "Stopped at a limit"} · not finished`,
      tone: "warning",
      action: "continue",
      duration,
      durationTitle: proseDuration(ms),
      model: opts.modelLabel,
      age,
    });
  }

  switch (run.stopReason) {
    case undefined:
    case "":
    case "end_turn":
      // A stopped status with no reason at all still reads as stopped.
      if (run.stopReason !== "end_turn" && run.status === "stopped") {
        return detail({
          kind: "abnormal",
          sentence: "Stopped",
          tone: "neutral",
          action: null,
          duration,
          durationTitle: proseDuration(ms),
          model: opts.modelLabel,
          age,
        });
      }
      return detail({
        kind: "done",
        tone: "neutral",
        action: null,
        duration,
        durationTitle: proseDuration(ms),
        model: opts.modelLabel,
        age,
      });
    case "cancelled":
      return detail({
        kind: "abnormal",
        sentence: "Stopped by you",
        tone: "neutral",
        action: null,
        duration,
        durationTitle: proseDuration(ms),
        model: opts.modelLabel,
        age,
      });
    case "max_tokens":
      return detail({
        kind: "abnormal",
        sentence: "Stopped at the output limit",
        tone: "warning",
        action: "continue",
        duration,
        durationTitle: proseDuration(ms),
        model: opts.modelLabel,
        age,
      });
    case "max_turn_requests":
      return detail({
        kind: "abnormal",
        sentence: "Hit the tool-turn limit",
        tone: "warning",
        action: "continue",
        duration,
        durationTitle: proseDuration(ms),
        model: opts.modelLabel,
        age,
      });
    case "daemon_restart_fresh":
      // Cut off by a restart before the agent had a session: there is nothing to continue from.
      return detail({
        kind: "abnormal",
        sentence: "Interrupted when the daemon restarted",
        tone: "warning",
        action: null,
        duration,
        durationTitle: proseDuration(ms),
        model: opts.modelLabel,
        age,
      });
    case "refusal":
      return detail({
        kind: "abnormal",
        sentence: "The agent declined to continue",
        tone: "warning",
        action: null,
        duration,
        durationTitle: proseDuration(ms),
        model: opts.modelLabel,
        age,
      });
    default:
      if (run.status === "stopped") {
        return detail({
          kind: "abnormal",
          sentence: "Stopped",
          tone: "neutral",
          action: null,
          duration,
          durationTitle: proseDuration(ms),
          model: opts.modelLabel,
          age,
        });
      }
      return detail({
        kind: "abnormal",
        sentence: `Ended: ${run.stopReason}`,
        tone: "neutral",
        action: null,
        duration,
        durationTitle: proseDuration(ms),
        model: opts.modelLabel,
        age,
      });
  }
}

function ariaLabel(run: RunFooterRun, view: Omit<RunFooterView, "ariaLabel">, ms: number | null, age: RunFooterAge | null): string {
  const parts: string[] = [];
  const precise = ms === null ? null : formatPreciseDuration(ms);
  if (view.sentence) parts.push(view.sentence);
  if (precise !== null) {
    const phrase = view.kind === "done" && !view.sentence ? `Finished in ${precise}` : `after ${precise}`;
    parts.push(view.model ? `${phrase} with ${view.model}` : phrase);
  } else if (view.model) {
    parts.push(`with ${view.model}`);
  }
  const cost = run.usage?.costUsd;
  if (cost !== null && cost !== undefined) parts.push(`cost ${formatCost(cost)}`);
  if (age) parts.push(age.label);
  return parts.join(", ") || "Finished";
}
