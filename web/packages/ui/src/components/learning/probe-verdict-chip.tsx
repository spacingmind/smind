import { StatusBadge } from "@/components/ui/status-badge";
import { VERDICT_LABELS, VERDICT_TONES } from "@/lib/probe-format";
import type { ProbeVerdict } from "@/lib/probe-types";

/**
 * The probe verdict as a pill. The label carries the meaning (never colour alone) and no verdict
 * uses the success tone: `Helped (consistent)` is information for the review, not an approval.
 */
export function ProbeVerdictChip({ verdict }: { verdict: ProbeVerdict }) {
  return (
    <StatusBadge status={VERDICT_TONES[verdict]} data-testid="probe-verdict" data-verdict={verdict}>
      {VERDICT_LABELS[verdict]}
    </StatusBadge>
  );
}
