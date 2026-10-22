import { act } from "react";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { CreateTaskDialog } from "@/components/crud-dialogs";
import { draftStorageKey } from "@/components/composer/use-composer-draft";
import type { Task, Workspace } from "@/lib/types";
import { RpcError, type WsClient } from "@/lib/ws-client";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";

const WORKSPACE = { ID: 3, Path: "/repo", Title: "Repo" } as Workspace;
const BRANCHES = {
  defaultBase: "develop",
  current: "develop",
  branches: [
    { name: "feat/login", remote: "local", ahead: 3, behind: 1 },
    { name: "remote-only", remote: "origin" },
  ],
};
const NOW = new Date().toISOString();
const PR = { number: 12, title: "Add the thing", state: "open", author: "ana", url: "https://github.com/o/r/pull/12", updatedAt: NOW };
const FORK_PR = { ...PR, number: 13, title: "Fork change", fromFork: true };
const ISSUE = { number: 7, title: "Crash on start", state: "open", author: "bo", url: "https://github.com/o/r/issues/7", updatedAt: NOW };
const TASK = { ID: 99, WorkspaceID: 3, Title: "x" } as Task;

async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

async function setup() {
  const client = new FakeWsClient();
  const created: Task[] = [];
  render(
    <CreateTaskDialog
      client={client as unknown as WsClient}
      workspace={WORKSPACE}
      spaces={[]}
      fixedSpaceId={null}
      open
      onOpenChange={() => {}}
      onCreated={(t) => created.push(t)}
    />,
  );
  await flush();
  client.nth("workspace.branches", 0).resolve(BRANCHES);
  await flush();
  return { client, created };
}

const mode = (name: string) => fireEvent.click(screen.getByRole("radio", { name }));
const searches = (c: FakeWsClient) => c.calls.filter((x) => x.method === "forge.search");
const title = (v: string) => fireEvent.change(screen.getByTestId("dialog-new-task-title-input"), { target: { value: v } });
const submit = async () => {
  fireEvent.click(screen.getByTestId("dialog-new-task-submit"));
  await flush();
};

beforeEach(() => localStorage.clear());
afterEach(() => cleanup());

describe("New Task dialog: Start from", () => {
  it("it has four start modes", async () => {
    await setup();
    for (const name of ["Workspace HEAD", "Branch…", "Pull request…", "Issue…"]) expect(screen.getByRole("radio", { name })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "Workspace HEAD" })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByTestId("start-from-helper")).toHaveTextContent("Branch from develop");
  });

  it("it sends baseRef/branch/prNumber", async () => {
    // Workspace HEAD: today's call, no ref.
    let { client } = await setup();
    title("t1");
    await submit();
    expect(client.nth("task.create", 0).params).toEqual({ workspaceId: 3, title: "t1" });
    cleanup();

    // Branch…: a new branch off a local / origin branch, or the branch itself.
    ({ client } = await setup());
    title("t2");
    mode("Branch…");
    fireEvent.click(await screen.findByRole("option", { name: /feat\/login/ }));
    expect(screen.getByTestId("start-from-branch")).toHaveTextContent("+3");
    expect(screen.getByTestId("start-from-branch")).toHaveTextContent("−1");
    await submit();
    expect(client.nth("task.create", 0).params).toEqual({ workspaceId: 3, title: "t2", baseRef: "feat/login" });
    cleanup();

    ({ client } = await setup());
    title("t3");
    mode("Branch…");
    fireEvent.click(await screen.findByRole("option", { name: /remote-only/ }));
    await submit();
    expect(client.nth("task.create", 0).params).toMatchObject({ baseRef: "origin/remote-only" });
    cleanup();

    ({ client } = await setup());
    title("t4");
    mode("Branch…");
    fireEvent.click(await screen.findByRole("option", { name: /remote-only/ }));
    fireEvent.click(screen.getByRole("radio", { name: "Check it out" }));
    await submit();
    expect(client.nth("task.create", 0).params).toEqual({ workspaceId: 3, title: "t4", branch: "remote-only" });
    cleanup();

    // Pull request…
    ({ client } = await setup());
    title("t5");
    mode("Pull request…");
    await flush();
    expect(searches(client)[0]!.params).toEqual({ forge: "github", workspaceId: 3, kind: "pr", query: "", limit: 30 });
    searches(client)[0]!.resolve({ forge: "github", repo: "o/r", items: [PR] });
    await flush();
    fireEvent.click(screen.getByRole("option", { name: /Add the thing/ }));
    await submit();
    expect(client.nth("task.create", 0).params).toEqual({ workspaceId: 3, title: "t5", prNumber: 12 });
  });

  it("it shows the fork badge", async () => {
    const { client } = await setup();
    mode("Pull request…");
    await flush();
    searches(client)[0]!.resolve({ forge: "github", repo: "o/r", items: [PR, FORK_PR] });
    await flush();
    const badges = screen.getAllByTestId("fork-badge");
    expect(badges).toHaveLength(1);
    expect(badges[0]).toHaveTextContent("From fork — setup scripts blocked until you run them");
    fireEvent.click(screen.getByRole("option", { name: /Fork change/ }));
    expect(screen.getByTestId("start-from-pr")).toHaveTextContent("#13 Fork change");
    expect(screen.getByTestId("fork-badge")).toBeInTheDocument();
  });

  it("it handles forge_unauthenticated", async () => {
    const { client } = await setup();
    mode("Pull request…");
    await flush();
    searches(client)[0]!.reject(new RpcError("raw daemon text", "forge_unauthenticated"));
    await flush();
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("GitHub is unavailable: gh is not logged in on the daemon host.");
    expect(alert).not.toHaveTextContent("raw daemon text");
    expect(screen.getByTestId("dialog-new-task-submit")).toBeEnabled();
  });

  it("it takes the title from a picked issue", async () => {
    const { client, created } = await setup();
    mode("Issue…");
    await flush();
    expect(searches(client)[0]!.params).toMatchObject({ workspaceId: 3, kind: "issue" });
    searches(client)[0]!.resolve({ forge: "github", repo: "o/r", items: [ISSUE] });
    await flush();
    fireEvent.click(screen.getByRole("option", { name: /Crash on start/ }));
    expect(screen.getByTestId("dialog-new-task-title-input")).toHaveValue("Crash on start");
    title("Crash on start (edited)");
    await submit();
    // A plain task: no ref param.
    expect(client.nth("task.create", 0).params).toEqual({ workspaceId: 3, title: "Crash on start (edited)" });
    client.nth("task.create", 0).resolve(TASK);
    await waitFor(() => expect(created).toHaveLength(1));
    expect(localStorage.getItem(draftStorageKey(99))).toBe("Resolve #7: Crash on start\nhttps://github.com/o/r/issues/7");
  });

  it("it needs a pick before creating from a branch, PR or issue", async () => {
    const { client } = await setup();
    title("t");
    mode("Pull request…");
    await submit();
    expect(screen.getByText("Choose a pull request.")).toBeInTheDocument();
    expect(client.calls.some((c) => c.method === "task.create")).toBe(false);
  });
});
