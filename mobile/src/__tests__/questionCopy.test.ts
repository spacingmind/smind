// questionCopy.test.ts: the note words and the refusal mapping, identical to the web card's.

import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { QUESTION_NOTE_HELPER, QUESTION_NOTE_LABEL, QUESTION_NOTE_PLACEHOLDER, humanizeQuestionError } from '../questionCopy';

const web = readFileSync(join(__dirname, '..', '..', '..', 'web', 'packages', 'ui', 'src', 'lib', 'question-copy.ts'), 'utf8');
const constant = (name: string) => new RegExp(`export const ${name} = "((?:[^"\\\\]|\\\\.)*)";`).exec(web)?.[1];

describe('question copy matches the web', () => {
  it('the note placeholder, helper and label are the web strings', () => {
    expect(constant('QUESTION_NOTE_PLACEHOLDER')).toBe(QUESTION_NOTE_PLACEHOLDER);
    expect(constant('QUESTION_NOTE_HELPER')).toBe(QUESTION_NOTE_HELPER);
    expect(constant('QUESTION_NOTE_LABEL')).toBe(QUESTION_NOTE_LABEL);
  });

  it('every mapped refusal sentence appears in the web table (same words, same order of cases)', () => {
    const sentences = [...web.matchAll(/\/i, "((?:[^"\\]|\\.)*)"\],/g)].map((m) => m[1]);
    expect(sentences.length).toBeGreaterThanOrEqual(10);
    for (const raw of [
      'question "q0": unknown option "o9"',
      'question "q0" answered twice',
      'need an answer for each of 2 questions, got 1',
      'question "q0" needs exactly one choice or one free text',
      'question "q0" needs at least one choice',
      'question "q0" does not take free text',
      'question "q0": text is longer than 2000 characters',
      'note is longer than 500 characters',
      'question "q0": needs a whole number',
      'question request "x": already resolved',
    ]) {
      expect(sentences, raw).toContain(humanizeQuestionError(raw).text);
    }
  });
});

describe('humanizeQuestionError', () => {
  it('turns the daemon refusals into words a person can act on, keeping the raw text', () => {
    const raw = 'run.answerQuestion: runs: question request "r": question "q0": unknown option "o9"';
    expect(humanizeQuestionError(raw)).toEqual({ text: 'That choice is no longer offered. Pick again.', details: raw });
  });
  it('an unknown refusal still says it could not send, with the raw text as details', () => {
    expect(humanizeQuestionError('kaboom')).toEqual({ text: "Couldn't send the answer.", details: 'kaboom' });
  });
});
