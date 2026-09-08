import { act } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { editorViewRegistry } from "@/components/code-mirror-editor";
import { FileEditorPane } from "@/components/file-editor-pane";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { Task } from "@/lib/types";

// Editor-behavior tests moved here from file-explorer-pane.test.tsx when
// the explorer pane became tree-only -- see the plan's Decisions.

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

const PATH = "README.md";

/** Renders the pane for path and loads content into the editor -- the shared prefix of every test here. */
async function openFile(path: string, content: string): Promise<FakeWsClient> {
  const client = new FakeWsClient();
  render(<FileEditorPane client={client} task={TASK} path={path} />);
  client.nth("file.read", 0).resolve({ content });
  await flush();
  return client;
}

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

describe("FileEditorPane", () => {
  it("reads the file on mount and displays its content", async () => {
    const client = new FakeWsClient();
    render(<FileEditorPane client={client} task={TASK} path={PATH} />);

    expect(client.nth("file.read", 0).params).toEqual({ taskId: TASK.ID, path: PATH });

    client.nth("file.read", 0).resolve({ content: "# Hello\n" });
    await flush();

    expect(screen.getByTestId("file-editor-path")).toHaveTextContent(PATH);
    const view = editorViewRegistry.get(screen.getByTestId("file-editor"));
    expect(view?.state.doc.toString()).toBe("# Hello\n");
  });

  it("shows a loading state until file.read resolves", async () => {
    const client = new FakeWsClient();
    render(<FileEditorPane client={client} task={TASK} path={PATH} />);

    expect(screen.getByText("Loading…")).toBeInTheDocument();
    expect(screen.queryByTestId("file-editor")).not.toBeInTheDocument();

    client.nth("file.read", 0).resolve({ content: "x" });
    await flush();
    expect(screen.getByTestId("file-editor")).toBeInTheDocument();
  });

  it("editing and saving calls file.write with the edited content", async () => {
    const client = new FakeWsClient();
    render(<FileEditorPane client={client} task={TASK} path={PATH} />);

    client.nth("file.read", 0).resolve({ content: "original" });
    await flush();

    const saveButton = screen.getByRole("button", { name: "Save" });
    expect(saveButton).toBeDisabled();

    typeInEditor("edited content");
    await flush();

    expect(saveButton).not.toBeDisabled();
    fireEvent.click(saveButton);
    await flush();

    expect(client.nth("file.write", 0).params).toEqual({ taskId: TASK.ID, path: PATH, content: "edited content" });

    client.nth("file.write", 0).resolve(undefined);
    await flush();

    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("a file.write failure surfaces an error without losing the user's edits", async () => {
    const client = new FakeWsClient();
    render(<FileEditorPane client={client} task={TASK} path={PATH} />);

    client.nth("file.read", 0).resolve({ content: "original" });
    await flush();

    typeInEditor("edited content");
    await flush();

    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await flush();

    client.nth("file.write", 0).reject(new Error("disk full"));
    await flush();

    expect(screen.getByText(/save failed: disk full/i)).toBeInTheDocument();

    const view = editorViewRegistry.get(screen.getByTestId("file-editor"));
    expect(view?.state.doc.toString()).toBe("edited content");
    expect(screen.getByRole("button", { name: "Save" })).not.toBeDisabled();
  });

  it("Ctrl-S in the editor triggers a save via file.write, same as clicking the Save button", async () => {
    const client = new FakeWsClient();
    render(<FileEditorPane client={client} task={TASK} path={PATH} />);

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
      path: PATH,
      content: "edited via keyboard",
    });
  });
});

// Preview-mode tests ported from file-explorer-pane.test.tsx (develop's
// PR #51) when the explorer became tree-only and the editor view moved
// here -- see the tab-registry plan's Decisions.

describe("FileEditorPane preview mode", () => {
  it("a .md file previews as rendered markdown, not raw source", async () => {
    await openFile("README.md", "# Title\n\nbody text");

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
    await openFile("table.md", "| a | b |\n| - | - |\n| 1 | 2 |");

    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    await flush();

    expect(screen.getByRole("table")).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "1" })).toBeInTheDocument();
  });

  it("preview shows the current buffer -- unsaved edits are visible, and no save happens", async () => {
    const client = await openFile("README.md", "# Original");

    typeInEditor("# Edited while unsaved");
    await flush();

    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    await flush();

    expect(screen.getByRole("heading", { level: 1, name: "Edited while unsaved" })).toBeInTheDocument();
    expect(client.calls.filter((c) => c.method === "file.write")).toHaveLength(0);
  });

  it("a .svg file previews as an image, not source text", async () => {
    const svg = '<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><circle cx="5" cy="5" r="4"/></svg>';
    await openFile("logo.svg", svg);

    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    await flush();

    const img = screen.getByRole("img", { name: "logo.svg" });
    expect(img.getAttribute("src")).toMatch(/^data:image\/svg\+xml,/);
    expect(screen.getByTestId("svg-preview").textContent).not.toContain("<svg");
  });

  it("an .html file previews inside a sandboxed iframe (no scripts, no same-origin, buffer via srcDoc)", async () => {
    await openFile("page.html", "<p>hello html</p>");

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
    await openFile("main.go", "package main");

    expect(screen.queryByTestId("preview-toggle")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Preview" })).not.toBeInTheDocument();

    const view = editorViewRegistry.get(screen.getByTestId("file-editor"));
    expect(view?.state.doc.toString()).toBe("package main");
  });

  it("Preview then Edit restores the same editor with its buffer and history intact", async () => {
    await openFile("README.md", "# Title");

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
