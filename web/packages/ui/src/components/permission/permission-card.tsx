import { useEffect, useRef } from "react";

import { OptionsCard } from "@/components/permission/options-card";
import { PlanReviewCard } from "@/components/permission/plan-review-card";
import { QuestionFormCard } from "@/components/permission/question-form-card";
import type { PendingPermission } from "@/hooks/use-run-timeline";

/** True when the currently focused element is a text input the user could be mid-keystroke in -- the composer's textarea, chiefly. */
function isTextEntryFocused(): boolean {
  const active = document.activeElement;
  if (!(active instanceof HTMLElement)) return false;
  return active.tagName === "TEXTAREA" || active.tagName === "INPUT" || active.isContentEditable;
}

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
 * `tabIndex={-1}` plus a programmatic focus once per requestId is what
 * makes "the pending card is focusable" true for a *screen-reader or
 * keyboard-only* user landing on the page fresh. It skips that focus grab
 * when the composer (or any other text field) already has focus: a new
 * request can arrive mid-keystroke -- a run doing several tool calls asks
 * more than once -- and yanking focus away from an input the user is
 * actively typing into would silently drop whatever they type next on the
 * floor, not just annoy them.
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
    if (isTextEntryFocused()) return;
    ref.current?.focus();
  }, [pending.requestId]);

  return (
    <div
      ref={ref}
      tabIndex={-1}
      role="group"
      aria-label={pending.summary}
      data-testid="permission-card"
      className="rounded-lg bg-card shadow-lg outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
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
