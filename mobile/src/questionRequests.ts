// questionRequests.ts is the question twin of permissionRequests.ts (ADR-0055): the board of every question_request this
// screen has seen, keyed by requestId and fed from ALL delivery paths (run.logs history, the initial run's attach, any
// follow-up run's attach), because a question can be resolved from anywhere (this device's tap, the web, a timeout, a
// restart). The event stream is the one source of truth: a question_resolved event always overwrites the optimistic
// state of a tap. answerToQuestion is the tap flow: resolve optimistically (buttons disable at once), then
// run.answerQuestion, rolling back only if the RPC itself rejects AND no authoritative resolution landed meanwhile.

import { answerQuestion } from './api';
import type { QuestionAnswer, QuestionRequest } from './questionAnswer';
import type { RelayConnection } from './relay/RelayConnection';

/** The wire params of a live question_request attach event. */
export interface QuestionRequestParams {
  requestId: string;
  request: QuestionRequest;
}

/** The wire params of a live question_resolved attach event. */
export interface QuestionResolvedParams {
  requestId: string;
  reason?: string;
  answer?: QuestionAnswer;
}

/** The run.logs entry shapes (the same data under other keys: `question`, `questionAnswer`). */
interface QuestionLogEntry {
  type?: string;
  requestId?: string;
  question?: QuestionRequest;
  reason?: string;
  questionAnswer?: QuestionAnswer;
}

export interface QuestionState {
  runId: string;
  requestId: string;
  request: QuestionRequest;
  status: 'pending' | 'resolved';
  /** Set when resolved: why, who closed it (this device's tap or an authoritative event) and, for a human answer, what. */
  resolution?: { reason: string; by: 'tap' | 'event'; answer?: QuestionAnswer };
  /** Inline error from a failed run.answerQuestion (cleared on the next attempt or resolution). */
  error: string | null;
}

export class QuestionBoard {
  /** Called after every change; the screen re-reads list(). */
  onChange: () => void = () => {};

  private requests = new Map<string, QuestionState>();

  /** A question_request from history or a live attach. A duplicate (the attach backfill) changes nothing. */
  requestReceived(runId: string, requestId: string, request: QuestionRequest | undefined): void {
    if (!requestId || !request || !Array.isArray(request.questions) || this.requests.has(requestId)) return;
    this.requests.set(requestId, { runId, requestId, request, status: 'pending', error: null });
    this.onChange();
  }

  /** A question_resolved from history or a live attach: authoritative over any optimistic state, whatever closed it. */
  resolvedEvent(requestId: string, reason: string | undefined, answer: QuestionAnswer | undefined): void {
    const req = this.requests.get(requestId);
    if (!req) return;
    req.status = 'resolved';
    req.resolution = { reason: reason ?? '', by: 'event', ...(answer ? { answer } : {}) };
    req.error = null;
    this.onChange();
  }

  /** Folds a run.logs history batch in order: a request+resolved pair lands resolved, never interactive. */
  applyLogEvents(runId: string, events: unknown[]): void {
    for (const ev of events as QuestionLogEntry[]) {
      if (ev.type === 'question_request') this.requestReceived(runId, ev.requestId ?? '', ev.question);
      else if (ev.type === 'question_resolved') this.resolvedEvent(ev.requestId ?? '', ev.reason, ev.questionAnswer);
    }
  }

  /** This device's answer or decline: resolve optimistically. Ignored once resolved (an event beat the tap). */
  choose(requestId: string, answer: QuestionAnswer): void {
    const req = this.requests.get(requestId);
    if (!req || req.status === 'resolved') return;
    req.status = 'resolved';
    req.resolution = { reason: answer.action === 'decline' ? 'declined' : 'human', by: 'tap', answer };
    req.error = null;
    this.onChange();
  }

  /** A failed run.answerQuestion: roll the optimistic resolution back, unless an event already resolved the request. */
  chooseFailed(requestId: string, message: string): void {
    const req = this.requests.get(requestId);
    if (!req || req.status !== 'resolved' || req.resolution?.by !== 'tap') return;
    req.status = 'pending';
    req.resolution = undefined;
    req.error = message;
    this.onChange();
  }

  list(): QuestionState[] {
    return [...this.requests.values()];
  }
}

/** Routes one live attach event into the board; true if it was a question event (those never render as timeline text). */
export function feedQuestionEvent(board: QuestionBoard, runId: string, event: string, params: unknown): boolean {
  if (event === 'question_request') {
    const p = params as QuestionRequestParams;
    board.requestReceived(runId, p?.requestId, p?.request);
    return true;
  }
  if (event === 'question_resolved') {
    const p = params as QuestionResolvedParams;
    board.resolvedEvent(p?.requestId, p?.reason, p?.answer);
    return true;
  }
  return false;
}

/** The tap flow: optimistic resolve, then run.answerQuestion against the request's own run; a rejection rolls back via chooseFailed. */
export async function answerToQuestion(conn: RelayConnection, board: QuestionBoard, requestId: string, answer: QuestionAnswer): Promise<void> {
  const req = board.list().find((r) => r.requestId === requestId);
  if (!req || req.status !== 'pending') return;
  board.choose(requestId, answer);
  try {
    await answerQuestion(conn, req.runId, requestId, answer);
  } catch (e) {
    board.chooseFailed(requestId, e instanceof Error ? e.message : String(e));
  }
}
