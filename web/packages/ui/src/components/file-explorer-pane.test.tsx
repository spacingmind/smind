import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { vi } from "vitest";
import { describe, expect, it } from "vitest";

import { FileExplorerPane } from "@/components/file-explorer-pane";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { FileEntry, Task } from "@/lib/types";

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

const ROOT_ENTRIES: FileEntry[] = [
  { name: "src", isDir: true, size: 0 },
  { name: "README.md", isDir: false, size: 42 },
];

/** Flushes pending microtasks, wrapped in `act` so React commits any resulting state updates before the caller asserts. */
async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("FileExplorerPane", () => {
  it("renders the tree from file.list's result", async () => {
    const client = new FakeWsClient();
    render(<FileExplorerPane client={client} task={TASK} />);

    expect(client.nth("file.list", 0).params).toEqual({ taskId: TASK.ID, path: "" });
    client.nth("file.list", 0).resolve(ROOT_ENTRIES);
    await flush();

    expect(screen.getByTestId("dir-row")).toHaveTextContent("src");
    expect(screen.getByTestId("file-row")).toHaveTextContent("README.md");
  });

  it("lazily expands a subdirectory, fetching file.list only on first expand", async () => {
    const client = new FakeWsClient();
    render(<FileExplorerPane client={client} task={TASK} />);

    client.nth("file.list", 0).resolve(ROOT_ENTRIES);
    await flush();

    expect(client.calls.filter((c) => c.method === "file.list")).toHaveLength(1);

    fireEvent.click(screen.getByTestId("dir-row"));
    await flush();

    const subCall = client.nth("file.list", 1);
    expect(subCall.params).toEqual({ taskId: TASK.ID, path: "src" });

    subCall.resolve([{ name: "main.go", isDir: false, size: 10 }] satisfies FileEntry[]);
    await flush();

    expect(screen.getByText("main.go")).toBeInTheDocument();

    // Collapsing and re-expanding must not re-fetch: file.list was already
    // resolved for "src".
    fireEvent.click(screen.getByTestId("dir-row"));
    await flush();
    fireEvent.click(screen.getByTestId("dir-row"));
    await flush();
    expect(client.calls.filter((c) => c.method === "file.list")).toHaveLength(2);
  });

  it("clicking a file highlights its row and calls onOpenFile with its path", async () => {
    const client = new FakeWsClient();
    const onOpenFile = vi.fn();
    render(<FileExplorerPane client={client} task={TASK} onOpenFile={onOpenFile} />);

    client.nth("file.list", 0).resolve(ROOT_ENTRIES);
    await flush();

    fireEvent.click(screen.getByTestId("file-row"));
    await flush();

    expect(onOpenFile).toHaveBeenCalledWith("README.md");
    // Row highlighting still works via the hook's selectedPath.
    expect(screen.getByTestId("file-row")).toHaveClass("bg-accent");
  });

  it("renders tree-only without an inline editor even after a file is clicked", async () => {
    const client = new FakeWsClient();
    render(<FileExplorerPane client={client} task={TASK} onOpenFile={() => {}} />);

    client.nth("file.list", 0).resolve(ROOT_ENTRIES);
    await flush();
    fireEvent.click(screen.getByTestId("file-row"));
    await flush();

    expect(screen.queryByTestId("file-editor")).not.toBeInTheDocument();
  });
});
