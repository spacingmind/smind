import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ForkChatDialog, forkReasonLabel } from "@/components/fork-chat-dialog";
import type { Chat } from "@/lib/types";
import { RpcError } from "@/lib/ws-client";
import { FakeWsClient } from "@/test/fake-ws-client";

const CHAT: Chat = { ID: 10, TaskID: 1, Title: "Refactor the parser", Provider: null, AgentSession: null, CreatedAt: "2024-01-01T00:00:00Z", ArchivedAt: null };
const FORKED: Chat = {
  ...CHAT,
  ID: 11,
  Title: "Fork of Refactor the parser",
  fork: { parentChatId: 10, forkedAtRunId: "run-1", kind: "fork", mode: "native", lossy: false },
};

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

function renderDialog() {
  const client = new FakeWsClient();
  const onForked = vi.fn();
  const onOpenChange = vi.fn();
  render(
    <ForkChatDialog
      open
      onOpenChange={onOpenChange}
      chatTitle={CHAT.Title}
      runId="run-1"
      onFork={(mode) => client.call<Chat>("chat.fork", { chatId: CHAT.ID, atRunId: "run-1", mode })}
      onForked={onForked}
    />,
  );
  return { client, onForked, onOpenChange };
}

function forkCalls(client: FakeWsClient) {
  return client.calls.filter((c) => c.method === "chat.fork");
}

describe("ForkChatDialog", () => {
  it("forks natively, hands the new chat back and closes", async () => {
    const { client, onForked, onOpenChange } = renderDialog();
    fireEvent.click(screen.getByTestId("dialog-fork-chat-confirm"));
    await flush();
    expect(client.nth("chat.fork", 0).params).toEqual({ chatId: CHAT.ID, atRunId: "run-1", mode: "native" });
    client.nth("chat.fork", 0).resolve(FORKED);
    await flush();
    expect(onForked).toHaveBeenCalledWith(FORKED);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("a fork_unavailable answer offers the seeded fork and runs it on confirm", async () => {
    const { client, onForked, onOpenChange } = renderDialog();
    fireEvent.click(screen.getByTestId("dialog-fork-chat-confirm"));
    client.nth("chat.fork", 0).reject(
      new RpcError("cannot fork here", "fork_unavailable", { reason: "fork_unsupported", seededAvailable: true }),
    );
    await flush();
    // The dialog stays open, now describing the seeded alternative instead of an error.
    expect(onOpenChange).not.toHaveBeenCalled();
    expect(screen.queryByTestId("dialog-fork-chat-error")).toBeNull();
    expect(screen.getByTestId("dialog-fork-chat")).toHaveTextContent("this agent has no session fork");
    expect(screen.getByTestId("dialog-fork-chat-confirm")).toHaveTextContent("Fork with a summary");
    fireEvent.click(screen.getByTestId("dialog-fork-chat-confirm"));
    await flush();
    expect(client.nth("chat.fork", 1).params).toEqual({ chatId: CHAT.ID, atRunId: "run-1", mode: "seeded" });
    client.nth("chat.fork", 1).resolve({ ...FORKED, fork: { ...FORKED.fork!, mode: "seeded", lossy: true } });
    await flush();
    expect(onForked).toHaveBeenCalledTimes(1);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("a fork_unavailable answer without a seeded option stays an error, with no second call", async () => {
    const { client, onOpenChange } = renderDialog();
    fireEvent.click(screen.getByTestId("dialog-fork-chat-confirm"));
    client.nth("chat.fork", 0).reject(
      new RpcError("cannot fork here", "fork_unavailable", { reason: "no session to branch", seededAvailable: false }),
    );
    await flush();
    expect(screen.getByTestId("dialog-fork-chat-error")).toHaveTextContent("cannot fork here");
    expect(screen.getByTestId("dialog-fork-chat-confirm")).toHaveTextContent("Fork");
    expect(forkCalls(client)).toHaveLength(1);
    expect(onOpenChange).not.toHaveBeenCalled();
  });

  it("any other refusal shows inline and keeps the dialog open", async () => {
    const { client, onOpenChange } = renderDialog();
    fireEvent.click(screen.getByTestId("dialog-fork-chat-confirm"));
    client.nth("chat.fork", 0).reject(new RpcError("daemon exploded", "internal"));
    await flush();
    expect(screen.getByTestId("dialog-fork-chat-error")).toHaveTextContent("daemon exploded");
    expect(onOpenChange).not.toHaveBeenCalled();
  });

  it("cancel makes no call", () => {
    const { client, onOpenChange } = renderDialog();
    fireEvent.click(screen.getByTestId("dialog-fork-chat-cancel"));
    expect(forkCalls(client)).toHaveLength(0);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });
});

describe("forkReasonLabel", () => {
  it("words every daemon reason and never shows the raw code", () => {
    for (const reason of ["fork_unsupported", "native_session_missing", "native_session_in_use", "slice_not_found", "format_drift", "provider_cannot_rebuild", "rebuild_busy"]) {
      const label = forkReasonLabel(reason);
      expect(label).not.toContain("_");
      expect(label).not.toBe("unavailable");
    }
    expect(forkReasonLabel("something_new")).toBe("unavailable");
    expect(forkReasonLabel(undefined)).toBe("unavailable");
  });
});
