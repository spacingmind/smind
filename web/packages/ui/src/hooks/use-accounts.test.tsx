import { act } from "react";
import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { FakeUsageDaemon } from "@/test/usage-test-client";
import { claudeAccount } from "@/test/account-usage-fixtures";
import { renderHookWithDaemon, renderWithDaemon } from "@/test/render-with-daemon";
import { useAccountStatus, useAccounts, useProviderTestResult, useRunProviderTest } from "@/hooks/use-accounts";
import type { Account } from "@/lib/types";

function account(id: number, provider: string, label: string): Account {
  return { id, provider, label, credentialType: "oauth", createdAt: "", updatedAt: "" };
}

function daemon(accounts: Account[]) {
  const d = new FakeUsageDaemon([claudeAccount()]);
  d.accounts = accounts;
  return d;
}

const PROVIDERS = { providers: [{ id: "glm" as const, label: "GLM", kind: "cli" as const }] };

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("useAccounts", () => {
  it("loads the list once and reflects an account.updated rename in the cache", async () => {
    const d = daemon([account(1, "anthropic", "main")]);
    d.providers = PROVIDERS;
    const view = renderHookWithDaemon((_: null) => useAccounts(), d, { initialProps: null });
    expect(view.result.current.data).toBeUndefined();
    await flush();
    expect(view.result.current.data).toEqual([account(1, "anthropic", "main")]);
    expect(d.countCalls("account.list")).toBe(1);

    const renamed = account(1, "anthropic", "renamed");
    await act(async () => {
      d.emit("account.updated", { account: renamed });
    });
    expect(view.result.current.data).toEqual([renamed]);
    // The event wrote the cache; nothing re-read the list.
    expect(d.countCalls("account.list")).toBe(1);
  });
});

describe("provider.test cache", () => {
  it("a test result persists across unmount/remount of a consumer on the same QueryClient", async () => {
    const d = daemon([account(1, "anthropic", "main")]);
    const queryClient = new (require("@tanstack/react-query").QueryClient)();
    function Probe() {
      const t = useProviderTestResult("anthropic");
      const { runTest, isTesting } = useRunProviderTest();
      return (
        <div>
          <span data-testid="result">{t?.detail ?? "none"}</span>
          <button data-testid="run" onClick={() => runTest("anthropic")} />
          <span data-testid="testing">{String(isTesting("anthropic"))}</span>
        </div>
      );
    }
    const first = renderWithDaemon(<Probe />, d, { queryClient });
    await flush();
    expect(screen.getByTestId("result").textContent).toBe("none");
    expect(d.countCalls("provider.test")).toBe(0);
    await act(async () => {
      screen.getByTestId("run").click();
    });
    await flush();
    expect(d.countCalls("provider.test")).toBe(1);
    expect(screen.getByTestId("result").textContent).toBe("ok");
    first.unmount();

    const second = renderWithDaemon(<Probe />, d, { queryClient });
    await flush();
    expect(screen.getByTestId("result").textContent).toBe("ok");
    expect(d.countCalls("provider.test")).toBe(1);
    second.unmount();
  });

  it("provider.test is called only on press: zero calls on mount", async () => {
    const d = daemon([account(1, "anthropic", "main")]);
    const view = renderHookWithDaemon((_: null) => useRunProviderTest(), d, { initialProps: null });
    await flush();
    expect(d.countCalls("provider.test")).toBe(0);
    await act(async () => {
      view.result.current.runTest("anthropic");
    });
    await flush();
    expect(d.countCalls("provider.test")).toBe(1);
  });
});

describe("useAccountStatus", () => {
  it("joins the account list, the usage cache and the test result; undefined for an unknown id", async () => {
    const d = daemon([account(1, "anthropic", "main")]);
    const view = renderHookWithDaemon((id: number) => useAccountStatus(id), d, { initialProps: 1 });
    await flush();
    expect(view.result.current?.name).toBe("Claude");
    expect(view.result.current?.label).toBe("Connected");
    view.unmount();
    const unknown = renderHookWithDaemon((id: number) => useAccountStatus(id), d, {
      initialProps: 99,
      queryClient: view.queryClient(),
    });
    await flush();
    expect(unknown.result.current).toBeUndefined();
  });
});
