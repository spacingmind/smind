/**
 * Every client word of the run failure classes (run failure classes plan,
 * R3a): one fallback sentence per class, replaced on the wire by the
 * daemon's `action.summary` so an older daemon still reads well and a
 * newer one decides the words. Nothing here says what to approve, allow
 * or merge -- a human-fix class is never retried from this surface.
 */

/** The fallback sentence of every class the daemon can report; an unknown kind has none. */
export const FAILURE_FALLBACK_SENTENCE: Record<string, string> = {
  plan_window_exhausted: "The plan's usage window is used up; it resets later.",
  transient_429: "The provider throttled the request for a moment.",
  rate_limited: "The provider limited how fast requests can be sent.",
  auth_expired: "The provider no longer accepts the sign-in.",
  model_not_in_plan: "The account's plan does not include this model.",
  context_window_exceeded: "The conversation no longer fits the model's context window.",
  session_budget_exceeded: "The provider's session budget for this run is used up.",
  too_many_denials: "The agent kept asking for actions that were denied, so the provider stopped the run.",
  cli_exited: "The agent stopped before finishing; its session is saved, so you can continue where it left off.",
};

/** The one plain sentence of a failure: the daemon's summary when it sent one, else the class's fallback, else "". */
export function failureSentence(kind: string | undefined | null, action?: { summary?: string } | null): string {
  if (action?.summary) return action.summary;
  return kind ? (FAILURE_FALLBACK_SENTENCE[kind] ?? "") : "";
}
