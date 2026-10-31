import { act, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ProviderDetail } from "@/components/providers/provider-detail";
import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/usage-section";
import type { ProviderUsageRow } from "@/lib/account-usage/provider-usage-types";
import type { ProviderNavItem } from "@/lib/providers-view";
import type { ProviderListResult } from "@/lib/types";
import { RpcError } from "@/lib/ws-client";
import { useProvidersViewStore } from "@/stores/providers-view-store";
import { claudeAccount } from "@/test/account-usage-fixtures";
import { renderWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 6; i++) await Promise.resolve();
  });
}

/** A Z.AI instance row: the five_hour / weekly windows of a claude fixture, keyed by instance. */
function zaiRow(instanceId: string): ProviderUsageRow {
  const base = claudeAccount();
  return { ...base, accountId: 0, key: instanceId, provider: "zai", displayName: "GLM Coding", instanceId };
}

function renderSettings(d: FakeUsageDaemon) {
  const section = listSettingsSections().find((s) => s.id === "usage");
  if (!section) throw new Error("usage section did not register");
  return renderWithDaemon(<>{section.render({ client: d.client, events: d.events })}</>, d);
}

const NAV: ProviderNavItem = { id: "glm-work", label: "GLM work", kind: "cli", accountIds: [], tone: "neutral", connected: false };

function detailDaemon(providers: unknown[], rows: ProviderUsageRow[]): FakeUsageDaemon {
  const d = new FakeUsageDaemon([]);
  d.providers = { providers } as unknown as ProviderListResult;
  d.providerUsageRows = rows;
  return d;
}

beforeEach(() => {
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
  useProvidersViewStore.getState().reset();
});
afterEach(() => vi.restoreAllMocks());

describe("TestPlanUsage", () => {
  it("Settings Usage lists a provider instance with five_hour and weekly bars", async () => {
    const d = new FakeUsageDaemon([claudeAccount()]);
    d.providerUsageRows = [zaiRow("glm-work")];
    renderSettings(d);
    await flush();
    const card = screen.getByTestId("provider-usage-glm-work");
    expect(card).toBeInTheDocument();
    expect(screen.getByTestId("provider-plan-usage")).toContainElement(card);
    expect(card.textContent).toMatch(/5-hour/);
    expect(card.textContent).toMatch(/Weekly/);
  });

  it("Settings Usage shows nothing extra when the daemon has no provider.usage rows", async () => {
    const d = new FakeUsageDaemon([claudeAccount()]);
    renderSettings(d);
    await flush();
    expect(screen.getByTestId("usage-report-1")).toBeInTheDocument();
    expect(screen.queryByTestId("provider-plan-usage")).toBeNull();
  });

  it("an old daemon answering unknown method shows nothing and is not asked again", async () => {
    const d = new FakeUsageDaemon([claudeAccount()]);
    d.providerUsageError = new RpcError("method not found: provider.usage");
    const view = renderSettings(d);
    await flush();
    expect(screen.queryByTestId("provider-plan-usage")).toBeNull();
    const asked = d.countCalls("provider.usage");
    view.rerenderWith(d);
    await flush();
    expect(d.countCalls("provider.usage")).toBe(asked);
    expect(asked).toBeLessThanOrEqual(1);
  });

  it("Provider detail of a zai instance shows its plan windows and sends one touch", async () => {
    const d = detailDaemon(
      [{ id: "glm-work", label: "GLM work", kind: "cli", vendor: "zai", extends: "glm", custom: true }],
      [zaiRow("glm-work"), zaiRow("other")],
    );
    renderWithDaemon(<ProviderDetail item={NAV} client={d.client} />, d);
    await flush();
    expect(screen.getByTestId("provider-usage-glm-work")).toBeInTheDocument();
    expect(screen.queryByTestId("provider-usage-other")).toBeNull();
    const touches = d.calls.filter((c) => c.method === "provider.usage" && (c.params as { mode?: string })?.mode === "touch");
    expect(touches).toHaveLength(1);
  });

  it("Provider detail of a non-zai provider sends no provider.usage call", async () => {
    const d = detailDaemon([{ id: "kimi", label: "Kimi", kind: "cli" }], [zaiRow("kimi")]);
    renderWithDaemon(<ProviderDetail item={{ ...NAV, id: "kimi", label: "Kimi" }} client={d.client} />, d);
    await flush();
    expect(screen.queryByTestId("provider-plan-usage")).toBeNull();
    expect(d.countCalls("provider.usage")).toBe(0);
  });
});
