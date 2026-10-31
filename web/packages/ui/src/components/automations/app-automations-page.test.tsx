import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { App } from "@/App";
import { WsClient } from "@/lib/ws-client";
import { FakeSocket } from "@/test/fake-socket";
import "@/test/query-scheduler";
import { resetTerminalSessions } from "@/lib/terminal-sessions";
import type { Task, Workspace } from "@/lib/types";

// Same rationale as App.test.tsx: every test here renders the whole <App>.
vi.setConfig({ testTimeout: 30_000 });

const WORKSPACE: Workspace = {
  ID: 1,
  Path: "/tmp/ws",
  Title: "My Workspace",
  RoutingPolicy: "",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
};

const TASK: Task = {
  ID: 42,
  WorkspaceID: 1,
  SpaceID: null,
  Title: "Fix the bug",
  Status: "active",
  WorktreePath: "/tmp/a",
  Branch: "fix-bug",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

const LISTING = { enabled: true, paused: false, automations: [] };

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 3; i++) await Promise.resolve();
  });
}

function respond(socket: FakeSocket, method: string, result: unknown, index = 0): void {
  const env = socket.sent.filter((e) => e.method === method)[index];
  if (!env?.id) throw new Error(`no ${method} request #${index} sent yet`);
  socket.emit({ id: env.id, result });
}

function respondAll(socket: FakeSocket, method: string, result: unknown): void {
  for (const env of socket.sent.filter((e) => e.method === method)) {
    if (env.id) socket.emit({ id: env.id, result });
  }
}

/** AppSidebar's workspace.list -> {space.list, task.list} sequence plus useTaskAttention's initial run.list. */
async function resolveSidebar(socket: FakeSocket): Promise<void> {
  await flush();
  respond(socket, "workspace.list", [WORKSPACE]);
  await flush();
  respond(socket, "space.list", []);
  respond(socket, "task.list", [TASK]);
  respond(socket, "run.list", []);
  await flush();
}

/** Renders the whole app on a live socket and answers the automations page's own automation.list. */
async function renderApp() {
  const socket = new FakeSocket();
  const connect = vi.fn().mockResolvedValue(new WsClient(socket));
  render(<App connect={connect} />);
  await resolveSidebar(socket);
  return socket;
}

function openSettings(): void {
  fireEvent.keyDown(document, { key: ",", code: "Comma", ctrlKey: true });
}

function openPalette(): void {
  fireEvent.keyDown(document, { key: "k", code: "KeyK", ctrlKey: true });
}

function paletteRow(title: string): HTMLElement {
  return screen
    .getAllByTestId("command-palette-row")
    .find((r) => (r.querySelector("span span")?.textContent ?? "") === title)!;
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  window.location.hash = "";
  window.localStorage.clear();
  resetTerminalSessions();
});

describe("Automations as a top-level page", () => {
  it("sidebar quick action opens the page", async () => {
    const socket = await renderApp();

    fireEvent.click(screen.getByTestId("sidebar-quick-automations"));
    await flush();
    respondAll(socket, "automation.list", LISTING);
    await flush();

    expect(screen.getByTestId("automations-screen")).toBeInTheDocument();
    expect(screen.queryByTestId("settings-screen")).not.toBeInTheDocument();
  });

  it("palette command opens the page", async () => {
    const socket = await renderApp();

    await act(async () => {
      openPalette();
    });
    await flush();
    fireEvent.click(paletteRow("Open Automations"));
    await flush();
    respondAll(socket, "automation.list", LISTING);
    await flush();

    expect(screen.getByTestId("automations-screen")).toBeInTheDocument();
    expect(screen.queryByTestId("command-palette")).not.toBeInTheDocument();
  });

  it("settings section only links to the page", async () => {
    const socket = await renderApp();

    await act(async () => {
      openSettings();
    });
    await flush();
    fireEvent.click(screen.getByTestId("settings-nav-automations"));
    await flush();
    respondAll(socket, "automation.list", LISTING);
    await flush();

    const section = screen.getByTestId("settings-section-automations");
    expect(section).toHaveTextContent("Automations are on");
    expect(screen.queryByTestId("automations-list")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("settings-open-automations"));
    await flush();
    expect(screen.getByTestId("automations-screen")).toBeInTheDocument();
    expect(screen.queryByTestId("settings-screen")).not.toBeInTheDocument();
  });
});
