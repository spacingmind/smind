import { act } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import {
  groupAccountsByProvider,
  removeWarning,
} from "@/components/settings/providers-section";
import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/providers-section";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { ProviderInfo, RunSummary } from "@/lib/types";
import { FakeWsClient } from "@/test/fake-ws-client";

const PROVIDERS: ProviderInfo[] = [
  { id: "claude-native", label: "Claude Code", credentialKind: "oauth", accountProvider: "anthropic" },
  { id: "glm", label: "GLM", kind: "cli" },
  { id: "kimi", label: "Kimi", credentialKind: "api-key", accountProvider: "kimi" },
  { id: "codex-native", label: "Codex", credentialKind: "oauth", accountProvider: "openai" },
];

function account(id: number, provider: string, label: string, credentialType = "oauth") {
  return { id, provider, label, credentialType, createdAt: "", updatedAt: "" };
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

function renderSection(client: FakeWsClient, events?: DaemonEvents) {
  const section = listSettingsSections().find((s) => s.id === "providers");
  if (!section) throw new Error("providers section did not register");
  render(<>{section.render({ client: client as never, events })}</>);
}

/** A minimal DaemonEvents fake: records listeners per topic and lets a test fire one directly (same shape profiles-section.test.tsx uses). */
function fakeDaemonEvents(): DaemonEvents & { fire(topic: string, payload: unknown): void } {
  const listeners = new Map<string, Set<(payload: unknown) => void>>();
  return {
    subscribe(topic, listener) {
      let set = listeners.get(topic);
      if (!set) {
        set = new Set();
        listeners.set(topic, set);
      }
      set.add(listener);
      return () => set!.delete(listener);
    },
    fire(topic, payload) {
      for (const fn of listeners.get(topic) ?? []) fn(payload);
    },
  };
}

/** Opens the ⋯ menu on a row and clicks the named item, the way app-sidebar-crud.test.tsx drives Radix menus. */
async function chooseMenuItem(rowId: number, name: string) {
  const menu = screen.getByTestId(`provider-row-menu-${rowId}`);
  fireEvent.pointerDown(menu, { button: 0, ctrlKey: false });
  fireEvent.click(await screen.findByRole("menuitem", { name }));
}

function run(id: string, provider: string, status: RunSummary["Status"]): RunSummary {
  return { ID: id, TaskID: 0, Provider: provider as RunSummary["Provider"], Prompt: "", Status: status, StartedAt: "", FinishedAt: null, StopReason: "", Err: "", ApprovalPolicy: "manual", ThinkingLevel: "" };
}

/** Renders the section with account.list and provider.list already resolved, so structure assertions need no extra flushing. */
async function renderLoaded(accounts: ReturnType<typeof account>[], providers = PROVIDERS) {
  const client = new FakeWsClient();
  renderSection(client);
  client.nth("account.list").resolve(accounts);
  client.nth("provider.list").resolve({ providers });
  await flush();
  return client;
}

describe("groupAccountsByProvider", () => {
  it("maps accounts to runtime-provider groups via accountProvider, in provider.list order", () => {
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

  it("collects accounts no runtime provider maps into `other`", () => {
    const accounts = [account(1, "xai", "grok"), account(2, "anthropic", "work")];
    const { other } = groupAccountsByProvider(PROVIDERS, accounts);
    expect(other.map((a) => a.provider)).toEqual(["xai"]);
  });
});

describe("providers-section", () => {
  it("registers with id providers and label Providers", () => {
    const section = listSettingsSections().find((s) => s.id === "providers");
    expect(section?.label).toBe("Providers");
  });

  it("renders accounts grouped under their runtime provider headers", async () => {
    await renderLoaded([
      account(1, "anthropic", "work-claude"),
      account(2, "openai", "codex-work"),
    ]);
    expect(screen.getByTestId("provider-group-claude-native")).toHaveTextContent("work-claude");
    expect(screen.getByTestId("provider-group-codex-native")).toHaveTextContent("codex-work");
    expect(within(screen.getByTestId("provider-group-claude-native")).queryByText("codex-work")).not.toBeInTheDocument();
  });

  it("renders a not-connected row for a provider with no accounts", async () => {
    await renderLoaded([account(1, "anthropic", "work-claude")]);
    expect(screen.getByTestId("provider-not-connected-kimi")).toHaveTextContent("Not connected");
    expect(screen.getByTestId("provider-not-connected-codex-native")).toBeInTheDocument();
  });

  it("renders the CLI provider's managed-externally row, not a not-connected row", async () => {
    await renderLoaded([]);
    expect(screen.getByTestId("provider-external-glm")).toHaveTextContent("Managed externally");
    expect(screen.queryByTestId("provider-not-connected-glm")).not.toBeInTheDocument();
  });

  it("renders unmapped-provider accounts in the Other accounts group", async () => {
    await renderLoaded([
      account(1, "xai", "grok"),
      account(2, "antigravity", "ag-1"),
      account(3, "anthropic", "work-claude"),
    ]);
    const other = screen.getByTestId("provider-other-group");
    expect(within(other).getByText("grok")).toBeInTheDocument();
    expect(within(other).getByText("ag-1")).toBeInTheDocument();
    expect(within(other).queryByText("work-claude")).not.toBeInTheDocument();
    // Not mixed into the runtime groups either.
    expect(within(screen.getByTestId("provider-group-claude-native")).queryByText("grok")).not.toBeInTheDocument();
  });

  it("shows the auth-type badge per row and runs provider.test from a row", async () => {
    const client = await renderLoaded([account(1, "anthropic", "work-claude")]);
    expect(screen.getByText("OAuth")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("provider-test-anthropic"));
    expect(client.nth("provider.test").params).toEqual({ provider: "anthropic" });
    client.nth("provider.test").resolve({ ok: true, detail: "using \"work-claude\" (oauth)" });
    await flush();

    expect(screen.getByTestId("provider-row-test-result-1")).toHaveTextContent("work-claude");
    expect(screen.getByTestId("provider-row-dot-1")).toHaveAttribute("data-status", "ok");
  });
});

describe("removeWarning", () => {
  const anthropic = account(1, "anthropic", "work-claude");

  it("counts running runs on the account's runtime provider and reports a sibling to fail over to", () => {
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

  it("flags the last account of its provider", () => {
    const runs = [run("a", "claude-native", "running")];
    const w = removeWarning(anthropic, PROVIDERS, [anthropic], runs);
    expect(w.runningCount).toBe(1);
    expect(w.lastAccount).toBe(true);
  });

  it("ignores an unmapped-provider account for runner purposes but still warns for its own removal", () => {
    const xai = account(9, "xai", "grok");
    const w = removeWarning(xai, PROVIDERS, [xai], [run("a", "glm", "running")]);
    expect(w.runningCount).toBe(0);
    expect(w.lastAccount).toBe(true);
    expect(w.providerLabel).toBe("xai");
  });
});

describe("providers-section actions", () => {
  it("renames inline via account.rename and applies the result", async () => {
    const client = await renderLoaded([account(1, "anthropic", "work-claude")]);

    await chooseMenuItem(1, "Rename");
    fireEvent.change(screen.getByTestId("provider-rename-input-1"), { target: { value: "renamed" } });
    fireEvent.click(screen.getByTestId("provider-rename-save-1"));
    expect(client.nth("account.rename").params).toEqual({ id: 1, label: "renamed" });

    client.nth("account.rename").resolve(account(1, "anthropic", "renamed"));
    await flush();

    expect(screen.getByText("renamed")).toBeInTheDocument();
    expect(screen.queryByText("work-claude")).not.toBeInTheDocument();
  });

  it("updates a credential via account.updateCredential with the pasted value and optional baseUrl", async () => {
    const client = await renderLoaded([account(1, "anthropic", "work-claude")]);

    await chooseMenuItem(1, "Update credential");
    fireEvent.change(screen.getByTestId("provider-credential-input-1"), { target: { value: "sk-new" } });
    fireEvent.change(screen.getByTestId("provider-credential-base-url-1"), { target: { value: "http://127.0.0.1:8080" } });
    fireEvent.click(screen.getByTestId("provider-credential-save-1"));
    expect(client.nth("account.updateCredential").params).toEqual({
      id: 1,
      credential: "sk-new",
      baseUrl: "http://127.0.0.1:8080",
    });

    client.nth("account.updateCredential").resolve(account(1, "anthropic", "work-claude", "api_key"));
    await flush();

    expect(screen.getByText("API key")).toBeInTheDocument();
  });

  it("removes via account.remove after showing the failover warning (sibling exists)", async () => {
    const client = await renderLoaded([
      account(1, "anthropic", "work-claude"),
      account(2, "anthropic", "alt"),
    ]);

    await chooseMenuItem(1, "Remove");
    client.nth("run.list").resolve([
      run("a", "claude-native", "running"),
      run("b", "claude-native", "running"),
    ]);
    await flush();

    expect(screen.getByTestId("provider-remove-warning-1")).toHaveTextContent(
      "2 running tasks will switch to another account",
    );

    fireEvent.click(screen.getByTestId("provider-remove-confirm-1"));
    expect(client.nth("account.remove").params).toEqual({ id: 1 });
    client.nth("account.remove").resolve({});
    await flush();

    expect(screen.queryByTestId("provider-row-1")).not.toBeInTheDocument();
    expect(screen.getByTestId("provider-row-2")).toBeInTheDocument();
  });

  it("shows the last-account failure branch in the remove warning", async () => {
    const client = await renderLoaded([account(1, "anthropic", "work-claude")]);

    await chooseMenuItem(1, "Remove");
    client.nth("run.list").resolve([run("a", "claude-native", "running")]);
    await flush();

    expect(screen.getByTestId("provider-remove-warning-1")).toHaveTextContent(
      "1 running task will fail: this is the last Claude Code account",
    );
  });

  it("reflects account.updated / account.removed events without a manual refresh", async () => {
    const client = new FakeWsClient();
    const events = fakeDaemonEvents();
    renderSection(client, events);
    client.nth("account.list").resolve([account(1, "anthropic", "work-claude")]);
    client.nth("provider.list").resolve({ providers: PROVIDERS });
    await flush();
    expect(screen.getByText("work-claude")).toBeInTheDocument();

    act(() => {
      events.fire("account.updated", { account: account(1, "anthropic", "renamed-elsewhere") });
    });
    await flush();
    expect(screen.getByText("renamed-elsewhere")).toBeInTheDocument();
    expect(screen.queryByText("work-claude")).not.toBeInTheDocument();

    act(() => {
      events.fire("account.removed", { id: 1 });
    });
    await flush();
    expect(screen.queryByTestId("provider-row-1")).not.toBeInTheDocument();
  });
});
