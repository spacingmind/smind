import { useState, type ReactNode } from "react";

import { ProbeArmRows } from "@/components/learning/probe-arm-rows";
import { ProbeContrastTable } from "@/components/learning/probe-contrast-table";
import { ProbeDiff } from "@/components/learning/probe-diff";
import { ProbeVerdictChip } from "@/components/learning/probe-verdict-chip";
import { Button } from "@/components/ui/button";
import { InlineSpinner } from "@/components/ui/inline-spinner";
import { LoadingRows } from "@/components/ui/loading-rows";
import { SegmentedControl } from "@/components/ui/segmented-control";
import { useCancelProbe, useProbe, useProbeEstimate, useStartProbe } from "@/hooks/use-learning";
import { useNow } from "@/hooks/use-now";
import { Icons } from "@/lib/icons";
import { cn } from "@/lib/utils";
import { ARM_LABELS, elapsedSince, formatDurationMs, formatMoney, formatSpendLine, isProbeActive, OVER_ESTIMATE_HINT } from "@/lib/probe-format";
import type { ProbeEstimate, ProbeView } from "@/lib/probe-types";

type RunMode = "full" | "quick";

const MODE_OPTIONS: { value: RunMode; label: string }[] = [
  { value: "full", label: "Full (3 arms)" },
  { value: "quick", label: "Quick (2 arms, no noise floor)" },
];

/** Linux and Windows have no probe at all: the card only explains it. */
function isUnsupported(estimate: ProbeEstimate): boolean {
  return estimate.reasons.some((r) => r.code === "platform_unsupported" || r.code === "windows");
}

function Quiet({ children, testId, role }: { children: ReactNode; testId?: string; role?: "alert" }) {
  return (
    <p data-testid={testId} role={role} className="text-ui-sm text-muted-foreground">
      {children}
    </p>
  );
}

/** Estimate, quick toggle, ineligibility reasons and the start button. */
function ProbeStart({ id, workspaceId, again, quick, onQuick }: { id: number; workspaceId: number; again: boolean; quick: boolean; onQuick: (quick: boolean) => void }) {
  const estimate = useProbeEstimate(id, quick, true);
  const start = useStartProbe();
  const e = estimate.data;
  // After a Full/Quick toggle the hook keeps the other size's answer on screen while the new one loads; that answer must not be started from.
  const stale = estimate.isPlaceholderData;
  if (estimate.isPending) return <InlineSpinner label="Estimating…" />;
  if (!e) return <Quiet testId="probe-estimate-error">{estimate.error?.message ?? "The estimate is not available."}</Quiet>;
  if (isUnsupported(e)) {
    return <Quiet testId="probe-unsupported">{e.reasons[0]?.message}</Quiet>;
  }
  const label = again ? "Run probe again" : "Run probe";
  return (
    <div className="flex flex-col gap-2" data-testid="probe-start">
      <SegmentedControl className="self-start" label="Probe size" options={MODE_OPTIONS} value={quick ? "quick" : "full"} onChange={(v) => onQuick(v === "quick")} />
      <div className={cn("flex flex-col gap-2", stale && "opacity-60")} data-stale={stale} data-testid="probe-estimate">
        <div className="flex flex-wrap items-center gap-2">
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={!e.eligible || start.isPending || stale}
            data-testid="probe-run"
            onClick={() => start.mutate({ id, workspaceId, quick })}
          >
            {start.isPending && <Icons.spinner className="animate-spin" aria-hidden />}
            {e.eligible ? `${label} (about ${formatMoney(e.estUSD)}, about ${e.expectedMinutes} min)` : label}
          </Button>
        </div>
        {/* Always laid out (hidden, not removed) so the block never changes size when the estimate refreshes. */}
        <span role="status" data-testid="probe-estimate-updating" className={cn("text-ui-sm text-foreground-muted", !stale && "invisible")}>
          updating…
        </span>
        {e.reasons.length > 0 && (
          <ul className="flex flex-col gap-0.5" data-testid="probe-reasons">
            {e.reasons.map((r) => (
              <li key={r.code} data-reason={r.code} className="text-ui-sm text-muted-foreground">
                {r.message}
              </li>
            ))}
          </ul>
        )}
      </div>
      {start.error && <Quiet testId="probe-start-error" role="alert">{start.error.message}</Quiet>}
    </div>
  );
}

function ProbeRunning({ probe, updatedAt, onCancel, cancelling }: { probe: ProbeView; updatedAt: number; onCancel: () => void; cancelling: boolean }) {
  // The daemon reports elapsed time only when the query refetches (events, 2 s safety net): tick locally in between.
  const now = useNow(1000);
  const spend = formatSpendLine(probe.costUSD, probe.estUSD);
  const arms = probe.arms.map((a) => (a.status === "running" ? { ...a, elapsedMs: elapsedSince(a.elapsedMs, updatedAt, now) } : a));
  return (
    <div className="flex flex-col gap-2" data-testid="probe-running" aria-live="polite">
      <ProbeArmRows arms={arms} workspaceId={probe.workspaceId} />
      <div className="flex flex-wrap items-center gap-3">
        <span className="text-ui-sm text-foreground-muted" data-testid="probe-spend" title={spend.over ? OVER_ESTIMATE_HINT : undefined}>
          {formatDurationMs(elapsedSince(probe.elapsedMs, updatedAt, now))} elapsed · {spend.text}
        </span>
        <Button type="button" size="sm" variant="ghost" disabled={cancelling} data-testid="probe-cancel" onClick={onCancel}>
          Cancel probe
        </Button>
      </div>
    </div>
  );
}

function yesNo(value: boolean | null | undefined): string {
  if (value === null || value === undefined) return "no signature";
  return value ? "yes" : "no";
}

function ProbeResult({ probe }: { probe: ProbeView }) {
  const skillArm = probe.arms.find((a) => a.role === "B");
  const control = probe.arms.find((a) => a.role === "A");
  const checks = probe.arms.filter((a) => a.check);
  const contrast = probe.contrast;
  return (
    <div className="flex flex-col gap-3" data-testid="probe-result">
      <div className="flex flex-wrap items-center gap-2">
        {probe.verdict && <ProbeVerdictChip verdict={probe.verdict} />}
        {probe.verdictReason && <span className="text-ui-base text-foreground-muted">{probe.verdictReason}</span>}
      </div>
      {probe.quick && <Quiet testId="probe-quick-notice">Quick probe: no noise floor, verdict capped at Inconclusive.</Quiet>}
      {contrast && (
        <>
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-0.5 text-ui-sm" data-testid="probe-facts">
            <dt className="text-foreground-subtle">Skill loaded</dt>
            <dd data-testid="probe-loaded" className="text-foreground-muted">{contrast.loaded ? "yes" : "no"}</dd>
            <dt className="text-foreground-subtle">Targeted failure recurred</dt>
            <dd data-testid="probe-recurred" className="text-foreground-muted">
              {ARM_LABELS.A.toLowerCase()}: {yesNo(control?.failureRecurred)} · with this skill: {yesNo(skillArm?.failureRecurred)}
            </dd>
            {checks.length > 0 && (
              <>
                <dt className="text-foreground-subtle">Check</dt>
                <dd data-testid="probe-check" className="text-foreground-muted">
                  {checks.map((a) => `${ARM_LABELS[a.role].toLowerCase()}: ${a.check?.passed ? "passed" : "failed"}`).join(" · ")}
                </dd>
              </>
            )}
          </dl>
          <ProbeContrastTable metrics={contrast.metrics} noiseFloor={contrast.noiseFloor} />
        </>
      )}
      <ProbeArmRows arms={probe.arms} workspaceId={probe.workspaceId} />
      {skillArm?.diff && <ProbeDiff diff={skillArm.diff} diffBytes={skillArm.diffBytes} />}
      <div className="flex flex-col gap-0.5" data-testid="probe-disclaimer">
        <Quiet>Behavioural contrast on one replay of the source episode, not a measurement.</Quiet>
        <Quiet>It says whether this skill fixes the observed failure here, nothing more. The decision stays yours.</Quiet>
      </div>
    </div>
  );
}

function summary(probe: ProbeView | null | undefined): ReactNode {
  if (!probe) return null;
  if (isProbeActive(probe.status)) return <span className="text-ui-sm text-foreground-muted">Running…</span>;
  if (probe.status === "cancelled") return <span className="text-ui-sm text-foreground-muted">Cancelled</span>;
  if (probe.verdict) return <ProbeVerdictChip verdict={probe.verdict} />;
  return null;
}

/**
 * Probe-before-approve (ADR-0037 §8): a collapsed card under the proposal's evidence that runs the
 * proposal's skill against its source episode in a sandbox and shows the contrast. Server state is
 * the shared probe queries (kept live by `learnedSkill.probe.*` events); open and quick are pure UI
 * toggles kept local. The verdict is advisory and never changes the proposal.
 */
export function ProbeCard({ id, workspaceId }: { id: number; workspaceId: number }) {
  const probe = useProbe(id);
  const cancel = useCancelProbe();
  const [open, setOpen] = useState(false);
  const [quick, setQuick] = useState(false);
  const p = probe.data;
  const Chevron = open ? Icons.chevronDown : Icons.chevronRight;

  let body: ReactNode;
  if (probe.isPending) {
    body = <LoadingRows rows={1} label="Loading probe…" />;
  } else if (probe.error) {
    body = (
      <div className="flex flex-wrap items-center gap-2" data-testid="probe-error">
        <Quiet role="alert">
          <span title={probe.error.message}>The probe could not be loaded.</span>
        </Quiet>
        <Button type="button" size="sm" variant="ghost" data-testid="probe-retry" onClick={() => void probe.refetch()}>
          <Icons.retry aria-hidden /> Retry
        </Button>
      </div>
    );
  } else if (p && isProbeActive(p.status)) {
    body = <ProbeRunning probe={p} updatedAt={probe.dataUpdatedAt} cancelling={cancel.isPending} onCancel={() => cancel.mutate({ id, workspaceId })} />;
  } else {
    const rerun = <ProbeStart id={id} workspaceId={workspaceId} again={p != null} quick={quick} onQuick={setQuick} />;
    if (!p) {
      body = rerun;
    } else if (p.status === "cancelled") {
      body = (
        <div className="flex flex-col gap-3" data-testid="probe-cancelled">
          <Quiet>Probe cancelled after {formatDurationMs(p.elapsedMs)}; {formatMoney(p.costUSD)} was spent. No verdict.</Quiet>
          {rerun}
        </div>
      );
    } else if (p.status === "failed" || (p.status === "done" && !p.verdict)) {
      body = (
        <div className="flex flex-col gap-3" data-testid="probe-failed">
          <div className="flex flex-wrap items-center gap-2">
            <ProbeVerdictChip verdict={p.verdict ?? "probe_failed"} />
            {p.verdictReason && <span className="text-ui-base text-foreground-muted">{p.verdictReason}</span>}
          </div>
          <ProbeArmRows arms={p.arms} workspaceId={p.workspaceId} />
          {rerun}
        </div>
      );
    } else {
      body = (
        <div className="flex flex-col gap-4">
          <ProbeResult probe={p} />
          {rerun}
        </div>
      );
    }
  }

  return (
    <section className="flex flex-col gap-2" data-testid="probe-card" aria-label="Probe">
      <button
        type="button"
        className="flex items-center gap-2 self-start rounded-md text-left outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
        aria-expanded={open}
        aria-controls={`probe-card-body-${id}`}
        data-testid="probe-toggle"
        onClick={() => setOpen((v) => !v)}
      >
        <Chevron aria-hidden className="size-3.5 text-foreground-subtle" />
        <h3 className="text-ui-base font-medium text-foreground">Probe</h3>
        {!open && summary(p)}
      </button>
      {open && (
        <div id={`probe-card-body-${id}`} data-testid="probe-body" className="flex flex-col gap-3 rounded-xl border border-border p-3">
          {body}
        </div>
      )}
    </section>
  );
}
