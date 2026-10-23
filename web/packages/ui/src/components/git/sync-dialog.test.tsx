import { fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { flush, gitState } from "@/components/git/pr-test-support";
import { SyncDialog } from "@/components/git/sync-dialog";
import type { SyncMode } from "@/components/git/sync-model";
import { useFocusedChatStore } from "@/stores/focused-chat-store";
import type { TaskGit } from "@/lib/types";
import { RpcError } from "@/lib/ws-client";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";

function Harness({ mode }: { client: FakeWsClient; mode: SyncMode }) {
  return <SyncDialog taskId={1} mode={mode} open onOpenChange={() => {}} />;
}

async function mount(mode: SyncMode, git: TaskGit, client = new FakeWsClient()) {
  render(<Harness client={client} mode={mode} />);
  client.nth("task.git", 0).resolve(git);
  await flush();
  return client;
}

const published = (over: Partial<TaskGit> = {}) =>
  gitState({
    origin: { upstream: "origin/smind/task-x", published: true, ahead: 3, behind: 0 },
    commits: { aheadOfBase: 3, behindBase: 5, sinceFork: 3 },
    actions: { push: { enabled: true }, updateFromBase: { enabled: true } },
    ...over,
  });

afterEach(() => useFocusedChatStore.setState({ byTask: {}, revealRequest: null }));

describe("SyncDialog push", () => {
  it("renders idle push card", async () => {
    await mount("push", published());
    expect(screen.getByRole("heading", { name: "Push" })).toBeInTheDocument();
    expect(screen.getByTestId("sync-row-branch")).toHaveTextContent("smind/task-x");
    expect(screen.getByTestId("sync-row-remote")).toHaveTextContent("origin/smind/task-x");
    expect(screen.getByTestId("sync-row-ahead")).toHaveTextContent("3 commits");
    expect(screen.getByTestId("sync-row-behind")).toHaveTextContent("0 commits");
    expect(screen.getByTestId("sync-row-next")).toHaveTextContent("Push 3 commits to origin/smind/task-x.");
    expect(screen.getByTestId("sync-submit")).toHaveTextContent("Push");
    expect(screen.getByTestId("sync-submit")).toBeEnabled();
    expect(screen.getByTestId("sync-cancel")).toBeEnabled();
  });

  it("shows the push target the daemon reports", async () => {
    const withTarget = (pushTarget?: { remote: string; branch: string }) =>
      published({ origin: { upstream: "origin/smind/task-x", published: true, ahead: 3, behind: 0, pushTarget } });
    await mount("push", withTarget({ remote: "origin", branch: "smind/task-x" }));
    expect(screen.getByTestId("sync-row-remote")).toHaveTextContent("origin/smind/task-x");
    expect(screen.getByTestId("sync-row-next")).toHaveTextContent("Push 3 commits to origin/smind/task-x.");
    expect(screen.queryByTestId("sync-remote-note")).not.toBeInTheDocument();
  });

  it.each([
    [{ remote: "mirror", branch: "smind/task-x" }, "mirror/smind/task-x", "Pushes to the `mirror` remote."],
    [{ remote: "origin", branch: "feat/x" }, "origin/feat/x", "Updates the pull request's branch."],
  ])("push target %j", async (pushTarget, remote, note) => {
    await mount("push", published({ origin: { upstream: "origin/smind/task-x", published: true, ahead: 3, behind: 0, pushTarget } }));
    expect(screen.getByTestId("sync-row-remote")).toHaveTextContent(remote);
    expect(screen.getByTestId("sync-row-next")).toHaveTextContent(`Push 3 commits to ${remote}.`);
    expect(screen.getByTestId("sync-remote-note")).toHaveTextContent(note);
  });

  it("falls back to origin/<branch> without a push target", async () => {
    await mount("push", published());
    expect(screen.getByTestId("sync-row-remote")).toHaveTextContent("origin/smind/task-x");
    expect(screen.getByTestId("sync-row-next")).toHaveTextContent("Push 3 commits to origin/smind/task-x.");
    expect(screen.queryByTestId("sync-remote-note")).not.toBeInTheDocument();
  });

  it("publish label without upstream", async () => {
    await mount("push", gitState({ origin: { published: false }, commits: { sinceFork: 2 }, actions: { push: { enabled: true } } }));
    expect(screen.getByRole("heading", { name: "Publish branch" })).toBeInTheDocument();
    expect(screen.getByTestId("sync-submit")).toHaveTextContent("Publish branch");
    expect(screen.getByTestId("sync-row-ahead")).toHaveTextContent("2 commits");
    expect(screen.getByTestId("sync-row-next")).toHaveTextContent("Publish smind/task-x to origin");
  });

  it("in flight", async () => {
    const client = await mount("push", published());
    fireEvent.click(screen.getByTestId("sync-submit"));
    await flush();
    expect(client.nth("task.push", 0).params).toEqual({ taskId: 1 });
    expect(screen.getByTestId("sync-progress")).toHaveTextContent("Pushing…");
    expect(screen.getByTestId("sync-submit")).toBeDisabled();
    expect(screen.getByTestId("sync-submit")).toHaveTextContent("Pushing…");
    expect(screen.getByTestId("sync-cancel")).toBeDisabled();
  });

  it("success line", async () => {
    const client = await mount("push", published());
    fireEvent.click(screen.getByTestId("sync-submit"));
    await flush();
    client.nth("task.push", 0).resolve({ branch: "smind/task-x", remote: "origin", upstreamCreated: false, pushed: 3 });
    await flush();
    expect(screen.getByTestId("sync-result")).toHaveTextContent("Pushed 3 commits to origin/smind/task-x");
    expect(screen.queryByTestId("sync-submit")).not.toBeInTheDocument();
    expect(screen.getByTestId("sync-close")).toBeInTheDocument();
  });

  it("rejected inline with details", async () => {
    const client = await mount("push", published());
    fireEvent.click(screen.getByTestId("sync-submit"));
    await flush();
    client
      .nth("task.push", 0)
      .reject(new RpcError("Origin has newer changes on this branch. Bring those changes in first, then push again.", "non_fast_forward", { detail: "! [rejected] (fetch first)" }));
    await flush();
    expect(screen.getByTestId("sync-error")).toHaveTextContent("Origin has newer changes on this branch.");
    expect(screen.getByTestId("sync-error-details")).toHaveTextContent("! [rejected] (fetch first)");
    // Retry stays possible and nothing was force-pushed: the only call carried just the task id.
    expect(screen.getByTestId("sync-submit")).toBeEnabled();
    expect(client.calls.filter((c) => c.method === "task.push")).toHaveLength(1);
  });
});

describe("SyncDialog update", () => {
  it("rebase option not default", async () => {
    const client = await mount("update", published());
    expect(screen.getByRole("heading", { name: "Update from develop" })).toBeInTheDocument();
    const box = screen.getByTestId("sync-rebase");
    expect(box).not.toBeChecked();
    expect(screen.getByTestId("sync-row-next")).toHaveTextContent("Merge 5 commits from origin/develop into this branch.");
    expect(screen.queryByTestId("sync-rebase-warning")).not.toBeInTheDocument();
    // Default: no strategy on the wire (the daemon merges).
    fireEvent.click(screen.getByTestId("sync-submit"));
    await flush();
    expect(client.nth("task.updateFromBase", 0).params).toEqual({ taskId: 1 });
  });

  it("warns about force-push when rebasing a published branch and sends the strategy", async () => {
    const client = await mount("update", published());
    fireEvent.click(screen.getByTestId("sync-rebase"));
    expect(screen.getByTestId("sync-rebase-warning")).toHaveTextContent("can't force-push");
    expect(screen.getByTestId("sync-submit")).toHaveTextContent("Rebase branch");
    fireEvent.click(screen.getByTestId("sync-submit"));
    await flush();
    expect(client.nth("task.updateFromBase", 0).params).toEqual({ taskId: 1, strategy: "rebase" });
    client.nth("task.updateFromBase", 0).resolve({ result: "rebased", commits: 5 });
    await flush();
    expect(screen.getByTestId("sync-result")).toHaveTextContent("Rebased onto origin/develop (5 commits)");
  });

  it("reports merged and up to date results", async () => {
    const client = await mount("update", published());
    fireEvent.click(screen.getByTestId("sync-submit"));
    await flush();
    client.nth("task.updateFromBase", 0).resolve({ result: "merged", commits: 5 });
    await flush();
    expect(screen.getByTestId("sync-result")).toHaveTextContent("Merged 5 commits from origin/develop");
  });

  it("conflict state lists files and asks the agent", async () => {
    const client = await mount("update", published());
    fireEvent.click(screen.getByTestId("sync-submit"));
    await flush();
    const files = Array.from({ length: 12 }, (_, i) => `pkg/file-${String(i).padStart(2, "0")}.go`);
    client.nth("task.updateFromBase", 0).reject(
      new RpcError("Merging `origin/develop` into this branch hit conflicts, and the worktree is now in the middle of the merge.", "conflict", {
        files,
        operation: "merge",
        base: "origin/develop",
        filesTruncated: false,
      }),
    );
    await flush();
    expect(screen.getByTestId("sync-conflict-message")).toHaveTextContent("hit conflicts");
    expect(screen.getByTestId("sync-conflict-files").querySelectorAll("li")).toHaveLength(11);
    expect(screen.getByTestId("sync-conflict-more")).toHaveTextContent("and 2 more");
    expect(screen.queryByTestId("sync-rebase")).not.toBeInTheDocument();

    useFocusedChatStore.getState().setFocusedChat(1, 22);
    fireEvent.click(screen.getByTestId("sync-ask-agent"));
    await flush();
    client.nth("chat.list", 0).resolve([
      { ID: 21, TaskID: 1, Title: "First", Provider: "claude-native", ArchivedAt: null },
      { ID: 22, TaskID: 1, Title: "Second", Provider: "claude-native", ArchivedAt: null },
    ]);
    await flush();
    const start = client.nth("run.start", 0).params as Record<string, unknown>;
    expect(start).toMatchObject({ taskId: 1, chatId: 22, provider: "claude-native", whenBusy: "queue" });
    expect(start.prompt).toContain("- pkg/file-00.go");
    expect(start.prompt).toContain("keeping the intent of both sides");
    expect(start.prompt).toContain("run the project's tests");
    expect(start.prompt).toContain("do not abort");
    client.nth("run.start", 0).resolve({ queued: true, queueItemId: 4 });
    await flush();
    expect(useFocusedChatStore.getState().revealRequest).toMatchObject({ taskId: 1, chatId: 22 });
  });

  it("says so when there is no chat to ask", async () => {
    const client = await mount("update", published());
    fireEvent.click(screen.getByTestId("sync-submit"));
    await flush();
    client.nth("task.updateFromBase", 0).reject(new RpcError("Merging hit conflicts.", "conflict", { files: ["a.go"], operation: "merge", base: "origin/develop", filesTruncated: false }));
    await flush();
    fireEvent.click(screen.getByTestId("sync-ask-agent"));
    await flush();
    client.nth("chat.list", 0).resolve([]);
    await flush();
    expect(screen.getByTestId("sync-ask-error")).toHaveTextContent("no open chat");
    expect(screen.getByTestId("sync-conflict-files")).toBeInTheDocument();
  });

  it("shows a typed refusal inline", async () => {
    const client = await mount("update", published());
    fireEvent.click(screen.getByTestId("sync-submit"));
    await flush();
    client.nth("task.updateFromBase", 0).reject(new RpcError("The worktree has uncommitted changes. Commit or stash them, then try again.", "worktree_dirty"));
    await flush();
    expect(screen.getByTestId("sync-error")).toHaveTextContent("uncommitted changes");
    expect(screen.queryByTestId("sync-conflict")).not.toBeInTheDocument();
  });
});
