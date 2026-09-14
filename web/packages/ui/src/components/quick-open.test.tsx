import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { QuickOpen } from "@/components/quick-open";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { Task } from "@/lib/types";

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

const PATHS = [
  "src/components/file-editor-pane.tsx",
  "src/components/file-preview.tsx",
  "src/lib/file-icons.ts",
  "README.md",
];

/** Flushes pending microtasks, wrapped in `act` so React commits any resulting state updates before the caller asserts. */
async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

async function renderOpen(onOpenFile = vi.fn(), onOpenChange = vi.fn()) {
  const client = new FakeWsClient();
  render(
    <QuickOpen client={client} task={TASK} open onOpenChange={onOpenChange} onOpenFile={onOpenFile} />,
  );
  expect(client.nth("task.searchIndex", 0).params).toEqual({ taskId: TASK.ID });
  client.nth("task.searchIndex", 0).resolve({ paths: PATHS });
  await flush();
  return { client, onOpenFile, onOpenChange };
}

describe("QuickOpen", () => {
  it("fetches the task's search index on open and lists every path for a blank query", async () => {
    await renderOpen();

    expect(screen.getAllByTestId("quick-open-result")).toHaveLength(PATHS.length);
  });

  it("prefetches the search index even while closed, so the first open has no spinner delay", async () => {
    const client = new FakeWsClient();
    render(<QuickOpen client={client} task={TASK} open={false} onOpenChange={vi.fn()} onOpenFile={vi.fn()} />);

    expect(client.nth("task.searchIndex", 0).params).toEqual({ taskId: TASK.ID });
    client.nth("task.searchIndex", 0).resolve({ paths: PATHS });
    await flush();

    // Still closed: nothing rendered, but the fetch already happened.
    expect(screen.queryByTestId("quick-open")).not.toBeInTheDocument();
  });

  it("ranks the expected path first as the query narrows, with the top match pre-selected", async () => {
    await renderOpen();

    fireEvent.change(screen.getByTestId("quick-open-input"), { target: { value: "editor" } });
    await flush();

    const results = screen.getAllByTestId("quick-open-result");
    expect(results[0]).toHaveTextContent("file-editor-pane.tsx");
    expect(results[0]).toHaveAttribute("data-active", "true");
  });

  it("Enter opens the active match as a file tab and closes the dialog", async () => {
    const { onOpenFile, onOpenChange } = await renderOpen();

    fireEvent.change(screen.getByTestId("quick-open-input"), { target: { value: "editor" } });
    await flush();
    fireEvent.keyDown(screen.getByTestId("quick-open-input"), { key: "Enter" });

    expect(onOpenFile).toHaveBeenCalledWith("src/components/file-editor-pane.tsx");
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("arrow keys move the active selection before Enter opens it", async () => {
    const { onOpenFile } = await renderOpen();

    fireEvent.change(screen.getByTestId("quick-open-input"), { target: { value: "file" } });
    await flush();

    fireEvent.keyDown(screen.getByTestId("quick-open-input"), { key: "ArrowDown" });
    const results = screen.getAllByTestId("quick-open-result");
    expect(results[1]).toHaveAttribute("data-active", "true");

    fireEvent.keyDown(screen.getByTestId("quick-open-input"), { key: "Enter" });
    expect(onOpenFile).toHaveBeenCalledWith(results[1]!.textContent);
  });

  it("Escape closes without opening a file", async () => {
    const { onOpenFile, onOpenChange } = await renderOpen();

    fireEvent.keyDown(screen.getByTestId("quick-open"), { key: "Escape" });

    expect(onOpenFile).not.toHaveBeenCalled();
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("resets the query and selection each time it's reopened", async () => {
    const client = new FakeWsClient();
    const { rerender } = render(
      <QuickOpen client={client} task={TASK} open onOpenChange={vi.fn()} onOpenFile={vi.fn()} />,
    );
    client.nth("task.searchIndex", 0).resolve({ paths: PATHS });
    await flush();

    fireEvent.change(screen.getByTestId("quick-open-input"), { target: { value: "editor" } });
    await flush();

    rerender(<QuickOpen client={client} task={TASK} open={false} onOpenChange={vi.fn()} onOpenFile={vi.fn()} />);
    rerender(<QuickOpen client={client} task={TASK} open onOpenChange={vi.fn()} onOpenFile={vi.fn()} />);
    await flush();

    expect(screen.getByTestId("quick-open-input")).toHaveValue("");
  });

  it("shows an empty state for a query with no matches, and an error if the fetch fails", async () => {
    const client = new FakeWsClient();
    render(<QuickOpen client={client} task={TASK} open onOpenChange={vi.fn()} onOpenFile={vi.fn()} />);
    client.nth("task.searchIndex", 0).reject(new Error("daemon unreachable"));
    await flush();

    expect(screen.getByTestId("quick-open-error")).toHaveTextContent("daemon unreachable");
  });

  it("shows no results, not an error, for a query matching nothing", async () => {
    await renderOpen();

    fireEvent.change(screen.getByTestId("quick-open-input"), { target: { value: "zzzznotarealquery" } });
    await flush();

    expect(screen.queryAllByTestId("quick-open-result")).toHaveLength(0);
    expect(screen.getByText("No matching files")).toBeInTheDocument();
  });
});
