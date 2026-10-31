import { describe, expect, it } from "vitest";
import { formatProviderReason } from "./provider-reason";

describe("formatProviderReason", () => {
  it("maps a missing executable to Not installed", () => {
    expect(
      formatProviderReason(
        'exec: "codex": executable file not found in $PATH; codex home: /Users/x/.codex [directory does not exist]',
      ),
    ).toBe("Not installed");
  });

  it("maps a missing directory to Not set up", () => {
    expect(formatProviderReason("codex home: /Users/x/.codex [directory does not exist]")).toBe("Not set up");
  });

  it("otherwise keeps the first sentence, capped at 80 chars", () => {
    expect(formatProviderReason("glm CLI not found on PATH. See docs.")).toBe("glm CLI not found on PATH");
    const long = "x".repeat(120);
    expect(formatProviderReason(long)).toBe(`${"x".repeat(79)}…`);
  });
});
