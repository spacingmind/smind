import { useEffect, useRef } from "react";

import { OptionsCard } from "@/components/permission/options-card";
import { PlanReviewCard } from "@/components/permission/plan-review-card";
import { QuestionFormCard } from "@/components/permission/question-form-card";
import type { PendingPermission } from "@/hooks/use-run-timeline";

/**
 * Dispatches one pending permission request to its variant, by shape:
 * `plan` wins over `questions` (a plan review that also carried a
 * question list would be a plan review with extra baggage, not the
 * reverse), then `questions`, else the plain per-option card every
 * request renders as today. A shape that's present but empty (e.g.
 * `questions: []`) falls through rather than rendering a variant with
 * nothing in it -- "a request with no recognised kind still renders
 * every option" (Item 11's own scenario) is this fallthrough.
 *
 * Wrapped in a focusable, labelled group (Item 11's keyboard scenario):
 * `tabIndex={-1}` plus a programmatic focus on first mount is what makes
 * "the pending card is focusable" true for a *screen-reader or
 * keyboard-only* user landing on the page fresh, without stealing focus
 * away from something the user is actively doing (typing in the
 * composer) on every subsequent render -- the effect's empty dependency
 * array means this fires once per requestId, not once per keystroke
 * elsewhere on the page.
 */
export function PermissionCard({
  runId,
  pending,
  onRespond,
  onChat,
}: {
  runId: string;
  pending: PendingPermission;
  onRespond: (runId: string, requestId: string, optionId: string) => Promise<void>;
  onChat: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    ref.current?.focus();
  }, [pending.requestId]);

  return (
    <div
      ref={ref}
      tabIndex={-1}
      role="group"
      aria-label={pending.summary}
      data-testid="permission-card"
      className="outline-none focus-visible:ring-2 focus-visible:ring-ring/50 rounded-lg"
    >
      {pending.plan ? (
        <PlanReviewCard runId={runId} pending={{ ...pending, plan: pending.plan }} onRespond={onRespond} onChat={onChat} />
      ) : pending.questions && pending.questions.length > 0 ? (
        <QuestionFormCard runId={runId} pending={{ ...pending, questions: pending.questions }} onRespond={onRespond} />
      ) : (
        <OptionsCard runId={runId} pending={pending} onRespond={onRespond} />
      )}
    </div>
  );
}
