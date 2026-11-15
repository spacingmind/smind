import { describe, expect, it } from "vitest";

import { asSentence } from "@/lib/as-sentence";

describe("asSentence", () => {
  it("capitalises a bare phrase and ends it with a period", () => {
    expect(asSentence("the notification could not be sent")).toBe("The notification could not be sent.");
  });
  it("leaves a finished sentence alone", () => {
    expect(asSentence("Wait a few seconds and try again.")).toBe("Wait a few seconds and try again.");
    expect(asSentence("Really?")).toBe("Really?");
  });
  it("keeps an empty message empty", () => {
    expect(asSentence("  ")).toBe("");
  });
});
