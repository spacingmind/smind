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

  it("maps a missing account to No account", () => {
    expect(formatProviderReason('no account configured for "kimi"')).toBe("No account");
  });

  it("maps an outdated binary to Update needed", () => {
    expect(
      formatProviderReason(
        "found /Users/x/.local/bin/cursor-agent, version 1.0.0 is too old: needs >= 2.0.0 (update: cursor-agent update)",
      ),
    ).toBe("Update needed");
    expect(formatProviderReason('found 2 account(s) for "openai", but all credentials are expired')).not.toBe(
      "Update needed",
    );
  });

  it("otherwise keeps the first sentence, capped at 80 chars", () => {
    expect(formatProviderReason("glm CLI not found on PATH. See docs.")).toBe("glm CLI not found on PATH");
    const long = "x".repeat(120);
    expect(formatProviderReason(long)).toBe(`${"x".repeat(79)}…`);
  });
});
