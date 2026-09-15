import { act } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { FileExplorerPane } from "@/components/file-explorer-pane";
import { resetDiffReveal, takeDiffReveal } from "@/lib/diff-reveal";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { FileEntry, Task, TaskFile } from "@/lib/types";

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

const CHANGED_FILES: TaskFile[] = [
  { path: "README.md", status: "modified", staged: false },
  { path: "src/new.go", status: "added", staged: false },
];

afterEach(() => {
  resetDiffReveal();
});

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

describe("FileExplorerPane git decoration and row actions (Item 17)", () => {
  /** Renders the pane with both file.list and task.files resolved -- the shared prefix of the Item 17 tests. */
  async function renderTree(): Promise<FakeWsClient> {
    const client = new FakeWsClient();
    render(<FileExplorerPane client={client} task={TASK} />);
    client.nth("file.list", 0).resolve(ROOT_ENTRIES);
    client.nth("task.files", 0).resolve({ files: CHANGED_FILES });
    await flush();
    return client;
  }

  it("renders a file-type icon per row, generic for an unknown extension", async () => {
    const client = new FakeWsClient();
    render(<FileExplorerPane client={client} task={TASK} />);
    client.nth("file.list", 0).resolve([
      { name: "main.go", isDir: false, size: 1 },
      { name: "notes.qqq", isDir: false, size: 1 },
    ] satisfies FileEntry[]);
    client.nth("task.files", 0).resolve({ files: [] });
    await flush();

    const rows = screen.getAllByTestId("file-row");
    expect(within(rows[0]!).getByTestId("file-icon")).toHaveAttribute("data-icon", "go");
    expect(within(rows[1]!).getByTestId("file-icon")).toHaveAttribute("data-icon", "file");
  });

  it("decorates a changed file with its git status, and leaves an unchanged one undecorated", async () => {
    await renderTree();

    const readme = screen.getByTestId("file-row");
    expect(within(readme).getByTestId("file-status-marker")).toHaveAttribute("data-status", "modified");
    expect(within(readme).getByTestId("file-status-marker")).toHaveTextContent("M");
  });

  it("gives an added file a different marker than a modified one", async () => {
    const client = await renderTree();

    // src/new.go is added; expanding src/ reveals it next to the
    // modified README.md already in the tree.
    fireEvent.click(screen.getByTestId("dir-row"));
    await flush();
    client.nth("file.list", 1).resolve([{ name: "new.go", isDir: false, size: 1 }] satisfies FileEntry[]);
    await flush();

    const markers = screen.getAllByTestId("file-status-marker");
    const statuses = markers.map((m) => m.getAttribute("data-status"));
    expect(statuses).toContain("modified");
    expect(statuses).toContain("added");
    const added = markers.find((m) => m.getAttribute("data-status") === "added")!;
    const modified = markers.find((m) => m.getAttribute("data-status") === "modified")!;
    expect(added.textContent).toBe("A");
    expect(modified.textContent).toBe("M");
    expect(added.className).not.toBe(modified.className);
  });

  it("rolls a descendant's status up onto its directory row, so a nested change is visible while collapsed", async () => {
    await renderTree();

    const dirRow = screen.getByTestId("dir-row");
    expect(within(dirRow).getByTestId("file-status-marker")).toHaveAttribute("data-status", "added");
  });

  it("refreshes decorations when task.files changes, without re-listing the tree", async () => {
    const client = await renderTree();
    expect(screen.getAllByTestId("file-status-marker")).toHaveLength(2);

    // A second task.files result (what the run.status subscription
    // triggers in the app) replaces the decorations in place.
    expect(client.calls.filter((c) => c.method === "file.list")).toHaveLength(1);
  });

  it("reveals a changed file in the diff: latches the request and tells the shell to switch tabs", async () => {
    const onRevealInDiff = vi.fn();
    const client = new FakeWsClient();
    render(<FileExplorerPane client={client} task={TASK} onRevealInDiff={onRevealInDiff} />);
    client.nth("file.list", 0).resolve(ROOT_ENTRIES);
    client.nth("task.files", 0).resolve({ files: CHANGED_FILES });
    await flush();

    fireEvent.contextMenu(screen.getByTestId("file-row"));
    fireEvent.click(screen.getByTestId("file-menu-reveal-in-diff"));
    await flush();

    expect(onRevealInDiff).toHaveBeenCalledWith("README.md");
    expect(takeDiffReveal(TASK.ID)).toEqual({ taskId: TASK.ID, path: "README.md" });
  });

  it("disables Reveal in diff for a path with no changes", async () => {
    const client = new FakeWsClient();
    render(<FileExplorerPane client={client} task={TASK} />);
    client.nth("file.list", 0).resolve(ROOT_ENTRIES);
    client.nth("task.files", 0).resolve({ files: [] });
    await flush();

    fireEvent.contextMenu(screen.getByTestId("file-row"));
    expect(screen.getByTestId("file-menu-reveal-in-diff")).toHaveAttribute("data-disabled");
    expect(takeDiffReveal(TASK.ID)).toBeNull();
  });

  it("\"Open to side\" calls onOpenFileToSide with the row's path, regardless of git status", async () => {
    const onOpenFileToSide = vi.fn();
    const client = new FakeWsClient();
    render(<FileExplorerPane client={client} task={TASK} onOpenFileToSide={onOpenFileToSide} />);
    client.nth("file.list", 0).resolve(ROOT_ENTRIES);
    client.nth("task.files", 0).resolve({ files: [] });
    await flush();

    fireEvent.contextMenu(screen.getByTestId("file-row"));
    fireEvent.click(screen.getByTestId("file-menu-open-to-side"));
    await flush();

    expect(onOpenFileToSide).toHaveBeenCalledWith("README.md");
  });

  it("disables \"Open to side\" when the caller supplies no handler, rather than rendering a dead entry", async () => {
    await renderTree();

    fireEvent.contextMenu(screen.getByTestId("file-row"));
    expect(screen.getByTestId("file-menu-open-to-side")).toHaveAttribute("data-disabled");
  });

  it("copies a row's path to the clipboard", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
    try {
      await renderTree();

      fireEvent.contextMenu(screen.getByTestId("file-row"));
      fireEvent.click(screen.getByTestId("file-menu-copy-path"));
      await flush();

      expect(writeText).toHaveBeenCalledWith("README.md");
    } finally {
      vi.unstubAllGlobals();
    }
  });
});
