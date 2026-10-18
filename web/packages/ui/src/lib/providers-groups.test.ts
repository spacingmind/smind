import { describe, expect, it } from "vitest";

import {
  groupAccountsByProvider,
  providerConsumer,
  removeWarning,
} from "@/lib/providers-groups";
import type { ProviderInfo, RunSummary } from "@/lib/types";

const PROVIDERS: ProviderInfo[] = [
  { id: "claude-native", label: "Claude Code", credentialKind: "oauth", accountProvider: "anthropic" },
  { id: "glm", label: "GLM", kind: "cli" },
  { id: "kimi", label: "Kimi", credentialKind: "api-key", accountProvider: "kimi" },
  { id: "codex-native", label: "Codex", credentialKind: "oauth", accountProvider: "openai" },
];

function account(id: number, provider: string, label: string, credentialType = "oauth") {
  return { id, provider, label, credentialType, createdAt: "", updatedAt: "" };
}

function run(id: string, provider: string, status: RunSummary["Status"]): RunSummary {
  return { ID: id, TaskID: 0, ChatID: 0, Provider: provider as RunSummary["Provider"], Prompt: "", Status: status, StartedAt: "", FinishedAt: null, StopReason: "", Err: "", PermissionMode: "", AutoAccept: false, ThinkingLevel: "" };
}

describe("groupAccountsByProvider", () => {
  it("groups accounts by runtime provider", () => {
    const accounts = [
      account(1, "anthropic", "work-claude"),
      account(2, "openai", "codex-work"),
      account(3, "anthropic", "personal-claude"),
    ];
    const { groups, other } = groupAccountsByProvider(PROVIDERS, accounts);
    expect(groups.map((g) => g.info.id)).toEqual(["claude-native", "glm", "kimi", "codex-native"]);
    expect(groups[0].accounts.map((a) => a.label)).toEqual(["work-claude", "personal-claude"]);
    expect(groups[1].accounts).toEqual([]);
    expect(groups[3].accounts.map((a) => a.label)).toEqual(["codex-work"]);
    expect(other).toEqual([]);
  });

  it("unmapped accounts go to other", () => {
    const accounts = [account(1, "xai", "grok"), account(2, "anthropic", "work")];
    const { other } = groupAccountsByProvider(PROVIDERS, accounts);
    expect(other.map((a) => a.provider)).toEqual(["xai"]);
  });
});

describe("removeWarning", () => {
  it("remove warning counts running runs on the provider", () => {
    const anthropic = account(1, "anthropic", "work-claude");
    const runs = [
      run("a", "claude-native", "running"),
      run("b", "claude-native", "running"),
      run("c", "claude-native", "done"),
      run("d", "glm", "running"),
    ];
    const w = removeWarning(anthropic, PROVIDERS, [anthropic, account(2, "anthropic", "alt")], runs);
    expect(w.runningCount).toBe(2);
    expect(w.lastAccount).toBe(false);
    expect(w.providerLabel).toBe("Claude Code");
  });
});

describe("providerConsumer", () => {
  it("provider consumer is proxy only for anthropic and openai", () => {
    expect(providerConsumer("anthropic")).toBe("proxy");
    expect(providerConsumer("openai")).toBe("proxy");
    expect(providerConsumer("kimi")).toBe("none");
    expect(providerConsumer("xai")).toBe("none");
    expect(providerConsumer("antigravity")).toBe("none");
    expect(providerConsumer("glm")).toBe("none");
  });
});
