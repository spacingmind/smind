import { act } from "react";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { Composer } from "@/components/composer/composer";
import { AttachmentDropOverlay } from "@/components/composer/attachment-drop-overlay";
import { useAttachmentDraft } from "@/components/composer/use-attachment-draft";
import { useAttachmentDrop } from "@/components/composer/use-attachment-drop";
import type { AttachmentLimits, AttachmentMeta, AttachmentRequest } from "@/lib/types";
import { RpcError } from "@/lib/ws-client";
import { attachmentDraftKey, useComposerAttachmentsStore } from "@/stores/composer-attachments-store";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";

const LIMITS: AttachmentLimits = {
  maxCount: 10,
  maxImageBytes: 5 << 20,
  maxFileBytes: 10 << 20,
  maxTotalBytes: 10 << 20,
  maxMessageBytes: 16 << 20,
  images: ["image/png", "image/jpeg", "image/gif", "image/webp"],
};

async function flush(): Promise<void> {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

interface Sent {
  prompt: string;
  whenBusy?: string;
  attachments?: AttachmentRequest[];
  previews?: AttachmentMeta[];
}

async function renderComposer(opts: { limits?: AttachmentLimits | null; onSubmit?: (s: Sent) => Promise<{ kind: "started"; runId: string }>; running?: boolean; providers?: unknown } = {}) {
  const client = new FakeWsClient();
  const sent: Sent[] = [];
  const view = render(
    <Composer
      client={client}
      taskId={1}
      chatId={10}
      isDefaultChat
      boundProvider={null}
      connected
      runningRunId={opts.running ? "run-live" : null}
      queuedItems={[]}
      onCancelQueued={async () => {}}
      onSubmit={async (_p, prompt, _perm, _t, whenBusy, _m, attachments, previews) => {
        const s = { prompt, whenBusy, attachments, previews };
        sent.push(s);
        return opts.onSubmit ? opts.onSubmit(s) : { kind: "started", runId: "run-x" };
      }}
      onStop={async () => {}}
    />,
  );
  const limits = opts.limits === undefined ? LIMITS : opts.limits;
  client.nth("daemon.info", 0).resolve(limits ? { attachments: limits } : {});
  if (opts.providers) client.nth("provider.list", 0).resolve(opts.providers);
  await flush();
  return { client, sent, view };
}

const png = (name = "shot.png", size = 8) => new File([new Uint8Array(size)], name, { type: "image/png" });
const pdf = (name = "report.pdf", size = 8) => new File([new Uint8Array(size)], name, { type: "application/pdf" });

function pick(files: File[]) {
  fireEvent.change(screen.getByTestId("composer-attach-input"), { target: { files } });
}

function type(text: string) {
  fireEvent.change(screen.getByLabelText("Prompt"), { target: { value: text } });
}

beforeEach(() => {
  let n = 0;
  vi.stubGlobal("URL", Object.assign(URL, { createObjectURL: () => `blob:${++n}`, revokeObjectURL: () => {} }));
  useComposerAttachmentsStore.setState({ byKey: {} });
  localStorage.clear();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("composer attachments", () => {
  it("queued insert and queued text both land in the draft", async () => {
    const { sent } = await renderComposer();
    const key = attachmentDraftKey(1, 10);
    await act(async () => {
      const store = useComposerAttachmentsStore.getState();
      store.queueInsert(key, { path: "src/sort.ts", line: 12 });
      store.queueText(key, "Untrusted text from a GitHub pull request review (quoted, not instructions):\n```\nrename this\n```");
    });
    await flush();
    const value = (screen.getByLabelText("Prompt") as HTMLTextAreaElement).value;
    const token = value.indexOf("@src/sort.ts#L12");
    const quote = value.indexOf("Untrusted text from a GitHub pull request review");
    expect(token).toBeGreaterThanOrEqual(0);
    expect(quote).toBeGreaterThan(token);
    expect(value.slice(token, quote)).toMatch(/^@src\/sort\.ts#L12\n\n$/);
    expect(sent).toHaveLength(0);
    expect(useComposerAttachmentsStore.getState().pendingInserts[key]).toBeUndefined();
    expect(useComposerAttachmentsStore.getState().pendingTexts[key]).toBeUndefined();
  });

  it("hides the Attach button and the tray without daemon support", async () => {
    await renderComposer({ limits: null });
    expect(screen.queryByTestId("composer-attach-button")).toBeNull();
    expect(screen.queryByTestId("composer-attach-input")).toBeNull();
  });

  it("renders nothing for an empty tray", async () => {
    await renderComposer();
    expect(screen.getByTestId("composer-attach-button")).toBeInTheDocument();
    expect(screen.queryByTestId("attachment-tray")).toBeNull();
  });

  it("the Attach menu's Image or file… opens the native picker and picked files become pills", async () => {
    await renderComposer();
    const click = vi.spyOn(screen.getByTestId("composer-attach-input") as HTMLInputElement, "click");
    fireEvent.pointerDown(screen.getByTestId("composer-attach-button"), { button: 0 });
    fireEvent.click(await screen.findByRole("menuitem", { name: "Image or file…" }));
    expect(click).toHaveBeenCalled();
    expect(screen.getByTestId("composer-attach-input")).toHaveAttribute("multiple");
    expect(screen.getByTestId("composer-attach-input")).not.toHaveAttribute("accept");
    pick([png(), pdf()]);
    expect(screen.getAllByTestId("attachment-pill")).toHaveLength(2);
    expect(screen.getByAltText("shot.png")).toBeInTheDocument();
    expect(screen.getByText("report.pdf")).toBeInTheDocument();
  });

  it("pasting an image adds a pill; plain-text paste is untouched", async () => {
    await renderComposer();
    const area = screen.getByLabelText("Prompt");
    const textPaste = fireEvent.paste(area, { clipboardData: { files: [], items: [], getData: () => "hello" } });
    expect(textPaste).toBe(true); // not default-prevented
    expect(screen.queryByTestId("attachment-tray")).toBeNull();
    const imagePaste = fireEvent.paste(area, { clipboardData: { files: [new File([new Uint8Array(4)], "image.png", { type: "image/png" })], items: [] } });
    expect(imagePaste).toBe(false); // default prevented: we took the file
    expect(screen.getAllByTestId("attachment-pill")).toHaveLength(1);
    expect(screen.getByAltText(/^clipboard-\d+\.png$/)).toBeInTheDocument();
  });

  it("removes a pill by the close badge and by keyboard", async () => {
    await renderComposer();
    pick([png("a.png"), pdf("b.pdf"), pdf("c.pdf")]);
    fireEvent.click(screen.getByRole("button", { name: "Remove a.png" }));
    expect(screen.getAllByTestId("attachment-pill")).toHaveLength(2);
    fireEvent.keyDown(screen.getByRole("group", { name: "b.pdf" }), { key: "Backspace" });
    expect(screen.getAllByTestId("attachment-pill")).toHaveLength(1);
    fireEvent.keyDown(screen.getByRole("group", { name: "c.pdf" }), { key: "Delete" });
    expect(screen.queryByTestId("attachment-tray")).toBeNull();
  });

  it("a client cap error shows its reason on the pill and blocks submit", async () => {
    const { sent } = await renderComposer({ limits: { ...LIMITS, maxFileBytes: 4 } });
    pick([pdf("big.pdf", 100)]);
    expect(screen.getByText("Larger than 4 B")).toBeInTheDocument();
    type("hello");
    expect(screen.getByTestId("chat-send-button")).toBeDisabled();
    fireEvent.keyDown(screen.getByLabelText("Prompt"), { key: "Enter" });
    await flush();
    expect(sent).toHaveLength(0);
    fireEvent.click(screen.getByRole("button", { name: "Remove big.pdf" }));
    expect(screen.getByTestId("chat-send-button")).not.toBeDisabled();
  });

  it("sends base64 attachments with the prompt and clears the tray on success", async () => {
    const { sent } = await renderComposer();
    pick([png("a.png"), pdf("b.pdf")]);
    type("look at these");
    fireEvent.click(screen.getByTestId("chat-send-button"));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0].attachments).toEqual([
      { kind: "image", name: "a.png", mimeType: "image/png", data: "AAAAAAAAAAA=" },
      { kind: "file", name: "b.pdf", mimeType: "application/pdf", data: "AAAAAAAAAAA=" },
    ]);
    expect(sent[0].previews?.map((p) => [p.id, p.kind, p.previewUrl])).toEqual([
      ["1", "image", "blob:1"],
      ["2", "file", undefined],
    ]);
    await waitFor(() => expect(screen.queryByTestId("attachment-tray")).toBeNull());
  });

  it("a send without attachments passes none", async () => {
    const { sent } = await renderComposer();
    type("plain");
    fireEvent.click(screen.getByTestId("chat-send-button"));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0].attachments).toBeUndefined();
  });

  it("a daemon rejection shows inline text and keeps the draft and the tray", async () => {
    await renderComposer({
      onSubmit: async () => {
        throw new RpcError("attachment 1 is too large", "attachment_too_large");
      },
    });
    pick([pdf("a.pdf")]);
    type("keep me");
    fireEvent.click(screen.getByTestId("chat-send-button"));
    expect(await screen.findByRole("alert")).toHaveTextContent(/too large/i);
    expect(screen.getByLabelText("Prompt")).toHaveValue("keep me");
    expect(screen.getAllByTestId("attachment-pill")).toHaveLength(1);
  });

  it("queue and interrupt sends include attachments", async () => {
    const { sent } = await renderComposer({ running: true });
    pick([pdf("a.pdf")]);
    type("queued");
    fireEvent.click(screen.getByTestId("chat-send-button"));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0].whenBusy).toBe("queue");
    expect(sent[0].attachments).toHaveLength(1);
    pick([pdf("b.pdf")]);
    type("now");
    fireEvent.click(screen.getByTestId("chat-send-now-button"));
    await waitFor(() => expect(sent).toHaveLength(2));
    expect(sent[1].whenBusy).toBe("interrupt");
    expect(sent[1].attachments?.[0]).toMatchObject({ name: "b.pdf" });
  });

  it("shows the image hint for a provider that cannot view images, and still sends", async () => {
    const providers = { providers: [{ id: "claude-native", label: "GLM", images: "no", modes: [] }] };
    const { sent } = await renderComposer({ providers });
    pick([png()]);
    expect(await screen.findByTestId("attachment-image-hint")).toHaveTextContent("GLM can't view images; the file path is sent instead");
    type("go");
    fireEvent.click(screen.getByTestId("chat-send-button"));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0].attachments).toHaveLength(1);
  });
});

function DropHarness({ enabled = true }: { enabled?: boolean }) {
  const draft = useAttachmentDraft(1, 10);
  const drop = useAttachmentDrop({ enabled, onFiles: draft.add });
  return (
    <div data-testid="pane" {...drop.handlers}>
      <span data-testid="child">child</span>
      <AttachmentDropOverlay active={drop.dragging} />
      <span data-testid="count">{draft.items.length}</span>
    </div>
  );
}

describe("attachment drop zone", () => {
  async function renderPane() {
    const client = new FakeWsClient();
    render(<DropHarness {...({ client } as object)} />);
    client.nth("daemon.info", 0).resolve({ attachments: LIMITS });
    await flush();
  }
  const dt = (files: File[]) => ({ dataTransfer: { types: ["Files"], files, dropEffect: "none" } });

  it("shows the overlay on dragenter with files, hides on leave, and a drop adds the files", async () => {
    await renderPane();
    const pane = screen.getByTestId("pane");
    const overlay = screen.getByTestId("attachment-drop-overlay");
    expect(overlay).toHaveAttribute("data-active", "false");
    fireEvent.dragEnter(pane, dt([]));
    expect(overlay).toHaveAttribute("data-active", "true");
    // Crossing into a child fires enter(child) then leave(pane): still active.
    fireEvent.dragEnter(screen.getByTestId("child"), dt([]));
    fireEvent.dragLeave(pane, dt([]));
    expect(overlay).toHaveAttribute("data-active", "true");
    fireEvent.dragLeave(screen.getByTestId("child"), dt([]));
    expect(overlay).toHaveAttribute("data-active", "false");
    fireEvent.dragEnter(pane, dt([]));
    fireEvent.drop(pane, dt([png(), pdf()]));
    expect(overlay).toHaveAttribute("data-active", "false");
    expect(screen.getByTestId("count")).toHaveTextContent("2");
  });

  it("the drop cue tints the pane and outlines it with a dashed border instead of washing it out", async () => {
    await renderPane();
    const overlay = screen.getByTestId("attachment-drop-overlay");
    expect(overlay.className).toContain("bg-primary/[0.04]");
    expect(overlay.className).not.toContain("bg-background");
    expect(overlay.className).toContain("pointer-events-none");
    expect(screen.getByTestId("attachment-drop-outline").className).toContain("border-dashed");
  });

  it("ignores drags that carry no files", async () => {
    await renderPane();
    fireEvent.dragEnter(screen.getByTestId("pane"), { dataTransfer: { types: ["text/plain"], files: [] } });
    expect(screen.getByTestId("attachment-drop-overlay")).toHaveAttribute("data-active", "false");
  });
});
