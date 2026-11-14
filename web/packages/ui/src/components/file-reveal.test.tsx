import { act } from "react";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { editorViewRegistry } from "@/components/code-mirror-editor";
import { FileEditorPane } from "@/components/file-editor-pane";
import { fileTabKey } from "@/components/tab-registry";
import { useFileRevealStore } from "@/stores/file-reveal-store";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { Task } from "@/lib/types";

// DT3-A AC4: a queued reveal request lands after the file loads, selects
// the whole line (the visible mark), clamps beyond-the-end lines, and is
// consumed exactly once.

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

const PATH = "src/app.ts";
const CONTENT = "l1\nl2\nl3\nl4\nl5";

/** Flushes pending microtasks, wrapped in `act` so React commits any resulting state updates before the caller asserts. */
async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** Mounts the pane with the read pending; the test resolves it when ready. */
async function mount(): Promise<FakeWsClient> {
  const client = new FakeWsClient();
  render(<FileEditorPane client={client} task={TASK} path={PATH} />);
  await flush();
  return client;
}

function view() {
  const v = editorViewRegistry.get(screen.getByTestId("file-editor"));
  if (!v) throw new Error("no CodeMirror view mounted");
  return v;
}

afterEach(() => {
  useFileRevealStore.setState({ requests: {} });
});

describe("file reveal", () => {
  it("a request queued while loading selects the whole line once the file has loaded", async () => {
    const tabKey = fileTabKey(TASK.ID, PATH);
    const client = await mount();
    act(() => useFileRevealStore.getState().request(tabKey, 3));
    await flush();

    client.nth("file.read", 0).resolve({ content: CONTENT });
    await flush();

    const ln = view().state.doc.line(3);
    const selection = view().state.selection.main;
    expect(selection.from).toBe(ln.from);
    expect(selection.to).toBe(ln.to);
    // Consumed: nothing left for the next mount.
    expect(useFileRevealStore.getState().requests[tabKey]).toBeUndefined();
  });

  it("a request queued BEFORE the pane mounts (as the terminal link does) waits for the read, then selects the line", async () => {
    const tabKey = fileTabKey(TASK.ID, PATH);
    useFileRevealStore.getState().request(tabKey, 3);
    const client = await mount();
    // Still unread: the request must not have been spent on the empty initial document.
    expect(useFileRevealStore.getState().requests[tabKey]).toBeDefined();

    client.nth("file.read", 0).resolve({ content: CONTENT });
    await flush();

    const ln = view().state.doc.line(3);
    expect(view().state.selection.main.from).toBe(ln.from);
    expect(view().state.selection.main.to).toBe(ln.to);
    expect(useFileRevealStore.getState().requests[tabKey]).toBeUndefined();
  });

  it("clamps a line beyond the end of the document to the last line", async () => {
    const tabKey = fileTabKey(TASK.ID, PATH);
    const client = await mount();
    client.nth("file.read", 0).resolve({ content: CONTENT });
    await flush();

    act(() => useFileRevealStore.getState().request(tabKey, 99));
    await flush();

    const ln = view().state.doc.line(view().state.doc.lines);
    const selection = view().state.selection.main;
    expect(selection.from).toBe(ln.from);
    expect(selection.to).toBe(ln.to);
  });

  it("a second request on the already-open tab moves the selection", async () => {
    const tabKey = fileTabKey(TASK.ID, PATH);
    const client = await mount();
    client.nth("file.read", 0).resolve({ content: CONTENT });
    await flush();

    act(() => useFileRevealStore.getState().request(tabKey, 2));
    await flush();
    expect(view().state.selection.main.from).toBe(view().state.doc.line(2).from);

    act(() => useFileRevealStore.getState().request(tabKey, 5));
    await flush();
    expect(view().state.selection.main.from).toBe(view().state.doc.line(5).from);

    expect(useFileRevealStore.getState().requests[tabKey]).toBeUndefined();
  });

  it("a request for a different tab is left untouched", async () => {
    const otherKey = fileTabKey(TASK.ID, "src/other.ts");
    const client = await mount();
    client.nth("file.read", 0).resolve({ content: CONTENT });
    await flush();

    act(() => useFileRevealStore.getState().request(otherKey, 2));
    await flush();

    expect(view().state.selection.main.from).toBe(0);
    expect(useFileRevealStore.getState().requests[otherKey]).toBeDefined();
  });
});
