import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";
import { TaskDetailPane } from "@/components/task-detail";
import { KeyboardProvider } from "@/keyboard/keyboard-provider";
import type { RunLogsResult, RunSummary, Task } from "@/lib/types";

/**
 * End-to-end coverage for chat Find (AC1): `Mod+F` claimed only while the
 * chat pane has focus, matches highlighted in the rendered transcript, and
 * everything cleared on close -- exercised through `TaskDetailPane` itself
 * (not `use-chat-find.ts` in isolation) since the whole point is that
 * `pane.find`'s claim, the DOM walk, and `RunTimeline`'s existing memoized
 * rows all keep working together.
 */

const TASK: Task = {
  ID: 1,
  WorkspaceID: 1,
  SpaceID: null,
  Title: "Task A",
  Status: "active",
  WorktreePath: "/tmp/a",
  Branch: "task-a",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

function runningRun(): RunSummary {
  return {
    ID: "run-1",
    TaskID: TASK.ID,
    Provider: "claude-native",
    Prompt: "describe the cat",
    Status: "running",
    StartedAt: "2024-01-01T00:00:00Z",
    FinishedAt: null,
    StopReason: "",
    Err: "",
    ApprovalPolicy: "manual",
    ThinkingLevel: "",
  };
}

function doneRun(): RunSummary {
  return {
    ID: "run-1",
    TaskID: TASK.ID,
    Provider: "claude-native",
    Prompt: "describe the cat",
    Status: "done",
    StartedAt: "2024-01-01T00:00:00Z",
    FinishedAt: "2024-01-01T00:00:05Z",
    StopReason: "end_turn",
    Err: "",
    ApprovalPolicy: "manual",
    ThinkingLevel: "",
  };
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** Renders the pane with one finished run whose assistant text has two occurrences of "cat" to search for. */
async function renderWithTranscript() {
  const client = new FakeWsClient();
  render(
    <KeyboardProvider>
      <TaskDetailPane client={client} task={TASK} />
    </KeyboardProvider>,
  );

  client.nth("run.list", 0).resolve([doneRun()]);
  await flush();

  const logs: RunLogsResult = {
    runId: "run-1",
    status: "done",
    stopReason: "end_turn",
    events: [{ type: "chunk", text: "the cat sat on the cat mat" }, { type: "done", stopReason: "end_turn" }],
  };
  client.nth("run.logs", 0).resolve(logs);
  await flush();

  return client;
}

function pressModF(): void {
  fireEvent.keyDown(document, { key: "f", code: "KeyF", ctrlKey: true });
}

function focusChatPane(): void {
  fireEvent.focus(screen.getByTestId("run-log-scroll"));
}

afterEach(() => {
  vi.useRealTimers();
});

describe("chat Find", () => {
  it("Mod+F opens the bar only once the chat pane is focused", async () => {
    await renderWithTranscript();

    pressModF();
    expect(screen.queryByTestId("find-bar")).not.toBeInTheDocument();

    focusChatPane();
    pressModF();
    expect(screen.getByTestId("find-bar")).toBeInTheDocument();
  });

  it("highlights every match, shows i/N, and clears highlights on close", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    await renderWithTranscript();

    focusChatPane();
    pressModF();

    fireEvent.change(screen.getByTestId("find-input"), { target: { value: "cat" } });
    act(() => {
      vi.advanceTimersByTime(200);
    });

    expect(document.querySelectorAll('mark[data-chat-find-match]')).toHaveLength(2);
    expect(screen.getByTestId("find-status")).toHaveTextContent("1/2");

    fireEvent.keyDown(screen.getByTestId("find-input"), { key: "Escape" });

    expect(screen.queryByTestId("find-bar")).not.toBeInTheDocument();
    expect(document.querySelectorAll('mark[data-chat-find-match]')).toHaveLength(0);
  });

  it("Enter/Shift+Enter move between matches with wraparound", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    await renderWithTranscript();

    focusChatPane();
    pressModF();
    fireEvent.change(screen.getByTestId("find-input"), { target: { value: "cat" } });
    act(() => {
      vi.advanceTimersByTime(200);
    });
    expect(screen.getByTestId("find-status")).toHaveTextContent("1/2");

    fireEvent.keyDown(screen.getByTestId("find-input"), { key: "Enter" });
    expect(screen.getByTestId("find-status")).toHaveTextContent("2/2");

    fireEvent.keyDown(screen.getByTestId("find-input"), { key: "Enter" });
    expect(screen.getByTestId("find-status")).toHaveTextContent("1/2");

    fireEvent.keyDown(screen.getByTestId("find-input"), { key: "Enter", shiftKey: true });
    expect(screen.getByTestId("find-status")).toHaveTextContent("2/2");
  });

  it("finds matches inside assistant text that streams in after Find is already open", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const client = new FakeWsClient();
    render(
      <KeyboardProvider>
        <TaskDetailPane client={client} task={TASK} />
      </KeyboardProvider>,
    );
    client.nth("run.list", 0).resolve([runningRun()]);
    await flush();

    focusChatPane();
    pressModF();
    fireEvent.change(screen.getByTestId("find-input"), { target: { value: "dog" } });
    act(() => {
      vi.advanceTimersByTime(200);
    });
    expect(screen.getByTestId("find-status")).toHaveTextContent("No matches");

    client.emit("run.attach", 0, "chunk", { text: "a dog barked" });
    await flush();
    act(() => {
      vi.advanceTimersByTime(200);
    });

    expect(document.querySelectorAll('mark[data-chat-find-match]')).toHaveLength(1);
    expect(screen.getByTestId("find-status")).toHaveTextContent("1/1");
  });

  it("an empty query shows no status and no highlights", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    await renderWithTranscript();

    focusChatPane();
    pressModF();
    act(() => {
      vi.advanceTimersByTime(200);
    });

    expect(screen.getByTestId("find-status")).toHaveTextContent("");
    expect(document.querySelectorAll('mark[data-chat-find-match]')).toHaveLength(0);
  });
});
