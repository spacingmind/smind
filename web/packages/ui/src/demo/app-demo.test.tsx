import { act } from "react";
import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import "@/test/query-scheduler";
import { reloadAllPreferenceStores } from "@/stores/create-preference-store";

import type { DemoClock } from "./clock";
import { buildTour } from "./scenario";

// Every test here renders the whole <App> (see App.test.tsx for why this gets a measured, longer timeout).
vi.setConfig({ testTimeout: 30_000 });

/** A clock that never fires: the scripted run stays at its first step, so nothing moves under the assertions. */
const frozen: DemoClock = { now: () => Date.parse("2026-11-02T10:00:00Z"), after: () => () => {} };

async function settle(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 20; i++) await new Promise((r) => setTimeout(r, 0));
  });
}

beforeEach(() => {
  window.localStorage.clear();
  window.sessionStorage.clear();
  window.location.hash = "";
  reloadAllPreferenceStores();
});

afterEach(() => {
  cleanup();
  (globalThis as { __SMIND_DEMO__?: boolean }).__SMIND_DEMO__ = false;
  vi.resetModules();
});

describe("App against the demo daemon", () => {
  it("B-02 renders the scenario's workspaces and tasks, and the connection label reads Demo (scripted)", async () => {
    // App.tsx reads __SMIND_DEMO__ when its module loads, so the flag is set before the (fresh) import.
    (globalThis as { __SMIND_DEMO__?: boolean }).__SMIND_DEMO__ = true;
    vi.resetModules();
    const { App } = await import("@/App");
    const { connectDemo, resetDemoSession } = await import("./connect");
    resetDemoSession();
    const tour = buildTour(frozen.now());

    render(<App connect={() => connectDemo({ scenario: tour, clock: frozen, reducedMotion: true })} />);
    await settle();

    expect(screen.getByText("Demo (scripted)")).toBeInTheDocument();
    expect(screen.queryByText("Connected to daemon")).not.toBeInTheDocument();
    for (const w of tour.workspaces) expect(screen.getAllByText(w.Title).length).toBeGreaterThan(0);
    const rows = screen.getAllByTestId("sidebar-task-row");
    expect(rows.length).toBe(tour.tasks.length);
    expect(within(rows[0]).getByTestId("task-title")).toHaveTextContent("Add rate limiting to the public API");
  });

  it("the production label is unchanged when the flag is off", async () => {
    vi.resetModules();
    const { App } = await import("@/App");
    const { WsClient } = await import("@/lib/ws-client");
    const { FakeSocket } = await import("@/test/fake-socket");
    const socket = new FakeSocket();
    render(<App connect={() => Promise.resolve(new WsClient(socket))} />);
    await settle();
    expect(screen.getByText("Connected to daemon")).toBeInTheDocument();
  });
});
