import { act } from "react";
import { cleanup, renderHook, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { clearToasts, Toaster } from "@/components/ui/toast";
import { useSendTextToChat } from "@/hooks/use-send-text-to-chat";
import { attachmentDraftKey, useComposerAttachmentsStore } from "@/stores/composer-attachments-store";
import { useFocusedChatStore } from "@/stores/focused-chat-store";

const store = () => useComposerAttachmentsStore.getState();

beforeEach(() => {
  useComposerAttachmentsStore.setState({ byKey: {}, mentions: {}, pendingInserts: {}, pendingTexts: {}, chips: {} });
  useFocusedChatStore.setState({ byTask: { 1: 10 }, revealRequest: null });
});
afterEach(() => {
  cleanup();
  clearToasts();
});

describe("useSendTextToChat", () => {
  it("adds exactly one terminal chip under the focused chat's draft key, queues no text, and offers Open chat", async () => {
    const { result } = renderHook(() => useSendTextToChat(1), {
      wrapper: ({ children }) => (
        <>
          <Toaster />
          {children}
        </>
      ),
    });
    expect(result.current.canSend).toBe(true);

    act(() => result.current.sendText("hello world!", "selection", "Terminal selection"));
    const key = attachmentDraftKey(1, 10);
    const chips = store().chips[key] ?? [];
    expect(chips).toHaveLength(1);
    expect(chips[0]).toMatchObject({ kind: "terminal", body: "hello world!", blockLabel: "selection", label: "Terminal selection · 1 line · 12 B" });
    // The raw text never goes near the draft's pending queue.
    expect(store().pendingTexts).toEqual({});
    // Nothing is sent, and the toast offers the jump to the chat.
    expect(await screen.findByText("Added to chat")).toBeTruthy();
    act(() => screen.getByText("Open chat").click());
    expect(useFocusedChatStore.getState().revealRequest).toMatchObject({ taskId: 1, chatId: 10 });
  });

  it("without a focused chat: canSend false, sendText a no-op", () => {
    useFocusedChatStore.setState({ byTask: {}, revealRequest: null });
    const { result } = renderHook(() => useSendTextToChat(1));
    expect(result.current.canSend).toBe(false);

    act(() => result.current.sendText("anything", "selection", "Terminal selection"));
    expect(store().chips).toEqual({});
    expect(store().pendingTexts).toEqual({});
  });
});
