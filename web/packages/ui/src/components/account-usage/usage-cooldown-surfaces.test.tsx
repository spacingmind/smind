import { act, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { UsageModalRoot } from "@/components/account-usage/usage-modal";
import { listSettingsSections } from "@/components/settings/settings-registry";
import { SectionTitleInHeaderProvider } from "@/components/settings/settings-title";
import "@/components/settings/usage-section";
import { closeUsageModal, openUsageModal } from "@/hooks/use-usage-modal";
import type { UsageCooldown } from "@/lib/account-usage/types";
import { claudeAccount, codexAccount } from "@/test/account-usage-fixtures";
import { FakeUsageDaemon } from "@/test/usage-test-client";

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

const cooldown = (over: Partial<UsageCooldown> = {}): UsageCooldown => ({
  until: new Date(Date.now() + 10 * 60_000).toISOString(),
  scope: "model",
  model: "claude-opus-4-with-a-rather-long-model-identifier-for-wrapping",
  reason: "fallback",
  window: "weekly_model",
  ...over,
});

function daemon(): FakeUsageDaemon {
  return new FakeUsageDaemon([
    claudeAccount({ displayName: "A very long account display name for the narrow container", cooldown: cooldown() }),
    codexAccount(),
  ]);
}

function expectCooldownOnlyOnAccountOne(): void {
  const box = within(screen.getByTestId("usage-report-1")).getByTestId("usage-cooldown");
  expect(box).toHaveTextContent("Rate limited");
  expect(box).toHaveTextContent("weekly limit for claude-opus-4-with-a-rather-lo");
  expect(box).toHaveTextContent("reset time unknown");
  expect(box).toHaveTextContent("seen on a proxied request");
  // The windows still render under it.
  expect(within(screen.getByTestId("usage-report-1")).getByTestId("usage-window-five_hour")).toHaveTextContent("31%");
  expect(within(screen.getByTestId("usage-report-2")).queryByTestId("usage-cooldown")).toBeNull();
}

function settingsSection() {
  const section = listSettingsSections().find((s) => s.id === "usage");
  if (!section) throw new Error("usage section did not register");
  return section;
}

beforeEach(() => {
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
  Object.defineProperty(window, "innerWidth", { configurable: true, writable: true, value: 1440 });
});
afterEach(() => {
  act(() => closeUsageModal());
  vi.restoreAllMocks();
});

describe("cooldown on the usage surfaces", () => {
  it("usage_modal_and_settings_render_cooldown (both containers)", async () => {
    // The modal.
    const modalDaemon = daemon();
    const modal = render(<UsageModalRoot client={modalDaemon.client} events={modalDaemon.events} />);
    act(() => openUsageModal());
    await flush();
    expect(screen.getByTestId("usage-modal")).toBeInTheDocument();
    expectCooldownOnlyOnAccountOne();
    modal.unmount();
    act(() => closeUsageModal());

    // Settings, in the in-shell layout and in a narrow detail page.
    for (const wrap of [
      (node: React.ReactNode) => <div style={{ width: 640 }}>{node}</div>,
      (node: React.ReactNode) => (
        <SectionTitleInHeaderProvider value={true}>
          <div data-testid="detail-page" style={{ width: 360 }}>
            {node}
          </div>
        </SectionTitleInHeaderProvider>
      ),
    ]) {
      const settingsDaemon = daemon();
      const view = render(<>{wrap(settingsSection().render({ client: settingsDaemon.client, events: settingsDaemon.events }))}</>);
      await flush();
      expectCooldownOnlyOnAccountOne();
      view.unmount();
    }
  });

  it("a cooldown event shows on an open surface and clears when the daemon sends none", async () => {
    const d = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    render(<UsageModalRoot client={d.client} events={d.events} />);
    act(() => openUsageModal());
    await flush();
    expect(screen.queryByTestId("usage-cooldown")).toBeNull();
    act(() => d.emit("account.usage", { usage: claudeAccount({ cooldown: cooldown() }) }));
    await flush();
    expect(within(screen.getByTestId("usage-report-1")).getByTestId("usage-cooldown")).toBeInTheDocument();
    act(() => d.emit("account.usage", { usage: claudeAccount({ cooldown: undefined }) }));
    await flush();
    expect(screen.queryByTestId("usage-cooldown")).toBeNull();
  });
});
