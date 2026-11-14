import { act } from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";
import { TerminalPane, type TerminalHandle } from "@/components/terminal-pane";
import { resetTerminalSessions } from "@/lib/terminal-sessions";
import { attachmentDraftKey, useComposerAttachmentsStore } from "@/stores/composer-attachments-store";
import { useFocusedChatStore } from "@/stores/focused-chat-store";
import type { Task } from "@/lib/types";

// Same reason as terminal-pane.test.tsx: the session<->tab binding lives
// outside React and survives across renders unless cleared between tests.
afterEach(() => {
  resetTerminalSessions();
  cleanup();
});

beforeEach(() => {
  useComposerAttachmentsStore.setState({ byKey: {}, mentions: {}, pendingInserts: {}, pendingTexts: {} });
  useFocusedChatStore.setState({ byTask: { 1: 10 }, revealRequest: null });
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

/**
 * terminal-restart.test.tsx's FakeTerminalHandle, plus the two reads the
 * send menu does at open time: the selection and the scrollback tail.
 */
class FakeTerminalHandle implements TerminalHandle {
  opened: HTMLElement | null = null;
  disposed = false;
  fitCalls = 0;
  selection = "";
  lastLines = "";
  writes: (string | Uint8Array)[] = [];

  open(container: HTMLElement): void {
    this.opened = container;
  }

  onData(_callback: (data: string) => void): { dispose(): void } {
    return { dispose: () => undefined };
  }

  onResize(_callback: (size: { cols: number; rows: number }) => void): { dispose(): void } {
    return { dispose: () => undefined };
  }

  write(data: string | Uint8Array): void {
    this.writes.push(data);
  }

  fit(): void {
    this.fitCalls++;
  }

  getSelection(): string {
    return this.selection;
  }

  getLastLines(count: number): string {
    return this.lastLines.split("\n").slice(-count).join("\n");
  }

  dispose(): void {
    this.disposed = true;
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

/** Mounts TASK_A into a running session (terminal-restart.test.tsx's mountRunning, trimmed). */
async function mount() {
  const client = new FakeWsClient();
  const fake = new FakeTerminalHandle();
  render(<TerminalPane client={client} task={TASK_A} createTerminal={() => fake} />);
  await flush();
  client.nth("terminal.list", 0).resolve([]);
  await flush();
  client.nth("terminal.create", 0).resolve({ terminalId: "term-1" });
  await flush();
  return { client, fake };
}

/** Opens the send menu (radix triggers on pointerdown) and waits for its items. */
async function openMenu() {
  fireEvent.pointerDown(screen.getByTestId("terminal-send-menu"), { button: 0 });
  return screen.findAllByTestId(/^send-entry-/);
}

describe("TerminalPane send set (DT3-C)", () => {
  it("is rendered with a focused chat for the task, and not without one", async () => {
    await mount();
    expect(screen.getByTestId("terminal-send-menu")).toBeInTheDocument();

    useFocusedChatStore.setState({ byTask: {}, revealRequest: null });
    await act(async () => {});
    expect(screen.queryByTestId("terminal-send-menu")).not.toBeInTheDocument();
  });

  it("Selection is disabled while nothing is selected, and shows its size once selected", async () => {
    const { fake } = await mount();
    fake.selection = "";
    let items = await openMenu();
    expect(items[0]).toHaveAttribute("data-disabled");
    expect(items[0]).toHaveTextContent("nothing selected");
    fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });

    fake.selection = "hello world!";
    items = await openMenu();
    expect(items[0]).not.toHaveAttribute("data-disabled");
    expect(items[0]).toHaveTextContent("12 B");
  });

  it("choosing Selection queues exactly one fenced block under the focused chat's key", async () => {
    const { fake } = await mount();
    fake.selection = "hello world!";
    const items = await openMenu();
    fireEvent.click(items[0]);

    const key = attachmentDraftKey(TASK_A.ID, 10);
    const texts = useComposerAttachmentsStore.getState().pendingTexts[key] ?? [];
    expect(texts).toHaveLength(1);
    expect(texts[0]!.startsWith("Terminal output, selection, 12 B:")).toBe(true);
    expect(texts[0]).toContain("```\nhello world!\n```");
  });

  it("Last 100 lines queues a block labelled with the tail's size", async () => {
    const { fake } = await mount();
    fake.lastLines = "first\nsecond\nthird\n";
    const items = await openMenu();
    expect(items[1]).not.toHaveAttribute("data-disabled");
    fireEvent.click(items[1]);

    const key = attachmentDraftKey(TASK_A.ID, 10);
    const texts = useComposerAttachmentsStore.getState().pendingTexts[key] ?? [];
    expect(texts).toHaveLength(1);
    expect(texts[0]!.split("\n")[0]).toBe("Terminal output, last 100 lines, 19 B:");
    expect(texts[0]).toContain("```\nfirst\nsecond\nthird\n```");
  });

  it("a 20 KiB tail is cut to the cap, says what was omitted, and the entry shows the cut size", async () => {
    const { fake } = await mount();
    fake.lastLines = "a".repeat(20 * 1024);
    const items = await openMenu();
    // The size shown is what will actually be added, not the full tail.
    expect(items[1]).toHaveTextContent("16 KiB");
    expect(items[1]).not.toHaveTextContent("20");
    fireEvent.click(items[1]);

    const key = attachmentDraftKey(TASK_A.ID, 10);
    const texts = useComposerAttachmentsStore.getState().pendingTexts[key] ?? [];
    expect(texts).toHaveLength(1);
    expect(texts[0]!.split("\n")[0]).toBe("Terminal output, last 100 lines, 16 KiB:");
    expect(texts[0]).toContain("(4.0 KiB omitted)");
    expect(texts[0]).not.toContain("a".repeat(17 * 1024));
  });

  it("opening the menu alone queues nothing", async () => {
    const { fake } = await mount();
    fake.selection = "hello world!";
    fake.lastLines = "first\nsecond";
    await openMenu();

    expect(useComposerAttachmentsStore.getState().pendingTexts).toEqual({});
    expect(useComposerAttachmentsStore.getState().pendingInserts).toEqual({});
  });
});
