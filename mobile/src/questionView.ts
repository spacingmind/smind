// questionView.ts is the pure view model of the mobile question card and its resolved marker (ADR-0055): the collapse
// rule for a long ask and the words for every way a question ends. No react-native import.

import type { Question, QuestionAnswer, QuestionRequest } from './questionAnswer';

/** A question over this many characters, or lines, collapses behind "Show all" (the web card's rule, so the two agree). */
export const LONG_ASK_CHARS = 480;
export const LONG_ASK_LINES = 8;

export function isLongAsk(text: string): boolean {
  return text.length > LONG_ASK_CHARS || text.split('\n').length > LONG_ASK_LINES;
}

/** The lines kept while a long ask is collapsed. */
export const COLLAPSED_LINES = 8;

/** The resolution reasons the daemon writes (taskrunner.QuestionResolution), each with the phrase the marker row shows. */
const RESOLUTION_WORDS: Record<string, string> = {
  human: 'Answered',
  declined: 'Declined',
  timeout: 'No one answered in time; the agent was told',
  refused_secret: 'Refused: an agent asked for a secret, nothing was stored',
  refused_unsupported: 'Refused: the form could not be shown',
  repeat: 'Declined: the agent asked the same thing again',
  profile_decline: 'Declined: this profile does not take questions',
  no_human: 'Declined: no one is attached to this run',
  provider_cancellation: 'Cancelled by the agent',
  run_ended: 'The run ended before an answer',
  daemon_restart: 'The daemon restarted before an answer',
};

/** How a resolution reads; an unknown reason still renders, as its raw word. */
export function resolutionWords(reason: string | undefined): string {
  if (!reason) return 'Closed';
  return RESOLUTION_WORDS[reason] ?? reason;
}

/** The reasons whose marker is a warning rather than plain information (an outcome that needs a human's attention). */
export function resolutionNeedsAttention(reason: string | undefined): boolean {
  return reason === 'timeout' || reason === 'refused_secret' || reason === 'daemon_restart' || reason === 'run_ended';
}

/** At most max characters (not UTF-16 units: a surrogate pair is never split), ending in an ellipsis when cut. */
function cap(text: string, max: number): string {
  const chars = Array.from(text);
  return chars.length > max ? `${chars.slice(0, max - 1).join('')}…` : text;
}

/** The one-line trace of what was asked: the first question's text, capped at 120 characters. */
export function questionSummary(request: QuestionRequest | undefined): string {
  const text = request?.questions?.[0]?.text?.trim();
  if (!text) return 'Agent asked a question';
  return cap(text, 120);
}

function labelsOf(question: Question, optionIds: readonly string[]): string[] {
  const byId = new Map((question.options ?? []).map((o) => [o.id, o.label]));
  return optionIds.map((id) => byId.get(id) ?? id);
}

/** What the human answered, for the marker: the chosen labels and typed text of each question, joined; capped. Empty for a decline. */
export function answerSummary(request: QuestionRequest | undefined, answer: QuestionAnswer | undefined): string {
  if (!request || !answer || answer.action !== 'answer') return '';
  const parts: string[] = [];
  for (const item of answer.answers ?? []) {
    const question = request.questions.find((q) => q.id === item.questionId);
    if (!question) continue;
    const bits = [...labelsOf(question, item.optionIds ?? []), ...(item.text ? [item.text] : [])];
    if (bits.length > 0) parts.push(bits.join(', '));
  }
  const joined = parts.join(' · ');
  return cap(joined, 160);
}
