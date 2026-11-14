// questionView.test.ts: the collapse rule of a long ask and the words of every way a question ends (ADR-0055).

import { describe, expect, it } from 'vitest';
import { answerSummary, isLongAsk, LONG_ASK_CHARS, LONG_ASK_LINES, questionSummary, resolutionNeedsAttention, resolutionWords } from '../questionView';
import type { QuestionRequest } from '../questionAnswer';

describe('isLongAsk', () => {
  it('collapses past 480 characters or 8 lines, the web card rule', () => {
    expect(isLongAsk('x'.repeat(LONG_ASK_CHARS))).toBe(false);
    expect(isLongAsk('x'.repeat(LONG_ASK_CHARS + 1))).toBe(true);
    expect(isLongAsk(Array(LONG_ASK_LINES).fill('a').join('\n'))).toBe(false);
    expect(isLongAsk(Array(LONG_ASK_LINES + 1).fill('a').join('\n'))).toBe(true);
  });
});

describe('resolutionWords', () => {
  it('has words for every resolution the daemon writes', () => {
    const reasons = ['human', 'declined', 'timeout', 'refused_secret', 'refused_unsupported', 'repeat', 'profile_decline', 'no_human', 'provider_cancellation', 'run_ended', 'daemon_restart'];
    const seen = new Set<string>();
    for (const r of reasons) {
      const words = resolutionWords(r);
      expect(words, r).not.toBe(r);
      expect(words.length).toBeGreaterThan(3);
      seen.add(words);
    }
    expect(seen.size).toBe(reasons.length); // no two outcomes read the same
  });

  it('an unknown reason renders as its raw word and a missing one as Closed', () => {
    expect(resolutionWords('brand_new')).toBe('brand_new');
    expect(resolutionWords(undefined)).toBe('Closed');
    expect(resolutionWords('')).toBe('Closed');
  });

  it('says in words (not colour) that a refused secret stored nothing, and a timeout told the agent', () => {
    expect(resolutionWords('refused_secret')).toMatch(/nothing was stored/);
    expect(resolutionWords('timeout')).toMatch(/agent was told/);
    expect(resolutionNeedsAttention('timeout')).toBe(true);
    expect(resolutionNeedsAttention('human')).toBe(false);
  });
});

const REQ: QuestionRequest = {
  source: 'claude.ask_user_question',
  questions: [
    { id: 'q0', text: 'Which target?', kind: 'single', options: [{ id: 'o0', label: 'staging' }, { id: 'o1', label: 'prod' }], allowOther: true },
    { id: 'q1', text: 'Which flags?', kind: 'multi', options: [{ id: 'f0', label: '--fast' }, { id: 'f1', label: '--safe' }] },
  ],
};

describe('questionSummary / answerSummary', () => {
  it('summarises by the first question, capped', () => {
    expect(questionSummary(REQ)).toBe('Which target?');
    expect(questionSummary(undefined)).toBe('Agent asked a question');
    expect(questionSummary({ source: 's', questions: [{ id: 'q', text: 'x'.repeat(300), kind: 'text' }] }).length).toBe(120);
  });

  it('shows the chosen labels and typed text, never ids; empty for a decline or no answer', () => {
    const answer = { action: 'answer' as const, answers: [{ questionId: 'q0', text: 'eu-west' }, { questionId: 'q1', optionIds: ['f0', 'f1'] }] };
    expect(answerSummary(REQ, answer)).toBe('eu-west · --fast, --safe');
    expect(answerSummary(REQ, { action: 'decline', note: 'x' })).toBe('');
    expect(answerSummary(REQ, undefined)).toBe('');
    expect(answerSummary(undefined, answer)).toBe('');
  });

  it('an option id the request does not list is shown as given, a question it does not list is skipped', () => {
    expect(answerSummary(REQ, { action: 'answer', answers: [{ questionId: 'q0', optionIds: ['zz'] }, { questionId: 'nope', text: 'x' }] })).toBe('zz');
  });
});
