import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { HeaderTaskMenu } from "@/components/app-header-task";
import { flush, gitState } from "@/components/git/pr-test-support";
import { SyncButtons } from "@/components/git/sync-actions";
import { TooltipProvider } from "@/components/ui/tooltip";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import type { Task, TaskGit } from "@/lib/types";

const TASK: Task = {
  ID: 1,
  WorkspaceID: 7,
  SpaceID: null,
  Title: "Fix the bug",
  Status: "active",
  WorktreePath: "/home/dev/repo-wt",
  Branch: "smind/task-x",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

function HeaderHarness({ client }: { client: FakeWsClient }) {
  return (
    <TooltipProvider>
      <HeaderTaskMenu task={TASK} client={client as never} localPathActions={null} />
    </TooltipProvider>
  );
}

function ButtonsHarness({}: { client: FakeWsClient }) {
  return <SyncButtons taskId={1} />;
}

async function openMenu(client: FakeWsClient, git: TaskGit): Promise<HTMLElement> {
  // The items mount with the menu content; in the app the title's own `useTaskGit` has already filled the shared cache.
  fireEvent.pointerDown(screen.getByTestId("app-header-task-menu"), { button: 0, ctrlKey: false });
  await waitFor(() => expect(screen.getByTestId("app-header-task-menu-content")).toBeInTheDocument());
  client.nth("task.git", 0).resolve(git);
  await flush();
  return screen.getByTestId("app-header-task-menu-content");
}

const state = (over: Partial<TaskGit> = {}) =>
  gitState({
    origin: { upstream: "origin/smind/task-x", published: true, ahead: 2, behind: 0 },
    actions: { push: { enabled: true }, updateFromBase: { enabled: true } },
    ...over,
  });

describe("sync items in the header menu", () => {
  it("offers Push and Update from <base> when enabled and opens the dialog", async () => {
    const client = new FakeWsClient();
    render(<HeaderHarness client={client} />);
    await openMenu(client, state());
    expect(screen.getByTestId("sync-menu-push")).toHaveTextContent("Push…");
    expect(screen.getByTestId("sync-menu-update")).toHaveTextContent("Update from develop…");
    expect(screen.getByTestId("sync-menu-push")).not.toHaveAttribute("aria-disabled");
    fireEvent.click(screen.getByTestId("sync-menu-update"));
    expect(await screen.findByTestId("sync-dialog")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Update from develop" })).toBeInTheDocument();
  });

  it("says Publish branch while there is no upstream", async () => {
    const client = new FakeWsClient();
    render(<HeaderHarness client={client} />);
    await openMenu(client, state({ origin: { published: false } }));
    expect(screen.getByTestId("sync-menu-push")).toHaveTextContent("Publish branch…");
  });

  it("shows the daemon's reason for a disabled item and does nothing when chosen", async () => {
    const client = new FakeWsClient();
    render(<HeaderHarness client={client} />);
    await openMenu(
      client,
      state({
        actions: {
          push: { enabled: false, code: "nothing_to_push", reason: "Nothing to push." },
          updateFromBase: { enabled: false, code: "worktree_dirty", reason: "Commit or stash your changes, then update." },
        },
      }),
    );
    const push = screen.getByTestId("sync-menu-push");
    expect(push).toHaveAttribute("aria-disabled", "true");
    expect(push).toHaveAttribute("title", "Nothing to push.");
    expect(screen.getByTestId("sync-menu-push-reason")).toHaveTextContent("Nothing to push.");
    expect(push.getAttribute("aria-describedby")).toBe(screen.getByTestId("sync-menu-push-reason").id);
    expect(screen.getByTestId("sync-menu-update-reason")).toHaveTextContent("Commit or stash your changes");
    fireEvent.click(push);
    await act(async () => {});
    expect(screen.queryByTestId("sync-dialog")).not.toBeInTheDocument();
    expect(screen.getByTestId("app-header-task-menu-content")).toBeInTheDocument();
  });

  it("leaves the items out when the daemon reports neither action", async () => {
    const client = new FakeWsClient();
    render(<HeaderHarness client={client} />);
    await openMenu(client, state({ actions: { createPr: { enabled: true } } }));
    expect(screen.queryByTestId("sync-menu-push")).not.toBeInTheDocument();
    expect(screen.queryByTestId("sync-menu-update")).not.toBeInTheDocument();
  });
});

describe("sync buttons in the Diff header", () => {
  it("gates each button by its action and keeps the reason as the tooltip", async () => {
    const client = new FakeWsClient();
    render(<ButtonsHarness client={client} />);
    client.nth("task.git", 0).resolve(
      state({
        actions: {
          push: { enabled: true },
          updateFromBase: { enabled: false, code: "up_to_date", reason: "Already up to date with develop." },
        },
      }),
    );
    await flush();
    expect(screen.getByTestId("sync-button-push")).toBeEnabled();
    const update = screen.getByTestId("sync-button-update");
    expect(update).toBeDisabled();
    expect(update).toHaveTextContent("Update from develop");
    expect(update.parentElement).toHaveAttribute("title", "Already up to date with develop.");
    fireEvent.click(screen.getByTestId("sync-button-push"));
    expect(await screen.findByRole("heading", { name: "Push" })).toBeInTheDocument();
  });

  it("renders nothing before the git state arrives", () => {
    const client = new FakeWsClient();
    render(<ButtonsHarness client={client} />);
    expect(screen.queryByTestId("sync-buttons")).not.toBeInTheDocument();
  });
});
