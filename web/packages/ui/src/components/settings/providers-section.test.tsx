import { act } from "react";
import { fireEvent, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import {
  groupAccountsByProvider,
  providerConsumer,
  removeWarning,
} from "@/components/settings/providers-section";
import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/providers-section";
import type { ProviderInfo, RunSummary } from "@/lib/types";
import { useProvidersViewStore } from "@/stores/providers-view-store";
import { FakeUsageDaemon } from "@/test/usage-test-client";
import { renderWithDaemon } from "@/test/render-with-daemon";

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
    await Promise.resolve();
  });
}

function renderSection(daemon: FakeUsageDaemon) {
  const section = listSettingsSections().find((s) => s.id === "providers");
  if (!section) throw new Error("providers section did not register");
  renderWithDaemon(<>{section.render({ client: daemon.client, events: daemon.events })}</>, daemon);
}

/** Selects a provider in the nav (both nav variants render in jsdom, so the first match). */
async function selectProvider(id: string) {
  await act(async () => {
    fireEvent.click(screen.getAllByTestId(`provider-nav-item-${id}`)[0]);
  });
}

/** Opens the ⋯ menu on a row and clicks the named item, the way app-sidebar-crud.test.tsx drives Radix menus. */
async function chooseMenuItem(rowId: number, name: string) {
  const menu = screen.getByTestId(`provider-row-menu-${rowId}`);
  fireEvent.pointerDown(menu, { button: 0, ctrlKey: false });
  fireEvent.click(await screen.findByRole("menuitem", { name }));
}

function run(id: string, provider: string, status: RunSummary["Status"]): RunSummary {
  return { ID: id, TaskID: 0, ChatID: 0, Provider: provider as RunSummary["Provider"], Prompt: "", Status: status, StartedAt: "", FinishedAt: null, StopReason: "", Err: "", PermissionMode: "", AutoAccept: false, ThinkingLevel: "" };
}

/** Renders the section with account.list and provider.list answered, so structure assertions need no extra flushing. */
async function renderLoaded(accounts: ReturnType<typeof account>[], providers = PROVIDERS) {
  const daemon = new FakeUsageDaemon([]);
  daemon.accounts = accounts.map((a) => ({ ...a }));
  daemon.providers = { providers };
  renderSection(daemon);
  await flush();
  return daemon;
}

// The view store is module state: a provider selected by an earlier test must not leak into the next.
beforeEach(() => useProvidersViewStore.getState().reset());

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
    // One provider nav item per runtime provider; the first is selected by default.
    expect(screen.getAllByTestId("provider-nav-item-claude-native")[0]).toHaveAttribute("aria-current", "true");
    expect(screen.getByTestId("provider-row-1")).toHaveTextContent("work-claude");
    await selectProvider("codex-native");
    expect(screen.getByTestId("provider-row-2")).toHaveTextContent("codex-work");
    expect(screen.queryByTestId("provider-row-1")).not.toBeInTheDocument();
  });

  it("renders a not-connected row for a provider with no accounts", async () => {
    await renderLoaded([account(1, "anthropic", "work-claude")]);
    await selectProvider("kimi");
    expect(screen.getByTestId("provider-not-connected-kimi")).toHaveTextContent("Not connected");
    await selectProvider("codex-native");
    expect(screen.getByTestId("provider-not-connected-codex-native")).toBeInTheDocument();
  });

  it("renders the CLI provider's managed-externally row, not a not-connected row", async () => {
    await renderLoaded([]);
    await selectProvider("glm");
    expect(screen.getByTestId("provider-external-glm")).toHaveTextContent("Managed externally");
    expect(screen.getByTestId("provider-external-glm")).toHaveTextContent("own CLI login");
    expect(screen.queryByTestId("provider-not-connected-glm")).not.toBeInTheDocument();
  });

  it("notes the proxy-only groups (Claude Code, Codex) but not Kimi or GLM", async () => {
    await renderLoaded([]);
    await selectProvider("claude-native");
    expect(screen.getByTestId("provider-group-note-claude-native")).toHaveTextContent(
      "Accounts here route sMind's /v1 proxy; agent runs use the CLI's own login.",
    );
    await selectProvider("codex-native");
    expect(screen.getByTestId("provider-group-note-codex-native")).toHaveTextContent(
      "Accounts here route sMind's /v1 proxy; agent runs use the CLI's own login.",
    );
    await selectProvider("kimi");
    expect(screen.queryByTestId("provider-group-note-kimi")).not.toBeInTheDocument();
    await selectProvider("glm");
    expect(screen.queryByTestId("provider-group-note-glm")).not.toBeInTheDocument();
  });

  it("renders unmapped-provider accounts in the Other accounts group", async () => {
    await renderLoaded([
      account(1, "xai", "grok"),
      account(2, "antigravity", "ag-1"),
      account(3, "anthropic", "work-claude"),
    ]);
    await selectProvider("other");
    expect(screen.getByText("grok")).toBeInTheDocument();
    expect(screen.getByText("ag-1")).toBeInTheDocument();
    expect(screen.queryByText("work-claude")).not.toBeInTheDocument();
    // Not mixed into the runtime groups either.
    await selectProvider("claude-native");
    expect(screen.queryByText("grok")).not.toBeInTheDocument();
    expect(screen.getByText("work-claude")).toBeInTheDocument();
  });

  it("shows the auth-type badge per row and runs provider.test from a row", async () => {
    const client = await renderLoaded([account(1, "anthropic", "work-claude")]);
    expect(screen.getByText("OAuth")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("provider-test-anthropic"));
    await flush();
    expect(client.calls.filter((c) => c.method === "provider.test").map((c) => c.params).find((p) => (p as { provider: string }).provider === "anthropic")).toEqual({ provider: "anthropic" });

    expect(screen.getByTestId("provider-row-test-result-1")).toHaveTextContent("ok");
    expect(screen.getByTestId("provider-row-dot-1")).toHaveAttribute("data-status", "ok");
  });
});

describe("providerConsumer", () => {
  it("reports the two proxy-routed account providers as proxy-consumed", () => {
    expect(providerConsumer("anthropic")).toBe("proxy");
    expect(providerConsumer("openai")).toBe("proxy");
  });

  it("reports every other provider string as having no consumer", () => {
    expect(providerConsumer("kimi")).toBe("none");
    expect(providerConsumer("xai")).toBe("none");
    expect(providerConsumer("antigravity")).toBe("none");
    expect(providerConsumer("glm")).toBe("none");
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
    await flush();
    expect(client.calls.find((c) => c.method === "account.rename")?.params).toEqual({ id: 1, label: "renamed" });
    await flush(); // the mutation result reaches the cache a few microtasks after the call

    expect(screen.getByText("renamed")).toBeInTheDocument();
    expect(screen.queryByText("work-claude")).not.toBeInTheDocument();
  });

  it("updates a credential via account.updateCredential with the pasted value and optional baseUrl", async () => {
    const client = await renderLoaded([account(1, "anthropic", "work-claude")]);
    client.onAccountMutation = (_m, _p, a) => ({ ...a, credentialType: "api_key" });

    await chooseMenuItem(1, "Update credential");
    fireEvent.change(screen.getByTestId("provider-credential-input-1"), { target: { value: "sk-new" } });
    fireEvent.change(screen.getByTestId("provider-credential-base-url-1"), { target: { value: "http://127.0.0.1:8080" } });
    fireEvent.click(screen.getByTestId("provider-credential-save-1"));
    await flush();
    expect(client.calls.find((c) => c.method === "account.updateCredential")?.params).toEqual({
      id: 1,
      credential: "sk-new",
      baseUrl: "http://127.0.0.1:8080",
    });
    await flush();

    expect(screen.getByText("API key")).toBeInTheDocument();
  });

  it("removes via account.remove after showing the failover warning (sibling exists)", async () => {
    const client = await renderLoaded([
      account(1, "anthropic", "work-claude"),
      account(2, "anthropic", "alt"),
    ]);

    client.runs = [run("a", "claude-native", "running"), run("b", "claude-native", "running")];
    await chooseMenuItem(1, "Remove");
    await flush();

    expect(screen.getByTestId("provider-remove-warning-1")).toHaveTextContent(
      "2 running tasks will switch to another account",
    );

    fireEvent.click(screen.getByTestId("provider-remove-confirm-1"));
    await flush();
    expect(client.calls.find((c) => c.method === "account.remove")?.params).toEqual({ id: 1 });

    expect(screen.queryByTestId("provider-row-1")).not.toBeInTheDocument();
    expect(screen.getByTestId("provider-row-2")).toBeInTheDocument();
  });

  it("shows the last-account failure branch in the remove warning", async () => {
    const client = await renderLoaded([account(1, "anthropic", "work-claude")]);

    client.runs = [run("a", "claude-native", "running")];
    await chooseMenuItem(1, "Remove");
    await flush();

    expect(screen.getByTestId("provider-remove-warning-1")).toHaveTextContent(
      "1 running task will fail: this is the last Claude Code account",
    );
  });

  it("reflects account.updated / account.removed events without a manual refresh", async () => {
    const client = new FakeUsageDaemon([]);
    client.accounts = [account(1, "anthropic", "work-claude")];
    client.providers = { providers: PROVIDERS };
    renderSection(client);
    await flush();
    expect(screen.getByText("work-claude")).toBeInTheDocument();

    client.accounts = [account(1, "anthropic", "renamed-elsewhere")];
    act(() => {
      client.emit("account.updated", { account: account(1, "anthropic", "renamed-elsewhere") });
    });
    await flush();
    expect(screen.getByText("renamed-elsewhere")).toBeInTheDocument();
    expect(screen.queryByText("work-claude")).not.toBeInTheDocument();

    client.accounts = [];
    act(() => {
      client.emit("account.removed", { id: 1 });
    });
    await flush();
    expect(screen.queryByTestId("provider-row-1")).not.toBeInTheDocument();
  });
});

describe("providers-section connect flows", () => {
  it("Connect account disclosure opens the shared ConnectAccountPanel and adds through it", async () => {
    const client = await renderLoaded([account(1, "anthropic", "work-claude")]);

    // The detail pane is the primary content; no connect form until asked.
    expect(screen.queryByTestId("connect-close")).not.toBeInTheDocument();

    // The header action targets the selected provider: Kimi is api-key, so
    // the shared panel opens its form (no login starts for an api-key provider).
    await selectProvider("kimi");
    fireEvent.click(screen.getByTestId("providers-connect-toggle"));
    await flush();
    expect(screen.getByLabelText("Credential")).toBeInTheDocument();
    expect(client.calls.filter((c) => c.method === "account.oauthStart")).toHaveLength(0);

    // The shared panel's manual-add path works from here: fill the api-key
    // form, submit -- account.add fires with accountProvider.
    fireEvent.change(document.getElementById("account-oauth-label")!, { target: { value: "alt" } });
    fireEvent.change(document.getElementById("account-credential")!, { target: { value: "sk-new" } });
    fireEvent.click(screen.getByTestId("accounts-add-submit"));
    expect(client.calls.find((c) => c.method === "account.add")?.params).toMatchObject({
      provider: "kimi",
      label: "alt",
      credential: "sk-new",
    });
    await flush();
    // account.add updated the daemon's own list; onConnected invalidates
    // account.list and the re-read brings the new account in.
    expect(screen.getByText("alt")).toBeInTheDocument();
  });

  it("a not-connected row's Connect opens the shared panel", async () => {
    const client = await renderLoaded([account(1, "anthropic", "work-claude")]);

    // An oauth provider's row-level Connect starts its login at once.
    await selectProvider("codex-native");
    fireEvent.click(screen.getByTestId("provider-connect-codex-native"));
    await flush();
    const start = client.calls.find((c) => c.method === "account.oauthStart");
    expect(start?.params).toMatchObject({ provider: "openai", method: "browser" });
    expect(typeof (start?.params as { label?: string })?.label).toBe("string");
    // The api-key provider shows the form instead of starting.
    await act(async () => {
      useProvidersViewStore.getState().clearConnect();
    });
    await selectProvider("kimi");
    fireEvent.click(screen.getByTestId("provider-connect-kimi"));
    await flush();
    expect(client.calls.filter((c) => c.method === "account.oauthStart")).toHaveLength(1);
    expect(screen.getByLabelText("Credential")).toBeInTheDocument();
  });
});

describe("Connect account header button", () => {
  it("header connect falls back to the first credential provider for a cli selection", async () => {
    await renderLoaded([account(1, "anthropic", "work")]);
    await selectProvider("glm");
    await act(async () => {
      fireEvent.click(screen.getByTestId("providers-connect-toggle"));
    });
    expect(useProvidersViewStore.getState().connectProviderId).toBe("claude-native");
  });

  it("header connect is disabled without a credential provider", async () => {
    await renderLoaded([], [{ id: "glm", label: "GLM", kind: "cli" }]);
    const button = screen.getByTestId("providers-connect-toggle");
    expect(button).toBeDisabled();
    fireEvent.click(button);
    expect(useProvidersViewStore.getState().connectProviderId).toBeNull();
  });
});

// ADR-0042 AC37: provider instances (providers.custom `extends`) and the one-click Add instance area.
describe("provider instances", () => {
  const INSTANCE: ProviderInfo = {
    id: "claude-work",
    label: "Claude work",
    kind: "cli",
    runner: "claude-native",
    extends: "claude-native",
    custom: true,
    env: { CLAUDE_CONFIG_DIR: "/home/u/.claude-work" },
    envDirExists: { CLAUDE_CONFIG_DIR: true },
    envEffective: { CLAUDE_CONFIG_DIR: { path: "/home/u/.claude-work", source: "settings", exists: true } },
  };
  const SUGGESTIONS = [
    { name: ".claude-spare", dir: "/home/u/.claude-spare", extends: "claude-native", suggestedId: "claude-spare", suggestedLabel: "Claude spare" },
    { name: ".codex-alt", dir: "/home/u/.codex-alt", extends: "codex-native", suggestedId: "codex-alt", suggestedLabel: "Codex alt" },
  ];

  async function renderWithInstances() {
    const daemon = await renderLoaded([], [...PROVIDERS, INSTANCE]);
    daemon.dirSuggestions = SUGGESTIONS.map((s) => ({ ...s }));
    return daemon;
  }

  async function openAddArea() {
    await act(async () => {
      fireEvent.click(screen.getByTestId("providers-add-instance-toggle"));
    });
    await flush();
  }

  it("lists an instance next to the built-ins with its extends, config dir, source line and a Test button", async () => {
    await renderWithInstances();
    expect(screen.getAllByTestId("provider-nav-item-claude-work")[0]).toHaveTextContent("Claude work");
    await selectProvider("claude-work");
    const detail = screen.getByTestId("provider-detail");
    expect(detail).toHaveTextContent("Instance of Claude Code");
    const dir = screen.getByTestId("provider-custom-dir-claude-work");
    expect(dir).toHaveTextContent("/home/u/.claude-work");
    expect(dir).toHaveTextContent("set in smind");
    expect(screen.getByTestId("provider-test-claude-work")).toBeInTheDocument();
    // An instance's dir is edited in config.yaml, so no editable env field / default-model picker is offered.
    expect(screen.queryByTestId("provider-env-claude-work")).toBeNull();
    expect(screen.queryByTestId("provider-default-model-claude-work")).toBeNull();
    expect(screen.getByTestId("provider-reload-claude-work")).toBeInTheDocument();
  });

  it("renders the suggestions as one-click chips with the directory and the label they would get", async () => {
    await renderWithInstances();
    expect(screen.queryByTestId("provider-add-instance")).toBeNull();
    await openAddArea();
    const chip = screen.getByTestId("provider-suggestion-claude-spare");
    expect(chip).toHaveTextContent("~/.claude-spare");
    expect(chip).toHaveTextContent("Claude spare");
    expect(screen.getByTestId("provider-suggestion-codex-alt")).toHaveTextContent("Codex alt");
  });

  it("one click calls provider.addInstance once with the suggested params and the instance appears live", async () => {
    const daemon = await renderWithInstances();
    await openAddArea();
    await act(async () => {
      fireEvent.click(screen.getByTestId("provider-suggestion-claude-spare"));
    });
    await flush();
    await flush();
    const adds = daemon.calls.filter((c) => c.method === "provider.addInstance");
    expect(adds).toHaveLength(1);
    expect(adds[0].params).toEqual({ extends: "claude-native", id: "claude-spare", label: "Claude spare", dir: "/home/u/.claude-spare" });
    // provider.updated re-read the catalog: the instance is a nav row; its chip stays in place as "added" (nothing moves).
    expect(screen.getAllByTestId("provider-nav-item-claude-spare")[0]).toHaveTextContent("Claude spare");
    expect(screen.getByTestId("provider-suggestion-claude-spare")).toBeDisabled();
    expect(screen.getByTestId("provider-suggestion-claude-spare")).toHaveAccessibleName("Claude spare added");
    expect(screen.getByTestId("provider-suggestion-codex-alt")).toBeInTheDocument();
    expect(screen.queryByTestId("provider-add-error")).toBeNull();
  });

  it("shows the daemon's refusal inline under the chips, keeps the chip, and adds nothing", async () => {
    const daemon = await renderWithInstances();
    daemon.addInstanceError = new Error("providers.custom[0].id: already in use");
    await openAddArea();
    await act(async () => {
      fireEvent.click(screen.getByTestId("provider-suggestion-claude-spare"));
    });
    await flush();
    expect(screen.getByTestId("provider-add-error")).toHaveTextContent("providers.custom[0].id: already in use");
    expect(screen.getByTestId("provider-suggestion-claude-spare")).toBeEnabled();
    expect(screen.queryByTestId("provider-nav-item-claude-spare")).toBeNull();
  });

  it("the manual form sends the typed extends/id/label/dir for a directory that is not suggested", async () => {
    const daemon = await renderWithInstances();
    await openAddArea();
    await act(async () => {
      fireEvent.click(screen.getByTestId("provider-add-manual-toggle"));
    });
    expect(screen.getByTestId("provider-add-submit")).toBeDisabled();
    fireEvent.click(screen.getByRole("radio", { name: "Codex" }));
    fireEvent.change(screen.getByTestId("provider-add-id"), { target: { value: "codex-lab" } });
    fireEvent.change(screen.getByTestId("provider-add-label"), { target: { value: "Codex lab" } });
    fireEvent.change(screen.getByTestId("provider-add-dir"), { target: { value: "~/labs/codex" } });
    await act(async () => {
      fireEvent.click(screen.getByTestId("provider-add-submit"));
    });
    await flush();
    await flush();
    const adds = daemon.calls.filter((c) => c.method === "provider.addInstance");
    expect(adds.map((c) => c.params)).toEqual([{ extends: "codex-native", id: "codex-lab", label: "Codex lab", dir: "~/labs/codex" }]);
    expect(screen.getAllByTestId("provider-nav-item-codex-lab")[0]).toBeInTheDocument();
  });

  it("does not ask the daemon for suggestions until the Add instance area is opened", async () => {
    const daemon = await renderWithInstances();
    expect(daemon.countCalls("provider.dirSuggestions")).toBe(0);
    await openAddArea();
    expect(daemon.countCalls("provider.dirSuggestions")).toBe(1);
  });

  it("Add from recipe opens its panel and closes the Add instance panel", async () => {
    await renderWithInstances();
    await openAddArea();
    expect(screen.getByTestId("provider-add-instance")).toBeInTheDocument();
    expect(screen.queryByTestId("provider-add-recipe")).toBeNull();
    await act(async () => {
      fireEvent.click(screen.getByTestId("providers-add-recipe-toggle"));
    });
    await flush();
    expect(screen.getByTestId("provider-add-recipe")).toBeInTheDocument();
    expect(screen.queryByTestId("provider-add-instance")).toBeNull();
    expect(screen.getByTestId("providers-add-recipe-toggle")).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByTestId("providers-add-instance-toggle")).toHaveAttribute("aria-expanded", "false");
  });
});
