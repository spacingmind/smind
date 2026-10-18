import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { AccountRow } from "@/components/providers/account-row";
import type { AccountRowData } from "@/hooks/use-account-rows";
import { accountStatus } from "@/lib/account-status";
import { healthBadge } from "@/lib/providers-view";
import { claudeAccount, problemAccount } from "@/test/account-usage-fixtures";
import { FakeUsageDaemon } from "@/test/usage-test-client";
import { renderWithDaemon } from "@/test/render-with-daemon";
import type { Account } from "@/lib/types";

const NOW = Date.parse("2026-10-12T12:00:00Z");

function acct(id: number, provider = "anthropic", credentialType: Account["credentialType"] = "oauth"): Account {
  return { id, provider, label: `Account ${id}`, credentialType, createdAt: "", updatedAt: "" };
}

function row(over: Partial<AccountRowData> = {}): AccountRowData {
  const account = over.account ?? acct(1);
  const usage = "usage" in over ? over.usage : claudeAccount({ accountId: account.id }, NOW);
  const test = "test" in over ? over.test : undefined;
  return {
    account,
    usage,
    test,
    status: accountStatus({ account, usage, test }),
    badge: healthBadge({ usage, test }),
    ...over,
  } as AccountRowData;
}

const noop = () => {};
const refresh = { state: "idle" as const, onRefresh: noop };
const daemon = () => new FakeUsageDaemon([]);

function element(props: { data: AccountRowData; mostHeadroom?: boolean; onTest?: (p: string) => void; onRelogin?: (a: Account) => void; action?: AccountRowData extends never ? never : import("@/components/providers/account-actions").RowAction | null }) {
  const { data, mostHeadroom = false, onTest = noop, onRelogin = noop, action = null } = props;
  return (
    <AccountRow
      data={data}
      mostHeadroom={mostHeadroom}
      displayAs="used"
      testing={false}
      onTest={onTest}
      refresh={refresh}
      action={action}
      onAction={noop}
      onRelogin={onRelogin}
    />
  );
}

describe("AccountRow", () => {
  it("row shows the name, credential badge, plan and health badge", () => {
    renderWithDaemon(element({ data: row() }), daemon());
    expect(screen.getByTestId("provider-row-1")).toHaveTextContent("Claude");
    expect(screen.getByTestId("provider-row-1")).toHaveTextContent("OAuth");
    expect(screen.getByTestId("provider-row-1")).toHaveTextContent("max");
    expect(screen.getByTestId("provider-row-health-1")).toHaveTextContent("Active");
  });

  it("row shows relogin only for oauth accounts needing it", () => {
    const expired = row({ usage: problemAccount("expired", { accountId: 1 }, NOW) });
    const { rerenderWith } = renderWithDaemon(element({ data: expired }), daemon());
    const onRelogin2 = vi.fn();
    rerenderWith(daemon(), element({ data: expired, onRelogin: onRelogin2 }));
    expect(screen.getByTestId("provider-row-health-1")).toHaveTextContent("Needs re-login");
    fireEvent.click(screen.getByTestId("provider-row-relogin-1"));
    expect(onRelogin2).toHaveBeenCalledWith(expired.account);
    rerenderWith(
      daemon(),
      element({ data: row({ account: acct(1, "anthropic", "api-key"), usage: problemAccount("rejected", { accountId: 1 }, NOW) }) }),
    );
    expect(screen.queryByTestId("provider-row-relogin-1")).toBeNull();
  });

  it("row shows most headroom pill", () => {
    renderWithDaemon(element({ data: row(), mostHeadroom: true }), daemon());
    expect(screen.getByTestId("provider-row-headroom")).toHaveTextContent("Most headroom");
  });

  it("row test button calls onTest with the account provider", () => {
    const onTest = vi.fn();
    renderWithDaemon(element({ data: row(), onTest }), daemon());
    fireEvent.click(screen.getByTestId("provider-test-anthropic"));
    expect(onTest).toHaveBeenCalledWith("anthropic");
  });

  it("row rename and credential forms replace or extend the row", () => {
    const data = row();
    const { rerenderWith } = renderWithDaemon(
      element({ data: data, action: { kind: "rename", account: data.account } }),
      daemon(),
    );
    expect(screen.getByTestId("provider-rename-form-1")).toBeInTheDocument();
    expect(screen.queryByTestId("provider-row-health-1")).toBeNull();
    rerenderWith(
      daemon(),
      element({ data: data, action: { kind: "credential", account: data.account } }),
    );
    expect(screen.getByTestId("provider-credential-form-1")).toBeInTheDocument();
    expect(screen.getByTestId("provider-row-health-1")).toBeInTheDocument();
  });

  it("row menu offers rename, update credential and remove", async () => {
    const data = row();
    renderWithDaemon(element({ data: data }), daemon());
    fireEvent.pointerDown(screen.getByTestId("provider-row-menu-1"), { button: 0, ctrlKey: false });
    expect(await screen.findByTestId("provider-row-rename-1")).toBeInTheDocument();
    expect(screen.getByTestId("provider-row-update-credential-1")).toBeInTheDocument();
    expect(screen.getByTestId("provider-row-remove-1")).toBeInTheDocument();
  });
});
