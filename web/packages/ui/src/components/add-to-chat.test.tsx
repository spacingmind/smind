import { act } from "react";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { Composer } from "@/components/composer/composer";
import { clearToasts, Toaster } from "@/components/ui/toast";
import type { AttachmentLimits, AttachmentRequest } from "@/lib/types";
import { attachmentDraftKey, useComposerAttachmentsStore } from "@/stores/composer-attachments-store";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";

export const LIMITS: AttachmentLimits = {
  maxCount: 10,
  maxImageBytes: 5 << 20,
  maxFileBytes: 10 << 20,
  maxTotalBytes: 10 << 20,
  maxMessageBytes: 16 << 20,
  images: ["image/png"],
};

async function flush(): Promise<void> {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

const KEY = attachmentDraftKey(1, 10);
const area = () => screen.getByLabelText("Prompt") as HTMLTextAreaElement;
const queue = (ref: { path: string; line?: number; endLine?: number }) => act(() => useComposerAttachmentsStore.getState().queueInsert(KEY, ref));

async function renderComposer() {
  const client = new FakeWsClient();
  const sent: { prompt: string; attachments?: AttachmentRequest[] }[] = [];
  render(
    <Composer
      client={client}
      taskId={1}
      chatId={10}
      isDefaultChat
      boundProvider={null}
      connected
      runningRunId={null}
      queuedItems={[]}
      onCancelQueued={async () => {}}
      onSubmit={async (_p, prompt, _perm, _t, _w, _m, attachments) => {
        sent.push({ prompt, attachments });
        return { kind: "started", runId: "run-x" };
      }}
      onStop={async () => {}}
    />,
  );
  client.nth("daemon.info", 0).resolve({ attachments: LIMITS });
  await flush();
  return { client, sent };
}

beforeEach(() => {
  useComposerAttachmentsStore.setState({ byKey: {}, mentions: {}, pendingInserts: {} });
  localStorage.clear();
});
afterEach(() => {
  cleanup();
  clearToasts();
});

describe("composer consumes queued inserts", () => {
  it("puts @path and a space into an empty draft, focuses with the caret at the end, and opens no popover", async () => {
    const { client } = await renderComposer();
    queue({ path: "src/a.go" });
    await flush();
    expect(area()).toHaveValue("@src/a.go ");
    expect(document.activeElement).toBe(area());
    expect(area().selectionStart).toBe("@src/a.go ".length);
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(client.calls.filter((c) => c.method === "task.searchIndex")).toHaveLength(0);
    expect(useComposerAttachmentsStore.getState().pendingInserts[KEY]).toBeUndefined();
  });

  it("appending to a non-empty draft keeps the text and adds one space", async () => {
    await renderComposer();
    fireEvent.change(area(), { target: { value: "look at" } });
    queue({ path: "a.go", line: 12, endLine: 20 });
    await flush();
    expect(area()).toHaveValue("look at @a.go#L12-L20 ");
    queue({ path: "b.go" });
    await flush();
    expect(area()).toHaveValue("look at @a.go#L12-L20 @b.go ");
  });

  it("two ranges of one file both go out, with their lines", async () => {
    const { sent } = await renderComposer();
    queue({ path: "a.go", line: 1, endLine: 5 });
    queue({ path: "a.go", line: 30, endLine: 40 });
    await flush();
    fireEvent.click(screen.getByTestId("chat-send-button"));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]!.attachments).toEqual([
      { kind: "file_ref", path: "a.go", line: 1, endLine: 5 },
      { kind: "file_ref", path: "a.go", line: 30, endLine: 40 },
    ]);
  });

  it("a token the user deleted is not sent", async () => {
    const { sent } = await renderComposer();
    queue({ path: "a.go", line: 3, endLine: 4 });
    await flush();
    fireEvent.change(area(), { target: { value: "never mind" } });
    fireEvent.click(screen.getByTestId("chat-send-button"));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]!.attachments).toBeUndefined();
  });
});

// --- Diff pane entry points ---------------------------------------------------------------------------------------

import { DiffViewerPane } from "@/components/diff-viewer-pane";
import { FileExplorerPane } from "@/components/file-explorer-pane";
import { useFocusedChatStore } from "@/stores/focused-chat-store";
import type { Task, TaskFilesResult } from "@/lib/types";

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

const hunkDiff = (path: string, headers: string[]) =>
  `diff --git a/${path} b/${path}\nindex a29bdeb..0226208 100644\n--- a/${path}\n+++ b/${path}\n${headers.map((h) => `${h}\n line\n+added\n`).join("")}`;

const RANGE_DIFF = hunkDiff("a.go", ["@@ -10,3 +12,9 @@"]);
const TWO_HUNKS = hunkDiff("a.go", ["@@ -1,2 +1,5 @@", "@@ -30,2 +33,8 @@"]);
const DELETION_DIFF = "diff --git a/a.go b/a.go\nindex a29bdeb..0226208 100644\n--- a/a.go\n+++ b/a.go\n@@ -5,2 +4,0 @@\n-gone\n-gone2\n";

const attachmentKindsOf = (l: AttachmentLimits) => l.kinds ?? ["image", "file", "file_ref"];

async function renderPanes(files: TaskFilesResult["files"], diff: string, limits: AttachmentLimits = LIMITS) {
  useFocusedChatStore.setState({ byTask: { 1: 10 } });
  const client = new FakeWsClient();
  const sent: { prompt: string; attachments?: AttachmentRequest[] }[] = [];
  render(
    <>
      <Toaster />
      <DiffViewerPane client={client} task={TASK} />
      <Composer
        client={client}
        taskId={1}
        chatId={10}
        isDefaultChat
        boundProvider={null}
        connected
        runningRunId={null}
        queuedItems={[]}
        onCancelQueued={async () => {}}
        onSubmit={async (_p, prompt, _perm, _t, _w, _m, attachments) => {
          sent.push({ prompt, attachments });
          return { kind: "started", runId: "run-x" };
        }}
        onStop={async () => {}}
      />
    </>,
  );
  client.nth("daemon.info", 0).resolve({ attachments: limits });
  client.nth("task.files", 0).resolve({ files });
  await flush();
  for (let i = 0; i < client.calls.filter((c) => c.method === "task.fileDiff").length; i++) {
    client.nth("task.fileDiff", i).resolve({ diff });
  }
  await flush();
  await flush();
  // The file button appears once the daemon info lands; the hunk buttons one effect later.
  if (attachmentKindsOf(limits).includes("file_ref")) await screen.findAllByRole("button", { name: /Add (hunk )?to chat/ });
  return { client, sent };
}

describe("diff pane Add to chat", () => {
  it("the file header button inserts @path and the next send carries a file_ref with no range", async () => {
    const { sent } = await renderPanes([{ path: "a.go", status: "modified", staged: false }], RANGE_DIFF);
    fireEvent.click(screen.getByRole("button", { name: "Add to chat" }));
    await flush();
    expect(area()).toHaveValue("@a.go ");
    fireEvent.click(screen.getByTestId("chat-send-button"));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]!.attachments).toEqual([{ kind: "file_ref", path: "a.go" }]);
  });

  it("adding shows a toast and never switches tabs; its Open chat action asks for the chat", async () => {
    await renderPanes([{ path: "a.go", status: "modified", staged: false }], RANGE_DIFF);
    useFocusedChatStore.setState({ revealRequest: null });
    fireEvent.click(screen.getByRole("button", { name: "Add to chat" }));
    await flush();
    expect(useFocusedChatStore.getState().revealRequest).toBeNull();
    expect(await screen.findByText("Added to chat")).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("@a.go");
    fireEvent.click(screen.getByRole("button", { name: "Open chat" }));
    expect(useFocusedChatStore.getState().revealRequest).toMatchObject({ taskId: 1, chatId: 10 });
  });

  it("a hunk header inserts @path#L12-L20 and the send carries line and endLine", async () => {
    const { sent } = await renderPanes([{ path: "a.go", status: "modified", staged: false }], RANGE_DIFF);
    fireEvent.click(screen.getByRole("button", { name: "Add hunk to chat" }));
    await flush();
    expect(area()).toHaveValue("@a.go#L12-L20 ");
    fireEvent.click(screen.getByTestId("chat-send-button"));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]!.attachments).toEqual([{ kind: "file_ref", path: "a.go", line: 12, endLine: 20 }]);
  });

  it("two hunks of one file both go out with their own ranges", async () => {
    const { sent } = await renderPanes([{ path: "a.go", status: "modified", staged: false }], TWO_HUNKS);
    for (const b of screen.getAllByRole("button", { name: "Add hunk to chat" })) fireEvent.click(b);
    await flush();
    expect(area()).toHaveValue("@a.go#L1-L5 @a.go#L33-L40 ");
    fireEvent.click(screen.getByTestId("chat-send-button"));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]!.attachments).toEqual([
      { kind: "file_ref", path: "a.go", line: 1, endLine: 5 },
      { kind: "file_ref", path: "a.go", line: 33, endLine: 40 },
    ]);
  });

  it("a pure-deletion hunk adds the file without a range", async () => {
    await renderPanes([{ path: "a.go", status: "modified", staged: false }], DELETION_DIFF);
    fireEvent.click(screen.getByRole("button", { name: "Add hunk to chat" }));
    await flush();
    expect(area()).toHaveValue("@a.go ");
  });

  it("a deleted file's button is disabled with its reason and it has no hunk buttons", async () => {
    await renderPanes([{ path: "a.go", status: "deleted", staged: false }], DELETION_DIFF);
    const button = screen.getByRole("button", { name: "Add to chat" });
    expect(button).toBeDisabled();
    expect(button).toHaveAccessibleDescription("Deleted in this task");
    expect(screen.queryByRole("button", { name: "Add hunk to chat" })).toBeNull();
  });

  it("a renamed file uses its new path", async () => {
    await renderPanes([{ path: "new/name.go", status: "R", staged: false }], RANGE_DIFF);
    fireEvent.click(screen.getByRole("button", { name: "Add to chat" }));
    await flush();
    expect(area()).toHaveValue("@new/name.go ");
  });

  it("the buttons are absent when the daemon lacks file_ref in kinds", async () => {
    await renderPanes([{ path: "a.go", status: "modified", staged: false }], RANGE_DIFF, { ...LIMITS, kinds: ["image", "file"] });
    expect(screen.queryByRole("button", { name: "Add to chat" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Add hunk to chat" })).toBeNull();
  });
});

// --- Files tab entry points ---------------------------------------------------------------------------------------

async function renderFiles(limits: AttachmentLimits = LIMITS) {
  useFocusedChatStore.setState({ byTask: { 1: 10 } });
  const client = new FakeWsClient();
  render(
    <>
      <FileExplorerPane client={client} task={TASK} />
      <Composer
        client={client}
        taskId={1}
        chatId={10}
        isDefaultChat
        boundProvider={null}
        connected
        runningRunId={null}
        queuedItems={[]}
        onCancelQueued={async () => {}}
        onSubmit={async () => ({ kind: "started", runId: "run-x" })}
        onStop={async () => {}}
      />
    </>,
  );
  client.nth("daemon.info", 0).resolve({ attachments: limits });
  client.nth("file.list", 0).resolve([
    { name: "src", isDir: true, size: 0 },
    { name: "README.md", isDir: false, size: 42 },
  ]);
  client.nth("task.files", 0).resolve({ files: [] });
  await flush();
  await flush();
}

describe("Files tab Add to chat", () => {
  it("a file row's button adds the file", async () => {
    await renderFiles();
    fireEvent.click(await screen.findByRole("button", { name: 'Add "README.md" to chat' }));
    await flush();
    expect(area()).toHaveValue("@README.md ");
  });

  it("the row menu has Add to chat for a file", async () => {
    await renderFiles();
    await screen.findByRole("button", { name: 'Add "README.md" to chat' });
    fireEvent.contextMenu(screen.getByTestId("file-row"));
    fireEvent.click(screen.getByTestId("file-menu-add-to-chat"));
    await flush();
    expect(area()).toHaveValue("@README.md ");
  });

  it("a directory row adds the directory, from its button and its menu", async () => {
    await renderFiles();
    fireEvent.click(await screen.findByRole("button", { name: 'Add "src" to chat' }));
    await flush();
    expect(area()).toHaveValue("@src ");
    fireEvent.change(area(), { target: { value: "" } });
    fireEvent.contextMenu(screen.getByTestId("dir-row"));
    fireEvent.click(screen.getByTestId("dir-menu-add-to-chat"));
    await flush();
    expect(area()).toHaveValue("@src ");
  });

  it("adding does not open or toggle the row", async () => {
    await renderFiles();
    fireEvent.click(await screen.findByRole("button", { name: 'Add "src" to chat' }));
    await flush();
    expect(screen.getByTestId("dir-row").nextElementSibling).toBeNull();
  });

  it("has no buttons or menu entries when the daemon lacks file_ref", async () => {
    await renderFiles({ ...LIMITS, kinds: ["image", "file"] });
    expect(screen.queryByRole("button", { name: /to chat/ })).toBeNull();
    fireEvent.contextMenu(screen.getByTestId("file-row"));
    expect(screen.queryByTestId("file-menu-add-to-chat")).toBeNull();
    fireEvent.contextMenu(screen.getByTestId("dir-row"));
    expect(screen.queryByTestId("dir-menu-add-to-chat")).toBeNull();
  });
});
