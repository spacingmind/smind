import { act } from "react";
import { fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { TaskDetailPane } from "@/components/task-detail";
import { SetupBadge } from "@/components/task-setup";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { Chat, Task } from "@/lib/types";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";

afterEach(() => window.localStorage.clear());

const TASK: Task = {
  ID: 1,
  WorkspaceID: 3,
  SpaceID: null,
  Title: "Fork PR",
  Status: "active",
  WorktreePath: "/tmp/a",
  Branch: "task-a",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};
const CHAT: Chat = { ID: 10, TaskID: 1, Title: "Chat", Provider: null, AgentSession: null, CreatedAt: "2024-01-01T00:00:00Z", ArchivedAt: null };

async function flush() {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("untrusted task banner (ADR-0039 §7)", () => {
  it("it shows the banner and Run setup for an untrusted task", () => {
    // No setup script: SetupStatus is empty, the flag alone drives the banner.
    render(<TaskDetailPane client={new FakeWsClient()} task={{ ...TASK, UntrustedSource: true, SetupStatus: "" }} chat={CHAT} isDefaultChat />);
    expect(screen.getByTestId("task-untrusted-banner")).toHaveTextContent("From a fork — setup scripts are blocked until you run them.");
    expect(screen.getByTestId("task-untrusted-banner")).toHaveTextContent("Review the code first.");
    expect(screen.getByRole("button", { name: "Run setup" })).toBeEnabled();
  });

  it("it clears after Run setup", async () => {
    const client = new FakeWsClient();
    const listeners: Array<(payload: unknown) => void> = [];
    const events: DaemonEvents = {
      subscribe(topic, listener) {
        if (topic === "task.updated") listeners.push(listener);
        return () => {};
      },
    };
    render(<TaskDetailPane client={client} task={{ ...TASK, UntrustedSource: true, SetupStatus: "blocked" }} chat={CHAT} isDefaultChat events={events} />);
    expect(screen.getByTestId("task-setup-badge")).toHaveTextContent("Setup blocked");

    fireEvent.click(screen.getByRole("button", { name: "Run setup" }));
    await flush();
    expect(client.nth("task.retrySetup", 0).params).toEqual({ id: 1 });
    expect(screen.getByRole("button", { name: "Run setup" })).toBeDisabled();
    client.nth("task.retrySetup", 0).resolve({ ...TASK, UntrustedSource: false, SetupStatus: "running" });
    await flush();

    expect(screen.queryByTestId("task-untrusted-banner")).toBeNull();
    expect(screen.getByTestId("task-setup-badge")).toHaveAttribute("data-setup-status", "running");
    await act(async () => listeners.forEach((fn) => fn({ task: { ...TASK, UntrustedSource: false, SetupStatus: "ok" } })));
    expect(screen.queryByTestId("task-untrusted-banner")).toBeNull();
  });

  it("it is absent for a trusted task", () => {
    render(<TaskDetailPane client={new FakeWsClient()} task={{ ...TASK, UntrustedSource: false }} chat={CHAT} isDefaultChat />);
    expect(screen.queryByTestId("task-untrusted-banner")).toBeNull();
  });
});

describe("setup badge: blocked", () => {
  it("renders a neutral warning pill, labelled and compact", () => {
    const { unmount } = render(<SetupBadge status="blocked" />);
    expect(screen.getByTestId("task-setup-badge")).toHaveTextContent("Setup blocked");
    expect(screen.getByTestId("task-setup-badge")).toHaveAttribute("data-setup-status", "blocked");
    unmount();
    render(<SetupBadge status="blocked" compact />);
    expect(screen.getByRole("img", { name: "Setup blocked" })).toBeInTheDocument();
  });
});
