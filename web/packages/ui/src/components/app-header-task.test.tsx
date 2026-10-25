// AH-AC3: the header title's hover/focus card. PR-H11: the header ⋯ menu's
// tree actions.
import { act, type ReactNode } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { HeaderTaskMenu, HeaderTaskTitle } from "@/components/app-header-task";
import { TooltipProvider } from "@/components/ui/tooltip";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { TaskTreeResult } from "@/lib/task-lineage";
import type { Task, TaskGit, TaskGitPr } from "@/lib/types";

const TASK: Task = {
  ID: 7,
  WorkspaceID: 1,
  SpaceID: null,
  Title: "Fix the bug",
  Status: "active",
  WorktreePath: "/home/dev/repo-wt",
  Branch: "fix-bug",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

describe("HeaderTaskTitle", () => {
  it("AH-AC3 hover card shows branch, status, last activity", async () => {
    render(<HeaderTaskTitle task={TASK} />);
    expect(screen.queryByTestId("app-header-task-card")).not.toBeInTheDocument();

    // Radix HoverCard opens on keyboard focus without any timer.
    await act(async () => {
      screen.getByTestId("app-header-title").focus();
    });
    const card = await screen.findByTestId("app-header-task-card");
    expect(card).toHaveTextContent("Last activity");
    expect(screen.getByTestId("app-header-task-branch")).toHaveTextContent("fix-bug");
    expect(screen.getByTestId("app-header-task-worktree")).toHaveTextContent("/home/dev/repo-wt");
    expect(screen.getByTestId("app-header-task-status")).toHaveTextContent(/active/i);
    expect(screen.getByTestId("app-header-task-activity").textContent).not.toBe("");
    fireEvent.blur(screen.getByTestId("app-header-title"));
  });
});

const PR: TaskGitPr = {
  number: 42,
  repo: "o/r",
  url: "https://github.com/o/r/pull/42",
  title: "Fix the bug",
  state: "open",
  draft: false,
  headRef: "fix-bug",
  headSha: "abc",
  baseRef: "develop",
  fromFork: false,
  mergeable: "mergeable",
  reviewDecision: "none",
  fetchedAt: "2026-10-22T00:00:00Z",
  stale: "",
  refreshAvailableAt: "",
};

function taskGit(over: Partial<TaskGit> = {}): TaskGit {
  return { taskId: TASK.ID, untrusted: false, computedAt: "2026-10-22T00:00:00Z", ...over };
}

/** Gives renderWithClientProp a client to find (HeaderTaskTitle takes none as a prop). */
function WithClient({ children }: { client: FakeWsClient; children: ReactNode }) {
  return <>{children}</>;
}

describe("HeaderTaskTitle git state (ADR-0039)", () => {
  it("it renders the PR chip when the task has a PR and nothing otherwise", async () => {
    const client = new FakeWsClient();
    render(
      <WithClient client={client}>
        <HeaderTaskTitle task={TASK} />
      </WithClient>,
    );
    await act(async () => client.nth("task.git", 0).resolve(taskGit()));
    expect(screen.queryByTestId("pr-chip")).not.toBeInTheDocument();

    act(() => client.pushNotification("task.git", { taskId: TASK.ID, git: taskGit({ pr: PR }) }));
    await waitFor(() => expect(screen.getByTestId("pr-chip")).toHaveTextContent("#42"));
  });

  it("the hover card shows `branch → base` and the git tokens", async () => {
    const client = new FakeWsClient();
    render(
      <WithClient client={client}>
        <HeaderTaskTitle task={TASK} />
      </WithClient>,
    );
    await act(async () =>
      client.nth("task.git", 0).resolve(
        taskGit({
          branch: { name: "fix-bug", owned: true },
          base: { ref: "develop", known: true },
          diff: { files: 1, insertions: 4, deletions: 2 },
        }),
      ),
    );
    await act(async () => {
      screen.getByTestId("app-header-title").focus();
    });
    await screen.findByTestId("app-header-task-card");
    expect(screen.getByTestId("app-header-task-branch")).toHaveTextContent("fix-bug → develop");
    expect(screen.getByTestId("git-tokens")).toHaveTextContent("+4");
    fireEvent.blur(screen.getByTestId("app-header-title"));
  });
});

describe("HeaderTaskTitle hover card, stacked", () => {
  it("hover card shows the parent branch for a stacked task", async () => {
    const client = new FakeWsClient();
    render(
      <WithClient client={client}>
        <HeaderTaskTitle task={TASK} />
      </WithClient>,
    );
    await act(async () =>
      client.nth("task.git", 0).resolve(taskGit({ branch: { name: "fix-bug", owned: true }, base: { ref: "smind/parent", known: true, parentBranch: "smind/parent" } })),
    );
    await act(async () => {
      screen.getByTestId("app-header-title").focus();
    });
    await screen.findByTestId("app-header-task-card");
    expect(screen.getByTestId("app-header-task-branch")).toHaveTextContent("fix-bug → smind/parent");
    expect(screen.getByTestId("app-header-task-branch")).toHaveAttribute("title", expect.stringContaining("stacked on smind/parent"));
    fireEvent.blur(screen.getByTestId("app-header-title"));
  });
});

function wsFake(): FakeWsClient {
  return new FakeWsClient();
}

function treeNode(id: number, parent: number | null, state: string): never {
  return {
    id,
    parentId: parent,
    workspaceId: 1,
    title: `Task ${id}`,
    depth: 0,
    taskStatus: "created",
    tier: null,
    childCount: 0,
    run: { state, runningRunIds: [], pendingPermissions: 0 },
    counts: { running: 0, waitingPermission: 0, failed: 0, done: 0, idle: 0 },
    usage: {
      own: { runs: 0, inputTokens: null, outputTokens: null, cacheReadTokens: null, cacheWriteTokens: null, reasoningTokens: null, costUsd: null },
      subtree: { runs: 0, inputTokens: null, outputTokens: null, cacheReadTokens: null, cacheWriteTokens: null, reasoningTokens: null, costUsd: null },
      archivedDescendants: 0,
    },
  } as never;
}

function tree(states: { self: string; descendants: string[] }): TaskTreeResult {
  const nodes = [treeNode(7, null, states.self)];
  let parent = 7;
  let id = 8;
  for (const state of states.descendants) {
    nodes.push(treeNode(id, parent, state));
    parent = id;
    id++;
  }
  return { nodes, truncated: false };
}

function flush(): Promise<void> {
  return act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

async function openMenu(): Promise<HTMLElement> {
  fireEvent.pointerDown(screen.getByTestId("app-header-task-menu"), { button: 0, ctrlKey: false });
  await waitFor(() => expect(screen.getByTestId("app-header-task-menu-content")).toBeInTheDocument());
  return screen.getByTestId("app-header-task-menu-content");
}

describe("HeaderTaskMenu tree actions (PR-H11)", () => {
  it("header menu entries behave like the sidebar's", async () => {
    const client = wsFake();
    render(
        <TooltipProvider>
          <HeaderTaskMenu task={TASK} client={client as never} localPathActions={null} />
        </TooltipProvider>,
      );
    const call = await waitFor(() => client.nth("task.tree", 0));
    await act(async () => {
      call.resolve(tree({ self: "idle", descendants: ["done", "running"] }));
    });
    await flush();

    const menu = await openMenu();
    // Enabled like the sidebar's would be: something runs, something finished.
    expect(menu.querySelector('[data-testid="app-header-stop-tree"]')?.getAttribute("aria-disabled")).not.toBe("true");
    expect(menu.querySelector('[data-testid="app-header-archive-finished"]')?.getAttribute("aria-disabled")).not.toBe("true");

    fireEvent.click(screen.getByTestId("app-header-stop-tree"));
    const stopDry = await waitFor(() => client.nth("task.stopTree", 0));
    await act(async () => {
      stopDry.resolve({ dryRun: true, runs: [{ id: 2, title: "Child", runId: "r1" }], queuedPrompts: 0, stopped: [], failed: [], queuedCancelled: 0 });
    });
    expect(await screen.findByRole("heading", { name: /Stop \d+ running task/ })).toBeInTheDocument();
  });

  it("header menu refetches task.tree when opened so enablement is current", async () => {
    const client = wsFake();
    render(
      <TooltipProvider>
        <HeaderTaskMenu task={TASK} client={client as never} localPathActions={null} />
      </TooltipProvider>,
    );
    const first = await waitFor(() => client.nth("task.tree", 0));
    await act(async () => {
      first.resolve(tree({ self: "idle", descendants: ["running"] }));
    });
    await flush();

    // Children finished since the header loaded; opening the menu refetches.
    await openMenu();
    const second = await waitFor(() => client.nth("task.tree", 1));
    await act(async () => {
      second.resolve(tree({ self: "idle", descendants: ["done"] }));
    });
    await flush();
    expect(
      document.querySelector('[data-testid="app-header-archive-finished"]')?.getAttribute("aria-disabled"),
    ).not.toBe("true");
  });

  it("header offers neither entry when the task has no descendants", async () => {
    const client = wsFake();
    render(
        <TooltipProvider>
          <HeaderTaskMenu task={TASK} client={client as never} localPathActions={null} />
        </TooltipProvider>,
      );
    const call = await waitFor(() => client.nth("task.tree", 0));
    await act(async () => {
      call.resolve(tree({ self: "running", descendants: [] }));
    });
    await flush();

    const menu = await openMenu();
    expect(menu.querySelector('[data-testid="app-header-stop-tree"]')).toBeNull();
    expect(menu.querySelector('[data-testid="app-header-archive-finished"]')).toBeNull();
    expect(menu.querySelector('[data-testid="app-header-archive"]')).not.toBeNull();
  });

  it("header disables entries with their reason like the sidebar", async () => {
    const client = wsFake();
    render(
        <TooltipProvider>
          <HeaderTaskMenu task={TASK} client={client as never} localPathActions={null} />
        </TooltipProvider>,
      );
    const call = await waitFor(() => client.nth("task.tree", 0));
    await act(async () => {
      call.resolve(tree({ self: "idle", descendants: ["idle", "idle"] }));
    });
    await flush();

    const menu = await openMenu();
    const stop = menu.querySelector('[data-testid="app-header-stop-tree"]');
    expect(stop?.getAttribute("aria-disabled")).toBe("true");
    expect(stop?.textContent).toContain("No running tasks in this tree");
    const archive = menu.querySelector('[data-testid="app-header-archive-finished"]');
    expect(archive?.getAttribute("aria-disabled")).toBe("true");
    expect(archive?.textContent).toContain("No finished sub-tasks");
  });

  it("disabled entries stay focusable, describe their reason, and do nothing when chosen", async () => {
    const client = wsFake();
    render(
      <TooltipProvider>
        <HeaderTaskMenu task={TASK} client={client as never} localPathActions={null} />
      </TooltipProvider>,
    );
    const call = await waitFor(() => client.nth("task.tree", 0));
    await act(async () => {
      call.resolve(tree({ self: "idle", descendants: ["idle"] }));
    });
    await flush();

    const menu = await openMenu();
    const stop = menu.querySelector('[data-testid="app-header-stop-tree"]') as HTMLElement;
    // Radix's own `disabled` would set data-disabled and drop it from roving focus.
    expect(stop.hasAttribute("data-disabled")).toBe(false);
    expect(stop.getAttribute("tabindex")).not.toBe(null);
    const reason = document.getElementById(stop.getAttribute("aria-describedby") ?? "");
    expect(reason?.textContent).toContain("No running tasks in this tree");
    act(() => stop.focus());
    expect(document.activeElement).toBe(stop);

    fireEvent.click(stop);
    expect(screen.queryByRole("heading", { name: /Stop \d+ running task/ })).toBeNull();
    // The menu stays open: choosing a disabled entry is a no-op.
    expect(document.querySelector('[data-testid="app-header-task-menu-content"]')).not.toBeNull();
  });
});
