/**
 * The words of the agent-question card that web and mobile must share (ADR-0055). The mobile app
 * keeps an identical copy in mobile/src/questionCopy.ts and a test there reads THIS file, so the two
 * cannot drift. The note is the human's reason for a decline: the daemon sends it to the agent only
 * with a decline, never with an answer.
 */
export const QUESTION_NOTE_PLACEHOLDER = "Note for the agent (optional)";
export const QUESTION_NOTE_HELPER = "Sent only if you decline.";
export const QUESTION_NOTE_LABEL = "Note for the agent, sent only if you decline (optional)";

/** One daemon refusal of an answer mapped to words a person can act on; the raw text stays available as details. */
const KNOWN_REFUSALS: Array<[RegExp, string]> = [
  [/unknown option/i, "That choice is no longer offered. Pick again."],
  [/answered twice|chosen twice/i, "Each question can only be answered once. Pick again."],
  [/need an answer for each|is not answered/i, "Answer every question to submit."],
  [/needs exactly one choice/i, "Choose one answer for each question."],
  [/needs at least one choice/i, "Choose at least one answer."],
  [/does not take free text/i, "That question does not take typed text."],
  [/text is longer than/i, "That answer is too long. Shorten it."],
  [/note is longer than/i, "The note is too long. Shorten it."],
  [/needs a whole number|needs a number|does not match the required format/i, "That answer is not in the format the agent asked for."],
  [/already resolved|not found|ErrNotFound/i, "This question was already answered or has ended."],
];

/** The human line for a refused answer, plus the daemon's own text for a Details disclosure. */
export function humanizeQuestionError(raw: string): { text: string; details: string } {
  for (const [re, text] of KNOWN_REFUSALS) if (re.test(raw)) return { text, details: raw };
  return { text: "Couldn't send the answer.", details: raw };
}
