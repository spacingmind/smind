import { act } from "react";
import { render, screen, fireEvent } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";
import { TerminalPane, type TerminalHandle } from "@/components/terminal-pane";
import type { Task, TerminalSessionStatus } from "@/lib/types";

const TASK_A: Task = {
  ID: 1,
  WorkspaceID: 1,
  SpaceID: null,
  Title: "Task A",
  Status: "active",
  WorktreePath: "/tmp/a",
  Branch: "task-a",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

const TASK_B: Task = { ...TASK_A, ID: 2, Title: "Task B", Branch: "task-b" };

function sessionStatus(overrides: Partial<TerminalSessionStatus> = {}): TerminalSessionStatus {
  return {
    ID: "term-1",
    TaskID: TASK_A.ID,
    StartedAt: "2024-01-01T00:00:00Z",
    Status: "running",
    ClosedAt: null,
    ...overrides,
  };
}

/**
 * A fake implementing TerminalHandle's surface, recording every call and
 * exposing emitData/emitResize to drive the callbacks TerminalPane
 * subscribes via onData/onResize -- the terminal-widget-boundary
 * counterpart to FakeWsClient, for exactly the reason TerminalHandle's own
 * doc comment gives: a real xterm.js instance needs browser canvas/layout
 * APIs jsdom doesn't implement, so these tests exercise TerminalPane's own
 * wiring logic (terminal.list/create/attach/write/resize, detach-on-unmount)
 * against this fake instead.
 */
class FakeTerminalHandle implements TerminalHandle {
  opened: HTMLElement | null = null;
  disposed = false;
  fitCalls = 0;
  writes: (string | Uint8Array)[] = [];
  private dataCallback: ((data: string) => void) | null = null;
  private resizeCallback: ((size: { cols: number; rows: number }) => void) | null = null;

  open(container: HTMLElement): void {
    this.opened = container;
  }

  onData(callback: (data: string) => void): { dispose(): void } {
    this.dataCallback = callback;
    return { dispose: () => (this.dataCallback = null) };
  }

  onResize(callback: (size: { cols: number; rows: number }) => void): { dispose(): void } {
    this.resizeCallback = callback;
    return { dispose: () => (this.resizeCallback = null) };
  }

  write(data: string | Uint8Array): void {
    this.writes.push(data);
  }

  fit(): void {
    this.fitCalls++;
  }

  dispose(): void {
    this.disposed = true;
  }

  emitData(data: string): void {
    this.dataCallback?.(data);
  }

  emitResize(cols: number, rows: number): void {
    this.resizeCallback?.({ cols, rows });
  }
}

/** Flushes pending microtasks, wrapped in `act` so React commits any resulting state updates before the caller asserts -- same helper as task-detail.test.tsx. */
async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

function base64Of(text: string): string {
  return btoa(text);
}

describe("TerminalPane", () => {
  it("mounting opens the terminal handle and calls terminal.list, then terminal.create then terminal.attach when no session exists yet", async () => {
    const client = new FakeWsClient();
    const fake = new FakeTerminalHandle();
    render(<TerminalPane client={client} task={TASK_A} createTerminal={() => fake} />);
    await flush();

    expect(fake.opened).toBe(screen.getByTestId("terminal-container"));

    const listCall = client.nth("terminal.list", 0);
    expect(listCall.params).toEqual({ taskId: TASK_A.ID });

    // terminal.create must not be issued before terminal.list resolves.
    expect(client.calls.some((c) => c.method === "terminal.create")).toBe(false);

    listCall.resolve([]);
    await flush();

    const createCall = client.nth("terminal.create", 0);
    expect(createCall.params).toEqual({ taskId: TASK_A.ID });
    expect(client.calls.some((c) => c.method === "terminal.attach")).toBe(false);

    createCall.resolve({ terminalId: "term-1" });
    await flush();

    const attachCall = client.nth("terminal.attach", 0);
    expect(attachCall.params).toEqual({ terminalId: "term-1" });
    expect(screen.getByTestId("terminal-status")).toHaveTextContent("term-1");
  });

  it("writes incoming base64-encoded 'data' events into the terminal handle, decoded to bytes", async () => {
    const client = new FakeWsClient();
    const fake = new FakeTerminalHandle();
    render(<TerminalPane client={client} task={TASK_A} createTerminal={() => fake} />);
    await flush();
    client.nth("terminal.list", 0).resolve([]);
    await flush();
    client.nth("terminal.create", 0).resolve({ terminalId: "term-1" });
    await flush();

    client.emit("terminal.attach", 0, "data", { data: base64Of("hello ") });
    await flush();
    client.emit("terminal.attach", 0, "data", { data: base64Of("world") });
    await flush();

    expect(fake.writes).toHaveLength(2);
    const decoded = fake.writes.map((w) => new TextDecoder().decode(w as Uint8Array)).join("");
    expect(decoded).toBe("hello world");
  });

  it("forwards keystrokes (the terminal handle's onData) via terminal.write", async () => {
    const client = new FakeWsClient();
    const fake = new FakeTerminalHandle();
    render(<TerminalPane client={client} task={TASK_A} createTerminal={() => fake} />);
    await flush();
    client.nth("terminal.list", 0).resolve([]);
    await flush();
    client.nth("terminal.create", 0).resolve({ terminalId: "term-1" });
    await flush();

    fake.emitData("echo hi\n");
    await flush();

    const writeCall = client.nth("terminal.write", 0);
    expect(writeCall.params).toEqual({ terminalId: "term-1", data: "echo hi\n" });
  });

  it("forwards the terminal handle's own resize (onResize) via terminal.resize", async () => {
    const client = new FakeWsClient();
    const fake = new FakeTerminalHandle();
    render(<TerminalPane client={client} task={TASK_A} createTerminal={() => fake} />);
    await flush();
    client.nth("terminal.list", 0).resolve([]);
    await flush();
    client.nth("terminal.create", 0).resolve({ terminalId: "term-1" });
    await flush();

    fake.emitResize(120, 40);
    await flush();

    const resizeCall = client.nth("terminal.resize", 0);
    expect(resizeCall.params).toEqual({ terminalId: "term-1", cols: 120, rows: 40 });
  });

  it("aborts (does not close) an actively streaming terminal.attach on unmount, and disposes the terminal handle", async () => {
    const client = new FakeWsClient();
    const fake = new FakeTerminalHandle();
    const { unmount } = render(<TerminalPane client={client} task={TASK_A} createTerminal={() => fake} />);
    await flush();
    client.nth("terminal.list", 0).resolve([]);
    await flush();
    client.nth("terminal.create", 0).resolve({ terminalId: "term-1" });
    await flush();

    const attachCall = client.nth("terminal.attach", 0);
    expect(attachCall.options?.signal?.aborted).toBeFalsy();

    unmount();

    expect(attachCall.options?.signal?.aborted).toBe(true);
    expect(client.calls.some((c) => c.method === "terminal.close")).toBe(false);
    expect(fake.disposed).toBe(true);
  });

  it("clicking 'Close terminal' calls terminal.close with the current session's id", async () => {
    const client = new FakeWsClient();
    const fake = new FakeTerminalHandle();
    render(<TerminalPane client={client} task={TASK_A} createTerminal={() => fake} />);
    await flush();
    client.nth("terminal.list", 0).resolve([]);
    await flush();
    client.nth("terminal.create", 0).resolve({ terminalId: "term-1" });
    await flush();

    const closeButton = screen.getByRole("button", { name: "Close terminal" });
    expect(closeButton).not.toBeDisabled();
    fireEvent.click(closeButton);
    await flush();

    const closeCall = client.nth("terminal.close", 0);
    expect(closeCall.params).toEqual({ terminalId: "term-1" });
    // Closing must go through terminal.close, never by aborting the
    // active terminal.attach subscription (that would only detach).
    expect(client.nth("terminal.attach", 0).options?.signal?.aborted).toBeFalsy();
  });

  it("the 'Close terminal' button is disabled until a session id exists", async () => {
    const client = new FakeWsClient();
    const fake = new FakeTerminalHandle();
    render(<TerminalPane client={client} task={TASK_A} createTerminal={() => fake} />);
    await flush();

    expect(screen.getByRole("button", { name: "Close terminal" })).toBeDisabled();
  });

  it("surfaces a terminal.list failure as an error message", async () => {
    const client = new FakeWsClient();
    const fake = new FakeTerminalHandle();
    render(<TerminalPane client={client} task={TASK_A} createTerminal={() => fake} />);
    await flush();

    client.nth("terminal.list", 0).reject(new Error("daemon unreachable"));
    await flush();

    expect(screen.getByTestId("terminal-error")).toHaveTextContent("daemon unreachable");
  });

  it("surfaces a terminal.create failure as an error message", async () => {
    const client = new FakeWsClient();
    const fake = new FakeTerminalHandle();
    render(<TerminalPane client={client} task={TASK_A} createTerminal={() => fake} />);
    await flush();
    client.nth("terminal.list", 0).resolve([]);
    await flush();

    client.nth("terminal.create", 0).reject(new Error("no worktree"));
    await flush();

    expect(screen.getByTestId("terminal-error")).toHaveTextContent("no worktree");
  });

  it("switching tasks detaches the previous task's attach and starts a fresh terminal.list/create/attach for the new task", async () => {
    const client = new FakeWsClient();
    const fake = new FakeTerminalHandle();
    const stableFactory = () => fake;
    const { rerender } = render(<TerminalPane client={client} task={TASK_A} createTerminal={stableFactory} />);
    await flush();
    client.nth("terminal.list", 0).resolve([]);
    await flush();
    client.nth("terminal.create", 0).resolve({ terminalId: "term-a" });
    await flush();
    const attachA = client.nth("terminal.attach", 0);
    expect(attachA.options?.signal?.aborted).toBeFalsy();

    rerender(<TerminalPane client={client} task={TASK_B} createTerminal={stableFactory} />);
    await flush();

    expect(attachA.options?.signal?.aborted).toBe(true);
    expect(client.calls.some((c) => c.method === "terminal.close")).toBe(false);

    const listB = client.nth("terminal.list", 1);
    expect(listB.params).toEqual({ taskId: TASK_B.ID });
    listB.resolve([]);
    await flush();

    const createB = client.nth("terminal.create", 1);
    expect(createB.params).toEqual({ taskId: TASK_B.ID });
    createB.resolve({ terminalId: "term-b" });
    await flush();

    const attachB = client.nth("terminal.attach", 1);
    expect(attachB.params).toEqual({ terminalId: "term-b" });
  });

  it("renders with no client without crashing, and never calls terminal.list/terminal.create", async () => {
    const fake = new FakeTerminalHandle();
    render(<TerminalPane client={null} task={TASK_A} createTerminal={() => fake} />);
    await flush();

    expect(screen.getByTestId("terminal-status")).toHaveTextContent("starting terminal");
    expect(fake.opened).not.toBeNull();
  });

  describe("reconnect resync", () => {
    it("given a new post-reconnect client, re-issues terminal.list and re-attaches to the same still-running id instead of a fresh terminal.create", async () => {
      const client1 = new FakeWsClient();
      const fake = new FakeTerminalHandle();
      const { rerender } = render(<TerminalPane client={client1} task={TASK_A} createTerminal={() => fake} />);
      await flush();
      client1.nth("terminal.list", 0).resolve([]);
      await flush();
      client1.nth("terminal.create", 0).resolve({ terminalId: "term-1" });
      await flush();
      expect(client1.nth("terminal.attach", 0).params).toEqual({ terminalId: "term-1" });

      // App.tsx swaps in a genuinely new WsClient instance after a
      // successful reconnect -- task.ID is unchanged, only the client
      // reference changed.
      const client2 = new FakeWsClient();
      rerender(<TerminalPane client={client2} task={TASK_A} createTerminal={() => fake} />);
      await flush();

      const listCall = client2.nth("terminal.list", 0);
      expect(listCall.params).toEqual({ taskId: TASK_A.ID });
      listCall.resolve([sessionStatus({ ID: "term-1", Status: "running" })]);
      await flush();

      // Re-attaches to the same id -- never a fresh terminal.create.
      expect(client2.calls.some((c) => c.method === "terminal.create")).toBe(false);
      const reattach = client2.nth("terminal.attach", 0);
      expect(reattach.params).toEqual({ terminalId: "term-1" });
      expect(screen.getByTestId("terminal-status")).toHaveTextContent("term-1");
    });

    it("given a session that comes back status: interrupted post-reconnect, renders the distinct 'session ended: daemon restarted' state, not a generic error", async () => {
      const client1 = new FakeWsClient();
      const fake = new FakeTerminalHandle();
      const { rerender } = render(<TerminalPane client={client1} task={TASK_A} createTerminal={() => fake} />);
      await flush();
      client1.nth("terminal.list", 0).resolve([]);
      await flush();
      client1.nth("terminal.create", 0).resolve({ terminalId: "term-1" });
      await flush();

      const client2 = new FakeWsClient();
      rerender(<TerminalPane client={client2} task={TASK_A} createTerminal={() => fake} />);
      await flush();

      client2.nth("terminal.list", 0).resolve([sessionStatus({ ID: "term-1", Status: "interrupted" })]);
      await flush();

      expect(client2.calls.some((c) => c.method === "terminal.attach")).toBe(false);
      expect(client2.calls.some((c) => c.method === "terminal.create")).toBe(false);
      expect(screen.getByTestId("terminal-ended")).toHaveTextContent("session ended: daemon restarted");
      expect(screen.queryByTestId("terminal-error")).not.toBeInTheDocument();
      expect(screen.getByTestId("terminal-status")).toHaveTextContent("interrupted");
    });

    it("shows a connection-lost banner while connectionStatus is 'reconnecting', without discarding the existing terminal output", async () => {
      const client = new FakeWsClient();
      const fake = new FakeTerminalHandle();
      const stableFactory = () => fake;
      const { rerender } = render(
        <TerminalPane client={client} task={TASK_A} connectionStatus="connected" createTerminal={stableFactory} />,
      );
      await flush();
      client.nth("terminal.list", 0).resolve([]);
      await flush();
      client.nth("terminal.create", 0).resolve({ terminalId: "term-1" });
      await flush();

      expect(screen.queryByTestId("connection-banner")).not.toBeInTheDocument();

      rerender(<TerminalPane client={client} task={TASK_A} connectionStatus="reconnecting" createTerminal={stableFactory} />);
      await flush();

      expect(screen.getByTestId("connection-banner")).toBeInTheDocument();
      // The terminal widget itself (and its already-rendered output) stays
      // mounted -- only a banner is added, nothing is torn down.
      expect(fake.disposed).toBe(false);
    });

    it("reconnect with two running sessions for the task lands on previousId's exact session, not just whichever is first in the list", async () => {
      const client1 = new FakeWsClient();
      const fake = new FakeTerminalHandle();
      const { rerender } = render(<TerminalPane client={client1} task={TASK_A} createTerminal={() => fake} />);
      await flush();
      client1.nth("terminal.list", 0).resolve([]);
      await flush();
      client1.nth("terminal.create", 0).resolve({ terminalId: "term-1" });
      await flush();
      expect(client1.nth("terminal.attach", 0).params).toEqual({ terminalId: "term-1" });

      const client2 = new FakeWsClient();
      rerender(<TerminalPane client={client2} task={TASK_A} createTerminal={() => fake} />);
      await flush();

      // Another tab's session for the same task happens to be running too,
      // and is listed *before* this pane's own session -- picking "the
      // first running entry" would silently swap this pane onto it.
      client2.nth("terminal.list", 0).resolve([
        sessionStatus({ ID: "term-other-tab", Status: "running" }),
        sessionStatus({ ID: "term-1", Status: "running" }),
      ]);
      await flush();

      expect(client2.calls.some((c) => c.method === "terminal.create")).toBe(false);
      const reattach = client2.nth("terminal.attach", 0);
      expect(reattach.params).toEqual({ terminalId: "term-1" });
      expect(screen.getByTestId("terminal-status")).toHaveTextContent("term-1");
    });

    it("explicitly closing the terminal, then a later reconnect, creates a fresh terminal instead of getting stuck showing 'session closed'", async () => {
      const client1 = new FakeWsClient();
      const fake = new FakeTerminalHandle();
      const stableFactory = () => fake;
      const { rerender } = render(<TerminalPane client={client1} task={TASK_A} createTerminal={stableFactory} />);
      await flush();
      client1.nth("terminal.list", 0).resolve([]);
      await flush();
      client1.nth("terminal.create", 0).resolve({ terminalId: "term-1" });
      await flush();
      expect(screen.getByTestId("terminal-status")).toHaveTextContent("term-1");

      fireEvent.click(screen.getByRole("button", { name: "Close terminal" }));
      await flush();
      client1.nth("terminal.close", 0).resolve(undefined);
      await flush();

      // The closed session's id must not linger in state -- otherwise a
      // later reconnect's list-then-attach effect would still treat this
      // exact (now-closed) id as "the session to resume".
      expect(screen.getByTestId("terminal-status")).toHaveTextContent("starting terminal");
      expect(screen.getByRole("button", { name: "Close terminal" })).toBeDisabled();

      const client2 = new FakeWsClient();
      rerender(<TerminalPane client={client2} task={TASK_A} createTerminal={stableFactory} />);
      await flush();

      const listCall = client2.nth("terminal.list", 0);
      expect(listCall.params).toEqual({ taskId: TASK_A.ID });
      // The daemon still remembers the now-closed session.
      listCall.resolve([sessionStatus({ ID: "term-1", Status: "closed" })]);
      await flush();

      // Must call terminal.create for a fresh session, not get stuck on
      // the old (already-closed-by-us) id showing "session closed"
      // forever.
      const createCall = client2.nth("terminal.create", 0);
      expect(createCall.params).toEqual({ taskId: TASK_A.ID });
      expect(screen.queryByTestId("terminal-ended")).not.toBeInTheDocument();

      createCall.resolve({ terminalId: "term-2" });
      await flush();
      expect(client2.nth("terminal.attach", 0).params).toEqual({ terminalId: "term-2" });
      expect(screen.getByTestId("terminal-status")).toHaveTextContent("term-2");
    });
  });
});
