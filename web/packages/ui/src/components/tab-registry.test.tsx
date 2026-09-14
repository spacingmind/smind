import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { editorViewRegistry } from "@/components/code-mirror-editor";
import { FileEditorPane } from "@/components/file-editor-pane";
import {
  TabLabel,
  defaultTabsForTask,
  fileTab,
  fileTabKey,
  filePathFromTabKey,
  nextTerminalTab,
  terminalTab,
} from "@/components/tab-registry";
import { resetDirtyBuffers } from "@/lib/dirty-buffers";
import { clearTerminalActivity, markTerminalActivity, resetTerminalSessions } from "@/lib/terminal-sessions";
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

const PATH = "cmd/smind/main.go";

afterEach(() => {
  resetDirtyBuffers();
  resetTerminalSessions();
});

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** See file-editor-pane.test.tsx's identical helper for why edits are driven through a real CodeMirror transaction. */
function typeInEditor(newContent: string): void {
  const container = screen.getByTestId("file-editor");
  const view = editorViewRegistry.get(container);
  if (!view) throw new Error("no CodeMirror view registered for file-editor");
  act(() => {
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: newContent } });
  });
}

describe("tab keys", () => {
  it("round-trips a file path through its tab key", () => {
    const key = fileTabKey(TASK.ID, PATH);
    expect(key).toBe("1:file:cmd/smind/main.go");
    expect(filePathFromTabKey(key)).toBe(PATH);
    expect(fileTab(TASK.ID, PATH).path).toBe(PATH);
  });

  it("returns an empty path for a non-file tab key", () => {
    expect(filePathFromTabKey("1:diff")).toBe("");
  });
});

describe("TabLabel", () => {
  it("renders each base tab's kind icon", () => {
    for (const entry of defaultTabsForTask(TASK.ID)) {
      const { unmount } = render(<TabLabel entry={entry} />);
      expect(screen.getByTestId("tab-icon")).toHaveAttribute("data-icon", entry.kind);
      unmount();
    }
  });

  it("renders a file tab's file-type icon rather than the generic kind icon", () => {
    render(<TabLabel entry={fileTab(TASK.ID, PATH)} />);
    expect(screen.getByTestId("file-icon")).toHaveAttribute("data-icon", "go");
    expect(screen.queryByTestId("tab-icon")).not.toBeInTheDocument();
  });

  it("marks the tab while its editor's buffer is dirty, and clears the marker on save", async () => {
    const client = new FakeWsClient();
    const entry = fileTab(TASK.ID, PATH);
    render(
      <>
        <TabLabel entry={entry} />
        <FileEditorPane client={client} task={TASK} path={PATH} />
      </>,
    );
    client.nth("file.read", 0).resolve({ content: "package main\n", mtime: "t0" });
    await flush();

    expect(screen.queryByTestId("tab-dirty-marker")).not.toBeInTheDocument();

    typeInEditor("package main // edited\n");
    await flush();

    const marker = screen.getByTestId("tab-dirty-marker");
    expect(marker).toHaveAttribute("data-tab-key", entry.key);
    expect(marker).toHaveAccessibleName("unsaved changes");

    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    client.nth("file.write", 0).resolve({ mtime: "t1" });
    await flush();

    expect(screen.queryByTestId("tab-dirty-marker")).not.toBeInTheDocument();
  });

  it("clears the marker when the editor unmounts with the buffer still dirty", async () => {
    const client = new FakeWsClient();
    const entry = fileTab(TASK.ID, PATH);

    function Harness({ editorOpen }: { editorOpen: boolean }) {
      return (
        <>
          <TabLabel entry={entry} />
          {editorOpen && <FileEditorPane client={client} task={TASK} path={PATH} />}
        </>
      );
    }

    const { rerender } = render(<Harness editorOpen />);
    client.nth("file.read", 0).resolve({ content: "package main\n", mtime: "t0" });
    await flush();

    typeInEditor("dirty\n");
    await flush();
    expect(screen.getByTestId("tab-dirty-marker")).toBeInTheDocument();

    rerender(<Harness editorOpen={false} />);
    await flush();

    expect(screen.queryByTestId("tab-dirty-marker")).not.toBeInTheDocument();
  });

  it("marks only the tab whose own buffer is dirty", async () => {
    const client = new FakeWsClient();
    const mine = fileTab(TASK.ID, PATH);
    const other = fileTab(TASK.ID, "README.md");
    render(
      <>
        <TabLabel entry={mine} />
        <TabLabel entry={other} />
        <FileEditorPane client={client} task={TASK} path={PATH} />
      </>,
    );
    client.nth("file.read", 0).resolve({ content: "package main\n", mtime: "t0" });
    await flush();

    typeInEditor("dirty\n");
    await flush();

    const markers = screen.getAllByTestId("tab-dirty-marker");
    expect(markers).toHaveLength(1);
    expect(markers[0]).toHaveAttribute("data-tab-key", mine.key);
  });
});

describe("terminal tabs (Item 20)", () => {
  it("assigns increasing indices starting at 2, the base terminal tab being index 1 implicitly", () => {
    const base = defaultTabsForTask(TASK.ID).find((t) => t.kind === "terminal")!;
    expect(base.key).toBe(`${TASK.ID}:terminal`);
    expect(base.closable).toBe(false);

    const second = nextTerminalTab(TASK.ID, [base]);
    expect(second).toEqual(terminalTab(TASK.ID, 2));
    expect(second.closable).toBe(true);

    const third = nextTerminalTab(TASK.ID, [base, second]);
    expect(third.key).toBe(`${TASK.ID}:terminal:3`);
  });

  it("reuses an index freed by closing a tab, rather than counting forever", () => {
    const base = defaultTabsForTask(TASK.ID).find((t) => t.kind === "terminal")!;
    const second = terminalTab(TASK.ID, 2);
    const third = terminalTab(TASK.ID, 3);

    // Tab 2 was closed; only base and tab 3 remain open.
    const next = nextTerminalTab(TASK.ID, [base, third]);
    expect(next.key).toBe(second.key);
  });

  it("marks a terminal tab with an activity dot, distinct from the dirty marker", () => {
    const entry = terminalTab(TASK.ID, 2);
    markTerminalActivity(entry.key);

    render(<TabLabel entry={entry} />);
    const marker = screen.getByTestId("tab-activity-marker");
    expect(marker).toHaveAttribute("data-tab-key", entry.key);
    expect(marker).toHaveAccessibleName("new output");
    expect(screen.queryByTestId("tab-dirty-marker")).not.toBeInTheDocument();

    clearTerminalActivity(entry.key);
  });
});
