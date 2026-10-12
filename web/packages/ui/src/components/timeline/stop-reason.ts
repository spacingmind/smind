export interface StopReasonView {
  label: string;
  tone: "neutral" | "warning";
  hint?: string;
}

/**
 * Turns a run's raw ACP stop reason into what the footer shows: a run that
 * stopped on a cap (turn/output limit) or a refusal must not look plainly
 * done (dogfood-gap-fixes AC5). `end_turn` is the normal "the agent finished
 * its turn" reason and adds nothing next to "done", so it shows nothing;
 * `cancelled` reads as "Stopped". Anything else is shown verbatim.
 */
export function describeStopReason(stopReason: string): StopReasonView | null {
  switch (stopReason) {
    case "end_turn":
      return null;
    case "cancelled":
      return { label: "Stopped", tone: "neutral" };
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
