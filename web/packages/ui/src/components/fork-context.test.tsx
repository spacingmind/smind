import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ForkContext } from "@/components/fork-context";
import { TaskDetailPane } from "@/components/task-detail";
import type { RunEntry } from "@/hooks/use-run-timeline";
import type { Chat, RunSummary, Task } from "@/lib/types";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp } from "@/test/render-with-daemon";

const TASK: Task = { ID: 1, WorkspaceID: 1, SpaceID: null, Title: "T", Status: "active", WorktreePath: "/tmp/a", Branch: "t", CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z", ArchivedAt: null };
const CHAT: Chat = { ID: 10, TaskID: 1, Title: "Chat", Provider: null, AgentSession: null, CreatedAt: "2024-01-01T00:00:00Z", ArchivedAt: null };

function forked(overrides: Partial<NonNullable<Chat["fork"]>> = {}): Chat {
  return { ...CHAT, fork: { parentChatId: 9, forkedAtRunId: "run-1", kind: "fork", mode: "native", lossy: false, ...overrides } };
}

const RUN: RunEntry = { id: "run-1", provider: "claude-native", prompt: "inherited prompt", status: "done", startedAt: "2024-01-01T00:00:00Z", items: [], permissionMode: "", autoAccept: false, thinkingLevel: "" };

describe("ForkContext", () => {
  it("a native fork shows the inherited turns read-only above the divider", () => {
    render(<ForkContext chat={forked()} parentTitle="Original chat" inherited={[RUN]} seed={null} loading={false} />);
    const block = screen.getByTestId("fork-inherited");
    expect(block).toHaveAttribute("aria-label", "Earlier turns from Original chat");
    expect(block).toHaveTextContent("inherited prompt");
    expect(screen.queryByTestId("run-rerun")).toBeNull();
    expect(screen.queryByTestId("run-fork")).toBeNull();
    expect(screen.queryByTestId("run-prompt-edit")).toBeNull();
    expect(screen.getByTestId("fork-divider")).toHaveAttribute("aria-label", "Forked here");
    expect(screen.queryByTestId("fork-seed")).toBeNull();
  });

  it("a seeded fork shows the labelled summary, collapsed until opened", () => {
    render(<ForkContext chat={forked({ mode: "seeded", lossy: true })} parentTitle="Original chat" inherited={null} seed={"User: hi\nAssistant: hello"} loading={false} />);
    const block = screen.getByTestId("fork-seed");
    expect(block).toHaveTextContent("Summary sent to the agent");
    expect(block).toHaveTextContent("does not remember tool calls or reasoning");
    const details = block.querySelector("details") as HTMLDetailsElement;
    expect(details.open).toBe(false);
    expect(screen.getByText("Show the summary")).toBeInTheDocument();
    fireEvent.click(screen.getByText("Show the summary"));
    expect(details.open).toBe(true);
    expect(block).toHaveTextContent("Assistant: hello");
    expect(screen.queryByTestId("fork-inherited")).toBeNull();
    expect(screen.getByTestId("fork-divider")).toBeInTheDocument();
  });

  it("a retry chat labels the divider Retry starts here", () => {
    render(<ForkContext chat={forked({ kind: "retry" })} parentTitle="Original chat" inherited={[RUN]} seed={null} loading={false} />);
    expect(screen.getByRole("separator", { name: "Retry starts here" })).toBeInTheDocument();
  });

  it("shows a spinner while the earlier turns load", () => {
    render(<ForkContext chat={forked()} parentTitle="Original chat" inherited={null} seed={null} loading />);
    expect(screen.getByText("Loading earlier turns…")).toBeInTheDocument();
    expect(screen.queryByTestId("fork-inherited")).toBeNull();
  });
});

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

function summary(chatId: number): RunSummary {
  return { ID: "run-1", TaskID: 1, ChatID: chatId, Provider: "claude-native", Prompt: "parent prompt", Status: "done", StartedAt: "2024-01-01T00:00:00Z", FinishedAt: "2024-01-01T00:00:05Z", StopReason: "end_turn", Err: "", PermissionMode: "", AutoAccept: false, ThinkingLevel: "" };
}

describe("TaskDetailPane fork context", () => {
  it("an empty forked chat shows the inherited turns, the divider and Continue from here", async () => {
    const client = new FakeWsClient();
    renderWithClientProp(<TaskDetailPane client={client} task={TASK} chat={forked()} isDefaultChat onOpenForkedChat={vi.fn()} />);
    client.nth("run.list", 0).resolve([]);
    await flush();
    client.nth("run.list", 1).resolve([summary(9)]);
    await flush();
    client.nth("run.logs", 0).resolve({ runId: "run-1", status: "done", stopReason: "end_turn", err: "", events: [] });
    await flush();
    expect(screen.getByTestId("fork-inherited")).toHaveTextContent("parent prompt");
    expect(screen.getByTestId("fork-divider")).toBeInTheDocument();
    expect(screen.getByText("Continue from here")).toBeInTheDocument();
    expect(screen.getByText("Send a prompt to continue this chat")).toBeInTheDocument();
    expect(screen.queryByText("No runs yet")).toBeNull();
  });

  it("an empty unforked chat still shows No runs yet", async () => {
    const client = new FakeWsClient();
    renderWithClientProp(<TaskDetailPane client={client} task={TASK} chat={CHAT} isDefaultChat onOpenForkedChat={vi.fn()} />);
    client.nth("run.list", 0).resolve([]);
    await flush();
    expect(screen.getByText("No runs yet")).toBeInTheDocument();
    expect(screen.queryByTestId("fork-divider")).toBeNull();
  });
});
