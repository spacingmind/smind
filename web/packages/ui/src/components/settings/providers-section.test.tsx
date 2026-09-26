import { act } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { groupAccountsByProvider } from "@/components/settings/providers-section";
import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/providers-section";
import type { ProviderInfo } from "@/lib/types";
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

function renderSection(client: FakeWsClient) {
  const section = listSettingsSections().find((s) => s.id === "providers");
  if (!section) throw new Error("providers section did not register");
  render(<>{section.render({ client: client as never })}</>);
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
