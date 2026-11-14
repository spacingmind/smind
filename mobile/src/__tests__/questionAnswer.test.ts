// questionAnswer.test.ts: the pure draft and payload rules of the mobile question card (ADR-0055 section 7: never
// preselect, never guess; the daemon's validation stays the authority).

import { describe, expect, it } from 'vitest';
import {
  buildAnswers,
  declinePayload,
  Draft,
  EMPTY_DRAFT,
  isAnswered,
  MAX_ANSWER_CHARS,
  MAX_NOTE_CHARS,
  Question,
  setFreeText,
  setOtherText,
  submitBlockedReason,
  submitPayload,
  toggleOption,
} from '../questionAnswer';

const single: Question = { id: 'q0', text: 'Target?', kind: 'single', options: [{ id: 'a', label: 'A' }, { id: 'b', label: 'B' }], allowOther: true };
const multi: Question = { id: 'q1', text: 'Which?', kind: 'multi', options: [{ id: 'a', label: 'A' }, { id: 'b', label: 'B' }], allowOther: true };
const text: Question = { id: 'q2', text: 'Name it', kind: 'text' };
const noOther: Question = { id: 'q3', text: 'Pick', kind: 'single', options: [{ id: 'a', label: 'A' }] };

describe('nothing is preselected', () => {
  it('an empty draft answers no question, so Submit is blocked with a reason', () => {
    for (const q of [single, multi, text, noOther]) expect(isAnswered(q, EMPTY_DRAFT)).toBe(false);
    expect(submitBlockedReason([single], {})).toBe('Choose or type an answer to submit.');
    expect(submitPayload([single], {})).toBeNull();
  });

  it('names how many questions are still open', () => {
    expect(submitBlockedReason([single, multi, text], {})).toBe('Answer 3 more questions to submit.');
    expect(submitBlockedReason([single, multi], { q0: { ...EMPTY_DRAFT, optionIds: ['a'] } })).toBe('Answer the last question to submit.');
  });

  it('is unblocked only when every question has an answer', () => {
    const drafts: Record<string, Draft> = { q0: { ...EMPTY_DRAFT, optionIds: ['b'] }, q2: { ...EMPTY_DRAFT, text: 'x' } };
    expect(submitBlockedReason([single, text], drafts)).toBeNull();
  });

  it('whitespace is not an answer', () => {
    expect(isAnswered(text, { ...EMPTY_DRAFT, text: '   \n' })).toBe(false);
    expect(isAnswered(single, { ...EMPTY_DRAFT, otherText: '  ' })).toBe(false);
  });

  it('Other text only counts when the question allows it', () => {
    expect(isAnswered(noOther, { ...EMPTY_DRAFT, otherText: 'zzz' })).toBe(false);
  });
});

describe('choosing', () => {
  it('a single question replaces its choice and clears Other', () => {
    let d = setOtherText(single, EMPTY_DRAFT, 'something else');
    d = toggleOption(single, d, 'a');
    expect(d).toMatchObject({ optionIds: ['a'], otherText: '' });
    d = toggleOption(single, d, 'b');
    expect(d.optionIds).toEqual(['b']);
  });

  it('typing Other on a single question clears the choice; a multi keeps its choices', () => {
    expect(setOtherText(single, { ...EMPTY_DRAFT, optionIds: ['a'] }, 'x').optionIds).toEqual([]);
    expect(setOtherText(multi, { ...EMPTY_DRAFT, optionIds: ['a'] }, 'x').optionIds).toEqual(['a']);
  });

  it('clearing the Other text on a single question keeps the choice', () => {
    expect(setOtherText(single, { ...EMPTY_DRAFT, optionIds: ['a'], otherText: 'x' }, '').optionIds).toEqual(['a']);
  });

  it('a multi question toggles each option on and off', () => {
    let d = toggleOption(multi, EMPTY_DRAFT, 'a');
    d = toggleOption(multi, d, 'b');
    expect(d.optionIds).toEqual(['a', 'b']);
    expect(toggleOption(multi, d, 'a').optionIds).toEqual(['b']);
  });

  it('free text and Other are cut at the daemon cap in characters, not UTF-16 units', () => {
    const emoji = '😀'.repeat(MAX_ANSWER_CHARS + 5);
    expect(Array.from(setFreeText(EMPTY_DRAFT, emoji).text)).toHaveLength(MAX_ANSWER_CHARS);
    expect(Array.from(setOtherText(single, EMPTY_DRAFT, emoji).otherText)).toHaveLength(MAX_ANSWER_CHARS);
  });
});

describe('the Submit payload', () => {
  it('carries one item per question, option ids and trimmed text, empty fields omitted', () => {
    const drafts: Record<string, Draft> = {
      q0: { ...EMPTY_DRAFT, otherText: '  prod-eu  ' },
      q1: { ...EMPTY_DRAFT, optionIds: ['a', 'b'] },
      q2: { ...EMPTY_DRAFT, text: ' hello ' },
    };
    expect(submitPayload([single, multi, text], drafts)).toEqual({
      action: 'answer',
      answers: [{ questionId: 'q0', text: 'prod-eu' }, { questionId: 'q1', optionIds: ['a', 'b'] }, { questionId: 'q2', text: 'hello' }],
    });
  });

  it('never sends Other text for a question that does not allow it, nor option ids for a text question', () => {
    const drafts: Record<string, Draft> = { q3: { ...EMPTY_DRAFT, optionIds: ['a'], otherText: 'sneaky' }, q2: { optionIds: ['a'], otherText: 'x', text: 'ok' } };
    expect(buildAnswers([noOther, text], drafts)).toEqual([{ questionId: 'q3', optionIds: ['a'] }, { questionId: 'q2', text: 'ok' }]);
  });
});

describe('the Decline payload', () => {
  it('has no note key for a blank note', () => {
    expect(declinePayload('')).toEqual({ action: 'decline' });
    expect(declinePayload('  \n ')).toEqual({ action: 'decline' });
  });

  it('trims the note and cuts it at the daemon cap in characters', () => {
    expect(declinePayload('  not now ')).toEqual({ action: 'decline', note: 'not now' });
    const note = declinePayload('😀'.repeat(MAX_NOTE_CHARS + 3)).note ?? '';
    expect(Array.from(note)).toHaveLength(MAX_NOTE_CHARS);
  });
});
