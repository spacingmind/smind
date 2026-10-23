import { act } from "react";
import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/providers-section";
import { claudeAccount, problemAccount } from "@/test/account-usage-fixtures";
import { FakeUsageDaemon } from "@/test/usage-test-client";
import { renderWithDaemon } from "@/test/render-with-daemon";
import type { Account } from "@/lib/types";

/**
 * ADR-0032 AC8/AC9: Providers reads the same cache as Plan usage, so the
 * surfaces cannot disagree anymore, and reading Providers never spends the
 * usage call budget (ADR-0028).
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

function renderProviders(d: FakeUsageDaemon) {
  const section = listSettingsSections().find((s) => s.id === "providers");
  if (!section) throw new Error("providers section did not register");
  return renderWithDaemon(<>{section.render({ client: d.client, events: d.events })}</>, d);
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("providers cache integration", () => {
  it("expired cached report shows expired in providers", async () => {
    const d = daemon([account(1, "anthropic", "work")], [problemAccount("expired")]);
    renderProviders(d);
    await flush();

    // The danger state comes from the shared usage cache, not from a provider.test of the account.
    // The detail's run-readiness line asks provider.test (local, network-free) once per key on mount; the account row's
    // own test is never run by a mount.
    expect(d.calls.filter((c) => c.method === "provider.test" && (c.params as { provider: string }).provider === "anthropic")).toHaveLength(1);
    const dot = screen.getByTestId("provider-row-dot-1");
    expect(dot).toHaveAttribute("data-status", "failed");
    expect(dot).toHaveAttribute("title", "Needs re-login");
    expect(screen.getByTestId("provider-row-health-1")).toHaveTextContent("Needs re-login");
    expect(screen.getByTestId("provider-row-test-result-1")).toHaveTextContent(
      /Login expired.*use Re-login\./,
    );
    expect(screen.getByTestId("provider-row-test-result-1")).not.toHaveTextContent(/Reconnect it in Providers/);
  });

  it("providers surface performs zero usage refreshes", async () => {
    const d = daemon([account(1, "anthropic", "work")]);
    renderProviders(d);
    await flush();

    // Mounting reads the daemon's cached list once and nothing else; no touch, no refresh.
    expect(d.count("touch")).toBe(0);
    expect(d.count("refresh")).toBe(0);
    expect(d.count("cached")).toBeLessThanOrEqual(1);
    const testsOnMount = d.countCalls("provider.test");

    fireEvent.click(screen.getByTestId("provider-test-anthropic"));
    await flush();
    expect(d.countCalls("provider.test")).toBe(testsOnMount + 1);
    expect(d.count("touch")).toBe(0);
    expect(d.count("refresh")).toBe(0);
  });
});
