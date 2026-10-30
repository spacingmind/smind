import { useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { toast } from "@/components/ui/toast";
import { formatResetClock, isRateLimitedRun, parseResetAt, retryNotBefore, waitUntilLabel } from "@/lib/rate-limit";
import type { FailoverSuccessor } from "@/lib/run-failover";
import type { RunFooterRun } from "@/components/timeline/run-footer-model";

/**
 * What the run footer needs to offer a rate-limited run's way out (ADR-0041
 * F2 UI): move the work to the profile's fallback, hold a retry until the
 * limit lifts, and show how a failed run and its successor link up. Built
 * once by the task pane (stable identity, so RunTimeline's memo holds).
 */
export interface RunFailover<T extends RunFooterRun = RunFooterRun> {
  /** The known profile's name for a fallback id; undefined when unknown (button hidden). */
  fallbackName: (profileId: number) => string | undefined;
  /** Starts the run's prompt on the fallback profile in a new chat of the task, then opens that chat. */
  retryOnFallback: (run: T) => Promise<void>;
  /** Queues the run's prompt on its own chat, held until `notBefore`. */
  waitUntil: (run: T, notBefore: string) => Promise<void>;
  /** Whether a held retry of this run's prompt is already in the chat's queue (survives a remount). */
  isHeld: (run: T) => boolean;
  /** Whether the run's provider still holds a rate-limit cooldown (ADR-0041 F1) the user could clear by hand. */
  cooldownActive: (provider: string) => boolean;
  /** Clears that cooldown ("I reset my quota, retry now"): the daemon releases it and wakes held retries. */
  clearCooldown: (provider: string) => Promise<void>;
  successor: (runId: string) => FailoverSuccessor | undefined;
  continues: (runId: string) => string | undefined;
  openChat: (chatId: number) => void;
}

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** Whether RunFailoverButtons would render anything: the footer then gives the actions their own row. */
export function hasFailoverActions<T extends RunFooterRun>(run: T, failover: RunFailover<T>, now: Date): boolean {
  if (!isRateLimitedRun(run) || failover.successor(run.id)) return false;
  const fallback = run.fallbackProfileId ? failover.fallbackName(run.fallbackProfileId) !== undefined : false;
  return (
    fallback || waitUntilLabel(run.failureResetAt, now, run.failureResetInferred) !== null || failover.isHeld(run) || failover.cooldownActive(run.provider)
  );
}

/** "I reset my quota", "Retry on <profile>" and "Wait until HH:MM" -- shown beside Retry on the latest rate-limited run. */
export function RunFailoverButtons<T extends RunFooterRun>({ run, failover, now }: { run: T; failover: RunFailover<T>; now: Date }) {
  const [fallbackBusy, setFallbackBusy] = useState(false);
  const [wait, setWait] = useState<"idle" | "pending" | "queued">("idle");
  const [clearBusy, setClearBusy] = useState(false);
  // Synchronous guards: a double click lands before the state update re-renders.
  const fallbackInFlight = useRef(false);
  const waitInFlight = useRef(false);
  const clearInFlight = useRef(false);

  if (!isRateLimitedRun(run) || failover.successor(run.id)) return null;

  const fallbackName = run.fallbackProfileId ? failover.fallbackName(run.fallbackProfileId) : undefined;
  const label = waitUntilLabel(run.failureResetAt, now, run.failureResetInferred);
  const notBefore = retryNotBefore(run.failureResetAt);
  const queued = failover.isHeld(run) || wait === "queued";

  const onFallback = () => {
    if (fallbackInFlight.current) return;
    fallbackInFlight.current = true;
    setFallbackBusy(true);
    failover
      .retryOnFallback(run)
      .catch((err: unknown) => toast({ variant: "error", title: "Retry on fallback failed", description: message(err) }))
      .finally(() => {
        fallbackInFlight.current = false;
        setFallbackBusy(false);
      });
  };

  const onWait = () => {
    if (waitInFlight.current || notBefore === null) return;
    waitInFlight.current = true;
    setWait("pending");
    failover
      .waitUntil(run, notBefore)
      .then(() => setWait("queued"))
      .catch((err: unknown) => {
        setWait("idle");
        toast({ variant: "error", title: "Could not queue the retry", description: message(err) });
      })
      .finally(() => {
        waitInFlight.current = false;
      });
  };

  const onClearCooldown = () => {
    if (clearInFlight.current) return;
    clearInFlight.current = true;
    setClearBusy(true);
    failover
      .clearCooldown(run.provider)
      .catch((err: unknown) => toast({ variant: "error", title: "Could not clear the cooldown", description: message(err) }))
      .finally(() => {
        clearInFlight.current = false;
        setClearBusy(false);
      });
  };

  const reset = parseResetAt(run.failureResetAt);
  return (
    <>
      {failover.cooldownActive(run.provider) && (
        <Button
          type="button"
          variant="outline"
          size="xs"
          data-testid="run-clear-cooldown"
          disabled={clearBusy}
          onClick={onClearCooldown}
        >
          I reset my quota, retry now
        </Button>
      )}
      {fallbackName !== undefined && (
        <Button
          type="button"
          variant="outline"
          size="xs"
          data-testid="run-retry-fallback"
          disabled={fallbackBusy}
          onClick={onFallback}
        >
          Retry on {fallbackName}
        </Button>
      )}
      {queued ? (
        reset !== null && (
          <span data-testid="run-wait-queued" role="status" className="text-foreground-muted">
            Queued · starts after {formatResetClock(reset, now, run.failureResetInferred)}
          </span>
        )
      ) : (
        label !== null &&
        reset !== null && (
          <Button
            type="button"
            variant="outline"
            size="xs"
            data-testid="run-wait"
            disabled={wait === "pending"}
            aria-busy={wait === "pending"}
            onClick={onWait}
          >
            {wait === "pending" ? "Queuing…" : label}
          </Button>
        )
      )}
    </>
  );
}

/** "continued on <provider> in chat #N" (the failed run) / "continues run <id>" (its successor), as footer segments. */
export function RunFailoverNotes<T extends RunFooterRun>({
  run,
  failover,
  segmentClass,
  providerLabels,
}: {
  run: T;
  failover: RunFailover<T>;
  segmentClass: string;
  providerLabels?: Record<string, string>;
}) {
  const next = failover.successor(run.id);
  const from = failover.continues(run.id);
  if (!next && !from) return null;
  return (
    <>
      {next && (
        <button
          type="button"
          data-testid="run-continued-on"
          className={`${segmentClass} rounded-sm underline-offset-2 hover:text-foreground hover:underline focus-visible:outline-2`}
          onClick={() => failover.openChat(next.chatId)}
        >
          continued on {providerLabels?.[next.provider] ?? next.provider} in chat #{next.chatId}
        </button>
      )}
      {from && (
        <span data-testid="run-continues" className={`${segmentClass} text-foreground-subtle`} title={`Continues run ${from}`}>
          continues run {shortRunId(from)}
        </span>
      )}
    </>
  );
}

/** The first 8 characters of a run id -- enough to recognise it, short enough for a footer. */
export function shortRunId(id: string): string {
  return id.slice(0, 8);
}
