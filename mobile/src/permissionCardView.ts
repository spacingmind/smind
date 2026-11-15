// permissionCardView.ts is the pure copy of the mobile permission and question cards: the words for every way a
// permission request ends (never a raw option id), the plain sentence a failed answer shows, and when the deny
// reason field is on screen. No react-native import.

/** How a resolved permission reads: this device's tap names what the human did; anything else was the agent's side. */
export function resolvedCopy(args: { by?: 'tap' | 'event'; optionKind?: string }): string {
  if (args.by !== 'tap') return 'Resolved by the agent';
  const kind = args.optionKind ?? '';
  if (kind.startsWith('reject')) return 'You denied this';
  if (kind.startsWith('allow')) return 'You allowed this';
  return 'You answered this';
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
