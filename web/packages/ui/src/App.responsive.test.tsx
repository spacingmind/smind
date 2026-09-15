// Item 21 (ui-redesign-parity.md): the compact/narrow shell layout.
//
// A separate file from App.test.tsx rather than another describe block in
// it -- these tests mutate `window.innerWidth` and `window.matchMedia`
// globals across a render, and Vitest gives each test *file* its own jsdom
// environment by default (no `isolate: false` in vitest.config.ts), so
// keeping that mutation local to its own file rules out any chance of it
// leaking into App.test.tsx's already-passing (desktop-width) assertions.
import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { App } from "@/App";
import { WsClient } from "@/lib/ws-client";
import { FakeSocket } from "@/test/fake-socket";
import type { Task, Workspace } from "@/lib/types";

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

/** Flushes pending microtasks, wrapped in `act` -- same helper App.test.tsx uses. */
async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

function respond(socket: FakeSocket, method: string, result: unknown, index = 0): void {
  const matches = socket.sent.filter((e) => e.method === method);
  const env = matches[index];
  if (!env?.id) throw new Error(`no ${method} request #${index} sent yet (have ${matches.length})`);
  socket.emit({ id: env.id, result });
}

function respondAll(socket: FakeSocket, method: string, result: unknown): void {
  for (const env of socket.sent.filter((e) => e.method === method)) {
    if (env.id) socket.emit({ id: env.id, result });
  }
}

async function resolveSidebar(socket: FakeSocket): Promise<void> {
  await flush();
  respond(socket, "workspace.list", [WORKSPACE]);
  await flush();
  respond(socket, "space.list", []);
  respond(socket, "task.list", [TASK]);
  respond(socket, "run.list", []);
  await flush();
}

function clickTaskRow(task: Task): void {
  fireEvent.click(screen.getAllByText(task.Title)[0]!);
}

/**
 * A controllable `window.innerWidth` + `matchMedia` stub so a test can
 * simulate crossing `hooks/use-mobile.ts`'s 768px breakpoint after mount
 * -- the same "stub matchMedia, then fire its change listener" shape
 * `use-theme.test.tsx` already established for `prefers-color-scheme`,
 * keyed on width instead. `useIsMobile()`'s own effect reads
 * `window.innerWidth` directly (not `matches`) both on mount and inside
 * the 'change' handler, so the stub's `matches` value only has to be
 * internally consistent, never asserted on directly.
 */
function stubViewportWidth(initialWidth: number) {
  Object.defineProperty(window, "innerWidth", { writable: true, configurable: true, value: initialWidth });
  const listeners = new Set<() => void>();
  window.matchMedia = vi.fn().mockImplementation(
    (query: string) =>
      ({
        matches: window.innerWidth < 768,
        media: query,
        onchange: null,
        addEventListener: (_: string, cb: () => void) => listeners.add(cb),
        removeEventListener: (_: string, cb: () => void) => listeners.delete(cb),
        // xterm's CoreBrowserService (mounted by the force-mounted terminal
        // tab -- see App.tsx's PaneTabStrip) still calls the deprecated
        // addListener/removeListener pair; setup.ts's own default stub
        // carries these no-ops too, for the same reason.
        addListener: () => {},
        removeListener: () => {},
        dispatchEvent: () => false,
      }) as unknown as MediaQueryList,
  );
  return {
    resize(width: number) {
      Object.defineProperty(window, "innerWidth", { writable: true, configurable: true, value: width });
      listeners.forEach((cb) => cb());
    },
  };
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  window.location.hash = "";
  window.localStorage.clear();
  // Restores jsdom's own default (1024) so a leftover override can't reach
  // a later test in this same file via the shared jsdom window.
  Object.defineProperty(window, "innerWidth", { writable: true, configurable: true, value: 1024 });
});

describe("App compact layout (Item 21)", () => {
  it("below the breakpoint, the sidebar has no resize handle -- it's a drawer, not a fixed column", async () => {
    stubViewportWidth(375);
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    const { container } = render(<App connect={connect} />);
    await resolveSidebar(socket);

    expect(screen.queryByTestId("sidebar-resize-handle")).not.toBeInTheDocument();
    // No resizable-panel-group at all above the breakpoint's threshold --
    // the compact branch renders a plain flex column instead of handing
    // the sidebar-vs-content split to react-resizable-panels.
    expect(container.querySelector('[data-slot="resizable-panel-group"]')).toBeNull();
  });

  it("below the breakpoint, opening the sidebar renders it as an overlay rather than reflowing the content area", async () => {
    stubViewportWidth(375);
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket);

    expect(document.querySelector('[data-mobile="true"]')).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Toggle Sidebar" }));
    await flush();

    // shadcn's Sidebar primitive renders its mobile branch as a Sheet
    // (Radix Dialog) once useIsMobile() is true -- it portals to
    // document.body rather than taking a column out of the shell's own
    // flex layout, which is what "graceful, not squeezed" means here.
    expect(document.querySelector('[data-mobile="true"]')).not.toBeNull();
    // The main content area is unaffected: still exactly one connection
    // status header, not duplicated or displaced.
    expect(screen.getAllByTestId("app-connection-status")).toHaveLength(1);
  });

  it("at/above the breakpoint (regression), the sidebar keeps its resize handle", async () => {
    stubViewportWidth(1024);
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket);

    expect(screen.getByTestId("sidebar-resize-handle")).toBeInTheDocument();
  });

  it("the side dock (Item 6) doesn't apply below the breakpoint even with an existing split, and reappears above it (graceful, not lossy)", async () => {
    const viewport = stubViewportWidth(1024);
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket);

    clickTaskRow(TASK);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    // Move the Diff tab (a movable kind) to the side pane, at desktop width.
    const moveButton = screen.getByLabelText("Open Diff to the side");
    fireEvent.click(moveButton);
    await flush();

    expect(screen.getByTestId("side-pane-resize-handle")).toBeInTheDocument();

    // Resize down: the split stops rendering, and so does the "open to
    // side" affordance (there's nowhere for it to move a tab to) -- but
    // the side pane's own tab isn't discarded from state, just not shown.
    act(() => viewport.resize(375));
    await flush();

    expect(screen.queryByTestId("side-pane-resize-handle")).not.toBeInTheDocument();
    expect(screen.queryByTestId("workspace-tab-move")).not.toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Diff" })).toBeInTheDocument();

    // Resize back up: the split reappears with the same tab, proving the
    // degrade was purely presentational.
    act(() => viewport.resize(1024));
    await flush();

    expect(screen.getByTestId("side-pane-resize-handle")).toBeInTheDocument();
  });
});
