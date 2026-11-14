import { act } from "react";
import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";
import { TerminalPane, type TerminalHandle } from "@/components/terminal-pane";
import type { TerminalLinkMatch } from "@/lib/terminal-links";
import { openExternalUrl } from "@/lib/external-link";
import type { Task } from "@/lib/types";

// DT3-A AC3/AC5: what a Ctrl+click on a terminal link does. The link
// provider itself (Ctrl/Cmd gating, hover) lives in createRealTerminal and
// needs a real xterm; these tests drive the pane's handler through a local
// fake that records setLinkHandler -- a copy of terminal-pane.test.tsx's
// FakeTerminalHandle (not shared, so the existing suite stays untouched).

vi.mock("@/lib/external-link", () => ({ openExternalUrl: vi.fn() }));

const TASK: Task = {
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

/** A minimal TerminalHandle whose setLinkHandler just stores the handler, so tests can fire it. */
class LinkTerminalHandle implements TerminalHandle {
  opened: HTMLElement | null = null;
  disposed = false;
  private linkHandler: ((match: TerminalLinkMatch, event: MouseEvent) => void) | null = null;

  open(container: HTMLElement): void {
    this.opened = container;
  }

  onData(): { dispose(): void } {
    return { dispose: () => {} };
  }

  onResize(): { dispose(): void } {
    return { dispose: () => {} };
  }

  write(): void {}

  fit(): void {}

  dispose(): void {
    this.disposed = true;
  }

  setLinkHandler(handler: ((match: TerminalLinkMatch, event: MouseEvent) => void) | null): { dispose(): void } {
    this.linkHandler = handler;
    return { dispose: () => (this.linkHandler = null) };
  }

  /** Test-only: the Ctrl+click the real provider would forward. */
  click(match: TerminalLinkMatch): void {
    this.linkHandler?.(match, new MouseEvent("click", { ctrlKey: true }));
  }
}

/** Flushes pending microtasks, wrapped in `act` so React commits any resulting state updates before the caller asserts. */
async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

async function mount(onOpenFile: (path: string, line?: number, col?: number) => void) {
  const client = new FakeWsClient();
  const fake = new LinkTerminalHandle();
  render(
    <TerminalPane client={client} task={TASK} createTerminal={() => fake} onOpenFile={onOpenFile} />,
  );
  await flush();
  return { client, fake };
}

beforeEach(() => {
  vi.mocked(openExternalUrl).mockClear();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("TerminalPane link open", () => {
  it("a path match stats the resolved path and opens it at line and col", async () => {
    const onOpenFile = vi.fn();
    const { client, fake } = await mount(onOpenFile);

    fake.click({ kind: "path", path: "src/app.ts", start: 0, end: 12, text: "src/app.ts:12", line: 12, col: 3 });
    await flush();

    expect(client.nth("file.stat", 0).params).toEqual({ taskId: TASK.ID, path: "src/app.ts" });

    client.nth("file.stat", 0).resolve({ name: "app.ts", size: 10 });
    await flush();

    expect(onOpenFile).toHaveBeenCalledTimes(1);
    expect(onOpenFile).toHaveBeenCalledWith("src/app.ts", 12, 3);
  });

  it("a rejected file.stat opens nothing and shows the notice, gone after 4s", async () => {
    vi.useFakeTimers();
    const onOpenFile = vi.fn();
    const { client, fake } = await mount(onOpenFile);

    fake.click({ kind: "path", path: "src/gone.ts", start: 0, end: 10, text: "src/gone.ts" });
    await flush();
    client.nth("file.stat", 0).reject(new Error("no such file"));
    await flush();

    expect(onOpenFile).not.toHaveBeenCalled();
    expect(screen.getByTestId("terminal-link-notice")).toHaveTextContent("Not a file in this task's workspace");

    await act(async () => {
      vi.advanceTimersByTime(4000);
    });
    expect(screen.queryByTestId("terminal-link-notice")).not.toBeInTheDocument();
  });

  it("a refused path (.. escape) never stats and shows the notice", async () => {
    const onOpenFile = vi.fn();
    const { client, fake } = await mount(onOpenFile);

    fake.click({ kind: "path", path: "../secrets", start: 0, end: 9, text: "../secrets", line: 1 });
    await flush();

    expect(client.calls.some((c) => c.method === "file.stat")).toBe(false);
    expect(onOpenFile).not.toHaveBeenCalled();
    expect(screen.getByTestId("terminal-link-notice")).toHaveTextContent("Not a file in this task's workspace");
  });

  it("a url match goes through openExternalUrl with the exact url", async () => {
    const { fake } = await mount(vi.fn());

    fake.click({ kind: "url", url: "https://example.com/docs?a=1", start: 0, end: 28, text: "https://example.com/docs?a=1" });
    await flush();

    expect(openExternalUrl).toHaveBeenCalledTimes(1);
    expect(openExternalUrl).toHaveBeenCalledWith("https://example.com/docs?a=1");
  });

  it("a url-kind match with a non-http scheme opens nothing", async () => {
    const { fake } = await mount(vi.fn());

    // Constructed by hand: the matcher can never yield this, and the pane
    // must not hand it to the system browser even if it somehow does.
    fake.click({ kind: "url", url: "javascript:alert(1)", start: 0, end: 20, text: "javascript:alert(1)" });
    await flush();

    expect(openExternalUrl).not.toHaveBeenCalled();
  });
});
