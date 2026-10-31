import { act } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { TAB_KINDS, fileTabKey } from "@/components/tab-registry";
import { FileEditorPane } from "@/components/file-editor-pane";
import { FakeWsClient } from "@/test/fake-ws-client";
import { RpcError } from "@/lib/ws-client";
import type { FileStatResult, Task } from "@/lib/types";

const TASK: Task = {
  ID: 7,
  WorkspaceID: 1,
  SpaceID: null,
  Title: "Task",
  Status: "active",
  WorktreePath: "/tmp/a",
  Branch: "task-a",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

const MTIME = "2026-10-31T10:00:00Z";
const GOOD = new TextEncoder().encode("%PDF-1.7\nbody\n%%EOF\n");
const CUT = new TextEncoder().encode("%PDF-1.7\nbody that stops mid str");

function stat(over: Partial<FileStatResult> = {}): FileStatResult {
  return { size: GOOD.length, mtime: MTIME, kind: "pdf", mime: "application/pdf", ext: ".pdf", mismatch: false, previewable: true, ...over };
}

function b64(bytes: Uint8Array): string {
  let s = "";
  for (const b of bytes) s += String.fromCharCode(b);
  return btoa(s);
}

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 6; i++) await Promise.resolve();
  });
}

/** Opens `path` and answers file.stat; resolves with the client. */
async function openWithStat(path: string, st: FileStatResult): Promise<FakeWsClient> {
  const client = new FakeWsClient();
  render(<FileEditorPane client={client} task={TASK} path={path} />);
  await waitFor(() => client.nth("file.stat", 0));
  await act(async () => client.nth("file.stat", 0).resolve(st));
  return client;
}

/** Answers daemon.info and one readRange with `bytes`; resolves once the viewer iframe is mounted. */
async function serveBytes(client: FakeWsClient, bytes: Uint8Array, files = { maxReadBytes: 50 * 1024 * 1024, maxChunkBytes: 512 * 1024, previewKinds: ["pdf"] }): Promise<HTMLIFrameElement> {
  await waitFor(() => client.nth("daemon.info", 0));
  await act(async () => client.nth("daemon.info", 0).resolve({ files }));
  await waitFor(() => client.nth("file.readRange", 0));
  await act(async () =>
    client.nth("file.readRange", 0).resolve({ offset: 0, length: bytes.length, eof: true, data: b64(bytes), mtime: MTIME }),
  );
  return (await screen.findByTestId("pdf-viewer-frame")) as HTMLIFrameElement;
}

/** Delivers a message from `frame` (the sender the host listens to) to the app window. */
function fromViewer(frame: HTMLIFrameElement | null, data: unknown, source: Window | null = frame?.contentWindow ?? null): void {
  act(() => {
    window.dispatchEvent(new MessageEvent("message", { data, source }));
  });
}

afterEach(() => vi.useRealTimers());

describe("PDF preview in the file tab", () => {
  it("pdf_opens_in_file_tab_no_new_tabkind", async () => {
    // the tab is the existing file tab, keyed ${taskId}:file:${path}; the kind list is unchanged
    expect(fileTabKey(TASK.ID, "reports/q3.pdf")).toBe("7:file:reports/q3.pdf");
    expect(Object.keys(TAB_KINDS).sort()).toEqual(["chat", "diff", "file", "files", "plugin", "pr", "terminal"]);

    const client = await openWithStat("reports/q3.pdf", stat());
    expect(client.nth("file.stat", 0).params).toEqual({ taskId: 7, path: "reports/q3.pdf" });
    const frame = await serveBytes(client, GOOD);
    expect(screen.getByTestId("pdf-preview-pane")).toBeInTheDocument();
    expect(client.calls.some((c) => c.method === "file.read")).toBe(false); // the editor never touched it

    // the chunk request is pinned to the stat'd version and the daemon's chunk ceiling
    expect(client.nth("file.readRange", 0).params).toEqual({
      taskId: 7, path: "reports/q3.pdf", offset: 0, length: 512 * 1024, ifMtime: MTIME,
    });

    // viewer says ready -> the host posts the bytes, and only bytes (no path, task id or token)
    const post = vi.spyOn(frame.contentWindow as Window, "postMessage");
    fromViewer(frame, { type: "ready" });
    expect(post).toHaveBeenCalledTimes(1);
    const [msg, target, transfer] = post.mock.calls[0] as unknown as [Record<string, unknown>, string, Transferable[]];
    expect(Object.keys(msg).sort()).toEqual(["data", "type"]);
    expect(msg.type).toBe("load");
    expect(Array.from(new Uint8Array(msg.data as ArrayBuffer))).toEqual(Array.from(GOOD));
    expect(transfer).toHaveLength(1);
    expect(target).toBe("*");
    expect(JSON.stringify(Array.from(new Uint8Array(msg.data as ArrayBuffer)))).not.toContain("q3.pdf");

    fromViewer(frame, { type: "pages", count: 3, total: 3 });
    await flush();
    expect(screen.getByTestId("pdf-page-indicator")).toHaveTextContent("1 / 3");
    fromViewer(frame, { type: "page", current: 2, count: 3, zoom: 1.25 });
    expect(screen.getByTestId("pdf-page-indicator")).toHaveTextContent("2 / 3");
    expect(screen.getByTestId("pdf-zoom")).toHaveTextContent("125%");
    // zoom and fit are commands to the viewer
    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    fireEvent.click(screen.getByRole("button", { name: "Fit" }));
    expect(post.mock.calls.slice(1).map((c) => c[0])).toEqual([
      { type: "zoom", mode: "step", value: 1.25 },
      { type: "zoom", mode: "fit" },
    ]);
    // Mod+= / Mod+- / Mod+0 and PageDown while the pane has focus
    const pane = screen.getByTestId("pdf-preview-pane");
    fireEvent.keyDown(pane, { key: "=", ctrlKey: true });
    fireEvent.keyDown(pane, { key: "0", metaKey: true });
    fireEvent.keyDown(pane, { key: "PageDown" });
    expect(post.mock.calls.slice(3).map((c) => c[0])).toEqual([
      { type: "zoom", mode: "step", value: 1.25 },
      { type: "zoom", mode: "fit" },
      { type: "goto", page: "next" },
    ]);
  });

  it("text_files_still_open_in_the_editor_without_a_stat", async () => {
    const client = new FakeWsClient();
    render(<FileEditorPane client={client} task={TASK} path="notes.md" />);
    expect(client.calls.map((c) => c.method)).toEqual(["file.read"]);
  });

  it("mismatch_never_reaches_viewer", async () => {
    // a .pdf that is really text
    const client = await openWithStat("fake.pdf", stat({ kind: "text", mismatch: true, previewable: false }));
    expect(await screen.findByTestId("file-mismatch-panel")).toHaveTextContent("looks like a text file");
    expect(screen.queryByTestId("pdf-viewer-frame")).toBeNull();
    expect(client.calls.some((c) => c.method === "file.readRange" || c.method === "daemon.info")).toBe(false);

    // a PDF that is named something else is not previewed either
    const other = await openWithStat("really-a-pdf.pdf", stat({ mismatch: true, previewable: false, ext: ".txt" }));
    expect(await screen.findAllByTestId("file-mismatch-panel")).toHaveLength(2);
    expect(other.calls.some((c) => c.method === "file.readRange")).toBe(false);
    expect(screen.queryByTestId("pdf-viewer-frame")).toBeNull();

    // the text one can be opened as text on purpose
    fireEvent.click(screen.getAllByRole("button", { name: "Open as text" })[0]);
    await waitFor(() => client.nth("file.read", 0));
    expect(client.nth("file.read", 0).params).toEqual({ taskId: 7, path: "fake.pdf" });
  });

  it("binary_shows_no_preview_panel", async () => {
    const client = await openWithStat("memo.docx", stat({ kind: "office", mime: "application/zip", ext: ".docx", previewable: false }));
    const panel = await screen.findByTestId("no-preview-panel");
    expect(panel).toHaveTextContent("No preview for this file type");
    expect(client.calls.some((c) => c.method === "file.read")).toBe(false); // never an editor error for a binary

    // an unknown extension that turns out not to be UTF-8: the read is refused, stat says what it is
    const c2 = new FakeWsClient();
    render(<FileEditorPane client={c2} task={TASK} path="data.dat" />);
    await act(async () => c2.nth("file.read", 0).reject(new Error('file.read: read "data.dat": not valid UTF-8 (binary files are not supported)')));
    await waitFor(() => c2.nth("file.stat", 0));
    await act(async () => c2.nth("file.stat", 0).resolve(stat({ kind: "other", mime: "application/octet-stream", ext: ".dat", previewable: false })));
    expect(await screen.findAllByTestId("no-preview-panel")).toHaveLength(2);

    // an older daemon has no file.stat: the editor shows its own error as before
    const c3 = new FakeWsClient();
    render(<FileEditorPane client={c3} task={TASK} path="old.pdf" />);
    await act(async () => c3.nth("file.stat", 0).reject(new RpcError("method not found", "method_not_found")));
    await waitFor(() => c3.nth("file.read", 0));
  });

  it("states_oversize_password_corrupt_conflict", async () => {
    // oversize: refused before a single byte is requested, and no viewer is created
    let client = await openWithStat("huge.pdf", stat({ size: 60 * 1024 * 1024 }));
    await waitFor(() => client.nth("daemon.info", 0));
    await act(async () => client.nth("daemon.info", 0).resolve({ files: { maxReadBytes: 50 * 1024 * 1024, maxChunkBytes: 4 * 1024 * 1024, previewKinds: ["pdf"] } }));
    expect(await screen.findByTestId("pdf-state-oversize")).toHaveTextContent("60 MiB");
    expect(client.calls.some((c) => c.method === "file.readRange")).toBe(false);
    expect(screen.queryByTestId("pdf-viewer-frame")).toBeNull();
    document.body.innerHTML = "";

    // password protected: the viewer says so; the iframe is torn down
    client = await openWithStat("locked.pdf", stat());
    let frame = await serveBytes(client, GOOD);
    fromViewer(frame, { type: "ready" });
    fromViewer(frame, { type: "error", reason: "password" });
    expect(await screen.findByTestId("pdf-state-password")).toHaveTextContent("password protected");
    expect(screen.queryByTestId("pdf-viewer-frame")).toBeNull();
    document.body.innerHTML = "";

    // corrupt: the file ends properly but cannot be opened
    client = await openWithStat("bad.pdf", stat());
    frame = await serveBytes(client, GOOD);
    fromViewer(frame, { type: "ready" });
    fromViewer(frame, { type: "error", reason: "corrupt" });
    expect(await screen.findByTestId("pdf-state-corrupt")).toHaveTextContent("could not be opened");
    expect(screen.queryByTestId("pdf-viewer-frame")).toBeNull();
    document.body.innerHTML = "";

    // truncated: no %%EOF at the end, a different panel
    client = await openWithStat("cut.pdf", stat({ size: CUT.length }));
    frame = await serveBytes(client, CUT);
    fromViewer(frame, { type: "ready" });
    fromViewer(frame, { type: "error", reason: "corrupt" });
    expect(await screen.findByTestId("pdf-state-truncated")).toHaveTextContent("looks incomplete");
    expect(screen.queryByTestId("pdf-state-corrupt")).toBeNull();
    document.body.innerHTML = "";

    // conflict: the file changed between chunks; Reload offers a fresh stat
    client = await openWithStat("moving.pdf", stat());
    await waitFor(() => client.nth("daemon.info", 0));
    await act(async () => client.nth("daemon.info", 0).resolve({}));
    await waitFor(() => client.nth("file.readRange", 0));
    await act(async () => client.nth("file.readRange", 0).reject(new RpcError("changed", "conflict")));
    expect(await screen.findByTestId("pdf-state-conflict")).toHaveTextContent("changed while it was being read");
    // no daemon.info files: the smallest transport's chunk size was used
    expect((client.nth("file.readRange", 0).params as { length: number }).length).toBe(512 * 1024);
    fireEvent.click(screen.getAllByRole("button", { name: "Reload" })[1]);
    await waitFor(() => expect(client.calls.filter((c) => c.method === "file.stat")).toHaveLength(2));
  });

  it("a_parse_that_never_finishes_times_out_and_tears_the_viewer_down", async () => {
    const client = await openWithStat("slow.pdf", stat());
    const frame = await serveBytes(client, GOOD);
    vi.useFakeTimers();
    fromViewer(frame, { type: "ready" });
    await act(async () => {
      vi.advanceTimersByTime(29_999);
    });
    expect(screen.getByTestId("pdf-viewer-frame")).toBeInTheDocument();
    await act(async () => {
      vi.advanceTimersByTime(2);
    });
    expect(screen.getByTestId("pdf-state-timeout")).toHaveTextContent("could not be opened");
    expect(screen.queryByTestId("pdf-viewer-frame")).toBeNull();
  });

  it("messages_from_other_windows_or_with_bad_shapes_change_nothing", async () => {
    const client = await openWithStat("a.pdf", stat());
    const frame = await serveBytes(client, GOOD);
    const post = vi.spyOn(frame.contentWindow as Window, "postMessage");
    // a message from the app window itself, and from nowhere, with a valid shape
    fromViewer(frame, { type: "ready" }, window);
    fromViewer(frame, { type: "pages", count: 3, total: 3 }, null);
    expect(post).not.toHaveBeenCalled();
    expect(screen.getByTestId("pdf-preview-pane")).toHaveAttribute("data-phase", "loading");
    // the right window with junk
    fromViewer(frame, { type: "pages", count: "many", total: 1 });
    fromViewer(frame, { type: "error", reason: "<script>" });
    fromViewer(frame, "ready");
    expect(screen.getByTestId("pdf-preview-pane")).toHaveAttribute("data-phase", "loading");
    expect(screen.queryByTestId("pdf-state-corrupt")).toBeNull();
  });
});
