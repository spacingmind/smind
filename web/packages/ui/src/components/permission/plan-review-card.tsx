import { useState } from "react";

import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { COMPACT_TOUCH_BUTTON_CLASS } from "@/components/permission/permission-option-button";
import { TimelineMarkdown } from "@/components/timeline/timeline-markdown";
import type { PendingPermission } from "@/hooks/use-run-timeline";

/** The synthesized option ids a plan review resolves to -- there is no real ACP/Claude option list backing this variant, so these are the client's own fixed vocabulary, same reasoning as taskrunner's claudeOptionAllow/claudeOptionDeny synthesis on the daemon side. */
export const PLAN_REVIEW_APPROVE = "plan_approve";
export const PLAN_REVIEW_REFUSE = "plan_refuse";

/**
 * The plan-review variant (Item 11): the plan rendered as markdown, with
 * `Chat about it / Refuse / Approve`. "Chat about it" doesn't resolve the
 * permission at all -- it dismisses the card locally so the human can
 * keep talking to the agent in the composer, matching Paseo's own
 * behaviour for this action.
 */
export function PlanReviewCard({
  runId,
  pending,
  onRespond,
  onChat,
}: {
  runId: string;
  pending: PendingPermission & { plan: string };
  onRespond: (runId: string, requestId: string, optionId: string) => Promise<void>;
  onChat: () => void;
}) {
  const [submitting, setSubmitting] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function resolve(optionId: string) {
    setSubmitting(optionId);
    setError(null);
    try {
      await onRespond(runId, pending.requestId, optionId);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setSubmitting(null);
    }
  }

  return (
    <Alert testId="pending-permission" variant="warning" title={pending.summary} description={error ?? undefined}>
      <div className="flex w-full flex-col gap-2" data-testid="plan-review">
        <div className="rounded-md border bg-surface px-2 py-1.5">
          <TimelineMarkdown content={pending.plan} />
        </div>
        <div className="flex items-center gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            className={COMPACT_TOUCH_BUTTON_CLASS}
            disabled={submitting !== null}
            data-testid="plan-review-chat"
            onClick={onChat}
          >
            Chat about it
          </Button>
          <Button
            type="button"
            variant="destructive"
            size="sm"
            className={COMPACT_TOUCH_BUTTON_CLASS}
            disabled={submitting !== null}
            data-testid="plan-review-refuse"
            onClick={() => resolve(PLAN_REVIEW_REFUSE)}
          >
            Refuse
          </Button>
          <Button
            type="button"
            size="sm"
            className={COMPACT_TOUCH_BUTTON_CLASS}
            disabled={submitting !== null}
            data-testid="plan-review-approve"
            onClick={() => resolve(PLAN_REVIEW_APPROVE)}
          >
            Approve
          </Button>
        </div>
      </div>
    </Alert>
  );
}
