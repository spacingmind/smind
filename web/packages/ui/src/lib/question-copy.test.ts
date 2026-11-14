import { describe, expect, it } from "vitest";

import { humanizeQuestionError, QUESTION_NOTE_HELPER, QUESTION_NOTE_LABEL, QUESTION_NOTE_PLACEHOLDER } from "@/lib/question-copy";

describe("humanizeQuestionError", () => {
  it("maps the daemon's refusals to words a person can act on and keeps the raw text", () => {
    const raw = 'run.answerQuestion: runs: question request "r1": question "q0": unknown option "o9"';
    expect(humanizeQuestionError(raw)).toEqual({ text: "That choice is no longer offered. Pick again.", details: raw });
    expect(humanizeQuestionError("need an answer for each of 2 questions, got 1").text).toBe("Answer every question to submit.");
    expect(humanizeQuestionError('question "q0" needs exactly one choice or one free text').text).toBe("Choose one answer for each question.");
    expect(humanizeQuestionError('runs: question request "x": already resolved').text).toBe("This question was already answered or has ended.");
    expect(humanizeQuestionError('question request "x": not found').text).toBe("This question was already answered or has ended.");
    expect(humanizeQuestionError("note is longer than 500 characters").text).toBe("The note is too long. Shorten it.");
  });

  it("an unknown refusal says it could not send and keeps the raw text as details", () => {
    expect(humanizeQuestionError("kaboom")).toEqual({ text: "Couldn't send the answer.", details: "kaboom" });
  });

  it("the note copy is the one the mobile card uses (it states the daemon's behaviour: a note goes only with a decline)", () => {
    expect(QUESTION_NOTE_PLACEHOLDER).toBe("Note for the agent (optional)");
    expect(QUESTION_NOTE_HELPER).toBe("Sent only if you decline.");
    expect(QUESTION_NOTE_LABEL).toContain("sent only if you decline");
  });
});
