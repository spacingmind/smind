import { useState } from "react";

import { Alert } from "@/components/ui/alert";
import { PermissionOptionButton, recommendedOptionId } from "@/components/permission/permission-option-button";
import type { PendingPermission } from "@/hooks/use-run-timeline";

/**
 * The default permission variant: a summary and one button per option,
 * styled by kind (Item 11 -- destructive/deny reads as deny, the
 * recommended action reads as primary). What every plain
 * allow/deny-shaped request renders as, and what the question-form/
 * plan-review variants fall back to if their own shape is incomplete.
 */
export function OptionsCard({
  runId,
  pending,
  onRespond,
}: {
  runId: string;
  pending: PendingPermission;
  onRespond: (runId: string, requestId: string, optionId: string) => Promise<void>;
}) {
  const [respondingTo, setRespondingTo] = useState<string | null>(null);
  const [respondError, setRespondError] = useState<string | null>(null);
  const recommended = recommendedOptionId(pending.options);

  async function handleClick(optionId: string) {
    setRespondingTo(optionId);
    setRespondError(null);
    try {
      await onRespond(runId, pending.requestId, optionId);
    } catch (err) {
      setRespondError(err instanceof Error ? err.message : String(err));
      setRespondingTo(null);
    }
    // On success, leave the buttons disabled: the run's own subscription
    // observes the matching "permission_resolved" event and this component
    // unmounts (pendingPermission clears) shortly -- no need to reset here.
  }

  return (
    <Alert
      testId="pending-permission"
      variant="warning"
      title={pending.summary}
      description={respondError ? `respond failed: ${respondError}` : undefined}
    >
      {pending.options.map((option, index) => (
        <PermissionOptionButton
          key={option.id}
          option={option}
          isRecommended={option.id === recommended}
          disabled={respondingTo !== null}
          testId={`chat-permission-option-${index}`}
          onClick={() => handleClick(option.id)}
        />
      ))}
    </Alert>
  );
}
