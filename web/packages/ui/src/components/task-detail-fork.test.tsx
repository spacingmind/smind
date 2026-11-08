import { act } from "react";
import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { TaskDetailPane } from "@/components/task-detail";
import type { Chat, RunSummary, Task } from "@/lib/types";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";

const TASK: Task = { ID: 1, WorkspaceID: 1, SpaceID: null, Title: "T", Status: "active", WorktreePath: "/tmp/a", Branch: "t", CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z", ArchivedAt: null };
const PARENT: Chat = { ID: 9, TaskID: 1, Title: "Original chat", Provider: null, AgentSession: null, CreatedAt: "2024-01-01T00:00:00Z", ArchivedAt: null };
const CHAT: Chat = { ID: 10, TaskID: 1, Title: "Chat", Provider: null, AgentSession: null, CreatedAt: "2024-01-01T00:00:00Z", ArchivedAt: null };

function forked(overrides: Partial<NonNullable<Chat["fork"]>>): Chat {
  return { ...CHAT, fork: { parentChatId: PARENT.ID, forkedAtRunId: "run-1", kind: "fork", mode: "native", lossy: false, ...overrides } };
}

const FORKED: Chat = {
  ...CHAT,
  ID: 11,
  Title: "Fork of Chat",
  fork: { parentChatId: CHAT.ID, forkedAtRunId: "run-1", kind: "fork", mode: "native", lossy: false },
};

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** A pane with one finished turn, like resubmit-reuse.test.tsx's renderRun. */
async function renderPane(chat: Chat, props: Partial<Parameters<typeof TaskDetailPane>[0]> = {}) {
  const client = new FakeWsClient();
  const onOpenForkedChat = vi.fn();
  render(<TaskDetailPane client={client} task={TASK} chat={chat} isDefaultChat onOpenForkedChat={onOpenForkedChat} {...props} />);
  const summary = (chatId: number): RunSummary => (
    {
      ID: "run-1",
      TaskID: 1,
      ChatID: chatId,
      Provider: "claude-native",
      Prompt: "do the thing",
      Status: "done",
      StartedAt: "2024-01-01T00:00:00Z",
      FinishedAt: "2024-01-01T00:00:05Z",
      StopReason: "end_turn",
      Err: "",
      PermissionMode: "",
      AutoAccept: false,
      ThinkingLevel: "",
    });
  client.nth("run.list", 0).resolve([summary(chat.ID)]);
  await flush();
  client.nth("run.logs", 0).resolve({ runId: "run-1", status: "done", stopReason: "end_turn", err: "", events: [] });
  await flush();
  if (chat.fork) {
    // The fork source's load of the parent's turns (use-fork-source), which names the banner's turn.
    client.nth("run.list", 1).resolve([summary(chat.fork.parentChatId)]);
    await flush();
    client.nth("run.logs", 1).resolve({ runId: "run-1", status: "done", stopReason: "end_turn", err: "", events: [] });
    await flush();
  }
  return { client, onOpenForkedChat };
}

describe("TaskDetailPane fork", () => {
  it("a retry names the next turn and View original opens the parent", async () => {
    const onOpenParentChat = vi.fn();
    await renderPane(forked({ kind: "retry" }), { parentChat: PARENT, onOpenParentChat });
    expect(screen.getByTestId("chat-fork-banner")).toHaveTextContent("Retry of turn 2 from Original chat.");
    fireEvent.click(screen.getByTestId("chat-fork-view-original"));
    expect(onOpenParentChat).toHaveBeenCalledWith(PARENT.ID);
  });

  it("no View original button when the parent is unknown", async () => {
    await renderPane(forked({}), { onOpenParentChat: vi.fn() });
    expect(screen.queryByTestId("chat-fork-view-original")).toBeNull();
  });

  it("Fork on a finished turn opens the dialog and a confirm forks the chat at that run", async () => {
    const { client, onOpenForkedChat } = await renderPane(CHAT);
    fireEvent.click(screen.getByTestId("run-fork"));
    expect(screen.getByTestId("dialog-fork-chat")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("dialog-fork-chat-confirm"));
    await flush();
    expect(client.nth("chat.fork", 0).params).toEqual({ chatId: CHAT.ID, atRunId: "run-1", mode: "native" });
    client.nth("chat.fork", 0).resolve(FORKED);
    await flush();
    expect(onOpenForkedChat).toHaveBeenCalledWith(FORKED);
    expect(screen.queryByTestId("dialog-fork-chat")).toBeNull();
  });

  it("a forked chat shows the banner naming its parent", async () => {
    await renderPane(forked({}), { parentChat: PARENT });
    expect(screen.getByTestId("chat-fork-banner")).toHaveTextContent("Forked from Original chat at turn 1.");
    expect(screen.getByTestId("chat-fork-banner")).not.toHaveTextContent("summary of the earlier turns");
  });

  it("a lossy fork adds the summary sentence; a missing parent falls back to another chat", async () => {
    await renderPane(forked({ mode: "seeded", lossy: true }));
    expect(screen.getByTestId("chat-fork-banner")).toHaveTextContent(
      "Forked from another chat at turn 1. The agent was given a summary of the earlier turns, so it does not remember tool calls or reasoning.",
    );
  });

  it("an unforked chat shows no banner", async () => {
    await renderPane(CHAT);
    expect(screen.queryByTestId("chat-fork-banner")).toBeNull();
  });
});
