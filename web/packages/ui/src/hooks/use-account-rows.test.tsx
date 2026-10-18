import { act } from "react";
import { describe, expect, it } from "vitest";

import { useAccountRows } from "@/hooks/use-account-rows";
import { FakeUsageDaemon } from "@/test/usage-test-client";
import { claudeAccount } from "@/test/account-usage-fixtures";
import { renderHookWithDaemon } from "@/test/render-with-daemon";
import type { Account } from "@/lib/types";

function account(id: number, provider: string, label: string): Account {
  return { id, provider, label, credentialType: "oauth", createdAt: "", updatedAt: "" };
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("useAccountRows", () => {
  it("account rows combine accounts, cached usage and cached test results", async () => {
    const d = new FakeUsageDaemon([claudeAccount()]);
    d.accounts = [account(1, "anthropic", "main"), account(2, "xai", "grok")];
    d.testResults = { anthropic: { ok: false, detail: "401" } };
    const view = renderHookWithDaemon((_: null) => useAccountRows(), d, { initialProps: null });
    await flush();
    // A Test button's result already in the shared cache: rows only read it.
    await act(async () => {
      view.queryClient().setQueryData(["provider.test", "anthropic"], { ok: false, detail: "401" });
    });

    const { rows, loaded } = view.result.current;
    expect(loaded).toBe(true);
    expect(rows.map((r) => r.account.id)).toEqual([1, 2]);
    const main = rows[0];
    expect(main.usage?.accountId).toBe(1);
    expect(main.test).toEqual({ ok: false, detail: "401" });
    expect(main.badge.kind).toBe("error");
    expect(main.status.tone).toBe("danger");
    expect(rows[1].test).toBeUndefined();
    expect(rows[1].badge.kind).toBe("untested");
    // provider.test is only ever a cache read here: no RPC.
    expect(d.countCalls("provider.test")).toBe(0);
  });

  it("account rows never trigger a usage refresh or touch", async () => {
    const d = new FakeUsageDaemon([claudeAccount()]);
    d.accounts = [account(1, "anthropic", "main")];
    renderHookWithDaemon((_: null) => useAccountRows(), d, { initialProps: null });
    await flush();
    await flush();

    expect(d.countCalls("account.usage")).toBe(0);
    expect(d.requests.length).toBe(1);
    expect(d.requests.every((r) => r.mode === "cached")).toBe(true);
    expect(d.count("touch")).toBe(0);
    expect(d.count("refresh")).toBe(0);
  });

  it("account rows are not loaded until the list arrives", () => {
    const d = new FakeUsageDaemon([claudeAccount()]);
    const view = renderHookWithDaemon((_: null) => useAccountRows(), d, { initialProps: null });
    expect(view.result.current.loaded).toBe(false);
    expect(view.result.current.rows).toEqual([]);
  });
});
