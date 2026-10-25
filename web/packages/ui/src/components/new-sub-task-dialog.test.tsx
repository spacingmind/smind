import { act } from "react";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { CreateTaskDialog } from "@/components/crud-dialogs";
import type { Task, Workspace } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";

const WORKSPACE = { ID: 3, Path: "/repo", Title: "Repo" } as Workspace;
const BRANCHES = { defaultBase: "develop", current: "develop", branches: [{ name: "develop", remote: "local" }] };
const PARENT = { ID: 5, WorkspaceID: 3, Title: "Parent task", Branch: "smind/parent" } as Task;

async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

async function setup(parent: Task) {
  const client = new FakeWsClient();
  render(
    <CreateTaskDialog client={client as unknown as WsClient} workspace={WORKSPACE} spaces={[]} fixedSpaceId={null} parent={parent} open onOpenChange={() => {}} onCreated={() => {}} />,
  );
  await flush();
  client.nth("workspace.branches", 0).resolve(BRANCHES);
  await flush();
  fireEvent.change(screen.getByTestId("dialog-new-task-title-input"), { target: { value: "child" } });
  return client;
}

const submit = async () => {
  fireEvent.click(screen.getByTestId("dialog-new-task-submit"));
  await flush();
};

beforeEach(() => localStorage.clear());
afterEach(() => cleanup());

describe("New sub-task dialog", () => {
  it("defaults to the parent branch and sends it as baseRef", async () => {
    const client = await setup(PARENT);
    expect(screen.getByRole("radio", { name: "Parent branch (smind/parent)" })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByTestId("stack-base-helper")).toHaveTextContent("Starts at the parent's last commit");
    await submit();
    expect(client.nth("task.create", 0).params).toEqual({ workspaceId: 3, title: "child", parentTaskId: 5, baseRef: "smind/parent" });
  });

  it("switching to the default base sends the default base", async () => {
    const client = await setup(PARENT);
    fireEvent.click(screen.getByRole("radio", { name: "Default base (develop)" }));
    expect(screen.getByTestId("stack-base-helper")).toHaveTextContent("Starts at develop, not stacked.");
    await submit();
    expect(client.nth("task.create", 0).params).toEqual({ workspaceId: 3, title: "child", parentTaskId: 5, baseRef: "develop" });
  });

  it("parent without a branch shows the fallback helper and sends no baseRef", async () => {
    const client = await setup({ ...PARENT, Branch: null });
    expect(screen.queryByRole("radio", { name: /Parent branch/ })).toBeNull();
    expect(screen.getByTestId("stack-base-helper")).toHaveTextContent("Parent has no branch yet; branching from develop.");
    await submit();
    expect(client.nth("task.create", 0).params).toEqual({ workspaceId: 3, title: "child", parentTaskId: 5 });
  });
});

describe("base switch frame agreement", () => {
  it("updates the selected pill and the helper text in the same flush, with no colour fade between them", async () => {
    await setup(PARENT);
    const parent = screen.getByRole("radio", { name: /Parent branch/ });
    const def = screen.getByRole("radio", { name: /Default base/ });
    expect(parent).toHaveAttribute("aria-checked", "true");
    fireEvent.click(def);
    // One synchronous check straight after the click: pill, text and the absence of a fade agree.
    expect(def).toHaveAttribute("aria-checked", "true");
    expect(parent).toHaveAttribute("aria-checked", "false");
    expect(screen.getByTestId("stack-base-helper")).toHaveTextContent("Starts at develop, not stacked.");
    expect(def).not.toHaveClass("transition-colors");
    expect(parent).not.toHaveClass("transition-colors");
  });
});
