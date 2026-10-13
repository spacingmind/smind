import { useEffect, useState, type ReactNode } from "react";
import { Loader2 } from "lucide-react";

import { RunUsagePill } from "@/components/run-usage-pill";
import { Button } from "@/components/ui/button";
import { StatusDot } from "@/components/ui/status-dot";
import {
  describeRunFooter,
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
export function RunFooter<T extends RunFooterRun>({
  run,
  providerLabels,
  modelLabels,
  isLatest = false,
  onContinue,
  onRerun,
  children,
}: {
  run: T;
  /** provider.list's id→label map ("label ?? id", run-config IA). */
  providerLabels?: Record<string, string>;
  /** `${provider}/${modelId}` → human label; `${provider}/` → that provider's default-model label. */
  modelLabels?: RunModelLabels;
  /** Continue/Retry are offered on the chat's latest run only. */
  isLatest?: boolean;
  /** Continue: re-submits with the prompt "Continue" on the run's own config (task-detail wires it). */
  onContinue?: (run: T) => void;
  /** Retry: the existing Re-run path, offered on a failed latest run. */
  onRerun?: (run: T) => void;
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
          "flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 sm:gap-x-2 transition-opacity",
          view.kind === "done" &&
            "opacity-70 group-has-[:focus-visible]/answer:opacity-100 group-hover/answer:opacity-100 [@media(hover:none)]:opacity-100",
        )}
        data-testid="run-meta"
        data-status={run.status}
      >
        {running ? (
          <span className={SEGMENT} role="status">
            <span className="flex items-center gap-1.5">
              <Loader2 aria-hidden className="size-3.5 animate-spin motion-reduce:animate-none" />
              <span data-testid="run-status">{view.duration ? `Working… ${view.duration}` : "Working…"}</span>
            </span>
          </span>
        ) : (
          view.sentence !== undefined && (
            <span
              className={cn(SEGMENT, "gap-1.5", view.tone === "warning" && "text-warning", view.tone === "danger" && "text-destructive")}
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
        {!running && view.model && (
          <span data-testid="run-model" className={cn(SEGMENT, "min-w-0 truncate")}>
            {view.model}
          </span>
        )}
        {/* While running the live "Working… 12s" label already carries the elapsed time. */}
        {!running && view.duration && (
          <span data-testid="run-elapsed" title={view.durationTitle ?? undefined} className={SEGMENT}>
            {view.duration}
          </span>
        )}
        {run.usage !== undefined && (
          // The pill carries its own px-2 (a hover target): trade the dot's margin and the trailing gap for it so the dots stay evenly spaced.
          <span className={cn(SEGMENT, "sm:-mr-2 sm:before:mr-0")}>
            <RunUsagePill usage={run.usage} provider={run.provider} />
          </span>
        )}
        {view.age && (
          <time data-testid="run-age" dateTime={run.finishedAt} title={view.age.title} className={cn(SEGMENT, "shrink-0")}>
            {view.age.label}
          </time>
        )}
      </div>
      {children}
    </div>
  );
}

/** One footer segment: from `sm` up a middle dot before every segment but the first (a pseudo-element, so assistive tech skips it); below `sm` a wrapped line would start with a stray dot, so plain gaps do the separating. */
const SEGMENT =
  "flex items-center sm:before:mr-2 sm:before:text-foreground-subtle sm:before:content-['·'] sm:first:before:content-none";

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
