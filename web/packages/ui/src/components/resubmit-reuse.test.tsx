import { act } from "react";
import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { TaskDetailPane } from "@/components/task-detail";
import { resendRequests } from "@/lib/attachments/reuse";
import type { AttachmentMeta, Chat, RunSummary, Task } from "@/lib/types";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";

const TASK: Task = { ID: 1, WorkspaceID: 1, SpaceID: null, Title: "T", Status: "active", WorktreePath: "/tmp/a", Branch: "t", CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z", ArchivedAt: null };
const CHAT: Chat = { ID: 10, TaskID: 1, Title: "Chat", Provider: null, AgentSession: null, CreatedAt: "2024-01-01T00:00:00Z", ArchivedAt: null };

const ATTACHMENTS: AttachmentMeta[] = [
  { id: "1", kind: "image", name: "a.png", mimeType: "image/png", size: 10 },
  { id: "2", kind: "file", name: "b.pdf", mimeType: "application/pdf", size: 10 },
  { id: "3", kind: "file_ref", path: "internal/x.go", isDir: false },
  { id: "4", kind: "github_pr", number: 7, repo: "o/r", title: "t", url: "https://github.com/o/r/pull/7" },
];
const REUSE = [
  { kind: "reuse", runId: "run-1", id: "1" },
  { kind: "reuse", runId: "run-1", id: "2" },
  { kind: "file_ref", path: "internal/x.go" },
  { kind: "github_pr", number: 7, repo: "o/r" },
];

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

async function renderRun(overrides: Partial<RunSummary>, withAttachments = true) {
  const client = new FakeWsClient();
  render(<TaskDetailPane client={client} task={TASK} chat={CHAT} isDefaultChat />);
  client.nth("run.list", 0).resolve([
    {
      ID: "run-1",
      TaskID: 1,
      ChatID: 10,
      Provider: "claude-native",
      Prompt: "do the thing",
      Status: "done",
      StartedAt: "2024-01-01T00:00:00Z",
      FinishedAt: "2024-01-01T00:00:05Z",
      StopReason: "end_turn",
      Err: "",
      PermissionMode: "acceptEdits",
      AutoAccept: false,
      ThinkingLevel: "",
      ...(withAttachments ? { attachments: ATTACHMENTS } : {}),
      ...overrides,
    } satisfies RunSummary,
  ]);
  await flush();
  client.nth("run.logs", 0).resolve({ runId: "run-1", status: overrides.Status ?? "done", stopReason: overrides.StopReason ?? "end_turn", err: overrides.Err, events: [] });
  await flush();
  return client;
}

describe("resubmit carries attachments via reuse", () => {
  it("resendRequests maps each kind and ignores items it cannot rebuild", () => {
    expect(resendRequests("run-1", ATTACHMENTS)).toEqual(REUSE);
    expect(resendRequests("run-1", [{ id: "1", kind: "file_ref" }, { id: "2", kind: "github_issue" }])).toEqual([]);
    expect(resendRequests("run-1", undefined)).toEqual([]);
  });

  it("Re-run sends reuse requests", async () => {
    const client = await renderRun({});
    fireEvent.click(screen.getByTestId("run-rerun"));
    await flush();
    expect(client.nth("run.start", 0).params).toMatchObject({ prompt: "do the thing", attachments: REUSE });
  });

  it("Continue sends reuse requests", async () => {
    const client = await renderRun({ StopReason: "max_tokens" });
    fireEvent.click(await screen.findByTestId("run-continue"));
    await flush();
    expect(client.nth("run.start", 0).params).toMatchObject({ prompt: "Continue", attachments: REUSE });
  });

  it("Retry with higher effort sends reuse requests", async () => {
    const client = await renderRun({ Status: "error", Err: "boom", ThinkingLevel: "off" });
    fireEvent.click(screen.getByTestId("run-retry-higher-effort"));
    await flush();
    expect(client.nth("run.start", 0).params).toMatchObject({ thinkingLevel: "standard", attachments: REUSE });
  });

  it("a run without attachments sends no attachments field", async () => {
    const client = await renderRun({}, false);
    fireEvent.click(screen.getByTestId("run-rerun"));
    await flush();
    expect(client.nth("run.start", 0).params).not.toHaveProperty("attachments");
  });
});
