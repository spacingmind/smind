import { useId, useState } from "react";

import { GrantControl } from "@/components/permission/grant-control";
import { DenyReasonField } from "@/components/permission/deny-reason-field";
import { PermissionDetailView } from "@/components/permission/permission-detail";
import {
  COMPACT_TOUCH_BUTTON_CLASS,
  PermissionOptionButton,
  recommendedOptionId,
} from "@/components/permission/permission-option-button";
import { Button } from "@/components/ui/button";
import type { PendingPermission } from "@/hooks/use-run-timeline";
import { Icons } from "@/lib/icons";

/**
 * The default permission variant: a summary and one button per option,
 * styled by kind (Item 11 -- destructive/deny reads as deny, the
 * recommended action reads as primary). What every plain
 * allow/deny-shaped request renders as, and what the plan-review
 * variant falls back to if its own shape is incomplete.
 *
 * Renders straight inside the shared card container -- no nested bordered
 * `Alert`: the summary is a heading row (warning icon plus 14px medium
 * text), then the detail view, then a plain-sentence error if a respond
 * failed, then the buttons and the on-request reason field.
 */
export function OptionsCard({
  runId,
  pending,
  onRespond,
  onAlwaysAllow,
}: {
  runId: string;
  pending: PendingPermission;
  onRespond: (
    runId: string,
    requestId: string,
    optionId: string,
    reason?: string,
  ) => Promise<void>;
  onAlwaysAllow?: (runId: string, requestId: string, optionId: string, allowProjectCode: boolean) => Promise<void>;
}) {
  const [respondingTo, setRespondingTo] = useState<string | null>(null);
  const [respondError, setRespondError] = useState<string | null>(null);
  const [reasonOpen, setReasonOpen] = useState(false);
  // Lives here, not in DenyReasonField, so the text survives the field's
  // collapse and re-open.
  const [reason, setReason] = useState("");
  const fieldId = useId();
  // A git rule is per-call: never offer a sticky "always" choice for it.
  const options = pending.detail?.gitRule
    ? pending.options.filter((o) => !o.kind.endsWith("_always"))
    : pending.options;
  const recommended = recommendedOptionId(options, pending.detail?.kind);
  const hasReject = options.some((o) => o.kind.startsWith("reject"));
  // The daemon offers "always allow in this project" only for a plain, grantable request, and
  // never with a git rule; the control needs the single-allow option it would save.
  const grantOffer = pending.detail?.gitRule ? undefined : pending.detail?.grant;
  const allowOnce = options.find((o) => o.kind === "allow_once");

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
    <div data-testid="pending-permission" className="flex w-full flex-col gap-2">
      <div className="flex items-start gap-2">
        <Icons.permissionDanger className="size-4 shrink-0 text-warning" aria-hidden />
        <span className="text-ui-base font-medium text-foreground">{pending.summary}</span>
      </div>
      {pending.detail && (
        <PermissionDetailView detail={pending.detail} requestId={pending.requestId} />
      )}
      {respondError && (
        <div
          role="alert"
          data-testid="permission-respond-error"
          className="flex flex-col gap-0.5 text-ui-sm"
        >
          <span className="text-destructive">Could not send your answer. Try again.</span>
          <span className="text-foreground-muted break-words">{respondError}</span>
        </div>
      )}
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
        {hasReject && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            data-testid="permission-deny-with-reason"
            aria-expanded={reasonOpen}
            aria-controls={fieldId}
            className={COMPACT_TOUCH_BUTTON_CLASS}
            disabled={respondingTo !== null}
            onClick={() => setReasonOpen((o) => !o)}
          >
            Deny with reason
          </Button>
        )}
      </div>
      {grantOffer && allowOnce && onAlwaysAllow && (
        <GrantControl
          key={pending.requestId}
          offer={grantOffer}
          disabled={respondingTo !== null}
          onSave={async (allowProjectCode) => {
            setRespondingTo(allowOnce.id);
            setRespondError(null);
            try {
              await onAlwaysAllow(runId, pending.requestId, allowOnce.id, allowProjectCode);
            } catch (err) {
              setRespondError(err instanceof Error ? err.message : String(err));
              setRespondingTo(null);
            }
          }}
        />
      )}
      {reasonOpen && (
        <div id={fieldId}>
          <DenyReasonField
            value={reason}
            onChange={setReason}
            disabled={respondingTo !== null}
            autoFocus
          />
        </div>
      )}
    </div>
  );
}
