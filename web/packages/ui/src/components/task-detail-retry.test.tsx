import { act } from "react";
import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { TaskDetailPane } from "@/components/task-detail";
import type { Chat, RunSummary, Task } from "@/lib/types";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";

const TASK: Task = { ID: 1, WorkspaceID: 1, SpaceID: null, Title: "T", Status: "active", WorktreePath: "/tmp/a", Branch: "t", CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z", ArchivedAt: null };
const CHAT: Chat = { ID: 10, TaskID: 1, Title: "Chat", Provider: null, AgentSession: null, CreatedAt: "2024-01-01T00:00:00Z", ArchivedAt: null };
const CHILD: Chat = { ...CHAT, ID: 12, Title: "Retry of Chat", fork: { parentChatId: CHAT.ID, forkedAtRunId: "run-1", kind: "retry", mode: "native", lossy: false } };

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 4; i++) await Promise.resolve();
  });
}

function summary(id: string, prompt: string): RunSummary {
  return {
    ID: id,
    TaskID: 1,
    ChatID: CHAT.ID,
    Provider: "claude-native",
    Prompt: prompt,
    Status: "done",
    StartedAt: "2024-01-01T00:00:00Z",
    FinishedAt: "2024-01-01T00:00:05Z",
    StopReason: "end_turn",
    Err: "",
    PermissionMode: "",
    AutoAccept: false,
    ThinkingLevel: "",
  };
}

async function renderPane(autoAccept = false, permissionMode = "") {
  const client = new FakeWsClient();
  const onOpenForkedChat = vi.fn();
  render(<TaskDetailPane client={client} task={TASK} chat={CHAT} isDefaultChat onOpenForkedChat={onOpenForkedChat} />);
  client.nth("run.list", 0).resolve([{ ...summary("run-1", "first prompt"), AutoAccept: autoAccept, PermissionMode: permissionMode, Provider: autoAccept ? "codex-native" : "claude-native" }, { ...summary("run-2", "second prompt"), AutoAccept: autoAccept, PermissionMode: permissionMode, Provider: autoAccept ? "codex-native" : "claude-native" }]);
  await flush();
  for (const [i, id] of ["run-1", "run-2"].entries()) {
    client.nth("run.logs", i).resolve({ runId: id, status: "done", stopReason: "end_turn", err: "", events: [] });
    await flush();
  }
  return { client, onOpenForkedChat };
}

function editAndConfirm(text: string) {
  fireEvent.change(screen.getByTestId("retry-prompt"), { target: { value: text } });
  fireEvent.click(screen.getByTestId("dialog-retry-chat-confirm"));
}

describe("TaskDetailPane edit & retry", () => {
  it("on the second turn forks at the first run (kind retry) and starts the edited prompt in the child", async () => {
    const { client, onOpenForkedChat } = await renderPane();
    const buttons = screen.getAllByTestId("run-prompt-retry");
    expect(buttons).toHaveLength(2);
    fireEvent.click(buttons[1]);
    expect(screen.getByTestId("retry-prompt")).toHaveValue("second prompt");
    editAndConfirm("second prompt, better");
    await flush();
    expect(client.nth("chat.fork", 0).params).toEqual({ chatId: CHAT.ID, atRunId: "run-1", mode: "native", kind: "retry" });
    client.nth("chat.fork", 0).resolve(CHILD);
    await flush();
    expect(client.nth("run.start", 0).params).toMatchObject({ chatId: CHILD.ID, prompt: "second prompt, better" });
    client.nth("run.start", 0).resolve({});
    await flush();
    expect(onOpenForkedChat).toHaveBeenCalledWith(CHILD);
  });

  it("on the first turn creates a plain chat (no fork) and starts the prompt there", async () => {
    const { client, onOpenForkedChat } = await renderPane();
    fireEvent.click(screen.getAllByTestId("run-prompt-retry")[0]);
    editAndConfirm("first prompt, better");
    await flush();
    expect(client.calls.some((c) => c.method === "chat.fork")).toBe(false);
    expect(client.nth("chat.create", 0).params).toEqual({ taskId: TASK.ID });
    client.nth("chat.create", 0).resolve(CHILD);
    await flush();
    expect(client.nth("run.start", 0).params).toMatchObject({ chatId: CHILD.ID, prompt: "first prompt, better" });
    client.nth("run.start", 0).resolve({});
    await flush();
    expect(onOpenForkedChat).toHaveBeenCalledWith(CHILD);
  });

  it("a retry never carries auto-accept from the replaced run into the new chat", async () => {
    const { client } = await renderPane(true);
    fireEvent.click(screen.getAllByTestId("run-prompt-retry")[1]);
    editAndConfirm("second prompt, better");
    await flush();
    client.nth("chat.fork", 0).resolve(CHILD);
    await flush();
    expect(client.nth("run.start", 0).params).not.toHaveProperty("autoAccept");
  });

  it("a turn whose provider is no longer configured is refused before any chat is created", async () => {
    const { client } = await renderPane();
    client.nth("provider.list", 0).resolve({ providers: [{ id: "glm", label: "GLM", kind: "cli" }] });
    await flush();
    fireEvent.click(screen.getAllByTestId("run-prompt-retry")[1]);
    editAndConfirm("second prompt, better");
    await flush();
    expect(screen.getByTestId("dialog-retry-chat-error")).toHaveTextContent("no longer available");
    expect(client.calls.some((c) => c.method === "chat.fork" || c.method === "chat.create" || c.method === "run.start")).toBe(false);
  });

  it("never runs in the old run's permission mode: the provider default when the composer is on another provider or absent", async () => {
    const { client } = await renderPane(false, "bypass_permissions");
    fireEvent.click(screen.getAllByTestId("run-prompt-retry")[1]);
    expect(screen.getByTestId("retry-permission-note")).toHaveTextContent("never in the mode this turn was switched to");
    editAndConfirm("second prompt, better");
    await flush();
    client.nth("chat.fork", 0).resolve(CHILD);
    await flush();
    expect(client.nth("run.start", 0).params).not.toHaveProperty("permissionMode");
    expect(client.nth("run.start", 0).params).not.toHaveProperty("autoAccept");
  });
});
