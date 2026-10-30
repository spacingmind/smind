import { describe, expect, it } from "vitest";

import type { ProviderInfo } from "@/lib/types";

import { accountProviderLabel, providerFamilyId, providerIdentity, providerLabel, providerRunner, providerShortName } from "@/lib/provider-catalog";
import { FALLBACK_PROVIDERS } from "@/lib/provider-catalog-fallback";

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

// ADR-0042 AC31: provider ids are strings; anything the catalog describes (an instance, a providers.custom ACP agent) works without a table row.
const CUSTOM: ProviderInfo[] = [
  { id: "claude-native", label: "Claude Code", runner: "claude-native" },
  { id: "claude-work", label: "Claude work", runner: "claude-native", extends: "claude-native", custom: true },
  { id: "my-acp", label: "My ACP agent", runner: "acp", custom: true },
];

describe("unknown provider ids (providers.custom)", () => {
  it("takes the label from the catalog, not from a table", () => {
    expect(providerLabel("my-acp", CUSTOM)).toBe("My ACP agent");
    expect(providerLabel("claude-work", CUSTOM)).toBe("Claude work");
    expect(providerLabel("my-acp")).toBe("my-acp");
  });

  it("asks the catalog how a provider runs, then the built-in table, then treats the id as ACP", () => {
    expect(providerRunner("claude-work", CUSTOM)).toBe("claude-native");
    expect(providerRunner("my-acp", CUSTOM)).toBe("acp");
    expect(providerRunner("codex-native")).toBe("codex-native");
    expect(providerRunner("glm")).toBe("acp");
    expect(providerRunner("never-heard-of-it")).toBe("acp");
  });

  it("names the built-in an instance extends", () => {
    expect(providerFamilyId("claude-work", CUSTOM)).toBe("claude-native");
    expect(providerFamilyId("my-acp", CUSTOM)).toBe("my-acp");
  });
});

describe("new ACP built-ins", () => {
  it("providerRunner and providerLabel know the five new ids offline", () => {
    const labels = {
      opencode: "OpenCode",
      copilot: "GitHub Copilot",
      cline: "Cline",
      goose: "Goose",
      cursor: "Cursor",
    } as const;
    for (const [id, label] of Object.entries(labels)) {
      expect(providerRunner(id)).toBe("acp");
      expect(providerLabel(id)).toBe(label);
    }
  });
});

describe("offline fallback catalog", () => {
  it("is exactly the four built-ins with their runners", () => {
    expect(FALLBACK_PROVIDERS.map((p) => [p.id, p.runner])).toEqual([
      ["claude-native", "claude-native"],
      ["codex-native", "codex-native"],
      ["glm", "acp"],
      ["kimi", "acp"],
    ]);
  });
});
