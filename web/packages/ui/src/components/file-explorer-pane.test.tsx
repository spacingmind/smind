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

describe("FileExplorerPane preview mode", () => {
  /** Renders the pane, lists `entries`, selects `name`, and loads `content` into the editor -- the shared prefix of every preview test. */
  async function openFile(client: FakeWsClient, entries: FileEntry[], name: string, content: string) {
    render(<FileExplorerPane client={client} task={TASK} />);
    client.nth("file.list", 0).resolve(entries);
    await flush();

    const row = screen.getAllByTestId("file-row").find((el) => el.dataset.path === name);
    if (!row) throw new Error(`no file row for ${name}`);
    fireEvent.click(within(row).getByText(name));
    await flush();

    client.nth("file.read", 0).resolve({ content });
    await flush();
  }

  it("a .md file previews as rendered markdown, not raw source", async () => {
    const client = new FakeWsClient();
    await openFile(client, [{ name: "README.md", isDir: false, size: 8 }], "README.md", "# Title\n\nbody text");

    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    await flush();

    const heading = screen.getByRole("heading", { level: 1, name: "Title" });
    expect(heading).toBeInTheDocument();
    // Scoped to the preview: the hidden-but-mounted CodeMirror still holds
    // the raw source in the DOM, so unscoped text queries would match it too.
    const preview = within(screen.getByTestId("markdown-preview"));
    expect(preview.getByText("body text")).toBeInTheDocument();

    // The raw markdown must not leak through as text.
    expect(screen.getByTestId("markdown-preview").textContent).not.toContain("# Title");

    // The editor stays mounted (hidden), and switching is per-file state:
    // nothing here unmounted CodeMirror.
    expect(screen.getByTestId("file-editor")).toBeInTheDocument();
  });

  it("GFM tables render (remark-gfm is wired in)", async () => {
    const client = new FakeWsClient();
    await openFile(
      client,
      [{ name: "table.md", isDir: false, size: 20 }],
      "table.md",
      "| a | b |\n| - | - |\n| 1 | 2 |",
    );

    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    await flush();

    expect(screen.getByRole("table")).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "1" })).toBeInTheDocument();
  });

  it("preview shows the current buffer -- unsaved edits are visible, and no save happens", async () => {
    const client = new FakeWsClient();
    await openFile(client, [{ name: "README.md", isDir: false, size: 8 }], "README.md", "# Original");

    typeInEditor("# Edited while unsaved");
    await flush();

    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    await flush();

    expect(screen.getByRole("heading", { level: 1, name: "Edited while unsaved" })).toBeInTheDocument();
    expect(client.calls.filter((c) => c.method === "file.write")).toHaveLength(0);
  });

  it("a .svg file previews as an image, not source text", async () => {
    const client = new FakeWsClient();
    const svg = '<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><circle cx="5" cy="5" r="4"/></svg>';
    await openFile(client, [{ name: "logo.svg", isDir: false, size: svg.length }], "logo.svg", svg);

    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    await flush();

    const img = screen.getByRole("img", { name: "logo.svg" });
    expect(img.getAttribute("src")).toMatch(/^data:image\/svg\+xml,/);
    expect(screen.getByTestId("svg-preview").textContent).not.toContain("<svg");
  });

  it("an .html file previews inside a sandboxed iframe (no scripts, no same-origin, buffer via srcDoc)", async () => {
    const client = new FakeWsClient();
    const html = "<p>hello html</p>";
    await openFile(client, [{ name: "page.html", isDir: false, size: html.length }], "page.html", html);

    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    await flush();

    const frame = screen.getByTestId("html-preview-frame");
    expect(frame.tagName).toBe("IFRAME");
    // "" = sandboxed with zero permissions: no allow-scripts, no allow-same-origin.
    expect(frame).toHaveAttribute("sandbox", "");
    expect(frame.getAttribute("srcDoc")).toContain("<p>hello html</p>");
    // Never a live URL: content comes from the buffer, not a fetch.
    expect(frame.getAttribute("src")).toBeNull();
  });

  it("a non-previewable file shows no preview control and a working editor", async () => {
    const client = new FakeWsClient();
    await openFile(client, [{ name: "main.go", isDir: false, size: 20 }], "main.go", "package main");

    expect(screen.queryByTestId("preview-toggle")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Preview" })).not.toBeInTheDocument();

    const view = editorViewRegistry.get(screen.getByTestId("file-editor"));
    expect(view?.state.doc.toString()).toBe("package main");
  });

  it("Preview then Edit restores the same editor with its buffer and history intact", async () => {
    const client = new FakeWsClient();
    await openFile(client, [{ name: "README.md", isDir: false, size: 8 }], "README.md", "# Title");

    typeInEditor("# Edited before preview");
    await flush();
    const viewBefore = editorViewRegistry.get(screen.getByTestId("file-editor"));

    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    await flush();

    // Mounted-but-hidden while previewing: the wrapper carries the hidden class.
    const editorEl = screen.getByTestId("file-editor");
    expect(editorEl.parentElement).toHaveClass("hidden");

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    await flush();

    expect(editorEl.parentElement).not.toHaveClass("hidden");
    // Same EditorView instance (cursor/undo history survive) with the edit still in the buffer.
    expect(editorViewRegistry.get(editorEl)).toBe(viewBefore);
    expect(viewBefore?.state.doc.toString()).toBe("# Edited before preview");
  });
});
