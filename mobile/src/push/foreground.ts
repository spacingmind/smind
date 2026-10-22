// Foreground presentation rule (ADR-0040 section 5, AC20): suppress the banner
// only for the task whose detail screen is open on a live connection.

export interface ForegroundInput {
  appActive: boolean;
  connected: boolean;
  openTaskId: number | null;
  eventTaskId: number | null;
}

export function shouldPresentInForeground(i: ForegroundInput): boolean {
  const suppressed =
    i.appActive && i.connected && i.openTaskId !== null && i.eventTaskId !== null && i.openTaskId === i.eventTaskId;
  return !suppressed;
}
