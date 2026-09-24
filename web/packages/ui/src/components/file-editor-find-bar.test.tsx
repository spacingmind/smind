import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { FileEditorPane } from "@/components/file-editor-pane";
import { KeyboardProvider } from "@/keyboard/keyboard-provider";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { Task } from "@/lib/types";

/**
 * End-to-end coverage for file Find (AC2), through `FileEditorPane` itself
 * so `pane.find`'s focus gating and the real `@codemirror/search` wiring
 * are both exercised together, the same shape as `chat-find.test.tsx`.
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

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

async function openFile(content: string): Promise<FakeWsClient> {
  const client = new FakeWsClient();
  render(
    <KeyboardProvider>
      <FileEditorPane client={client} task={TASK} path="README.md" />
    </KeyboardProvider>,
  );
  client.nth("file.read", 0).resolve({ content });
  await flush();
  return client;
}

function pressModF(): void {
  fireEvent.keyDown(document, { key: "f", code: "KeyF", ctrlKey: true });
}

function focusEditor(): void {
  fireEvent.focus(screen.getByTestId("file-editor-pane"));
}

describe("file Find", () => {
  it("Mod+F opens the bar only once the editor pane is focused", async () => {
    await openFile("hello world hello");

    pressModF();
    expect(screen.queryByTestId("find-bar")).not.toBeInTheDocument();

    focusEditor();
    pressModF();
    expect(screen.getByTestId("find-bar")).toBeInTheDocument();
  });

  it("shows the match count and navigates with prev/next", async () => {
    await openFile("hello world hello");
    focusEditor();
    pressModF();

    fireEvent.change(screen.getByTestId("find-input"), { target: { value: "hello" } });
    expect(screen.getByTestId("find-status")).toHaveTextContent("1/2");

    fireEvent.click(screen.getByRole("button", { name: "Next match" }));
    expect(screen.getByTestId("find-status")).toHaveTextContent("2/2");

    fireEvent.click(screen.getByRole("button", { name: "Previous match" }));
    expect(screen.getByTestId("find-status")).toHaveTextContent("1/2");
  });

  it("replace and replace all edit the document through the shared bar", async () => {
    await openFile("hello world hello");
    focusEditor();
    pressModF();

    fireEvent.change(screen.getByTestId("find-input"), { target: { value: "hello" } });
    fireEvent.click(screen.getByTestId("find-toggle-replace"));
    fireEvent.change(screen.getByTestId("find-replace-input"), { target: { value: "hi" } });

    fireEvent.click(screen.getByTestId("find-replace"));
    expect(screen.getByTestId("file-editor")).toHaveTextContent("hi world hello");

    fireEvent.click(screen.getByTestId("find-replace-all"));
    expect(screen.getByTestId("file-editor")).toHaveTextContent("hi world hi");
  });

  it("Escape closes the bar", async () => {
    await openFile("hello world");
    focusEditor();
    pressModF();
    expect(screen.getByTestId("find-bar")).toBeInTheDocument();

    fireEvent.keyDown(screen.getByTestId("find-input"), { key: "Escape" });
    expect(screen.queryByTestId("find-bar")).not.toBeInTheDocument();
  });
});
