import { act } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { listSettingsSections } from "@/components/settings/settings-registry";
import { SectionTitleInHeaderProvider } from "@/components/settings/settings-title";
import "@/components/settings/usage-section";
import { claudeAccount, codexAccount } from "@/test/account-usage-fixtures";
import { renderWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

function usageSection() {
  const section = listSettingsSections().find((s) => s.id === "usage");
  if (!section) throw new Error("usage section did not register");
  return section;
}

function renderSection(daemon: FakeUsageDaemon, wrap: (node: React.ReactNode) => React.ReactNode = (n) => n) {
  return renderWithDaemon(<>{wrap(usageSection().render({ client: daemon.client, events: daemon.events }))}</>, daemon);
}

/** Test doubles of the two Settings containers: the in-shell layout, and the full-screen screen's framed view. */
const IN_SHELL = (node: React.ReactNode) => <div style={{ width: 640 }}>{node}</div>;
const FULL_SCREEN = (node: React.ReactNode) => (
  <SectionTitleInHeaderProvider value={true}>
    <div data-testid="framed-content-view" className="mx-auto max-w-2xl">
      {node}
    </div>
  </SectionTitleInHeaderProvider>
);
const NARROW_DETAIL = (node: React.ReactNode) => (
  <SectionTitleInHeaderProvider value={true}>
    <div data-testid="detail-page" style={{ width: 390 }}>
      {node}
    </div>
  </SectionTitleInHeaderProvider>
);

beforeEach(() => {
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
});
afterEach(() => vi.restoreAllMocks());

describe("Settings -> Usage: Plan usage", () => {
  it("settings_usage_renders_every_account_returned", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    renderSection(daemon);
    await flush();
    expect(screen.getByTestId("usage-report-1")).toBeInTheDocument();
    expect(screen.getByTestId("usage-report-2")).toBeInTheDocument();
  });

  it("plan_usage_block_sits_above_token_usage_and_token_table_is_unchanged", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    renderSection(daemon);
    await flush();
    const plan = screen.getByTestId("settings-plan-usage");
    const tokens = screen.getByTestId("settings-token-usage");
    expect(plan.compareDocumentPosition(tokens) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(within(plan).getByRole("heading", { name: "Plan usage" })).toBeInTheDocument();
    expect(within(tokens).getByRole("heading", { name: "Token usage" })).toBeInTheDocument();
    // The token block keeps its own controls and ids.
    expect(within(tokens).getByTestId("usage-refresh")).toBeInTheDocument();
    expect(screen.getByTestId("settings-usage-scope-runs")).toBeInTheDocument();
    expect(screen.getByTestId("settings-section-usage")).toBeInTheDocument();
  });

  it("settings_control_is_visible_above_the_reports (labelled, in the block body)", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    renderSection(daemon);
    await flush();
    const plan = screen.getByTestId("settings-plan-usage");
    const control = within(plan).getByRole("button", { name: "Usage settings" });
    expect(control).toHaveTextContent("Settings");
    expect(control.compareDocumentPosition(screen.getByTestId("usage-report-1")) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("settings_usage_controls_live_in_the_section_body_not_the_screen_header", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    renderSection(daemon, FULL_SCREEN);
    await flush();
    const body = screen.getByTestId("framed-content-view");
    expect(within(body).getByRole("button", { name: "Refresh all" })).toBeInTheDocument();
    expect(within(body).getByRole("button", { name: "Usage settings" })).toBeInTheDocument();
    expect(within(body).getByRole("combobox", { name: "Account" })).toBeInTheDocument();
  });

  it.each([
    ["the in-shell container", IN_SHELL],
    ["the full-screen settings container", FULL_SCREEN],
    ["the narrow detail page", NARROW_DETAIL],
  ])("settings_usage_section_is_registered_and_renders_in %s", async (_name, wrap) => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    renderSection(daemon, wrap);
    await flush();
    // Both blocks keep a heading even where the screen header already names the section.
    expect(screen.getByRole("heading", { name: "Plan usage" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Token usage" })).toBeInTheDocument();
    expect(screen.getByTestId("usage-report-1")).toBeInTheDocument();
    expect(screen.getByTestId("settings-section-usage")).toBeInTheDocument();
  });

  it("refresh_all_forces_a_fresh_report (exactly one forced request; plain loads never force)", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    renderSection(daemon);
    await flush();
    expect(daemon.count("refresh")).toBe(0);
    daemon.onRequest = (p) =>
      p.mode === "refresh"
        ? [claudeAccount({ report: { ...claudeAccount().report!, fetchedAt: "2026-10-12T12:20:00Z", windows: [{ id: "five_hour", label: "5-hour", usedPct: 64, remainingPct: 36 }] } })]
        : undefined;
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Refresh all" }));
    });
    await flush();
    expect(daemon.count("refresh")).toBe(1);
    expect(screen.getByText("64%")).toBeInTheDocument();
  });

  it("opening Settings -> Usage sends one touch and nothing else", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    renderSection(daemon);
    await flush();
    expect(daemon.modes()).toEqual(["touch"]);
  });

  it("asks_to_update_a_daemon_without_account_usage_and_never_calls_it_again", async () => {
    const daemon = new FakeUsageDaemon([]);
    daemon.makeOld();
    renderSection(daemon);
    await flush();
    expect(screen.getByTestId("usage-unavailable")).toHaveTextContent("Update the daemon to see plan usage");
    expect(daemon.requests).toHaveLength(1);
    expect(screen.queryByRole("button", { name: "Refresh all" })).not.toBeInTheDocument();
  });

  it("polling disabled shows the disabled message in the block", async () => {
    const daemon = new FakeUsageDaemon([]);
    daemon.enabled = false;
    renderSection(daemon);
    await flush();
    expect(within(screen.getByTestId("settings-plan-usage")).getByTestId("usage-disabled")).toBeInTheDocument();
  });

  it("a section with no client makes no account.usage call", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    render(<>{usageSection().render({ client: null, events: null })}</>);
    await flush();
    expect(daemon.requests).toHaveLength(0);
  });
});
