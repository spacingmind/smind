import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
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
