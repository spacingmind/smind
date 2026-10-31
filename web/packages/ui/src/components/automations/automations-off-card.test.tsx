import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AutomationsOffCard } from "@/components/automations/automations-off-card";
import { DaemonQueryProvider } from "@/lib/query/daemon-query-provider";
import { createQueryClient } from "@/lib/query/query-client";
import type { WsClient } from "@/lib/ws-client";
import { FakeWsClient } from "@/test/fake-ws-client";
import "@/test/query-scheduler";

const SERVICE = "smind service restart";
const MANUAL = "smind stop && smind serve";

function renderCard(client: FakeWsClient) {
  return render(
    <DaemonQueryProvider queryClient={createQueryClient()} client={client as unknown as WsClient} events={null}>
      <AutomationsOffCard />
    </DaemonQueryProvider>,
  );
}

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 12; i++) await Promise.resolve();
  });
}

describe("AutomationsOffCard", () => {
  beforeEach(() => {
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
    });
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("shows the exact command and copies it", async () => {
    renderCard(new FakeWsClient());
    expect(screen.getByTestId("automations-off-card")).toHaveTextContent("smind config set automations.enabled true");
    await act(async () => {
      fireEvent.click(screen.getByTestId("automations-off-copy"));
    });
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith("smind config set automations.enabled true");
  });

  it("shows the restart line and the rails", () => {
    renderCard(new FakeWsClient());
    const card = screen.getByTestId("automations-off-card");
    expect(card).toHaveTextContent("smind stop && smind serve");
    expect(card).toHaveTextContent("never merge or release");
    expect(card).toHaveTextContent("Each run has a budget cap");
    expect(card).toHaveTextContent("config.yaml on purpose");
  });

  it("names `smind service restart` only for a launchd-managed daemon", async () => {
    const client = new FakeWsClient();
    renderCard(client);
    client.nth("daemon.info", 0).resolve({ version: "0.17.0", managedBy: "launchd" });
    await flush();
    const card = screen.getByTestId("automations-off-card");
    expect(card).toHaveTextContent(SERVICE);
    expect(card).not.toHaveTextContent(MANUAL);
  });

  it("names `smind stop && smind serve` only for an unmanaged daemon", async () => {
    const client = new FakeWsClient();
    renderCard(client);
    client.nth("daemon.info", 0).resolve({ version: "0.17.0" });
    await flush();
    const card = screen.getByTestId("automations-off-card");
    expect(card).toHaveTextContent(MANUAL);
    expect(card).not.toHaveTextContent(SERVICE);
  });

  it("lists both restart lines while daemon.info is pending or failed", async () => {
    const client = new FakeWsClient();
    renderCard(client);
    const card = screen.getByTestId("automations-off-card");
    expect(card).toHaveTextContent(SERVICE);
    expect(card).toHaveTextContent(MANUAL);
    client.nth("daemon.info", 0).reject(new Error("offline"));
    await flush();
    expect(card).toHaveTextContent(SERVICE);
    expect(card).toHaveTextContent(MANUAL);
  });
});
