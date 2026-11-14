import { act } from "react";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { Composer } from "@/components/composer/composer";
import type { AttachmentLimits, AttachmentMeta, AttachmentRequest } from "@/lib/types";
import { terminalChipLabel } from "@/lib/send-set";
import { RpcError } from "@/lib/ws-client";
import { attachmentDraftKey, useComposerAttachmentsStore } from "@/stores/composer-attachments-store";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";

// DT3-D AC15-AC17: the chips never touch the draft text; the fenced blocks are built at send
// time into the outgoing prompt only, file chips leave as file_ref attachments (deduped against
// the `@` tokens), chips are cleared after a successful send and kept after a failed one.

const LIMITS: AttachmentLimits = {
  maxCount: 10,
  maxImageBytes: 5 << 20,
  maxFileBytes: 10 << 20,
  maxTotalBytes: 10 << 20,
  maxMessageBytes: 16 << 20,
  images: ["image/png", "image/jpeg", "image/gif", "image/webp"],
  kinds: ["image", "file", "file_ref"],
};

const KEY = attachmentDraftKey(1, 10);

async function flush(): Promise<void> {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

interface Sent {
  prompt: string;
  attachments?: AttachmentRequest[];
  previews?: AttachmentMeta[];
}

async function renderComposer(opts: { onSubmit?: () => Promise<never> } = {}) {
  const client = new FakeWsClient();
  const sent: Sent[] = [];
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
      onSubmit={async (_p, prompt, _perm, _t, _w, _m, attachments, previews) => {
        sent.push({ prompt, attachments, previews });
        if (opts.onSubmit) return await opts.onSubmit();
        return { kind: "started", runId: "run-x" };
      }}
      onStop={async () => {}}
    />,
  );
  client.nth("daemon.info", 0).resolve({ attachments: LIMITS });
  await flush();
  return { sent };
}

function type(text: string) {
  fireEvent.change(screen.getByLabelText("Prompt"), { target: { value: text } });
}

function addTerminalChip(body: string) {
  act(() => {
    useComposerAttachmentsStore.getState().addChip(KEY, { kind: "terminal", body, blockLabel: "last 100 lines", label: terminalChipLabel(body, "last 100 lines") });
  });
}

beforeEach(() => {
  useComposerAttachmentsStore.setState({ byKey: {}, mentions: {}, pendingInserts: {}, pendingTexts: {}, chips: {} });
  localStorage.clear();
});
afterEach(cleanup);

describe("composer send with context chips (DT3-D)", () => {
  it("a terminal chip shows as a chip and never puts its text in the textarea", async () => {
    await renderComposer();
    addTerminalChip("hello world!");
    await flush();

    const area = screen.getByLabelText("Prompt") as HTMLTextAreaElement;
    expect(area.value).toBe("");
    expect(screen.getByTestId("context-chip-tray")).toBeInTheDocument();
    expect(screen.getAllByTestId("context-chip")).toHaveLength(1);
    expect(screen.getAllByTestId("context-chip")[0]).toHaveTextContent("Terminal output · 1 line · 12 B");
    expect(area.value).not.toContain("Terminal output");
  });

  it("send passes the typed prompt plus the chips' blocks, then clears the chips", async () => {
    const { sent } = await renderComposer();
    addTerminalChip("hello world!");
    type("please look");
    fireEvent.click(screen.getByTestId("chat-send-button"));
    await waitFor(() => expect(sent).toHaveLength(1));

    expect(sent[0].prompt.startsWith("please look")).toBe(true);
    expect(sent[0].prompt).toContain("Terminal output, last 100 lines, 12 B:");
    expect(sent[0].prompt).toContain("\n```\nhello world!\n```");
    expect(sent[0].attachments).toBeUndefined();
    await waitFor(() => expect(screen.queryByTestId("context-chip-tray")).not.toBeInTheDocument());
    expect(useComposerAttachmentsStore.getState().chips[KEY]).toBeUndefined();
  });

  it("a rejected send keeps the chip and restores the typed text", async () => {
    await renderComposer({
      onSubmit: async () => {
        throw new RpcError("attachment 1 is too large", "attachment_too_large");
      },
    });
    addTerminalChip("hello world!");
    type("keep me");
    fireEvent.click(screen.getByTestId("chat-send-button"));
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(screen.getByLabelText("Prompt")).toHaveValue("keep me");
    expect(screen.getAllByTestId("context-chip")).toHaveLength(1);
  });

  it("a file_ref chip leaves as one file_ref attachment, deduped against its @ token", async () => {
    const { sent } = await renderComposer();
    act(() => {
      useComposerAttachmentsStore.getState().addChip(KEY, { kind: "file_ref", ref: { path: "a/b.go", line: 2, endLine: 3 }, label: "b.go L2–3 · 40 B" });
    });
    type("check this");
    fireEvent.click(screen.getByTestId("chat-send-button"));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0].attachments).toEqual([{ kind: "file_ref", path: "a/b.go", line: 2, endLine: 3 }]);

    // The same ref remembered as a mention, its token in the text: still exactly one.
    cleanup();
    const second = await renderComposer();
    act(() => {
      const store = useComposerAttachmentsStore.getState();
      store.addChip(KEY, { kind: "file_ref", ref: { path: "a/b.go", line: 2, endLine: 3 }, label: "b.go L2–3 · 40 B" });
      store.rememberMention(KEY, { path: "a/b.go", line: 2, endLine: 3 });
    });
    type("see @a/b.go#L2-L3");
    fireEvent.click(screen.getByTestId("chat-send-button"));
    await waitFor(() => expect(second.sent).toHaveLength(1));
    expect(second.sent[0].attachments).toEqual([{ kind: "file_ref", path: "a/b.go", line: 2, endLine: 3 }]);
  });

  it("chips alone leave Send disabled", async () => {
    await renderComposer();
    addTerminalChip("hello world!");
    await flush();
    expect(screen.getByTestId("context-chip")).toBeInTheDocument();
    expect(screen.getByTestId("chat-send-button")).toBeDisabled();
  });
});
