import { useEffect, useState, type ReactNode } from "react";
import { Loader2 } from "lucide-react";

import { RunUsagePill } from "@/components/run-usage-pill";
import { Button } from "@/components/ui/button";
import { StatusDot } from "@/components/ui/status-dot";
import {
  describeRunFooter,
  formatDuration,
  formatPreciseDuration,
  formatRelativeTime,
  resolveRunModelLabel,
  type RunModelLabels,
  type RunFooterRun,
} from "@/components/timeline/run-footer-model";
import { cn } from "@/lib/utils";

/**
 * A run footer that reads like a sentence (readable run footers): the
 * status word, provider id and token counts are gone; what stays is a
 * human duration, the model's human label, the compact cost, and -- for
 * an abnormal ending -- the sentence and a Continue/Retry button. All
 * wording is decided by describeRunFooter (see run-footer-model.ts); this
 * component only renders it, plus the two live tickers (the running
 * elapsed label and the relative age) that a pure view model can't own.
 */
export function RunFooter({
  run,
  providerLabels,
  modelLabels,
  isLatest = false,
  onContinue,
  onRerun,
  children,
}: {
  run: RunFooterRun;
  /** provider.list's id→label map ("label ?? id", run-config IA). */
  providerLabels?: Record<string, string>;
  /** `${provider}/${modelId}` → human label; `${provider}/` → that provider's default-model label. */
  modelLabels?: RunModelLabels;
  /** Continue/Retry are offered on the chat's latest run only. */
  isLatest?: boolean;
  /** Continue: re-submits with the prompt "Continue" on the run's own config (task-detail wires it). */
  onContinue?: (run: RunFooterRun) => void;
  /** Retry: the existing Re-run path, offered on a failed latest run. */
  onRerun?: (run: RunFooterRun) => void;
  /** The hover action bar (copy / re-run) -- rendered as-is at the end of the line. */
  children?: ReactNode;
}) {
  const running = run.status === "running";
  // The live tickers: a running label re-renders every second, an age
  // label every 30. Pinned to the least frequent rate the text changes at
  // (Paseo's own rationale), and only while the run is mounted.
  const tickSecond = useTicker(running ? 1000 : null);
  const tickAge = useTicker(run.finishedAt ? 30_000 : null);
  const now = running && tickSecond !== null ? new Date(tickSecond) : tickAge !== null ? new Date(tickAge) : new Date();

  const modelLabel = running ? "" : resolveRunModelLabel(run, modelLabels, providerLabels?.[run.provider]);
  const view = describeRunFooter(run, { now, modelLabel });

  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-ui-sm text-foreground-muted" data-testid="run-footer">
      <div
        role="group"
        aria-label={view.ariaLabel}
        className={cn(
          "flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 transition-opacity",
          run.status === "done" &&
            "opacity-70 group-has-[:focus-visible]/answer:opacity-100 group-hover/answer:opacity-100 [@media(hover:none)]:opacity-100",
        )}
        data-testid="run-meta"
      >
        {running ? (
          <span className="flex items-center gap-1.5" role="status">
            <Loader2 aria-hidden className="size-3.5 animate-spin motion-reduce:animate-none" />
            <span data-testid="run-status" data-status={run.status}>
              Working…{liveLabel(run, now) ? ` ${liveLabel(run, now)}` : ""}
            </span>
          </span>
        ) : (
          view.sentence !== undefined && (
            <span
              className={cn("flex items-center gap-1.5", view.tone === "warning" && "text-warning", view.tone === "danger" && "text-destructive")}
            >
              {view.tone !== "neutral" && <StatusDot status={view.tone === "danger" ? "danger" : "warning"} />}
              <span data-testid="run-stop-reason" data-tone={view.tone}>
                {view.sentence}
              </span>
              {view.action === "continue" && isLatest && onContinue && (
                <Button
                  type="button"
                  variant="outline"
                  size="xs"
                  data-testid="run-continue"
                  className="ml-1"
                  onClick={() => onContinue(run)}
                >
                  Continue
                </Button>
              )}
              {view.action === "retry" && isLatest && onRerun && (
                <Button
                  type="button"
                  variant="outline"
                  size="xs"
                  data-testid="run-retry"
                  className="ml-1"
                  onClick={() => onRerun(run)}
                >
                  Retry
                </Button>
              )}
            </span>
          )
        )}
        {!running && view.sentence === undefined && (
          <span data-testid="run-status" data-status={run.status} />
        )}
        {!running && view.model && (
          <span data-testid="run-model" className="min-w-0 truncate">
            {view.model}
          </span>
        )}
        {view.duration && (
          <span data-testid="run-elapsed" title={view.durationTitle ?? undefined}>
            {view.duration}
          </span>
        )}
        <RunUsagePill usage={"usage" in run ? run.usage : undefined} provider={run.provider} />
        {view.age && (
          <time data-testid="run-age" dateTime={run.finishedAt} title={view.age.title} className="shrink-0">
            {view.age.label}
          </time>
        )}
      </div>
      {children}
    </div>
  );
}

function liveElapsedMs(run: RunFooterRun, now: Date): number | null {
  const start = Date.parse(run.startedAt);
  if (Number.isNaN(start)) return null;
  return Math.max(0, now.getTime() - start);
}

function liveLabel(run: RunFooterRun, now: Date): string {
  const ms = liveElapsedMs(run, now);
  return ms === null ? "" : formatDuration(ms);
}

/** Re-renders every `intervalMs` while mounted (null = off), returning the tick's timestamp. */
function useTicker(intervalMs: number | null): number | null {
  const [tick, setTick] = useState<number | null>(null);
  useEffect(() => {
    if (intervalMs === null) return;
    setTick(Date.now());
    const id = setInterval(() => setTick(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return tick;
}

// Re-exported for the component tests' precise-duration title assertions.
export { formatPreciseDuration, formatRelativeTime };
