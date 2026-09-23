// permissionRequests.ts is Milestone 3 Item 2's approve/deny logic, kept
// out of the component so it can be unit-tested against a fake connection
// (the same discipline as api.ts/followUpPrompt.ts). PermissionBoard
// tracks every permission_request this screen has seen, keyed by
// requestId and fed from ALL three delivery paths -- run.logs history,
// the initial run's attach, and any follow-up run's attach -- because a
// request can be resolved from anywhere (this device's tap, the web UI,
// an auto-safe/timeout), and the one source of truth is the event
// stream: a permission_resolved event always overwrites optimistic
// local state. respondToPermission is the tap flow: resolve optimistically
// first (buttons disable immediately, like a sent message in a chat
// app), then run.respondPermission, rolling back only if the RPC itself
// rejects AND no authoritative resolution landed in the meantime.

import { respondPermission } from './api';
import { RelayConnection } from './relay/RelayConnection';

/** One option of a permission_request (handlers.go's permissionOptionParams). */
export interface PermissionOption {
  id: string;
  label: string;
  kind: string;
}

/** The wire params of a permission_request event/log-entry. */
export interface PermissionRequestParams {
  requestId: string;
  summary: string;
  options: PermissionOption[];
}

/** The wire params of a permission_resolved event/log-entry. */
export interface PermissionResolvedParams {
  requestId: string;
  optionId: string;
  reason?: string;
}

/** One tracked request's renderable state. */
export interface PermissionRequestState {
  /** The run this request belongs to -- where run.respondPermission must land. */
  runId: string;
  requestId: string;
  summary: string;
  options: PermissionOption[];
  status: 'pending' | 'resolved';
  /** Set when status is resolved: which option won, and whether by this device's own tap or by an authoritative event. */
  resolvedWith?: { optionId: string; by: 'tap' | 'event' };
  /** Inline error from a failed run.respondPermission (cleared on the next attempt or resolution). */
  error: string | null;
}

export class PermissionBoard {
  /** Called after every state change; the screen re-reads list(). */
  onChange: () => void = () => {};

  private requests = new Map<string, PermissionRequestState>();

  /** A permission_request from history or a live attach. A duplicate (attach backfill after the screen already saw it) changes nothing. */
  requestReceived(runId: string, params: PermissionRequestParams): void {
    if (this.requests.has(params.requestId)) return;
    this.requests.set(params.requestId, {
      runId,
      requestId: params.requestId,
      summary: params.summary,
      options: params.options ?? [],
      status: 'pending',
      error: null,
    });
    this.onChange();
  }

  /** A permission_resolved from history or a live attach -- authoritative over any optimistic state, whatever resolved it. */
  resolvedEvent(params: PermissionResolvedParams): void {
    const req = this.requests.get(params.requestId);
    if (!req) return;
    req.status = 'resolved';
    req.resolvedWith = { optionId: params.optionId, by: 'event' };
    req.error = null;
    this.onChange();
  }

  /** Folds a run.logs history batch in order: a request+resolved pair lands directly resolved, never interactive. */
  applyLogEvents(runId: string, events: unknown[]): void {
    for (const ev of events as Array<{ type?: string } & PermissionRequestParams & PermissionResolvedParams>) {
      if (ev.type === 'permission_request') this.requestReceived(runId, ev);
      else if (ev.type === 'permission_resolved') this.resolvedEvent(ev);
    }
  }

  /** This device's tap: resolve optimistically. Ignored if already resolved (an event beat the tap to it). */
  choose(requestId: string, optionId: string): void {
    const req = this.requests.get(requestId);
    if (!req || req.status === 'resolved') return;
    req.status = 'resolved';
    req.resolvedWith = { optionId, by: 'tap' };
    req.error = null;
    this.onChange();
  }

  /** A failed run.respondPermission: roll the optimistic resolution back -- unless an authoritative event already resolved the request (e.g. an auto-safe timeout fired before the tap's RPC landed), in which case the event wins and the failure is moot. */
  chooseFailed(requestId: string, optionId: string, message: string): void {
    const req = this.requests.get(requestId);
    if (!req || req.status !== 'resolved' || req.resolvedWith?.by !== 'tap' || req.resolvedWith.optionId !== optionId) return;
    req.status = 'pending';
    req.resolvedWith = undefined;
    req.error = message;
    this.onChange();
  }

  /** All tracked requests, in arrival order. */
  list(): PermissionRequestState[] {
    return [...this.requests.values()];
  }
}

/**
 * Routes one live attach event (from the initial run's or any follow-up
 * run's run.attach) into the board. Returns true if the event was a
 * permission event (permission events render through the board, never as
 * timeline text lines).
 */
export function feedPermissionEvent(board: PermissionBoard, runId: string, event: string, params: unknown): boolean {
  if (event === 'permission_request') {
    board.requestReceived(runId, params as PermissionRequestParams);
    return true;
  }
  if (event === 'permission_resolved') {
    board.resolvedEvent(params as PermissionResolvedParams);
    return true;
  }
  return false;
}

/**
 * The tap flow: optimistically resolve (so the UI disables immediately),
 * then run.respondPermission against the request's own run. A rejection
 * rolls back via chooseFailed -- which is a no-op if a permission_resolved
 * event already landed authoritatively. The error surfaces inline through
 * the board's state, so nothing is rethrown.
 */
export async function respondToPermission(
  conn: RelayConnection,
  board: PermissionBoard,
  requestId: string,
  optionId: string,
): Promise<void> {
  const req = board.list().find((r) => r.requestId === requestId);
  if (!req || req.status !== 'pending') return;
  board.choose(requestId, optionId);
  try {
    await respondPermission(conn, req.runId, requestId, optionId);
  } catch (e) {
    board.chooseFailed(requestId, optionId, e instanceof Error ? e.message : String(e));
  }
}
