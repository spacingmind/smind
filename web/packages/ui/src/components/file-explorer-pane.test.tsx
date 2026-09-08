import { act } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { editorViewRegistry } from "@/components/code-mirror-editor";
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

/** Dispatches a real CodeMirror transaction replacing the mounted editor's whole document -- see code-mirror-editor.tsx's editorViewRegistry doc comment for why tests drive edits this way instead of simulating contentEditable input events. */
function typeInEditor(newContent: string): void {
  const container = screen.getByTestId("file-editor");
  const view = editorViewRegistry.get(container);
  if (!view) throw new Error("no CodeMirror view registered for file-editor");
  act(() => {
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: newContent } });
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

  it("selecting a file fetches and displays its content via file.read", async () => {
    const client = new FakeWsClient();
    render(<FileExplorerPane client={client} task={TASK} />);

    client.nth("file.list", 0).resolve(ROOT_ENTRIES);
    await flush();

    fireEvent.click(screen.getByTestId("file-row"));
    await flush();

    const readCall = client.nth("file.read", 0);
    expect(readCall.params).toEqual({ taskId: TASK.ID, path: "README.md" });

    readCall.resolve({ content: "# Hello\n" });
    await flush();

    expect(screen.getByTestId("file-editor-path")).toHaveTextContent("README.md");
    const editorEl = screen.getByTestId("file-editor");
    const view = editorViewRegistry.get(editorEl);
    expect(view?.state.doc.toString()).toBe("# Hello\n");
  });

  it("editing and saving calls file.write with the edited content", async () => {
    const client = new FakeWsClient();
    render(<FileExplorerPane client={client} task={TASK} />);

    client.nth("file.list", 0).resolve(ROOT_ENTRIES);
    await flush();
    fireEvent.click(screen.getByTestId("file-row"));
    await flush();
    client.nth("file.read", 0).resolve({ content: "original" });
    await flush();

    const saveButton = screen.getByRole("button", { name: "Save" });
    expect(saveButton).toBeDisabled();

    typeInEditor("edited content");
    await flush();

    expect(saveButton).not.toBeDisabled();
    fireEvent.click(saveButton);
    await flush();

    const writeCall = client.nth("file.write", 0);
    expect(writeCall.params).toEqual({ taskId: TASK.ID, path: "README.md", content: "edited content" });

    writeCall.resolve(undefined);
    await flush();

    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("a file.write failure surfaces an error without losing the user's edits", async () => {
    const client = new FakeWsClient();
    render(<FileExplorerPane client={client} task={TASK} />);

    client.nth("file.list", 0).resolve(ROOT_ENTRIES);
    await flush();
    fireEvent.click(screen.getByTestId("file-row"));
    await flush();
    client.nth("file.read", 0).resolve({ content: "original" });
    await flush();

    typeInEditor("edited content");
    await flush();

    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await flush();

    client.nth("file.write", 0).reject(new Error("disk full"));
    await flush();

    expect(screen.getByText(/save failed: disk full/i)).toBeInTheDocument();

    // The edit itself must still be there -- Save re-enabled since content
    // still differs from the last successfully-saved (i.e. originally
    // loaded) content, and the editor's own document is untouched.
    const editorEl = screen.getByTestId("file-editor");
    const view = editorViewRegistry.get(editorEl);
    expect(view?.state.doc.toString()).toBe("edited content");
    expect(screen.getByRole("button", { name: "Save" })).not.toBeDisabled();
  });

  it("switching the selected file loads the new file's content, replacing the old", async () => {
    const client = new FakeWsClient();
    render(<FileExplorerPane client={client} task={TASK} />);

    client.nth("file.list", 0).resolve([
      { name: "a.txt", isDir: false, size: 1 },
      { name: "b.txt", isDir: false, size: 1 },
    ] satisfies FileEntry[]);
    await flush();

    const fileRows = screen.getAllByTestId("file-row");
    fireEvent.click(within(fileRows[0]!).getByText("a.txt"));
    await flush();
    client.nth("file.read", 0).resolve({ content: "AAA" });
    await flush();
    expect(screen.getByTestId("file-editor-path")).toHaveTextContent("a.txt");

    fireEvent.click(within(screen.getAllByTestId("file-row")[1]!).getByText("b.txt"));
    await flush();
    client.nth("file.read", 1).resolve({ content: "BBB" });
    await flush();

    expect(screen.getByTestId("file-editor-path")).toHaveTextContent("b.txt");
    const view = editorViewRegistry.get(screen.getByTestId("file-editor"));
    expect(view?.state.doc.toString()).toBe("BBB");
  });

  it("Ctrl-S in the editor triggers a save via file.write, same as clicking the Save button", async () => {
    const client = new FakeWsClient();
    render(<FileExplorerPane client={client} task={TASK} />);

    client.nth("file.list", 0).resolve(ROOT_ENTRIES);
    await flush();
    fireEvent.click(screen.getByTestId("file-row"));
    await flush();
    client.nth("file.read", 0).resolve({ content: "original" });
    await flush();

    typeInEditor("edited via keyboard");
    await flush();

    const editorEl = screen.getByTestId("file-editor");
    const view = editorViewRegistry.get(editorEl);
    if (!view) throw new Error("no view");
    fireEvent.keyDown(view.contentDOM, { key: "s", code: "KeyS", ctrlKey: true });
    await flush();

    expect(client.nth("file.write", 0).params).toEqual({
      taskId: TASK.ID,
      path: "README.md",
      content: "edited via keyboard",
    });
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
