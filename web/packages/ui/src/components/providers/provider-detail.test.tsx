import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";

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
    expect(block).toHaveTextContent("No proxy account");
    expect(block).not.toHaveTextContent("Not connected");
    expect(within(block).getByTestId("provider-connect-claude-native")).toHaveTextContent("Connect Claude Code");
  });

  it("a claude-native provider with no accounts is Ready (CLI login) with No proxy account; kimi stays Not connected", async () => {
    const d = daemon([]);
    const view = renderWithDaemon(<ProviderDetail item={item({ accountIds: [], tone: "neutral", connected: false })} client={d.client} />, d);
    await flush();
    await waitFor(() => expect(screen.getByTestId("provider-readiness-claude-native")).toHaveTextContent("Ready (CLI login)"));
    expect(screen.getByTestId("provider-not-connected-claude-native")).toHaveTextContent("No proxy account");
    view.rerenderWith(
      d,
      <ProviderDetail item={item({ id: "kimi", label: "Kimi", kind: "api-key", accountProvider: "kimi", accountIds: [], tone: "neutral", connected: false })} client={d.client} />,
    );
    await flush();
    await waitFor(() => expect(screen.getByTestId("provider-readiness-kimi")).toHaveTextContent(/^Ready$/));
    expect(screen.getByTestId("provider-not-connected-kimi")).toHaveTextContent("Not connected");
  });

  it("readiness line: not ok shows the detail, unanswered shows Checking…, a failed RPC shows Couldn't check", async () => {
    const d = daemon([], { "claude-native": { ok: false, detail: "claude not on PATH" } as PT });
    d.testRpcErrors = { codex: new Error("boom"), "codex-native": new Error("boom") };
    const view = renderWithDaemon(<ProviderDetail item={item()} client={d.client} />, d);
    // Before any answer lands the line is neutral and says Checking…
    expect(screen.getByTestId("provider-readiness-claude-native")).toHaveTextContent("Checking…");
    await flush();
    await waitFor(() => expect(screen.getByTestId("provider-readiness-claude-native")).toHaveTextContent("Not ready — claude not on PATH"));
    expect(screen.getByTestId("provider-readiness-claude-native").querySelector("[data-slot=status-dot]")).toHaveAttribute("data-status", "danger");
    view.rerenderWith(
      d,
      <ProviderDetail item={item({ id: "codex-native", label: "Codex", accountProvider: "openai" })} client={d.client} />,
    );
    await flush();
    await waitFor(() => expect(screen.getByTestId("provider-readiness-codex-native")).toHaveTextContent("Couldn't check"));
  });

  it("readiness line is not shown for the other bucket", async () => {
    const d = daemon([account(3, "xai")]);
    renderWithDaemon(<ProviderDetail item={item({ id: "other", label: "Other accounts", kind: "other", accountProvider: undefined, accountIds: [3], connected: true })} client={d.client} />, d);
    await flush();
    expect(screen.queryByTestId("provider-readiness-other")).toBeNull();
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
    renderWithDaemon(<ProviderDetail item={item({ accountIds: [1], tone: "ok", connected: true })} client={d.client} />, d);
    await flush();
    fireEvent.click(screen.getByTestId("provider-diagnostics-toggle"));
    const text = screen.getByTestId("provider-diagnostics-text").textContent ?? "";
    expect(text).toContain("Account 1");
    expect(text).toContain("health=");
  });

  it("detail survives item going from null to a provider", async () => {
    const d = daemon([]);
    const view = renderWithDaemon(<ProviderDetail item={null} client={d.client} />, d);
    expect(screen.getByTestId("provider-detail")).toHaveTextContent("Select a provider");
    view.rerenderWith(d, <ProviderDetail item={item({ accountIds: [1], tone: "ok", connected: true })} client={d.client} />);
    await flush();
    expect(screen.getByTestId("provider-detail")).toHaveTextContent("Claude Code");
  });

  it("relogin of another account remounts the panel and keeps its confirm", async () => {
    const ws = new FakeWsClient();
    const work = { ...account(1, "anthropic"), hasIdentity: true };
    const oldKey = { ...account(9, "anthropic"), label: "old-key", hasIdentity: false };
    const d = new FakeUsageDaemon([]);
    d.providers = { providers: PROVIDERS } as unknown as ProviderListResult;
    d.accounts = [work, oldKey];
    renderWithDaemon(
      <ProviderDetail item={item({ accountIds: [1, 9], tone: "ok", connected: true })} client={ws as never} />,
      d,
    );
    await flush();
    act(() => {
      useProvidersViewStore.getState().requestRelogin(1, "claude-native");
    });
    await flush();
    const first = await waitFor(() => ws.nth("account.oauthStart", 0));
    expect(first.params).toMatchObject({ accountId: 1 });
    act(() => {
      useProvidersViewStore.getState().requestRelogin(9, "claude-native");
    });
    await flush();
    expect(first.options?.signal?.aborted).toBe(true);
    expect(screen.getByTestId("relogin-confirm")).toHaveTextContent("old-key");
    await act(async () => {
      fireEvent.click(screen.getByTestId("relogin-confirm-continue"));
    });
    const second = await waitFor(() => ws.nth("account.oauthStart", 1));
    expect(second.params).toMatchObject({ accountId: 9, label: "old-key" });
    expect(second.options?.signal?.aborted).not.toBe(true);
  });

  it("models search is cleared when the provider changes", async () => {
    const withModels = [
      { ...PROVIDERS[0], models: Array.from({ length: 9 }, (_, i) => ({ id: `m-${i}`, label: `Model ${i}` })) },
      { id: "codex-native", label: "Codex", credentialKind: "oauth", accountProvider: "openai", models: Array.from({ length: 9 }, (_, i) => ({ id: `c-${i}`, label: `Codex Model ${i}` })) },
    ];
    const d = new FakeUsageDaemon([]);
    d.providers = { providers: withModels } as unknown as ProviderListResult;
    d.accounts = [];
    const view = renderWithDaemon(
      <ProviderDetail item={item({ accountIds: [], tone: "neutral", connected: false })} client={d.client} />,
      d,
    );
    await flush();
    fireEvent.change(screen.getByTestId("models-search"), { target: { value: "Model 3" } });
    expect(screen.getByTestId("models-search")).toHaveValue("Model 3");
    view.rerenderWith(
      d,
      <ProviderDetail
        item={item({ id: "codex-native", label: "Codex", accountProvider: "openai", accountIds: [], tone: "neutral", connected: false })}
        client={d.client}
      />,
    );
    await flush();
    expect(screen.getByTestId("models-search")).toHaveValue("");
  });
});
