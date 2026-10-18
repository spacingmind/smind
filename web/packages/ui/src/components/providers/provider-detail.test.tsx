import { act, fireEvent, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { ProviderDetail } from "@/components/providers/provider-detail";
import type { ProviderNavItem } from "@/lib/providers-view";
import { FakeUsageDaemon } from "@/test/usage-test-client";
import { renderWithDaemon } from "@/test/render-with-daemon";
import { useProvidersViewStore } from "@/stores/providers-view-store";
import { queryKeys } from "@/lib/query-keys";
import type { Account, ProviderListResult, ProviderTestResult } from "@/lib/types";
import type { ProviderTestResult as PT } from "@/lib/types";

const PROVIDERS = [
  { id: "claude-native", label: "Claude Code", credentialKind: "oauth", accountProvider: "anthropic", models: [{ id: "sonnet", label: "Sonnet" }, { id: "opus", label: "Opus", default: true }], modelsDiscovered: true },
  { id: "codex-native", label: "Codex", credentialKind: "oauth", accountProvider: "openai" },
  { id: "kimi", label: "Kimi", credentialKind: "api-key", accountProvider: "kimi" },
  { id: "glm", label: "GLM", kind: "cli" },
];

function account(id: number, provider: string): Account {
  return { id, provider, label: `Account ${id}`, credentialType: "oauth", createdAt: "", updatedAt: "" };
}

function item(over: Partial<ProviderNavItem> = {}): ProviderNavItem {
  return { id: "claude-native", label: "Claude Code", kind: "oauth", accountProvider: "anthropic", accountIds: [], tone: "neutral", connected: false, ...over };
}

function daemon(accounts: Account[], tests: Record<string, PT> = {}): FakeUsageDaemon {
  const d = new FakeUsageDaemon([]);
  d.providers = { providers: PROVIDERS } as unknown as ProviderListResult;
  d.accounts = accounts;
  d.testResults = tests;
  return d;
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

function openTestResult(ok: boolean): PT {
  return { ok, detail: ok ? "ok" : "boom" } as unknown as ProviderTestResult;
}

describe("ProviderDetail", () => {
  beforeEach(() => {
    useProvidersViewStore.getState().reset();
  });

  it("detail header summarizes account health", async () => {
    const d = daemon([account(1, "anthropic"), account(2, "anthropic"), account(3, "anthropic")]);
    const view = renderWithDaemon(<ProviderDetail item={item({ accountIds: [1, 2, 3], tone: "danger", connected: true })} client={d.client} />, d);
    view.queryClient().setQueryData(queryKeys.providerTest("anthropic"), openTestResult(false));
    await flush();
    expect(screen.getByTestId("provider-detail-summary")).toHaveTextContent("3 accounts");
    // account 1+2+3: the provider-level failing test marks all three as needing attention.
    expect(screen.getByTestId("provider-detail-summary")).toHaveTextContent("3 need attention");
    const d2 = daemon([account(4, "kimi")]);
    renderWithDaemon(<ProviderDetail item={item({ id: "kimi", label: "Kimi", kind: "api-key", accountProvider: "kimi", accountIds: [4], tone: "ok", connected: true })} client={d2.client} />, d2);
    await flush();
    expect(screen.getAllByTestId("provider-detail-summary")[1]).toHaveTextContent("1 account · all healthy");
  });

  it("detail shows the cli card for managed providers", async () => {
    const d = daemon([]);
    renderWithDaemon(<ProviderDetail item={item({ id: "glm", label: "GLM", kind: "cli", accountIds: [], tone: "neutral", connected: false })} client={d.client} />, d);
    await flush();
    const card = screen.getByTestId("provider-external-glm");
    expect(card).toHaveTextContent("Managed externally via CLI");
    expect(within(card).getByTestId("provider-test-glm")).toBeInTheDocument();
  });

  it("detail shows the not-connected block with a connect button", async () => {
    const d = daemon([]);
    renderWithDaemon(<ProviderDetail item={item({ accountIds: [], tone: "neutral", connected: false })} client={d.client} />, d);
    await flush();
    const block = screen.getByTestId("provider-not-connected-claude-native");
    expect(block).toHaveTextContent("Not connected");
    expect(within(block).getByTestId("provider-connect-claude-native")).toHaveTextContent("Connect Claude Code");
  });

  it("detail connect button requests a connect for that provider", async () => {
    const d = daemon([]);
    renderWithDaemon(<ProviderDetail item={item({ accountIds: [], tone: "neutral", connected: false })} client={d.client} />, d);
    await flush();
    await act(async () => {
      fireEvent.click(screen.getByTestId("provider-connect-claude-native"));
    });
    expect(useProvidersViewStore.getState().connectProviderId).toBe("claude-native");
    expect(useProvidersViewStore.getState().selectedProviderId).toBe("claude-native");
  });

  it("detail shows the proxy note for claude and codex only", async () => {
    const d = daemon([]);
    const { rerenderWith } = renderWithDaemon(
      <ProviderDetail item={item({ accountIds: [1], tone: "ok", connected: true })} client={d.client} />,
      d,
    );
    await flush();
    expect(screen.getByTestId("provider-group-note-claude-native")).toHaveTextContent(
      "Accounts here route sMind's /v1 proxy; agent runs use the CLI's own login.",
    );
    rerenderWith(
      d,
      <ProviderDetail
        item={item({ id: "codex-native", label: "Codex", accountProvider: "openai", accountIds: [], tone: "neutral", connected: false })}
        client={d.client}
      />,
    );
    await flush();
    expect(screen.getByTestId("provider-group-note-codex-native")).toBeInTheDocument();
    rerenderWith(
      d,
      <ProviderDetail
        item={item({ id: "kimi", label: "Kimi", kind: "api-key", accountProvider: "kimi", accountIds: [], tone: "neutral", connected: false })}
        client={d.client}
      />,
    );
    await flush();
    expect(screen.queryByTestId("provider-group-note-kimi")).toBeNull();
  });

  it("detail shows models and a collapsed diagnostics block", async () => {
    const d = daemon([account(1, "anthropic")]);
    renderWithDaemon(<ProviderDetail item={item({ accountIds: [1], tone: "ok", connected: true })} client={d.client} />, d);
    await flush();
    const models = screen.getByTestId("provider-models");
    expect(models).toHaveTextContent("Sonnet");
    expect(models).toHaveTextContent("Reported by the agent");
    expect(screen.getByTestId("provider-diagnostics-toggle").getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByTestId("provider-diagnostics-text")).toBeNull();
  });

  it("detail diagnostics reflects the account health", async () => {
    const d = daemon([account(1, "anthropic")]);
    const view = renderWithDaemon(<ProviderDetail item={item({ accountIds: [1], tone: "ok", connected: true })} client={d.client} />, d);
    await flush();
    fireEvent.click(screen.getByTestId("provider-diagnostics-toggle"));
    const text = screen.getByTestId("provider-diagnostics-text").textContent ?? "";
    expect(text).toContain("Account 1");
    expect(text).toContain("health=");
  });
});
