import { describe, expect, it } from "vitest";

import { describeProfile } from "@/lib/describe-profile";
import type { AgentProfile, ProviderInfo } from "@/lib/types";

const PROVIDERS: ProviderInfo[] = [
  {
    id: "claude-native",
    label: "Claude Code",
    models: [
      { id: "sonnet", label: "Sonnet" },
      { id: "opus", label: "Opus" },
    ],
    modes: [
      { id: "acceptEdits", label: "Accept File Edits" },
      { id: "default", label: "Always Ask" },
      { id: "plan", label: "Plan Mode" },
      { id: "bypassPermissions", label: "Bypass", autoApproves: true },
    ],
    defaultMode: "acceptEdits",
  },
  { id: "glm", label: "GLM" },
];

function profile(overrides: Partial<AgentProfile> = {}): AgentProfile {
  return {
    ID: 1,
    Name: "UI work",
    Provider: "claude-native",
    PermissionMode: "",
    AutoAccept: false,
    ThinkingLevel: "",
    Notes: "",
    CreatedAt: "2026-09-25T00:00:00Z",
    UpdatedAt: "2026-09-25T00:00:00Z",
    ...overrides,
  } as AgentProfile;
}

describe("describeProfile", () => {
  it("shows the provider label, resolved mode, and Standard thinking even when the profile leaves them default", () => {
    expect(describeProfile(profile(), PROVIDERS)).toBe("Claude Code · Accept File Edits · Standard");
  });

  it("includes a pinned model's label and the given mode/thinking labels", () => {
    expect(describeProfile(profile({ Model: "opus", PermissionMode: "plan", ThinkingLevel: "extended" }), PROVIDERS)).toBe(
      "Claude Code · Opus · Plan Mode · Extended",
    );
  });

  it("falls back to the raw model id when the catalog lacks it, and appends Auto-accept", () => {
    expect(describeProfile(profile({ Model: "mystery", AutoAccept: true }), PROVIDERS)).toBe(
      "Claude Code · mystery · Accept File Edits · Auto-accept · Standard",
    );
  });

  it("omits thinking for a non-Claude provider", () => {
    expect(describeProfile(profile({ Provider: "glm" }), PROVIDERS)).toBe("GLM · Default");
  });

  it("is a pure function of the profile and the provider list", () => {
    const defaults = profile();
    const pinned = profile({ Model: "sonnet", PermissionMode: "plan", ThinkingLevel: "off" });
    expect(describeProfile(defaults, PROVIDERS)).toBe("Claude Code · Accept File Edits · Standard");
    expect(describeProfile(pinned, PROVIDERS)).toBe("Claude Code · Sonnet · Plan Mode · Off");
    expect(describeProfile(pinned, PROVIDERS)).toBe(describeProfile(pinned, PROVIDERS));
  });
});
