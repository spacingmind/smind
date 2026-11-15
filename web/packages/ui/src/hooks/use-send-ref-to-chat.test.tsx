import { act, cleanup, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { clearToasts, Toaster } from "@/components/ui/toast";
import { useSendRefToChat } from "@/hooks/use-send-ref-to-chat";
import type { AttachmentLimits } from "@/lib/types";
import { attachmentDraftKey, useComposerAttachmentsStore } from "@/stores/composer-attachments-store";
import { useFocusedChatStore } from "@/stores/focused-chat-store";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp } from "@/test/render-with-daemon";

const LIMITS: AttachmentLimits = {
  maxCount: 10,
  maxImageBytes: 1 << 20,
  maxFileBytes: 1 << 20,
  maxTotalBytes: 1 << 20,
  maxMessageBytes: 1 << 22,
  images: ["image/png"],
  kinds: ["image", "file", "file_ref"],
};
const KEY = attachmentDraftKey(1, 10);
const store = () => useComposerAttachmentsStore.getState();

function Probe({ onApi }: { client: FakeWsClient; onApi: (api: ReturnType<typeof useSendRefToChat>) => void }) {
  onApi(useSendRefToChat(1));
  return <Toaster />;
}

async function setup(limits: AttachmentLimits | null = LIMITS) {
  const client = new FakeWsClient();
  let api!: ReturnType<typeof useSendRefToChat>;
  renderWithClientProp(<Probe client={client} onApi={(a) => (api = a)} />);
  await waitFor(() => client.nth("daemon.info", 0));
  await act(async () => client.nth("daemon.info", 0).resolve(limits ? { attachments: limits } : {}));
  return { api: () => api };
}

beforeEach(() => {
  useComposerAttachmentsStore.setState({ byKey: {}, mentions: {}, pendingInserts: {}, pendingTexts: {}, chips: {} });
  useFocusedChatStore.setState({ byTask: { 1: 10 }, revealRequest: null });
});
afterEach(() => {
  cleanup();
  clearToasts();
});

describe("useSendRefToChat", () => {
  it("adds one file_ref chip with its label and no insert or text", async () => {
    const { api } = await setup();
    expect(api().canSend).toBe(true);

    act(() => api().sendRef({ path: "src/handler.go", line: 2, endLine: 3 }, 40));
    expect(store().chips[KEY]).toHaveLength(1);
    expect(store().chips[KEY]![0]).toMatchObject({
      kind: "file_ref",
      ref: { path: "src/handler.go", line: 2, endLine: 3 },
      label: "handler.go L2–3 · 40 B",
    });
    expect(store().pendingInserts).toEqual({});
    expect(store().pendingTexts).toEqual({});
    expect(await screen.findByText("Added to chat")).toBeTruthy();
  });

  it("without file_ref support canSend is false (the menu is not rendered)", async () => {
    const { api } = await setup({ ...LIMITS, kinds: ["image", "file"] });
    expect(api().canSend).toBe(false);
  });

  it("without a focused chat canSend is false and sendRef is a no-op", async () => {
    useFocusedChatStore.setState({ byTask: {}, revealRequest: null });
    const { api } = await setup();
    expect(api().canSend).toBe(false);
    act(() => api().sendRef({ path: "a.go" }, 10));
    expect(store().chips).toEqual({});
  });
});
