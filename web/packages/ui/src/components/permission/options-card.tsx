import { useState } from "react";

import { DenyReasonField } from "@/components/permission/deny-reason-field";
import { PermissionDetailView } from "@/components/permission/permission-detail";
import { Alert } from "@/components/ui/alert";
import {
  PermissionOptionButton,
  recommendedOptionId,
} from "@/components/permission/permission-option-button";
import type { PendingPermission } from "@/hooks/use-run-timeline";

/**
 * The default permission variant: a summary and one button per option,
 * styled by kind (Item 11 -- destructive/deny reads as deny, the
 * recommended action reads as primary). What every plain
 * allow/deny-shaped request renders as, and what the plan-review
 * variant falls back to if its own shape is incomplete.
 */
export function OptionsCard({
  runId,
  pending,
  onRespond,
}: {
  runId: string;
  pending: PendingPermission;
  onRespond: (
    runId: string,
    requestId: string,
    optionId: string,
    reason?: string,
  ) => Promise<void>;
}) {
  const [respondingTo, setRespondingTo] = useState<string | null>(null);
  const [respondError, setRespondError] = useState<string | null>(null);
  const [reason, setReason] = useState("");
  // A git rule is per-call: never offer a sticky "always" choice for it.
  const options = pending.detail?.gitRule
    ? pending.options.filter((o) => !o.kind.endsWith("_always"))
    : pending.options;
  const recommended = recommendedOptionId(options);

  async function handleClick(optionId: string, kind: string) {
    setRespondingTo(optionId);
    setRespondError(null);
    // Only a reject-kind answer may carry the reason; an allow never does,
    // and an empty/blank one falls back to the exact old three-arg call.
    const trimmed = kind.startsWith("reject")
      ? reason.trim() || undefined
      : undefined;
    try {
      if (trimmed !== undefined)
        await onRespond(runId, pending.requestId, optionId, trimmed);
      else await onRespond(runId, pending.requestId, optionId);
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
      description={
        respondError || pending.detail ? (
          <>
            {pending.detail && (
              <PermissionDetailView
                detail={pending.detail}
                requestId={pending.requestId}
              />
            )}
            {respondError && <div>{`respond failed: ${respondError}`}</div>}
          </>
        ) : undefined
      }
    >
      <div className="flex w-full flex-col gap-2">
        <div className="flex flex-wrap items-center gap-2">
          {options.map((option, index) => (
            <PermissionOptionButton
              key={option.id}
              option={option}
              isRecommended={option.id === recommended}
              disabled={respondingTo !== null}
              testId={`chat-permission-option-${index}`}
              onClick={() => handleClick(option.id, option.kind)}
            />
          ))}
        </div>
        {options.some((o) => o.kind.startsWith("reject")) && (
          <DenyReasonField
            value={reason}
            onChange={setReason}
            disabled={respondingTo !== null}
          />
        )}
      </div>
    </Alert>
  );
}
