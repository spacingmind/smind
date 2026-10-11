export interface StopReasonView {
  label: string;
  tone: "neutral" | "warning";
  hint?: string;
}

/**
 * Turns a run's raw ACP stop reason into what the footer shows: a run that
 * stopped on a cap (turn/output limit) or a refusal must not look plainly
 * done (dogfood-gap-fixes AC5). Anything else is shown verbatim.
 */
export function describeStopReason(stopReason: string): StopReasonView {
  switch (stopReason) {
    case "max_turn_requests":
      return { label: "Turn limit reached", tone: "warning", hint: "Send a message to continue" };
    case "max_tokens":
      return { label: "Output limit reached", tone: "warning", hint: "Send a message to continue" };
    case "refusal":
      return { label: "Agent refused", tone: "warning" };
    default:
      return { label: stopReason, tone: "neutral" };
  }
}
