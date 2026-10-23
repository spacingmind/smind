import { PRSection } from "@/components/pr-panel/pr-section";
import { UntrustedText } from "@/components/pr-panel/pr-text";
import { StatusBadge, type StatusBadgeStatus } from "@/components/ui/status-badge";
import { useNow } from "@/hooks/use-now";
import { formatRelativeTime } from "@/lib/relative-time";
import type { PRDetails, ReviewItemState } from "@/lib/types";

const DECISION: Partial<Record<PRDetails["reviewDecision"], { label: string; status: StatusBadgeStatus }>> = {
  approved: { label: "Approved", status: "success" },
  changes_requested: { label: "Changes requested", status: "danger" },
  review_required: { label: "Review required", status: "warning" },
};

const STATE: Record<ReviewItemState, string> = {
  approved: "Approved",
  changes_requested: "Changes requested",
  commented: "Commented",
  dismissed: "Dismissed",
};

/** "1 of 2 approvals", or "1 approval" when the required count is unknown. */
export function approvalsText(reviews: PRDetails["reviews"]): string {
  if (reviews.required !== null) return `${reviews.approvals} of ${reviews.required} approvals`;
  return reviews.approvals === 1 ? "1 approval" : `${reviews.approvals} approvals`;
}

export function PRReviewsSection({ details }: { details: PRDetails }) {
  const now = useNow();
  const { reviews, reviewDecision } = details;
  const decision = DECISION[reviewDecision];
  return (
    <PRSection id="reviews" title="Reviews" summary={approvalsText(reviews)}>
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-ui-base">
        {decision && (
          <StatusBadge status={decision.status} data-testid="pr-review-decision">
            {decision.label}
          </StatusBadge>
        )}
        <span data-testid="pr-approvals">{approvalsText(reviews)}</span>
      </div>
      {reviews.items.length === 0 ? (
        <p className="mt-1 text-ui-sm text-foreground-muted">No reviews yet.</p>
      ) : (
        <ul className="mt-2 flex flex-col" data-testid="pr-reviews-list">
          {reviews.items.map((review, i) => (
            <li key={`${review.author}-${i}`} data-testid="pr-review-row" className="flex min-w-0 items-baseline gap-2 py-0.5 text-ui-base">
              <UntrustedText className="font-medium">{review.author}</UntrustedText>
              <span className="shrink-0 text-ui-sm text-foreground-muted">{STATE[review.state]}</span>
              <span className="ml-auto shrink-0 text-ui-xs text-foreground-subtle">{formatRelativeTime(review.submittedAt, now)}</span>
            </li>
          ))}
        </ul>
      )}
      {reviews.awaiting.length > 0 && (
        <p className="mt-1 min-w-0 text-ui-sm text-foreground-muted" data-testid="pr-awaiting">
          Awaiting: <UntrustedText>{reviews.awaiting.join(", ")}</UntrustedText>
        </p>
      )}
      {reviews.truncated && <p className="mt-1 text-ui-sm text-foreground-muted">More reviews are not shown.</p>}
    </PRSection>
  );
}
