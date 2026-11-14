import { act } from "react";
import { render, screen, fireEvent } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";
import { TerminalPane, type TerminalHandle } from "@/components/terminal-pane";
import { resetTerminalSessions } from "@/lib/terminal-sessions";
import type { Task, TerminalSessionStatus } from "@/lib/types";

// Same reason as terminal-pane.test.tsx: the session<->tab binding lives
// outside React and survives across renders unless cleared between tests.
afterEach(() => {
  resetTerminalSessions();
});

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
 * terminal-pane.test.tsx's FakeTerminalHandle, trimmed to what the
 * restart tests drive (no selection reporting): records every write and
 * exposes emitData to fire the onData callback TerminalPane subscribes.
 */
class FakeTerminalHandle implements TerminalHandle {
  opened: HTMLElement | null = null;
  disposed = false;
  fitCalls = 0;
  focusCalls = 0;
  resets = 0;
  writes: (string | Uint8Array)[] = [];
  size?: () => { cols: number; rows: number };
  appCursor = false;
  private dataCallback: ((data: string) => void) | null = null;
  private resizeCallback: ((size: { cols: number; rows: number }) => void) | null = null;

  focus(): void {
    this.focusCalls++;
  }

  reset(): void {
    this.resets++;
  }

  applicationCursorKeys(): boolean {
    return this.appCursor;
  }

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

/** Flushes pending microtasks so React commits before the caller asserts -- same helper as terminal-pane.test.tsx. */
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

function decodedWrites(fake: FakeTerminalHandle): string {
  return fake.writes.map((w) => (typeof w === "string" ? w : new TextDecoder().decode(w))).join("");
}

describe("TerminalPane restart (DT3-B)", () => {
  /** Mounts TASK_A (or `task`) into a running session term-1, leaving the attach stream pending -- the shared starting point of every scenario here. */
  async function mountRunning(client: FakeWsClient, fake: FakeTerminalHandle, task: Task = TASK_A) {
    render(<TerminalPane client={client} task={task} createTerminal={() => fake} />);
    await flush();
    client.nth("terminal.list", 0).resolve([]);
    await flush();
    client.nth("terminal.create", 0).resolve({ terminalId: "term-1" });
    await flush();
  }

  /** Rebinding reconnect, the pattern terminal-pane.test.tsx's interrupted-state test drives: a genuinely new client, same task, same tab key. */
  async function reconnect(task: Task = TASK_A) {
    const client1 = new FakeWsClient();
    const fake = new FakeTerminalHandle();
    const { rerender } = render(<TerminalPane client={client1} task={task} createTerminal={() => fake} />);
    await flush();
    client1.nth("terminal.list", 0).resolve([]);
    await flush();
    client1.nth("terminal.create", 0).resolve({ terminalId: "term-1" });
    await flush();

    const client2 = new FakeWsClient();
    rerender(<TerminalPane client={client2} task={task} createTerminal={() => fake} />);
    await flush();
    return { client2, fake, rerender };
  }

  it("ended on connect: replays the retained output through terminal.attach and offers Restart", async () => {
    const { client2, fake } = await reconnect();
    client2.nth("terminal.list", 0).resolve([sessionStatus({ ID: "term-1", Status: "closed" })]);
    await flush();

    const replay = client2.nth("terminal.attach", 0);
    expect(replay.params).toEqual({ terminalId: "term-1" });
    // The emulator is cleared first so a reconnect never shows the retained output twice.
    expect(fake.resets).toBe(1);
    // The replay never adopts the dead session: no binding, no create.
    expect(client2.calls.some((c) => c.method === "terminal.create")).toBe(false);

    client2.emit("terminal.attach", 0, "data", { data: base64Of("retained output\n") });
    await flush();
    expect(decodedWrites(fake)).toContain("retained output\n");

    expect(screen.getByTestId("terminal-status")).toHaveTextContent("session closed");
    expect(screen.getByTestId("terminal-restart")).toBeInTheDocument();
    expect(screen.queryByTestId("terminal-output-gone")).not.toBeInTheDocument();
  });

  it("output not retained: a rejected replay shows 'no longer kept' and Restart", async () => {
    const { client2 } = await reconnect();
    client2.nth("terminal.list", 0).resolve([sessionStatus({ ID: "term-1", Status: "closed" })]);
    await flush();

    client2.nth("terminal.attach", 0).reject(new Error("not retained"));
    await flush();

    expect(screen.getByTestId("terminal-output-gone")).toHaveTextContent("no longer kept");
    expect(screen.getByTestId("terminal-status")).toHaveTextContent("session closed");
    expect(screen.getByTestId("terminal-restart")).toBeInTheDocument();
  });

  it("session missing from the list: no attach, shows 'no longer kept' and Restart", async () => {
    const { client2 } = await reconnect();
    client2.nth("terminal.list", 0).resolve([]);
    await flush();

    expect(client2.calls.some((c) => c.method === "terminal.attach")).toBe(false);
    expect(client2.calls.some((c) => c.method === "terminal.create")).toBe(false);
    expect(screen.getByTestId("terminal-output-gone")).toHaveTextContent("no longer kept");
    expect(screen.getByTestId("terminal-status")).toHaveTextContent("session closed");
    expect(screen.getByTestId("terminal-restart")).toBeInTheDocument();
  });

  it("the live attach stream finishing (shell exit) shows the ended state with Restart; a rejected stream does not", async () => {
    const client = new FakeWsClient();
    const fake = new FakeTerminalHandle();
    const { unmount } = render(<TerminalPane client={client} task={TASK_A} createTerminal={() => fake} />);
    await flush();
    client.nth("terminal.list", 0).resolve([]);
    await flush();
    client.nth("terminal.create", 0).resolve({ terminalId: "term-1" });
    await flush();

    // The stream resolving is the shell exiting (the daemon returns the
    // terminal result) -- an end, with Restart offered.
    client.nth("terminal.attach", 0).resolve({ terminalId: "term-1" });
    await flush();
    expect(screen.getByTestId("terminal-status")).toHaveTextContent("session closed");
    expect(screen.getByTestId("terminal-restart")).toBeInTheDocument();

    unmount();

    // A rejected stream (abort, lost connection) is not an end: the pane
    // keeps its running session, no ended banner. Fresh task -> fresh tab
    // key -> no previousId, so this mount is a plain running session too.
    const client2 = new FakeWsClient();
    const fake2 = new FakeTerminalHandle();
    render(<TerminalPane client={client2} task={TASK_B} createTerminal={() => fake2} />);
    await flush();
    client2.nth("terminal.list", 0).resolve([]);
    await flush();
    client2.nth("terminal.create", 0).resolve({ terminalId: "term-1" });
    await flush();

    client2.nth("terminal.attach", 0).reject(new Error("connection lost"));
    await flush();

    expect(screen.queryByTestId("terminal-status")).not.toBeInTheDocument();
    expect(screen.queryByTestId("terminal-restart")).not.toBeInTheDocument();
  });

  it("Restart makes exactly one terminal.create, writes the dim separator, attaches the new id and hides the banner; typing then goes to the new id", async () => {
    const client = new FakeWsClient();
    const fake = new FakeTerminalHandle();
    await mountRunning(client, fake);
    client.nth("terminal.attach", 0).resolve({ terminalId: "term-1" });
    await flush();
    expect(screen.getByTestId("terminal-restart")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("terminal-restart"));
    await flush();

    // One click is exactly one terminal.create -- nothing else yet.
    const creates = client.calls.filter((c) => c.method === "terminal.create");
    expect(creates).toHaveLength(2);
    expect(client.nth("terminal.create", 1).params).toEqual({ taskId: TASK_A.ID });

    client.nth("terminal.create", 1).resolve({ terminalId: "term-2" });
    await flush();

    // The old output stays, separated by the dim line, in the same xterm.
    expect(decodedWrites(fake)).toContain("--- restarted ---");
    expect(client.nth("terminal.attach", 1).params).toEqual({ terminalId: "term-2" });
    expect(screen.queryByTestId("terminal-status")).not.toBeInTheDocument();

    fake.emitData("echo hi\n");
    await flush();
    expect(client.nth("terminal.write", 0).params).toEqual({ terminalId: "term-2", data: "echo hi\n" });
  });

  it("a double click on Restart is one terminal.create and the button is disabled while pending", async () => {
    const client = new FakeWsClient();
    const fake = new FakeTerminalHandle();
    await mountRunning(client, fake);
    client.nth("terminal.attach", 0).resolve({ terminalId: "term-1" });
    await flush();

    // Two clicks inside one act(): the second lands before React has painted the disabled state, so only the
    // ref guard stands between it and a second terminal.create.
    act(() => {
      const button = screen.getByTestId("terminal-restart");
      button.click();
      button.click();
    });
    await flush();

    expect(screen.getByTestId("terminal-restart")).toBeDisabled();
    // The first create is the pane's own initial one; the restart added exactly one.
    expect(client.calls.filter((c) => c.method === "terminal.create")).toHaveLength(2);

    client.nth("terminal.create", 1).resolve({ terminalId: "term-2" });
    await flush();

    // The old output stays, separated by the dim line, in the same xterm.
    expect(decodedWrites(fake)).toContain("--- restarted ---");
    expect(client.nth("terminal.attach", 1).params).toEqual({ terminalId: "term-2" });
    expect(screen.queryByTestId("terminal-status")).not.toBeInTheDocument();

    fake.emitData("echo hi\n");
    await flush();
    expect(client.nth("terminal.write", 0).params).toEqual({ terminalId: "term-2", data: "echo hi\n" });
  });

  it("a double click on Restart is one terminal.create and the button is disabled while pending", async () => {
    const client = new FakeWsClient();
    const fake = new FakeTerminalHandle();
    await mountRunning(client, fake);
    client.nth("terminal.attach", 0).resolve({ terminalId: "term-1" });
    await flush();

    fireEvent.click(screen.getByTestId("terminal-restart"));
    await flush();

    expect(screen.getByTestId("terminal-restart")).toBeDisabled();

    // fireEvent dispatches straight through the disabled state, the way a
    // real double click's second event can land before the state paints.
    fireEvent.click(screen.getByTestId("terminal-restart"));
    await flush();
    expect(client.calls.filter((c) => c.method === "terminal.create")).toHaveLength(2);

    client.nth("terminal.create", 1).resolve({ terminalId: "term-2" });
    await flush();
    expect(screen.queryByTestId("terminal-restart")).not.toBeInTheDocument();
  });

  it("a terminal.create error shows inline in terminal-error and Restart stays available", async () => {
    const client = new FakeWsClient();
    const fake = new FakeTerminalHandle();
    await mountRunning(client, fake);
    client.nth("terminal.attach", 0).resolve({ terminalId: "term-1" });
    await flush();

    fireEvent.click(screen.getByTestId("terminal-restart"));
    await flush();
    client.nth("terminal.create", 1).reject(new Error("no worktree"));
    await flush();

    expect(screen.getByTestId("terminal-error")).toHaveTextContent("no worktree");
    expect(screen.getByTestId("terminal-restart")).toBeEnabled();
    expect(decodedWrites(fake)).not.toContain("--- restarted ---");
  });
});
