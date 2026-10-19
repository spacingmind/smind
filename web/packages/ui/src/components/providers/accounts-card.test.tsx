import { act, fireEvent, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { AccountsCard } from "@/components/providers/accounts-card";
import { accountStatus } from "@/lib/account-status";
import { healthBadge, type ProviderNavItem } from "@/lib/providers-view";
import { claudeAccount, problemAccount } from "@/test/account-usage-fixtures";
import { FakeUsageDaemon } from "@/test/usage-test-client";
import { renderWithDaemon } from "@/test/render-with-daemon";
import { useProvidersViewStore } from "@/stores/providers-view-store";
import type { Account, ProviderInfo, RunSummary } from "@/lib/types";

const NOW = Date.parse("2026-10-12T12:00:00Z");

const PROVIDERS: ProviderInfo[] = [
  { id: "claude-native", label: "Claude Code", credentialKind: "oauth", accountProvider: "anthropic" },
];

function account(id: number, provider = "anthropic"): Account {
  return { id, provider, label: `Account ${id}`, credentialType: "oauth", createdAt: "", updatedAt: "" };
}

function makeRow(id: number, usageOverride?: ReturnType<typeof claudeAccount> | undefined) {
  const acct = account(id);
  const usage = usageOverride === undefined ? claudeAccount({ accountId: id, displayName: `Claude ${id}` }, NOW) : usageOverride;
  return { account: acct, usage, test: undefined, status: accountStatus({ account: acct, usage, test: undefined }), badge: healthBadge({ usage, test: undefined }) };
}

const item = (ids: number[]): ProviderNavItem => ({
  id: "claude-native",
  label: "Claude Code",
  kind: "oauth",
  accountProvider: "anthropic",
  accountIds: ids,
  tone: "ok",
  connected: true,
});

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

function run(provider: string, status: RunSummary["Status"]): RunSummary {
  return { ID: "r", TaskID: 0, ChatID: 0, Provider: provider as RunSummary["Provider"], Prompt: "", Status: status, StartedAt: "", FinishedAt: null, StopReason: "", Err: "", PermissionMode: "", AutoAccept: false, ThinkingLevel: "" };
}

describe("AccountsCard", () => {
  beforeEach(() => {
    useProvidersViewStore.getState().reset();
  });

  it("controls appear only with four or more accounts", async () => {
    const d = new FakeUsageDaemon([]);
    d.providers = { providers: PROVIDERS } as never;
    d.accounts = [account(1), account(2), account(3)];
    const { rerenderWith } = renderWithDaemon(
      <AccountsCard item={item([1, 2, 3])} rows={[makeRow(1), makeRow(2), makeRow(3)]} providers={PROVIDERS} allAccounts={[]} client={d.client} onRelogin={() => {}} />,
      d,
    );
    await flush();
    expect(screen.queryByTestId("accounts-search")).toBeNull();
    rerenderWith(
      d,
      <AccountsCard item={item([1, 2, 3, 4])} rows={[makeRow(1), makeRow(2), makeRow(3), makeRow(4)]} providers={PROVIDERS} allAccounts={[]} client={d.client} onRelogin={() => {}} />,
    );
    await flush();
    expect(screen.getByTestId("accounts-search")).toBeInTheDocument();
    expect(screen.queryByTestId("accounts-no-match")).toBeNull();
  });

  it("search and filter narrow the list", async () => {
    const d = new FakeUsageDaemon([]);
    const rows = [makeRow(2), makeRow(3, problemAccount("expired", { accountId: 3 }, NOW)), makeRow(5), makeRow(7)];
    renderWithDaemon(
      <AccountsCard item={item([2, 3, 5, 7])} rows={rows} providers={PROVIDERS} allAccounts={[]} client={d.client} onRelogin={() => {}} />,
      d,
    );
    await flush();
    expect(screen.getByTestId("accounts-list").children).toHaveLength(4);
    await act(async () => {
      fireEvent.change(screen.getByTestId("accounts-search"), { target: { value: "Claude 5" } });
    });
    expect(screen.getByTestId("accounts-list").children).toHaveLength(1);
    expect(within(screen.getByTestId("provider-row-5")).getByTestId("provider-row-dot-5")).toBeInTheDocument();
    await act(async () => {
      fireEvent.change(screen.getByTestId("accounts-search"), { target: { value: "zzz" } });
    });
    expect(screen.queryByTestId("accounts-list")).toBeNull();
    expect(screen.getByTestId("accounts-no-match")).toHaveTextContent("No accounts match.");
  });

  it("most headroom is marked per provider", async () => {
    const d = new FakeUsageDaemon([]);
    const rows = [
      makeRow(2), // 31% used, wins the id tiebreak against row 3
      makeRow(3, claudeAccount({ accountId: 3, displayName: "Claude 3" }, NOW)),
      makeRow(5, claudeAccount({ accountId: 5, displayName: "Claude 5", report: { ...claudeAccount({}, NOW).report!, windows: [{ id: "five_hour", label: "5-hour", summary: true, usedPct: 90, remainingPct: 10 }] } }, NOW)),
    ];
    renderWithDaemon(
      <AccountsCard item={item([2, 3, 5])} rows={rows} providers={PROVIDERS} allAccounts={rows.map((r) => r.account)} client={d.client} onRelogin={() => {}} />,
      d,
    );
    await flush();
    expect(within(screen.getByTestId("provider-row-2")).getByTestId("provider-row-headroom")).toBeInTheDocument();
    expect(within(screen.getByTestId("provider-row-3")).queryByTestId("provider-row-headroom")).toBeNull();
    expect(within(screen.getByTestId("provider-row-5")).queryByTestId("provider-row-headroom")).toBeNull();
  });

  it("remove asks for confirmation with the failover warning", async () => {
    const d = new FakeUsageDaemon([]);
    d.providers = { providers: PROVIDERS } as never;
    d.accounts = [account(1), account(2)];
    d.runs = [run("claude-native", "running")];
    const rows = [makeRow(1), makeRow(2)];
    renderWithDaemon(
      <AccountsCard item={item([1, 2])} rows={rows} providers={PROVIDERS} allAccounts={rows.map((r) => r.account)} client={d.client} onRelogin={() => {}} />,
      d,
    );
    await flush();
    fireEvent.pointerDown(screen.getByTestId("provider-row-menu-2"), { button: 0, ctrlKey: false });
    fireEvent.click(await screen.findByTestId("provider-row-remove-2"));
    expect(await screen.findByTestId("provider-remove-warning-2")).toHaveTextContent(
      "1 running task will switch to another account",
    );
  });
});
