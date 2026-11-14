// questionAnswer.ts is the pure answer logic of the mobile question card (ADR-0055): what a draft is, when it may be
// submitted, and the run.answerQuestion payload it becomes. No react-native import, so vitest loads it as is.
// The rules mirror the web card (components/permission/question-card.tsx) and the daemon's validation
// (taskrunner.ValidateQuestionAnswer, which stays the authority): nothing is ever preselected, an "Other" text and a
// single choice exclude each other, empty fields are omitted, a decline note is trimmed and cut at the daemon's cap.

import { clampRunes } from './permissionRequests';

/** One selectable choice of a "single"/"multi" question (the daemon's wire shape). */
export interface QuestionOption {
  id: string;
  label: string;
  description?: string;
}

export interface Question {
  id: string;
  header?: string;
  text: string;
  kind: 'single' | 'multi' | 'text';
  options?: QuestionOption[];
  allowOther?: boolean;
}

export interface QuestionRequest {
  source: string;
  questions: Question[];
  truncated?: boolean;
}

export interface QuestionAnswerItem {
  questionId: string;
  optionIds?: string[];
  text?: string;
}

export interface QuestionAnswer {
  action: 'answer' | 'decline';
  answers?: QuestionAnswerItem[];
  note?: string;
}

/** The daemon's caps (ADR-0055 section 1), in runes (characters), enforced by the clamp helpers; inputs carry no UTF-16 maxLength of their own. */
export const MAX_ANSWER_CHARS = 2000;
export const MAX_NOTE_CHARS = 500;

/** One question's in-progress answer. */
export interface Draft {
  optionIds: string[];
  otherText: string;
  text: string;
}

export const EMPTY_DRAFT: Draft = { optionIds: [], otherText: '', text: '' };

export function draftOf(drafts: Record<string, Draft>, questionId: string): Draft {
  return drafts[questionId] ?? EMPTY_DRAFT;
}

/** Whether a question has a submittable answer: a chosen option or a non-blank text, never a guess. */
export function isAnswered(question: Question, draft: Draft): boolean {
  if (question.kind === 'text') return draft.text.trim() !== '';
  return draft.optionIds.length > 0 || (question.allowOther === true && draft.otherText.trim() !== '');
}

/** How many questions still lack an answer. */
export function unansweredCount(questions: readonly Question[], drafts: Record<string, Draft>): number {
  return questions.filter((q) => !isAnswered(q, draftOf(drafts, q.id))).length;
}

/** Why Submit is disabled (shown under the form), or null when it may be tapped. */
export function submitBlockedReason(questions: readonly Question[], drafts: Record<string, Draft>): string | null {
  const n = unansweredCount(questions, drafts);
  if (n === 0) return null;
  return questions.length === 1 ? 'Choose or type an answer to submit.' : n === 1 ? 'Answer the last question to submit.' : `Answer ${n} more questions to submit.`;
}

/** A tap on an option: a single question replaces the choice and clears any Other text; a multi toggles. */
export function toggleOption(question: Question, draft: Draft, optionId: string): Draft {
  if (question.kind === 'single') return { ...draft, optionIds: [optionId], otherText: '' };
  const selected = draft.optionIds.includes(optionId);
  return { ...draft, optionIds: selected ? draft.optionIds.filter((id) => id !== optionId) : [...draft.optionIds, optionId] };
}

/** Typing an Other answer deselects a single question's choice; a multi keeps its choices (Other is one more). */
export function setOtherText(question: Question, draft: Draft, text: string): Draft {
  const otherText = clampRunes(text, MAX_ANSWER_CHARS);
  return { ...draft, otherText, optionIds: question.kind === 'single' && otherText !== '' ? [] : draft.optionIds };
}

export function setFreeText(draft: Draft, text: string): Draft {
  return { ...draft, text: clampRunes(text, MAX_ANSWER_CHARS) };
}

/** The answers array a Submit sends: one item per question, empty fields omitted. */
export function buildAnswers(questions: readonly Question[], drafts: Record<string, Draft>): QuestionAnswerItem[] {
  return questions.map((question) => {
    const draft = draftOf(drafts, question.id);
    const item: QuestionAnswerItem = { questionId: question.id };
    if (question.kind !== 'text' && draft.optionIds.length > 0) item.optionIds = draft.optionIds;
    const text = (question.kind === 'text' ? draft.text : question.allowOther ? draft.otherText : '').trim();
    if (text !== '') item.text = text;
    return item;
  });
}

/** The Submit payload, or null while Submit is blocked. */
export function submitPayload(questions: readonly Question[], drafts: Record<string, Draft>): QuestionAnswer | null {
  return submitBlockedReason(questions, drafts) === null ? { action: 'answer', answers: buildAnswers(questions, drafts) } : null;
}

/** The Decline payload: the trimmed note cut at the daemon's cap; no `note` key at all when blank. */
export function declinePayload(note: string): QuestionAnswer {
  const trimmed = note.trim();
  return trimmed === '' ? { action: 'decline' } : { action: 'decline', note: clampRunes(trimmed, MAX_NOTE_CHARS) };
}
