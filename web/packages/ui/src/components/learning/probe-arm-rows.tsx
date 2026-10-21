import { RunLink } from "@/components/learning/run-link";
import { StatusDot, type StatusDotStatus } from "@/components/ui/status-dot";
import { ARM_LABELS, armStatusLabel, formatCostUSD, formatDurationMs } from "@/lib/probe-format";
import type { ProbeArmStatus, ProbeArmView } from "@/lib/probe-types";

function dotFor(status: ProbeArmStatus): StatusDotStatus {
  if (status === "running") return "running";
  if (status === "pending" || status === "done") return "neutral";
  return "warning";
}

/** The planned arms of a probe in order, one row each: status, elapsed, cost so far and the hidden run. */
export function ProbeArmRows({ arms, workspaceId }: { arms: ProbeArmView[]; workspaceId: number }) {
  return (
    <ul className="flex flex-col divide-y divide-border rounded-lg border border-border" data-testid="probe-arms">
      {arms.map((arm) => {
        const started = arm.status !== "pending";
        return (
          <li key={arm.role} data-testid="probe-arm" data-role={arm.role} data-status={arm.status} className="flex items-center gap-3 px-3 py-2">
            <StatusDot status={dotFor(arm.status)} aria-hidden />
            <span className="min-w-0 flex-1 truncate text-ui-sm text-foreground">{ARM_LABELS[arm.role]}</span>
            <span data-testid="probe-arm-status" className="text-ui-sm text-foreground-muted">
              {armStatusLabel(arm.status)}
            </span>
            <span data-testid="probe-arm-elapsed" className="w-14 text-right text-ui-sm tabular-nums text-foreground-muted">
              {started ? formatDurationMs(arm.elapsedMs) : "–"}
            </span>
            <span data-testid="probe-arm-cost" className="w-16 text-right text-ui-sm tabular-nums text-foreground-muted">
              {started ? formatCostUSD(arm.costUSD) : "–"}
            </span>
            <span className="w-28 text-right">{arm.runId ? <RunLink runId={arm.runId} workspaceId={workspaceId} /> : null}</span>
          </li>
        );
      })}
    </ul>
  );
}
