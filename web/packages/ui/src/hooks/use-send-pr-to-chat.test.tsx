import { act, cleanup, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { clearToasts, Toaster } from "@/components/ui/toast";
import { useSendPRToChat } from "@/hooks/use-send-pr-to-chat";
import type { AttachmentLimits } from "@/lib/types";
import { attachmentDraftKey, useComposerAttachmentsStore } from "@/stores/composer-attachments-store";
import { useFocusedChatStore } from "@/stores/focused-chat-store";
import { FakeWsClient } from "@/test/fake-ws-client";
import { check, prDetails, thread } from "@/test/pr-details-fixture";
import { renderWithClientProp } from "@/test/render-with-daemon";

const LIMITS: AttachmentLimits = {
  maxCount: 10,
  maxImageBytes: 1 << 20,
  maxFileBytes: 1 << 20,
  maxTotalBytes: 1 << 20,
  maxMessageBytes: 1 << 22,
  images: ["image/png"],
  kinds: ["image", "file", "file_ref", "pr", "issue"],
};
const KEY = attachmentDraftKey(1, 10);
const store = () => useComposerAttachmentsStore.getState();

function Probe({ onApi }: { client: FakeWsClient; onApi: (api: ReturnType<typeof useSendPRToChat>) => void }) {
  onApi(useSendPRToChat(1));
  return <Toaster />;
}

async function setup(limits: AttachmentLimits | null = LIMITS) {
  const client = new FakeWsClient();
  let api!: ReturnType<typeof useSendPRToChat>;
  renderWithClientProp(<Probe client={client} onApi={(a) => (api = a)} />);
  await waitFor(() => client.nth("daemon.info", 0));
  await act(async () => client.nth("daemon.info", 0).resolve(limits ? { attachments: limits } : {}));
  return { client, api: () => api };
}

beforeEach(() => {
  useComposerAttachmentsStore.setState({ byKey: {}, mentions: {}, pendingInserts: {}, pendingTexts: {} });
  useFocusedChatStore.setState({ byTask: { 1: 10 }, revealRequest: null });
});
afterEach(() => {
  cleanup();
  clearToasts();
});

describe("useSendPRToChat", () => {
  it("thread inserts pr chip, file_ref and quoted text, unsent", async () => {
    const { client, api } = await setup();
    act(() => api().sendThread(prDetails(), thread()));
    expect(store().byKey[KEY]).toMatchObject([{ kind: "pr", forge: "github", number: 7, repo: "o/r", title: "Add thing" }]);
    expect(store().pendingInserts[KEY]).toEqual([{ path: "src/a.go", line: 12 }]);
    expect(store().pendingTexts[KEY]).toHaveLength(1);
    expect(store().pendingTexts[KEY]![0]).toContain("alice: please rename");
    // Nothing is sent: no run.start, and the toast offers to open the chat.
    expect(client.calls.some((c) => c.method === "run.start")).toBe(false);
    expect(await screen.findByText("Added to chat")).toBeTruthy();
    act(() => screen.getByText("Open chat").click());
    expect(useFocusedChatStore.getState().revealRequest).toMatchObject({ taskId: 1, chatId: 10 });
  });

  it("skips the chip and file_ref the daemon does not accept, keeps the text", async () => {
    const { api } = await setup({ ...LIMITS, kinds: ["image", "file"] });
    act(() => api().sendThread(prDetails(), thread()));
    expect(store().byKey[KEY]).toBeUndefined();
    expect(store().pendingInserts[KEY]).toBeUndefined();
    expect(store().pendingTexts[KEY]).toHaveLength(1);
  });

  it("add all unresolved", async () => {
    const { api } = await setup();
    const details = prDetails({ threads: { truncated: false, items: [thread({ id: "1" }), thread({ id: "2", path: "b.go", line: 3 }), thread({ id: "3", isResolved: true })] } });
    act(() => api().sendAllUnresolved(details));
    expect(store().pendingInserts[KEY]).toEqual([{ path: "src/a.go", line: 12 }, { path: "b.go", line: 3 }]);
    expect(store().pendingTexts[KEY]).toHaveLength(1);
    expect(store().pendingTexts[KEY]![0]).toContain("2 unresolved review threads");
  });

  it("check log tail is fetched on click only and quoted with the check label", async () => {
    const { client, api } = await setup();
    expect(client.calls.some((c) => c.method === "task.prCheckLog")).toBe(false);
    let done!: Promise<void>;
    act(() => {
      done = api().sendCheck(prDetails(), check());
    });
    await waitFor(() => client.nth("task.prCheckLog", 0));
    expect(client.nth("task.prCheckLog", 0).params).toEqual({ taskId: 1, checkId: "run:1:job:2" });
    expect(store().pendingTexts[KEY]).toBeUndefined();
    await act(async () => {
      client.nth("task.prCheckLog", 0).resolve({ checkId: "run:1:job:2", text: "boom", lines: 1, truncated: false, fetchedAt: "" });
      await done;
    });
    expect(store().pendingTexts[KEY]![0]).toContain("Untrusted text from a GitHub check log");
    expect(store().pendingTexts[KEY]![0]).toContain("boom");
  });

  it("a failed log read sends nothing", async () => {
    const { client, api } = await setup();
    let done!: Promise<void>;
    act(() => {
      done = api().sendCheck(prDetails(), check());
    });
    await waitFor(() => client.nth("task.prCheckLog", 0));
    await act(async () => {
      client.nth("task.prCheckLog", 0).reject(Object.assign(new Error(""), { code: "check_unknown" }));
      await done;
    });
    expect(store().pendingTexts[KEY]).toBeUndefined();
    expect(await screen.findByText(/no longer on the pull request/)).toBeTruthy();
  });

  it("no chat means no-op", async () => {
    useFocusedChatStore.setState({ byTask: {} });
    const { client, api } = await setup();
    expect(api().canSend).toBe(false);
    act(() => api().sendThread(prDetails(), thread()));
    act(() => api().sendAllUnresolved(prDetails()));
    await act(async () => api().sendCheck(prDetails(), check()));
    expect(store().pendingTexts).toEqual({});
    expect(store().byKey).toEqual({});
    expect(client.calls.some((c) => c.method === "task.prCheckLog")).toBe(false);
  });
});
