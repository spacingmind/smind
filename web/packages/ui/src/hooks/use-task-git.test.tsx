import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { act, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { useTaskGit, useWorkspaceTaskGit } from "@/hooks/use-task-git";
import { queryKeys } from "@/lib/query-keys";
import { useTaskGitFocus, useTaskGitFocusStore } from "@/lib/task-git-focus";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp } from "@/test/render-with-daemon";
import type { TaskGit } from "@/lib/types";

const git = (taskId: number, files = 1): TaskGit => ({
  taskId,
  untrusted: false,
  computedAt: "2026-10-22T00:00:00Z",
  diff: { files, insertions: 0, deletions: 0 },
});

function Probe({ ws, task }: { client: FakeWsClient; ws: number[]; task: number }) {
  const map = useWorkspaceTaskGit(ws);
  const one = useTaskGit(task);
  return (
    <div>
      <span data-testid="map">{[...map.values()].map((g) => `${g.taskId}:${g.diff?.files}`).join(",")}</span>
      <span data-testid="one">{one ? `${one.taskId}:${one.diff?.files}` : "none"}</span>
    </div>
  );
}

function Focus({ taskId }: { client: FakeWsClient; taskId: number | null }) {
  useTaskGitFocus(taskId);
  return null;
}

async function answer(client: FakeWsClient, method: string, index: number, value: unknown) {
  await act(async () => client.nth(method, index).resolve(value));
}

afterEach(() => useTaskGitFocusStore.setState({ stack: [] }));

describe("task git hooks", () => {
  it("keys from query-keys", () => {
    expect(queryKeys.taskGit(7)).toEqual(["task.git", { taskId: 7 }]);
    expect(queryKeys.workspaceTaskGit(3)).toEqual(["task.gitList", { workspaceId: 3 }]);
  });

  it("patches the task entry and the workspace list on a task.git event", async () => {
    const client = new FakeWsClient();
    renderWithClientProp(<Probe client={client} ws={[1, 2]} task={10} />);
    await waitFor(() => client.nth("task.gitList", 1));
    await answer(client, "task.gitList", 0, { items: [git(10), git(11)] });
    await answer(client, "task.gitList", 1, { items: [git(20)] });
    expect(screen.getByTestId("map").textContent).toBe("10:1,11:1,20:1");
    expect(screen.getByTestId("one").textContent).toBe("10:1");

    act(() => client.pushNotification("task.git", { taskId: 10, git: git(10, 5) }));
    await waitFor(() => expect(screen.getByTestId("map").textContent).toBe("10:5,11:1,20:1"));
    expect(screen.getByTestId("one").textContent).toBe("10:5");
    // A task no list holds is not refetched or added.
    act(() => client.pushNotification("task.git", { taskId: 99, git: git(99) }));
    await act(async () => {});
    expect(screen.getByTestId("map").textContent).toBe("10:5,11:1,20:1");
    expect(client.calls.filter((c) => c.method === "task.gitList")).toHaveLength(2);
  });

  it("invalidates on reconnect", async () => {
    const first = new FakeWsClient();
    const view = renderWithClientProp(<Probe client={first} ws={[1]} task={10} />);
    await waitFor(() => first.nth("task.gitList", 0));
    await answer(first, "task.gitList", 0, { items: [git(10)] });

    const second = new FakeWsClient();
    view.rerender(<Probe client={second} ws={[1]} task={10} />);
    await waitFor(() => second.nth("events.subscribe", 0));
    await answer(second, "events.subscribe", 0, null);
    await waitFor(() => second.nth("task.gitList", 0));
    await answer(second, "task.gitList", 0, { items: [git(10, 9)] });
    expect(screen.getByTestId("map").textContent).toBe("10:9");
  });

  it("calls task.gitFocus for the task on screen and clears it on unmount", async () => {
    const client = new FakeWsClient();
    const view = renderWithClientProp(<Focus client={client} taskId={4} />);
    await waitFor(() => client.nth("task.gitFocus", 0));
    expect(client.nth("task.gitFocus", 0).params).toEqual({ taskId: 4 });
    view.unmount();
    expect(client.nth("task.gitFocus", 1).params).toEqual({ taskId: null });
  });

  it("keeps the last mounted focus and restores the previous one", async () => {
    const client = new FakeWsClient();
    function Two({ second }: { client: FakeWsClient; second: boolean }) {
      return (
        <>
          <Focus client={client} taskId={1} />
          {second ? <Focus client={client} taskId={2} /> : null}
        </>
      );
    }
    const view = renderWithClientProp(<Two client={client} second />);
    const sentIds = () => client.calls.filter((c) => c.method === "task.gitFocus").map((c) => (c.params as { taskId: number | null }).taskId);
    await waitFor(() => expect(sentIds().at(-1)).toBe(2));
    expect(useTaskGitFocusStore.getState().stack.map((e) => e.taskId)).toEqual([1, 2]);
    view.rerender(<Two client={client} second={false} />);
    await waitFor(() => expect(sentIds().at(-1)).toBe(1));
    expect(useTaskGitFocusStore.getState().stack.map((e) => e.taskId)).toEqual([1]);
  });

  it("never calls task.git outside the hook", () => {
    const src = join(__dirname, "..");
    const files = (dir: string): string[] =>
      readdirSync(dir).flatMap((name) => {
        const path = join(dir, name);
        if (statSync(path).isDirectory()) return files(path);
        return /\.(ts|tsx)$/.test(name) && !/\.test\.(ts|tsx)$/.test(name) ? [path] : [];
      });
    const offenders = files(src)
      .filter((p) => relative(src, p) !== join("hooks", "use-task-git.ts"))
      .filter((p) => /\.call(<[^>]*>)?\(\s*["']task\.git(List)?["']/.test(readFileSync(p, "utf8")))
      .map((p) => relative(src, p));
    expect(offenders).toEqual([]);
  });
});
