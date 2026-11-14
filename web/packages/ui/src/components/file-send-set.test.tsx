import { act } from "react";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { editorViewRegistry } from "@/components/code-mirror-editor";
import { FileEditorPane } from "@/components/file-editor-pane";
import { attachmentDraftKey, useComposerAttachmentsStore } from "@/stores/composer-attachments-store";
import { useFocusedChatStore } from "@/stores/focused-chat-store";
import { clearToasts } from "@/components/ui/toast";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp } from "@/test/render-with-daemon";
import type { AttachmentLimits, Task } from "@/lib/types";

// DT3-C AC10/AC11: the editor's "Send to agent" menu offers the file
// reference and the selected lines as `@path` mentions through the same
// queue as "Add to chat", sized at open time, gated on the daemon's
// file_ref support and a focused chat.

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
const KEY = attachmentDraftKey(TASK.ID, 10);

const LIMITS: AttachmentLimits = {
  maxCount: 10,
  maxImageBytes: 1 << 20,
  maxFileBytes: 1 << 20,
  maxTotalBytes: 1 << 20,
  maxMessageBytes: 1 << 22,
  images: ["image/png"],
  kinds: ["image", "file", "file_ref"],
};

/** Flushes pending microtasks, wrapped in `act` so React commits before the caller asserts -- same helper as file-reveal.test.tsx. */
async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** Mounts the editor with the read done and daemon.info reporting `limits`, leaving a clean canAdd. */
async function mount(limits: AttachmentLimits | null = LIMITS): Promise<FakeWsClient> {
  const client = new FakeWsClient();
  renderWithClientProp(<FileEditorPane client={client} task={TASK} path={PATH} />);
  await flush();
  client.nth("daemon.info", 0).resolve(limits ? { attachments: limits } : {});
  client.nth("file.read", 0).resolve({ content: CONTENT });
  await flush();
  return client;
}

function view() {
  const v = editorViewRegistry.get(screen.getByTestId("file-editor"));
  if (!v) throw new Error("no CodeMirror view mounted");
  return v;
}

/** Opens the send menu (radix triggers on pointerdown) and returns its items by testid. */
async function openMenu() {
  fireEvent.pointerDown(screen.getByTestId("editor-send-menu"), { button: 0 });
  await screen.findByTestId("send-entry-open-file");
  return {
    openFile: screen.getByTestId("send-entry-open-file"),
    selection: screen.getByTestId("send-entry-selection"),
  };
}

beforeEach(() => {
  useComposerAttachmentsStore.setState({ byKey: {}, mentions: {}, pendingInserts: {}, pendingTexts: {} });
  useFocusedChatStore.setState({ byTask: { 1: 10 }, revealRequest: null });
});
afterEach(() => {
  cleanup();
  clearToasts();
});

describe("file editor send set (DT3-C)", () => {
  it("Open file queues the {path} reference and shows the buffer's size", async () => {
    await mount();
    const items = await openMenu();
    expect(items.openFile).toHaveTextContent("14 B");
    expect(items.openFile).not.toHaveAttribute("data-disabled");

    fireEvent.click(items.openFile);
    expect(useComposerAttachmentsStore.getState().pendingInserts[KEY]).toEqual([{ path: PATH }]);
    expect(useComposerAttachmentsStore.getState().pendingTexts[KEY]).toBeUndefined();
  });

  it("a selection spanning lines 2-3 queues {path, line: 2, endLine: 3} and its range and size", async () => {
    await mount();
    const doc = view().state.doc;
    act(() => view().dispatch({ selection: { anchor: doc.line(2).from, head: doc.line(3).to } }));

    const items = await openMenu();
    expect(items.selection).toHaveTextContent("lines 2-3, 5 B");
    fireEvent.click(items.selection);

    expect(useComposerAttachmentsStore.getState().pendingInserts[KEY]).toEqual([{ path: PATH, line: 2, endLine: 3 }]);
  });

  it("a selection ending at a line's very start names the previous line", async () => {
    await mount();
    const doc = view().state.doc;
    act(() => view().dispatch({ selection: { anchor: doc.line(2).from, head: doc.line(4).from } }));

    const items = await openMenu();
    expect(items.selection).toHaveTextContent("lines 2-3, 6 B");
    fireEvent.click(items.selection);

    expect(useComposerAttachmentsStore.getState().pendingInserts[KEY]).toEqual([{ path: PATH, line: 2, endLine: 3 }]);
  });

  it("a single-line selection queues {path, line}", async () => {
    await mount();
    const doc = view().state.doc;
    act(() => view().dispatch({ selection: { anchor: doc.line(2).from, head: doc.line(2).to } }));

    const items = await openMenu();
    expect(items.selection).toHaveTextContent("line 2, 2 B");
    fireEvent.click(items.selection);

    expect(useComposerAttachmentsStore.getState().pendingInserts[KEY]).toEqual([{ path: PATH, line: 2 }]);
  });

  it("an empty selection leaves the Selection entry disabled", async () => {
    await mount();
    const items = await openMenu();
    expect(items.selection).toHaveAttribute("data-disabled");
    expect(items.selection).toHaveTextContent("nothing selected");

    fireEvent.click(items.selection);
    expect(useComposerAttachmentsStore.getState().pendingInserts[KEY]).toBeUndefined();
  });

  it("is not rendered without file_ref support or without a focused chat", async () => {
    await mount({ ...LIMITS, kinds: ["image", "file"] });
    expect(screen.queryByTestId("editor-send-menu")).not.toBeInTheDocument();
    cleanup();

    useFocusedChatStore.setState({ byTask: {}, revealRequest: null });
    await mount();
    expect(screen.queryByTestId("editor-send-menu")).not.toBeInTheDocument();
  });
});
