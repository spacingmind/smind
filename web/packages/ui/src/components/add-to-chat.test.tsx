import { act } from "react";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { Composer } from "@/components/composer/composer";
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
afterEach(cleanup);

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
