import { act } from "react";
import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/providers-section";
import { ChatPlanUsage } from "@/components/account-usage/chat-plan-usage";
import { UsageList } from "@/components/account-usage/usage-list";
import { claudeAccount } from "@/test/account-usage-fixtures";
import { FakeUsageDaemon } from "@/test/usage-test-client";
import { renderWithDaemon } from "@/test/render-with-daemon";
import { useChatPlanUsage } from "@/hooks/use-chat-plan-usage";
import type { Account } from "@/lib/types";

/**
 * ADR-0032 AC9: one `account.updated` event renames the account on every
 * surface at once (Providers, a usage card, the chat pill) — the shared
 * cache plus the event bridge, with no remount and no refetch.
 */

const PROVIDERS = {
  providers: [
    { id: "claude-native" as const, label: "Claude Code", credentialKind: "oauth" as const, accountProvider: "anthropic" },
  ],
};

function account(id: number, provider: string, label: string): Account {
  return { id, provider, label, credentialType: "oauth", createdAt: "", updatedAt: "" };
}

function daemon(accounts: Account[], reports = [claudeAccount()]): FakeUsageDaemon {
  const d = new FakeUsageDaemon(reports);
  d.accounts = accounts;
  d.providers = PROVIDERS;
  return d;
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** The chat pill: ChatPlanUsage over useChatPlanUsage, as the usage hover card mounts it. */
function ChatPill({ chatProvider }: { chatProvider: string }) {
  return <ChatPlanUsage plan={useChatPlanUsage(chatProvider)} />;
}

/** A UsageList-shaped consumer: the usage card list as the modal/Settings render it. */
function UsageListView() {
  // Reading via the chat pill hook keeps one connection to the shared cache;
  // the list itself maps reports onto cards.
  const { snapshot, displayAs } = useChatPlanUsage("claude-native");
  return (
    <UsageList
      reports={[...snapshot.reports]}
      displayAs={displayAs}
      refreshing={snapshot.refreshing}
      failed={snapshot.failed}
      pressedAt={snapshot.pressedAt}
      onRefreshOne={() => {}}
    />
  );
}

describe("account.updated across surfaces", () => {
  it("account.updated renames the account in providers, usage card and chat pill", async () => {
    const d = daemon([account(1, "anthropic", "work")]);
    const section = listSettingsSections().find((s) => s.id === "providers")!;
    renderWithDaemon(
      <>
        {section.render({ client: d.client, events: d.events })}
        <UsageListView />
        <ChatPill chatProvider="claude-native" />
      </>,
      d,
    );
    await flush();

    // All three surfaces show the pre-rename name.
    expect(screen.getAllByText("Claude").length).toBeGreaterThan(0);
    expect(screen.queryByText("Renamed")).not.toBeInTheDocument();

    // The daemon renames the account: its list and its usage displayName both change,
    // then it pushes account.updated.
    const renamed = account(1, "anthropic", "Renamed");
    d.accounts = [renamed];
    d.reports = [claudeAccount({ displayName: "Renamed" })];
    await act(async () => {
      d.emit("account.updated", { account: renamed });
    });
    await flush();

    // Providers' row shows the new displayName (AC58), so does the usage card and the pill.
    expect(screen.getByTestId("provider-row-1")).toHaveTextContent("Renamed");
    expect(screen.getByTestId("usage-report-1")).toHaveTextContent("Renamed");
    // The chat pill's compact card carries the new name too.
    expect(screen.getByTestId("plan-usage-1")).toHaveTextContent("Renamed");
    expect(screen.queryByText("Claude")).not.toBeInTheDocument();
  });

  it("a newly added account appears in plan usage without reload", async () => {
    const d = daemon([account(1, "anthropic", "work")]);
    renderWithDaemon(<UsageListView />, d);
    await flush();
    expect(screen.getByTestId("usage-report-1")).toBeInTheDocument();
    expect(screen.queryByTestId("usage-report-2")).not.toBeInTheDocument();

    // A second account appears in account.list and gets its first usage report;
    // the event bridge invalidates the usage query, whose re-read is cached-only.
    d.accounts = [account(1, "anthropic", "work"), account(2, "anthropic", "alt")];
    const second = claudeAccount({ accountId: 2, key: "anthropic-2", displayName: "alt" });
    d.reports = [claudeAccount(), second];
    await act(async () => {
      d.emit("account.updated", { account: account(2, "anthropic", "alt") });
    });
    await flush();

    expect(await screen.findByTestId("usage-report-2")).toBeInTheDocument();
    expect(screen.getByTestId("usage-report-2")).toHaveTextContent("alt");
    // The re-read stayed cached-only: no budget was spent.
    expect(d.count("touch")).toBe(0);
    expect(d.count("refresh")).toBe(0);
  });
});
