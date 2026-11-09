import { describe, expect, it } from "vitest";

import { FAILURE_FALLBACK_SENTENCE, FAILURE_SHORT_LABEL, failureSentence, failureShortLabel } from "@/lib/failure-text";

describe("FAILURE_FALLBACK_SENTENCE", () => {
  it("has one plain sentence for each of the nine classes", () => {
    expect(Object.keys(FAILURE_FALLBACK_SENTENCE).sort()).toEqual(
      [
        "plan_window_exhausted",
        "transient_429",
        "rate_limited",
        "auth_expired",
        "model_not_in_plan",
        "context_window_exceeded",
        "session_budget_exceeded",
        "too_many_denials",
        "cli_exited",
      ].sort(),
    );
    for (const sentence of Object.values(FAILURE_FALLBACK_SENTENCE)) {
      expect(sentence).toMatch(/^[A-Z].*\.$/);
      // A sentence says what happened, never what to approve, allow or merge (AC7).
      expect(sentence).not.toMatch(/merge|approve|always[ _-]?allow/i);
    }
  });

  it("reads as the daemon's own words (Describe table)", () => {
    expect(FAILURE_FALLBACK_SENTENCE.context_window_exceeded).toBe("The conversation no longer fits the model's context window.");
    expect(FAILURE_FALLBACK_SENTENCE.session_budget_exceeded).toBe("The provider's session budget for this run is used up.");
    expect(FAILURE_FALLBACK_SENTENCE.too_many_denials).toBe("The agent kept asking for actions that were denied, so the provider stopped the run.");
    expect(FAILURE_FALLBACK_SENTENCE.cli_exited).toBe(
      "The agent stopped before finishing; its session is saved, so you can continue where it left off.",
    );
  });
});

describe("failureSentence", () => {
  it("prefers the daemon's summary when it is non-empty", () => {
    expect(failureSentence("cli_exited", { summary: "The agent stopped before finishing." })).toBe("The agent stopped before finishing.");
    expect(failureSentence("martian", { summary: "Something else entirely." })).toBe("Something else entirely.");
  });

  it("falls back to the class sentence when the summary is absent or empty (an older daemon)", () => {
    expect(failureSentence("context_window_exceeded")).toBe("The conversation no longer fits the model's context window.");
    expect(failureSentence("too_many_denials", {})).toBe("The agent kept asking for actions that were denied, so the provider stopped the run.");
    expect(failureSentence("rate_limited", { summary: "" })).toBe("The provider limited how fast requests can be sent.");
    expect(failureSentence("session_budget_exceeded", null)).toBe("The provider's session budget for this run is used up.");
  });

  it("is empty for an unknown kind and for no failure at all", () => {
    expect(failureSentence("martian")).toBe("");
    expect(failureSentence(undefined)).toBe("");
    expect(failureSentence("")).toBe("");
    expect(failureSentence(null)).toBe("");
  });
});

describe("FAILURE_SHORT_LABEL", () => {
  it("a short label for each human-action class the footer names", () => {
    expect(FAILURE_SHORT_LABEL).toEqual({
      context_window_exceeded: "Context window full",
      session_budget_exceeded: "Session budget used up",
      too_many_denials: "Stopped after too many denials",
    });
    for (const label of Object.values(FAILURE_SHORT_LABEL)) {
      expect(label).toMatch(/^[A-Z]/);
      expect(label).not.toMatch(/\.$/);
      // A label says what happened, never what to approve, allow or merge (AC7).
      expect(label).not.toMatch(/merge|approve|always[ _-]?allow/i);
    }
  });
});

describe("failureShortLabel", () => {
  it("is the class's short label, empty for any other kind or no failure at all", () => {
    expect(failureShortLabel("context_window_exceeded")).toBe("Context window full");
    expect(failureShortLabel("session_budget_exceeded")).toBe("Session budget used up");
    expect(failureShortLabel("too_many_denials")).toBe("Stopped after too many denials");
    expect(failureShortLabel("auth_expired")).toBe("");
    expect(failureShortLabel("martian")).toBe("");
    expect(failureShortLabel(undefined)).toBe("");
    expect(failureShortLabel(null)).toBe("");
  });
});
