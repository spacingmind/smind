// questionRequests.test.ts: the board and the tap flow of the mobile question card (ADR-0055), against a scripted fake
// connection so the exact run.answerQuestion params are asserted on the recorded wire traffic.

import { describe, expect, it } from 'vitest';
import { answerQuestion } from '../api';
import { Draft, EMPTY_DRAFT, QuestionAnswer, QuestionRequest, submitPayload, toggleOption } from '../questionAnswer';
import { answerToQuestion, feedQuestionEvent, QuestionBoard, showsCard } from '../questionRequests';
import { RelayConnection } from '../relay/RelayConnection';
import { PairingOffer } from '../relay/pairing';

const dummyOffer: PairingOffer = { daemonId: '', publicKey: new Uint8Array(), relay: '', relayFingerprint: '', secret: new Uint8Array(), workspaceId: '' };

function fakeConnection(handler: (method: string, params: unknown) => unknown): { conn: RelayConnection; calls: Array<[string, unknown]> } {
  const calls: Array<[string, unknown]> = [];
  const conn = new RelayConnection(dummyOffer, { close: () => {} }, {} as never);
  (conn as unknown as { call: (m: string, p?: unknown) => unknown }).call = (method: string, params?: unknown) => {
    calls.push([method, params]);
    return Promise.resolve().then(() => handler(method, params));
  };
  return { conn, calls };
}

const REQ: QuestionRequest = {
  source: 'claude.ask_user_question',
  questions: [{ id: 'q0', text: 'Which target?', kind: 'single', options: [{ id: 'o0', label: 'staging' }, { id: 'o1', label: 'prod' }], allowOther: true }],
};
const ANSWER: QuestionAnswer = { action: 'answer', answers: [{ questionId: 'q0', optionIds: ['o0'] }] };

describe('QuestionBoard', () => {
  it('records a request pending and ignores a duplicate (the attach backfill)', () => {
    const board = new QuestionBoard();
    board.requestReceived('run-1', 'rq-1', REQ);
    board.requestReceived('run-1', 'rq-1', REQ);
    expect(board.list()).toHaveLength(1);
    expect(board.list()[0]).toMatchObject({ runId: 'run-1', requestId: 'rq-1', status: 'pending', error: null });
  });

  it('ignores a request with no id or no questions array', () => {
    const board = new QuestionBoard();
    board.requestReceived('run-1', '', REQ);
    board.requestReceived('run-1', 'rq-2', undefined);
    board.requestReceived('run-1', 'rq-3', {} as QuestionRequest);
    expect(board.list()).toEqual([]);
  });

  it('a history request+resolved pair lands resolved, never interactive (logs shape: question / questionAnswer)', () => {
    const board = new QuestionBoard();
    board.applyLogEvents('run-1', [
      { type: 'text' },
      { type: 'question_request', requestId: 'rq-1', question: REQ },
      { type: 'question_resolved', requestId: 'rq-1', reason: 'human', questionAnswer: ANSWER },
    ]);
    expect(board.list()[0]).toMatchObject({ status: 'resolved', resolution: { reason: 'human', by: 'event', answer: ANSWER } });
  });

  it('a live resolved event is authoritative over a tap', () => {
    const board = new QuestionBoard();
    board.requestReceived('run-1', 'rq-1', REQ);
    board.choose('rq-1', ANSWER);
    expect(board.list()[0].resolution).toMatchObject({ by: 'tap', reason: 'human' });
    board.resolvedEvent('run-1', 'rq-1', 'timeout', undefined);
    expect(board.list()[0].resolution).toEqual({ reason: 'timeout', by: 'event' });
  });

  it('a decline tap resolves as declined', () => {
    const board = new QuestionBoard();
    board.requestReceived('run-1', 'rq-1', REQ);
    board.choose('rq-1', { action: 'decline', note: 'no' });
    expect(board.list()[0].resolution).toMatchObject({ reason: 'declined', by: 'tap' });
  });

  it('a second tap after resolution changes nothing', () => {
    const board = new QuestionBoard();
    board.requestReceived('run-1', 'rq-1', REQ);
    board.resolvedEvent('run-1', 'rq-1', 'declined', undefined);
    board.choose('rq-1', ANSWER);
    expect(board.list()[0].resolution?.by).toBe('event');
  });

  it('a failed answer rolls the tap back and keeps the card interactive with the error', () => {
    const board = new QuestionBoard();
    board.requestReceived('run-1', 'rq-1', REQ);
    board.choose('rq-1', ANSWER);
    board.chooseFailed('rq-1', 'unknown option');
    expect(board.list()[0]).toMatchObject({ status: 'pending', error: 'unknown option' });
    expect(board.list()[0].resolution).toBeUndefined();
  });

  it('question-keeps-answers-after-failed-submit', () => {
    const board = new QuestionBoard();
    board.requestReceived('run-1', 'rq-1', REQ);
    // The card's drafts are local state the board never touches: the pure helpers build the same payload before and
    // after the failure.
    const drafts: Record<string, Draft> = { q0: toggleOption(REQ.questions[0], EMPTY_DRAFT, 'o1') };
    const payload = submitPayload(REQ.questions, drafts);
    expect(payload).toEqual({ action: 'answer', answers: [{ questionId: 'q0', optionIds: ['o1'] }] });
    board.choose('rq-1', payload!);
    board.chooseFailed('rq-1', 'connection closed');
    expect(board.list()[0]).toMatchObject({ status: 'pending', error: 'connection closed' });
    expect(showsCard(board.list()[0])).toBe(true);
    expect(submitPayload(REQ.questions, drafts)).toEqual(payload);
  });

  it('a failed answer does NOT roll back after an authoritative event resolved the request', () => {
    const board = new QuestionBoard();
    board.requestReceived('run-1', 'rq-1', REQ);
    board.choose('rq-1', ANSWER);
    board.resolvedEvent('run-1', 'rq-1', 'timeout', undefined);
    board.chooseFailed('rq-1', 'already resolved');
    expect(board.list()[0]).toMatchObject({ status: 'resolved', error: null, resolution: { reason: 'timeout', by: 'event' } });
  });

  it('an unknown resolution reason is kept as its raw word', () => {
    const board = new QuestionBoard();
    board.requestReceived('run-1', 'rq-1', REQ);
    board.resolvedEvent('run-1', 'rq-1', 'brand_new_reason', undefined);
    expect(board.list()[0].resolution?.reason).toBe('brand_new_reason');
  });
});

describe('resolutions without (or before) a request, and the card lifetime', () => {
  it('a refusal has no request event: the resolution alone becomes a closed marker, not dropped', () => {
    const board = new QuestionBoard();
    board.resolvedEvent('run-1', 'rq-9', 'refused_secret', undefined);
    expect(board.list()).toHaveLength(1);
    expect(board.list()[0]).toMatchObject({ runId: 'run-1', requestId: 'rq-9', status: 'resolved', resolution: { reason: 'refused_secret', by: 'event' } });
    expect(showsCard(board.list()[0])).toBe(false);
  });

  it('a resolution that raced ahead of its request keeps the request and stays resolved (never a live card)', () => {
    const board = new QuestionBoard();
    board.resolvedEvent('run-1', 'rq-1', 'timeout', undefined);
    board.requestReceived('run-1', 'rq-1', REQ);
    expect(board.list()).toHaveLength(1);
    expect(board.list()[0]).toMatchObject({ status: 'resolved', request: REQ });
    expect(showsCard(board.list()[0])).toBe(false);
  });

  it('the card stays mounted while this device answer is in flight, and after a rollback (drafts survive); only an event closes it', () => {
    const board = new QuestionBoard();
    board.requestReceived('run-1', 'rq-1', REQ);
    expect(showsCard(board.list()[0])).toBe(true);
    board.choose('rq-1', ANSWER);
    expect(showsCard(board.list()[0])).toBe(true);
    board.chooseFailed('rq-1', 'network');
    expect(showsCard(board.list()[0])).toBe(true);
    board.choose('rq-1', ANSWER);
    board.resolvedEvent('run-1', 'rq-1', 'human', ANSWER);
    expect(showsCard(board.list()[0])).toBe(false);
  });

  it('clear() forgets everything, so a reload re-feeds the board and a still-pending question reappears', () => {
    const board = new QuestionBoard();
    let changes = 0;
    board.requestReceived('run-1', 'rq-1', REQ);
    board.onChange = () => changes++;
    board.clear();
    expect(board.list()).toEqual([]);
    board.requestReceived('run-1', 'rq-1', REQ);
    expect(changes).toBe(1);
    expect(board.list()[0].status).toBe('pending');
  });
});

describe('feedQuestionEvent', () => {
  it('routes the live attach events (params: request / answer) and claims them', () => {
    const board = new QuestionBoard();
    expect(feedQuestionEvent(board, 'run-1', 'question_request', { requestId: 'rq-1', request: REQ })).toBe(true);
    expect(feedQuestionEvent(board, 'run-1', 'question_resolved', { requestId: 'rq-1', reason: 'human', answer: ANSWER })).toBe(true);
    expect(board.list()[0]).toMatchObject({ status: 'resolved', resolution: { reason: 'human', answer: ANSWER } });
  });

  it('does not claim other events', () => {
    const board = new QuestionBoard();
    expect(feedQuestionEvent(board, 'run-1', 'chunk', { text: 'x' })).toBe(false);
    expect(feedQuestionEvent(board, 'run-1', 'permission_request', {})).toBe(false);
  });

  it('survives a malformed params object', () => {
    const board = new QuestionBoard();
    expect(feedQuestionEvent(board, 'run-1', 'question_request', null)).toBe(true);
    expect(feedQuestionEvent(board, 'run-1', 'question_resolved', undefined)).toBe(true);
    expect(board.list()).toEqual([]);
  });
});

describe('answerToQuestion (tap flow, fake connection)', () => {
  it('sends run.answerQuestion with the answer on the request own run and resolves optimistically', async () => {
    const { conn, calls } = fakeConnection(() => ({}));
    const board = new QuestionBoard();
    board.requestReceived('run-9', 'rq-1', REQ);
    await answerToQuestion(conn, board, 'rq-1', ANSWER);
    expect(calls).toEqual([['run.answerQuestion', { runId: 'run-9', requestId: 'rq-1', action: 'answer', answers: [{ questionId: 'q0', optionIds: ['o0'] }] }]]);
    expect(board.list()[0]).toMatchObject({ status: 'resolved', resolution: { by: 'tap' } });
  });

  it('a decline goes out with the note and no answers key', async () => {
    const { conn, calls } = fakeConnection(() => ({}));
    await answerQuestion(conn, 'run-9', 'rq-1', { action: 'decline', note: 'later' });
    expect(calls).toEqual([['run.answerQuestion', { runId: 'run-9', requestId: 'rq-1', action: 'decline', note: 'later' }]]);
  });

  it('a decline with no note carries no note key at all', async () => {
    const { conn, calls } = fakeConnection(() => ({}));
    await answerQuestion(conn, 'run-9', 'rq-1', { action: 'decline' });
    expect(calls[0][1]).toEqual({ runId: 'run-9', requestId: 'rq-1', action: 'decline' });
  });

  it('a rejected RPC rolls back and shows the daemon message', async () => {
    const { conn } = fakeConnection(() => {
      throw new Error('answer names an unknown option');
    });
    const board = new QuestionBoard();
    board.requestReceived('run-9', 'rq-1', REQ);
    await answerToQuestion(conn, board, 'rq-1', ANSWER);
    expect(board.list()[0]).toMatchObject({ status: 'pending', error: 'answer names an unknown option' });
  });

  it('does nothing for an already resolved or unknown request', async () => {
    const { conn, calls } = fakeConnection(() => ({}));
    const board = new QuestionBoard();
    board.requestReceived('run-9', 'rq-1', REQ);
    board.resolvedEvent('run-1', 'rq-1', 'timeout', undefined);
    await answerToQuestion(conn, board, 'rq-1', ANSWER);
    await answerToQuestion(conn, board, 'nope', ANSWER);
    expect(calls).toEqual([]);
  });
});
