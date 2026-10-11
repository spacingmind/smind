import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";
import { TaskDetailPane } from "@/components/task-detail";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { Chat, Task, WorkspaceWithScripts } from "@/lib/types";

afterEach(() => {
  window.localStorage.clear();
});

const TASK: Task = {
  ID: 1,
  WorkspaceID: 3,
  SpaceID: null,
  Title: "Task A",
  Status: "active",
  WorktreePath: "/tmp/a",
  Branch: "task-a",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

const CHAT: Chat = {
  ID: 10,
  TaskID: 1,
  Title: "Chat",
  Provider: null,
  AgentSession: null,
  CreatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

const WORKSPACE: WorkspaceWithScripts = {
  ID: 3,
  Path: "/repo",
  Title: "Repo",
  RoutingPolicy: "",
  HasSetupScript: true,
  HasTeardownScript: false,
  SetupScript: "npm ci",
  TeardownScript: "",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
};

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("task setup status", () => {
  it("task shows setup running badge", () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={{ ...TASK, SetupStatus: "running" }} chat={CHAT} isDefaultChat />);

    expect(screen.getByTestId("task-setup-badge")).toHaveAttribute("data-setup-status", "running");
    expect(screen.getByTestId("task-setup-badge")).toHaveTextContent("Setup running");
    expect(screen.queryByTestId("task-setup-retry")).toBeNull();
  });

  it("task shows setup failed badge with the error", async () => {
    const client = new FakeWsClient();
    const task: Task = { ...TASK, SetupStatus: "failed", SetupError: "exit status 1", SetupLog: "npm ERR! boom", Port: 31002 };
    render(<TaskDetailPane client={client} task={task} chat={CHAT} isDefaultChat />);

    expect(screen.getByTestId("task-setup-badge")).toHaveAttribute("data-setup-status", "failed");
    expect(screen.getByTestId("task-setup-error")).toHaveTextContent("exit status 1");
    expect(screen.getByTestId("task-port-chip")).toHaveTextContent(":31002");

    expect(screen.queryByTestId("task-setup-log")).toBeNull();
    fireEvent.click(screen.getByTestId("task-setup-log-toggle"));
    expect(screen.getByTestId("task-setup-log")).toHaveTextContent("npm ERR! boom");
  });

  it("retry setup button calls task.retrySetup", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={{ ...TASK, SetupStatus: "failed", SetupError: "exit status 1" }} chat={CHAT} isDefaultChat />);

    expect(screen.queryByTestId("task-setup-retry")).toBeNull();
    client.nth("workspace.get", 0).resolve(WORKSPACE);
    await flush();

    fireEvent.click(screen.getByTestId("task-setup-retry"));
    expect(client.nth("task.retrySetup", 0).params).toEqual({ id: 1 });
    client.nth("task.retrySetup", 0).resolve({ ...TASK, SetupStatus: "running" });
    await flush();

    expect(screen.getByTestId("task-setup-badge")).toHaveAttribute("data-setup-status", "running");
    expect(screen.queryByTestId("task-setup-retry")).toBeNull();
  });

  it("no retry button when the workspace script was cleared", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={{ ...TASK, SetupStatus: "ok", SetupLog: "done" }} chat={CHAT} isDefaultChat />);

    client.nth("workspace.get", 0).resolve({ ...WORKSPACE, HasSetupScript: false, SetupScript: "" });
    await flush();

    expect(screen.queryByTestId("task-setup-retry")).toBeNull();
  });

  it("no setup badge when the workspace has no script", () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={{ ...TASK, SetupStatus: "" }} chat={CHAT} isDefaultChat />);

    expect(screen.queryByTestId("task-setup-badge")).toBeNull();
    expect(screen.queryByTestId("task-setup-details")).toBeNull();
    expect(screen.queryByTestId("task-port-chip")).toBeNull();
  });

  it("follows task.updated events as setup finishes", async () => {
    const client = new FakeWsClient();
    // Multiple hooks subscribe to task.updated (useLiveTask and the
    // lineage strip's refetch), so capture every listener, not just the
    // latest subscriber.
    const listeners: Array<(payload: unknown) => void> = [];
    const events: DaemonEvents = {
      subscribe(topic, listener) {
        if (topic === "task.updated") listeners.push(listener);
        return () => {};
      },
    };
    const push = (payload: unknown) => {
      for (const fn of listeners) fn(payload);
    };
    render(<TaskDetailPane client={client} task={{ ...TASK, SetupStatus: "running" }} chat={CHAT} isDefaultChat events={events} />);

    await act(async () => push({ task: { ...TASK, SetupStatus: "ok", Port: 31000 } }));

    expect(screen.getByTestId("task-setup-badge")).toHaveAttribute("data-setup-status", "ok");
    expect(screen.getByTestId("task-port-chip")).toHaveTextContent(":31000");
  });
});
