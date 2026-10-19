import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AppSidebar } from "@/components/app-sidebar";
import { SidebarProvider } from "@/components/ui/sidebar";
import { STORAGE_KEYS } from "@/lib/storage";
import { claudeAccount, usageResult } from "@/test/account-usage-fixtures";
import { DaemonQueryProvider } from "@/lib/query/daemon-query-provider";
import { createQueryClient } from "@/lib/query/query-client";
import "@/test/query-scheduler";
import { RpcError } from "@/lib/ws-client";
import { FakeWsClient } from "@/test/fake-ws-client";
import { reloadAllPreferenceStores } from "@/stores/create-preference-store";

function renderSidebar(client: FakeWsClient, open = true) {
  return render(
    <DaemonQueryProvider queryClient={createQueryClient()} client={client as never} events={null}>
      <SidebarProvider defaultOpen={open}>
        <AppSidebar client={client as never} selectedTaskId={null} />
      </SidebarProvider>
    </DaemonQueryProvider>,
  );
}

const usageCalls = (client: FakeWsClient) => client.calls.filter((c) => c.method === "account.usage");

beforeEach(() => {
  window.localStorage.clear();
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
});
afterEach(() => vi.restoreAllMocks());

describe("AppSidebar plan-usage summary (ADR-0028 PR-D2)", () => {
  it("default off: no footer row and not a single account.usage request", async () => {
    const client = new FakeWsClient();
    renderSidebar(client);
    await act(async () => {});
    expect(screen.queryByTestId("sidebar-footer-rows")).not.toBeInTheDocument();
    expect(screen.queryByTestId("sidebar-usage-summary")).not.toBeInTheDocument();
    expect(screen.getByTestId("sidebar-footer-usage")).toBeInTheDocument(); // the icon stays
    expect(usageCalls(client)).toHaveLength(0);
  });

  it("on: the footer row appears above the icon line and sends exactly one touch", async () => {
    window.localStorage.setItem(STORAGE_KEYS.usage, JSON.stringify({ displayAs: "used", accountKey: null, inSidebar: true, pins: null }));
    reloadAllPreferenceStores(); // the usage preferences are a store now: re-read what the test just seeded
    const client = new FakeWsClient();
    renderSidebar(client);
    await act(async () => {});
    expect(usageCalls(client)).toHaveLength(1);
    expect(usageCalls(client)[0].params).toEqual({ mode: "touch" });
    await act(async () => usageCalls(client)[0].resolve(usageResult([claudeAccount()])));
    const rows = screen.getByTestId("sidebar-footer-rows");
    expect(rows).toContainElement(screen.getByTestId("sidebar-usage-summary"));
    expect(screen.getByTestId("sidebar-usage-summary")).toHaveTextContent("31%");
    expect(usageCalls(client)).toHaveLength(1);
  });

  it("on but the icon-collapsed rail: no row and no request", async () => {
    window.localStorage.setItem(STORAGE_KEYS.usage, JSON.stringify({ inSidebar: true }));
    reloadAllPreferenceStores(); // the usage preferences are a store now: re-read what the test just seeded
    const client = new FakeWsClient();
    renderSidebar(client, false);
    await act(async () => {});
    expect(screen.queryByTestId("sidebar-usage-summary")).not.toBeInTheDocument();
    expect(usageCalls(client)).toHaveLength(0);
  });

  it("on with a daemon that has no account.usage: the empty rows wrapper hides itself", async () => {
    window.localStorage.setItem(STORAGE_KEYS.usage, JSON.stringify({ inSidebar: true }));
    reloadAllPreferenceStores(); // the usage preferences are a store now: re-read what the test just seeded
    const client = new FakeWsClient();
    renderSidebar(client);
    await act(async () => {});
    await act(async () => usageCalls(client)[0].reject(new RpcError("method not found: account.usage")));
    expect(screen.queryByTestId("sidebar-usage-summary")).not.toBeInTheDocument();
    const rows = screen.getByTestId("sidebar-footer-rows");
    expect(rows).toBeEmptyDOMElement();
    expect(rows.className).toContain("empty:hidden");
  });
});
