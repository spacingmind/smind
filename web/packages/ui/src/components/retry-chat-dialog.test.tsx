import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { RetryChatDialog } from "@/components/retry-chat-dialog";
import { RetryStartError } from "@/hooks/use-retry-chat";
import type { Chat } from "@/lib/types";
import { RpcError } from "@/lib/ws-client";

const CHAT: Chat = { ID: 10, TaskID: 1, Title: "Refactor the parser", Provider: null, AgentSession: null, CreatedAt: "2024-01-01T00:00:00Z", ArchivedAt: null };
const CHILD: Chat = { ...CHAT, ID: 11, Title: "Retry of Refactor the parser" };

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

function renderDialog(overrides: Partial<Parameters<typeof RetryChatDialog>[0]> = {}) {
  const onRetry = vi.fn<(prompt: string, mode: "native" | "seeded") => Promise<Chat>>();
  const onRetried = vi.fn();
  const onOpenChange = vi.fn();
  const { rerender } = render(
    <RetryChatDialog
      open
      onOpenChange={onOpenChange}
      chatTitle={CHAT.Title}
      turn={2}
      initialPrompt="do the thing"
      hasAnchor
      onRetry={onRetry}
      onRetried={onRetried}
      {...overrides}
    />,
  );
  return { onRetry, onRetried, onOpenChange, rerender };
}

function textarea(): HTMLTextAreaElement {
  return screen.getByTestId("retry-prompt") as HTMLTextAreaElement;
}

function edit(value: string): void {
  fireEvent.change(textarea(), { target: { value } });
}

describe("RetryChatDialog", () => {
  it("prefills the edited prompt and resets to it on reopen", () => {
    const { rerender } = renderDialog();
    expect(textarea().value).toBe("do the thing");
    edit("my edit");
    expect(textarea().value).toBe("my edit");
    rerender(
      <RetryChatDialog
        open={false}
        onOpenChange={vi.fn()}
        chatTitle={CHAT.Title}
        turn={2}
        initialPrompt="do the thing"
        hasAnchor
        onRetry={vi.fn()}
        onRetried={vi.fn()}
      />,
    );
    rerender(
      <RetryChatDialog
        open
        onOpenChange={vi.fn()}
        chatTitle={CHAT.Title}
        turn={2}
        initialPrompt="do the thing"
        hasAnchor
        onRetry={vi.fn()}
        onRetried={vi.fn()}
      />,
    );
    expect(textarea().value).toBe("do the thing");
  });

  it("disables confirm while the prompt is empty or whitespace only", () => {
    renderDialog();
    edit("   ");
    expect(screen.getByTestId("dialog-retry-chat-confirm")).toBeDisabled();
    edit("x");
    expect(screen.getByTestId("dialog-retry-chat-confirm")).not.toBeDisabled();
  });

  it("retries natively, hands the new chat back and closes", async () => {
    const { onRetry, onRetried, onOpenChange } = renderDialog();
    onRetry.mockResolvedValue(CHILD);
    edit("the edited prompt");
    fireEvent.click(screen.getByTestId("dialog-retry-chat-confirm"));
    await flush();
    expect(onRetry).toHaveBeenCalledWith("the edited prompt", "native");
    expect(onRetried).toHaveBeenCalledWith(CHILD);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("a fork_unavailable answer offers the seeded retry and runs it on confirm", async () => {
    const { onRetry, onRetried, onOpenChange } = renderDialog();
    onRetry.mockRejectedValueOnce(new RpcError("cannot fork here", "fork_unavailable", { reason: "fork_unsupported", seededAvailable: true }));
    fireEvent.click(screen.getByTestId("dialog-retry-chat-confirm"));
    await flush();
    // The dialog stays open, now describing the seeded alternative instead of an error.
    expect(onOpenChange).not.toHaveBeenCalled();
    expect(screen.queryByTestId("dialog-retry-chat-error")).toBeNull();
    expect(screen.getByTestId("dialog-retry-chat")).toHaveTextContent("this agent has no session fork");
    expect(screen.getByTestId("dialog-retry-chat-confirm")).toHaveTextContent("Retry with a summary");
    onRetry.mockResolvedValueOnce({ ...CHILD, fork: { parentChatId: CHAT.ID, forkedAtRunId: "run-1", kind: "retry", mode: "seeded", lossy: true } });
    fireEvent.click(screen.getByTestId("dialog-retry-chat-confirm"));
    await flush();
    expect(onRetry).toHaveBeenLastCalledWith("do the thing", "seeded");
    expect(onRetried).toHaveBeenCalledTimes(1);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("a transient refusal stays an inline error and offers no lossy retry", async () => {
    const { onRetry, onRetried, onOpenChange } = renderDialog();
    onRetry.mockRejectedValueOnce(new RpcError("cannot fork here", "fork_unavailable", { reason: "native_session_in_use", seededAvailable: true }));
    fireEvent.click(screen.getByTestId("dialog-retry-chat-confirm"));
    await flush();
    expect(screen.getByTestId("dialog-retry-chat-error")).toHaveTextContent("right now");
    expect(screen.getByTestId("dialog-retry-chat-confirm")).toHaveTextContent("Retry");
    expect(onRetry).toHaveBeenCalledTimes(1);
    expect(onRetried).not.toHaveBeenCalled();
    expect(onOpenChange).not.toHaveBeenCalled();
  });

  it("any other refusal shows inline and keeps the dialog open", async () => {
    const { onRetry, onOpenChange } = renderDialog();
    onRetry.mockRejectedValueOnce(new RpcError("daemon exploded", "internal"));
    fireEvent.click(screen.getByTestId("dialog-retry-chat-confirm"));
    await flush();
    expect(screen.getByTestId("dialog-retry-chat-error")).toHaveTextContent("daemon exploded");
    expect(onOpenChange).not.toHaveBeenCalled();
  });

  it("a started-nothing failure says so and offers the new chat instead of closing", async () => {
    const { onRetry, onRetried, onOpenChange } = renderDialog();
    onRetry.mockRejectedValueOnce(new RetryStartError(CHILD, new Error("daemon busy")));
    fireEvent.click(screen.getByTestId("dialog-retry-chat-confirm"));
    await flush();
    expect(screen.getByTestId("dialog-retry-chat-error")).toHaveTextContent("The new chat was created but the prompt could not be started: daemon busy");
    expect(onRetried).not.toHaveBeenCalled();
    expect(onOpenChange).not.toHaveBeenCalled();
    expect(screen.getByTestId("dialog-retry-chat-confirm")).toHaveTextContent("Open the new chat");
    fireEvent.click(screen.getByTestId("dialog-retry-chat-confirm"));
    await flush();
    expect(onRetry).toHaveBeenCalledTimes(1);
    expect(onRetried).toHaveBeenCalledWith(CHILD);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("two clicks before the dialog re-renders start one retry", async () => {
    const { onRetry } = renderDialog();
    onRetry.mockReturnValue(new Promise(() => {}));
    const confirm = screen.getByTestId("dialog-retry-chat-confirm");
    // Both clicks land in one batch, before the first has re-rendered the button disabled.
    await act(async () => {
      confirm.click();
      confirm.click();
    });
    await flush();
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it("describes the no-anchor case: the edit starts in a new chat", () => {
    renderDialog({ hasAnchor: false });
    expect(screen.getByTestId("dialog-retry-chat")).toHaveTextContent(
      "There is no earlier turn to branch from, so the edited prompt starts in a new chat. This chat stays as it is.",
    );
  });

  it("cancel makes no call", () => {
    const { onRetry, onOpenChange } = renderDialog();
    fireEvent.click(screen.getByTestId("dialog-retry-chat-cancel"));
    expect(onRetry).not.toHaveBeenCalled();
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });
});
