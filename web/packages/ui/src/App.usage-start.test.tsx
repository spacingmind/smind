import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { App } from "@/App";
import { WsClient } from "@/lib/ws-client";
import { FakeSocket } from "@/test/fake-socket";
import "@/test/query-scheduler";
import type { Chat, Task, Workspace } from "@/lib/types";

vi.setConfig({ testTimeout: 30_000 });

const WORKSPACE: Workspace = {
  ID: 1,
  Path: "/tmp/ws",
  Title: "My Workspace",
  RoutingPolicy: "",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
};

const TASK_A: Task = {
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

const CHAT: Chat = {
  ID: 101,
  TaskID: 1,
  Title: "Chat",
  Provider: null,
  AgentSession: null,
  CreatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

function respond(socket: FakeSocket, method: string, result: unknown): void {
  for (const env of socket.sent.filter((e) => e.method === method)) {
    if (env.id) socket.emit({ id: env.id, result });
  }
}

function usageRequests(socket: FakeSocket): { method?: string; params?: { mode?: string; accountIds?: number[] } }[] {
  return socket.sent.filter((e) => e.method === "account.usage") as {
    method?: string;
    params?: { mode?: string; accountIds?: number[] };
  }[];
}

/** A 92 % five-hour window on the composer provider's account (claude-native -> anthropic), resetting in an hour. */
function usageResult() {
  const now = Date.now();
  return {
    enabled: true,
    ttlSeconds: 300,
    reports: [
      {
        accountId: 1,
        key: "anthropic-1",
        provider: "anthropic",
        displayName: "Claude",
        credentialType: "oauth",
        stale: false,
        nextRefreshAt: null,
        refreshBlocked: null,
        inFlight: false,
        lastError: null,
        report: {
          v: 2,
          status: "available",
          planLabel: "max",
          windows: [
            {
              id: "five_hour",
              label: "5-hour",
              shortLabel: "5h",
              usedPct: 92,
              remainingPct: 8,
              summary: true,
              resetsAt: new Date(now + 3_600_000).toISOString(),
            },
          ],
          fetchedAt: new Date(now - 60_000).toISOString(),
        },
      },
    ],
  };
}

describe("App start usage fetch (ADR-0028 amendment 2026-11-27)", () => {
  beforeEach(() => {
    window.localStorage.clear();
    // jsdom's window is never focused; a real viewer's is, and the start fetch waits for an active window.
    vi.spyOn(document, "visibilityState", "get").mockImplementation(() => "visible");
    vi.spyOn(document, "hasFocus").mockImplementation(() => true);
  });

  afterEach(() => {
    vi.restoreAllMocks();
    window.location.hash = "";
    window.localStorage.clear();
  });

  it("quota_app_start_sends_one_touch_then_the_notice_shows", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await flush();
    respond(socket, "workspace.list", [WORKSPACE]);
    await flush();
    respond(socket, "space.list", []);
    respond(socket, "task.list", [TASK_A]);
    respond(socket, "run.list", []);
    await flush();
    fireEvent.click(screen.getAllByText(TASK_A.Title)[0]!);
    await flush();
    respond(socket, "chat.list", [CHAT]);
    await flush();

    // The app start sent the one touch; nothing has answered it yet, so no notice.
    expect(usageRequests(socket)).toHaveLength(1);
    expect(usageRequests(socket)[0]?.params).toEqual({ mode: "touch" });
    expect(screen.queryByTestId("ambient-quota-notice")).not.toBeInTheDocument();

    respond(socket, "account.usage", usageResult());
    await flush();
    await flush();

    expect(usageRequests(socket)).toHaveLength(1);
    expect(screen.queryByTestId("usage-modal")).not.toBeInTheDocument();
    expect(screen.getByTestId("ambient-quota-notice")).toBeInTheDocument();
  });
});
