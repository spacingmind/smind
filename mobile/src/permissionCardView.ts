// permissionCardView.ts is the pure copy of the mobile permission and question cards: the words for every way a
// permission request ends (never a raw option id), the plain sentence a failed answer shows, and when the deny
// reason field is on screen. No react-native import.

/** Plain words for the reasons the daemon gives a resolution; the same labels the web timeline uses. */
const REASON_COPY: Record<string, string> = {
  timeout: 'Timed out',
  auto_accept: 'Auto-accepted',
  auto_safe: 'Auto-approved (legacy)',
  policy_allow: 'Allowed by policy',
  policy_deny: 'Denied by policy',
  provider_cancellation: 'Cancelled by the agent',
};

/**
 * How a resolved permission reads, never with an option id. A person's own answer reads by what they chose: this device's
 * tap, or the daemon's resolved event with reason `human` (which overwrites the tap, so `by` alone cannot tell the two
 * apart). A standing rule or a timeout reads as that; anything unknown, or an older daemon with no reason, is "Resolved".
 */
export function resolvedCopy(args: { by?: 'tap' | 'event'; optionKind?: string; reason?: string }): string {
  const person = args.reason === 'human' || (args.by === 'tap' && args.reason === undefined);
  if (person) {
    const kind = args.optionKind ?? '';
    if (kind.startsWith('reject')) return 'You denied this';
    if (kind.startsWith('allow')) return 'You allowed this';
    return 'You answered this';
  }
  return (args.reason !== undefined ? REASON_COPY[args.reason] : undefined) ?? 'Resolved';
}

/** A failed answer: a plain sentence first, the technical detail on its own muted line. */
export function errorCopy(detail: string): { headline: string; detail: string } {
  return { headline: 'Could not send your answer. Try again.', detail };
}

/** The reason field renders only once its toggle opened it; the typed text itself stays in the card's state. */
export function showReasonField(args: { hasReject: boolean; expanded: boolean }): boolean {
  return args.hasReject && args.expanded;
}

/** The "Deny with reason" toggle exists only when a reject option does. */
export function showReasonToggle(hasReject: boolean): boolean {
  return hasReject;
}
