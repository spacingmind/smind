// failureText.ts is the mobile half of the daemon's failure.Describe table
// (R3a AC4): one plain sentence per failure kind, carried on the wire as the
// failure action's `summary`, with this module's fallbacks for an older
// daemon or a missing sentence. Pure data + one function, no react-native
// imports, so the footer and the continue prompt share it testably.

/** The fallback sentence per failure kind (the daemon's Describe table); an unknown kind has none. */
export const FAILURE_FALLBACK_SENTENCE: Record<string, string> = {
  context_window_exceeded: "The conversation no longer fits the model's context window.",
  session_budget_exceeded: "The provider's session budget for this run is used up.",
  too_many_denials: 'The agent kept asking for actions that were denied, so the provider stopped the run.',
  cli_exited: 'The agent stopped before finishing; its session is saved, so you can continue where it left off.',
  plan_window_exhausted: "The plan's usage window is used up; it resets later.",
  transient_429: 'The provider throttled the request for a moment.',
  rate_limited: 'The provider limited how fast requests can be sent.',
  auth_expired: 'The provider no longer accepts the sign-in.',
  model_not_in_plan: "The account's plan does not include this model.",
};

/**
 * The daemon's one-sentence read of a failed run: the wire's action.summary
 * when non-empty, else the fallback sentence; '' for an unknown kind.
 */
export function failureSentence(kind: string | undefined, action?: { summary?: string }): string {
  if (action?.summary) return action.summary;
  return (kind && FAILURE_FALLBACK_SENTENCE[kind]) || '';
}
