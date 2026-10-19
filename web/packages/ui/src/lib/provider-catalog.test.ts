import { describe, expect, it } from "vitest";

import type { ProviderInfo } from "@/lib/types";

import { accountProviderLabel, providerIdentity, providerLabel, providerShortName } from "@/lib/provider-catalog";

const SERVED: ProviderInfo[] = [
  { id: "claude-native", label: "Claude (daemon)", accountProvider: "anthropic" },
  { id: "kimi", label: "Kimi K2", accountProvider: "kimi" },
];

describe("providerIdentity", () => {
  it("matches the runtime vocabulary, case-insensitively", () => {
    expect(providerIdentity("claude-native")?.key).toBe("claude");
    expect(providerIdentity("Codex-Native")?.key).toBe("codex");
    expect(providerIdentity("GLM")?.key).toBe("glm");
  });

  it("matches the account vocabulary and the bare family words", () => {
    expect(providerIdentity("anthropic")?.key).toBe("claude");
    expect(providerIdentity("openai")?.key).toBe("codex");
    expect(providerIdentity("kimi")?.accountProvider).toBe("kimi");
    expect(providerIdentity("claude")?.label).toBe("Claude Code");
    expect(providerIdentity("codex")?.label).toBe("Codex");
  });

  it("is null-safe and returns undefined for unknown ids", () => {
    expect(providerIdentity(null)).toBeUndefined();
    expect(providerIdentity(undefined)).toBeUndefined();
    expect(providerIdentity("xai")).toBeUndefined();
  });
});

describe("providerLabel maps both id vocabularies", () => {
  it("resolves runtime ids through the identity table", () => {
    expect(providerLabel("claude-native")).toBe("Claude Code");
    expect(providerLabel("codex-native")).toBe("Codex");
    expect(providerLabel("glm")).toBe("GLM");
    expect(providerLabel("kimi")).toBe("Kimi");
  });

  it("resolves account ids through the identity table", () => {
    expect(providerLabel("anthropic")).toBe("Claude Code");
    expect(providerLabel("openai")).toBe("Codex");
  });

  it("prefers the daemon's own label when provider.list has one", () => {
    expect(providerLabel("claude-native", SERVED)).toBe("Claude (daemon)");
    expect(providerLabel("kimi", SERVED)).toBe("Kimi K2");
    expect(providerLabel("glm", SERVED)).toBe("GLM");
  });

  it("falls back to the raw id for unknown providers", () => {
    expect(providerLabel("xai")).toBe("xai");
  });
});

describe("accountProviderLabel", () => {
  it("maps account ids both with and without the daemon's list", () => {
    expect(accountProviderLabel("anthropic")).toBe("Claude Code");
    expect(accountProviderLabel("openai")).toBe("Codex");
    expect(accountProviderLabel("anthropic", SERVED)).toBe("Claude (daemon)");
    expect(accountProviderLabel("kimi", SERVED)).toBe("Kimi K2");
  });

  it("falls back to the raw id", () => {
    expect(accountProviderLabel("antigravity")).toBe("antigravity");
  });
});

describe("providerShortName", () => {
  it("short-names both vocabularies and falls back to the raw id", () => {
    expect(providerShortName("claude-native")).toBe("Claude");
    expect(providerShortName("anthropic")).toBe("Claude");
    expect(providerShortName("openai")).toBe("Codex");
    expect(providerShortName("glm")).toBe("GLM");
    expect(providerShortName("xai")).toBe("xai");
  });
});
