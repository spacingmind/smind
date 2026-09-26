import { useState } from "react";

import { liveSwitchablePolicies } from "@/lib/approval-policies";

import { Button } from "@/components/ui/button";
import type { ApprovalPolicy } from "@/lib/types";

// Shared vocabulary (lib/approval-policies.ts) -- "Manual approval"
// everywhere, not "Manual" here and "Manual approval" in the composer.
const LIVE_SWITCHABLE_POLICIES = liveSwitchablePolicies().map((p) => ({ value: p.id, label: p.label }));

/**
 * Live control for switching a running task's approvalPolicy between
 * "manual" and "auto-safe" while it's in flight
 * (docs/plans/active/mid-run-approval-and-retry-effort.md's Item A).
 *
 * "full-access" is never offered here -- it can only be chosen at
 * submission time (the composer), since entering/leaving it mid-run would
 * require respawning the provider's client (see
 * internal/runs.Registry.SetApprovalPolicy's doc comment). task-detail.tsx
 * doesn't even mount this component when the running run's current policy
 * is "full-access" or the run has already finished.
 */
export function ApprovalPolicyControl({
  policy,
  onChange,
}: {
  policy: ApprovalPolicy;
  onChange: (policy: ApprovalPolicy) => Promise<void>;
}) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function change(next: ApprovalPolicy) {
    if (next === policy || pending) return;
    setPending(true);
    try {
      await onChange(next);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setPending(false);
    }
  }

  return (
    <div
      data-testid="approval-policy-control"
      className="mx-auto flex w-full max-w-3xl shrink-0 flex-wrap items-center gap-2 px-4 py-1.5 text-ui-sm text-foreground-muted"
    >
      <span>Approval:</span>
      {LIVE_SWITCHABLE_POLICIES.map((candidate) => (
        <Button
          key={candidate.value}
          type="button"
          variant={candidate.value === policy ? "default" : "outline"}
          size="xs"
          disabled={pending}
          aria-pressed={candidate.value === policy}
          data-testid={`approval-policy-option-${candidate.value}`}
          onClick={() => change(candidate.value)}
        >
          {candidate.label}
        </Button>
      ))}
      {error && (
        <span data-testid="approval-policy-error" role="alert" className="text-destructive">
          {error}
        </span>
      )}
    </div>
  );
}
