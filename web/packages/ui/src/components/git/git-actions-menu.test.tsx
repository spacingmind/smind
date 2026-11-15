import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { GitActionsMenu } from "@/components/git/git-actions-menu";
import { flush, gitState, openPr } from "@/components/git/pr-test-support";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import type { TaskGit } from "@/lib/types";

function Harness(_: { client: FakeWsClient }) {
  return <GitActionsMenu taskId={1} workspaceId={7} title="Fix" />;
}

async function mount(git: TaskGit) {
  const client = new FakeWsClient();
  render(<Harness client={client} />);
  client.nth("task.git", 0).resolve(git);
  await flush();
  return client;
}

async function openMenu() {
  fireEvent.pointerDown(screen.getByTestId("git-actions-menu-trigger"), { button: 0, ctrlKey: false });
  await waitFor(() => expect(screen.getByTestId("git-actions-menu-content")).toBeInTheDocument());
  return screen.getByTestId("git-actions-menu-content");
}

afterEach(() => vi.restoreAllMocks());

describe("GitActionsMenu", () => {
  it("one primary button and one menu replace the row of git buttons", async () => {
    await mount(gitState({ origin: { published: true }, actions: { push: { enabled: true }, updateFromBase: { enabled: true }, createPr: { enabled: true }, updatePr: { enabled: false, reason: "no PR" } } }));
    expect(screen.getAllByRole("button").map((b) => b.getAttribute("data-testid"))).toEqual(["git-primary-action", "git-actions-menu-trigger"]);
    expect(screen.getByTestId("git-primary-action")).toHaveTextContent("Create PR…");
    expect(screen.queryByTestId("sync-button-push")).not.toBeInTheDocument();
    expect(screen.queryByTestId("create-pr-button")).not.toBeInTheDocument();
  });

  it("the menu lists every action, the primary one marked", async () => {
    await mount(gitState({ origin: { published: true }, actions: { push: { enabled: true }, updateFromBase: { enabled: true }, createPr: { enabled: true }, updatePr: { enabled: false, reason: "no PR" } } }));
    const menu = await openMenu();
    expect(["git-action-push", "git-action-update", "git-action-create-pr"].map((id) => menu.querySelector(`[data-testid="${id}"]`) !== null)).toEqual([true, true, true]);
    expect(menu.querySelector('[data-testid="git-action-create-pr"]')?.getAttribute("data-primary")).toBe("true");
    expect(menu.querySelector('[data-testid="git-action-push"]')?.getAttribute("data-primary")).toBeNull();
  });

  it("diff-disabled-action-shows-its-reason: plain text under the label, reachable, and selecting it does nothing", async () => {
    const client = await mount(
      gitState({ origin: { published: true }, actions: { push: { enabled: false, code: "nothing_to_push", reason: "Nothing to push: the branch is up to date." }, updateFromBase: { enabled: true }, createPr: { enabled: true } } }),
    );
    const menu = await openMenu();
    const push = menu.querySelector('[data-testid="git-action-push"]') as HTMLElement;
    expect(push).toHaveAttribute("aria-disabled", "true");
    const reason = menu.querySelector('[data-testid="git-action-push-reason"]') as HTMLElement;
    expect(reason).toHaveTextContent("Nothing to push: the branch is up to date.");
    expect(push.getAttribute("aria-describedby")).toBe(reason.id);
    fireEvent.click(push);
    await flush();
    expect(client.calls.some((c) => c.method === "task.pushPreview" || c.method === "task.push")).toBe(false);
    expect(screen.getByTestId("git-actions-menu-content")).toBeInTheDocument(); // the menu stays open
  });

  it("the primary button runs its action: Create PR opens the preview, Update PR calls task.updatePr, View PR opens the https url", async () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    const client = await mount(gitState({ pr: openPr(), origin: { published: true }, actions: { push: { enabled: false, reason: "x" }, createPr: { enabled: false, reason: "pr exists" }, updatePr: { enabled: true } } }));
    expect(screen.getByTestId("git-primary-action")).toHaveTextContent("Update PR");
    fireEvent.click(screen.getByTestId("git-primary-action"));
    await flush();
    expect(client.nth("task.updatePr", 0).params).toEqual({ taskId: 1 });
    client.nth("task.updatePr", 0).resolve({ pr: openPr(), pushed: { from: "a", to: "b" } });
    await flush();
    const menu = await openMenu();
    fireEvent.click(menu.querySelector('[data-testid="git-action-view-pr"]') as HTMLElement);
    expect(open).toHaveBeenCalledWith("https://github.com/owner/repo/pull/42", "_blank", "noopener,noreferrer");
  });

  it("a merged PR makes Archive… the primary action", async () => {
    const client = await mount(gitState({ pr: openPr({ state: "merged" }), merged: { state: "merged", via: "pr" }, actions: { createPr: { enabled: false, reason: "x" }, updatePr: { enabled: false, reason: "y" } } }));
    expect(screen.getByTestId("git-primary-action")).toHaveTextContent("Archive…");
    fireEvent.click(screen.getByTestId("git-primary-action"));
    await flush();
    expect(client.nth("task.archivePreview", 0).params).toEqual({ taskId: 1 });
  });

  it("a disabled primary keeps the reason as its tooltip and in the menu", async () => {
    await mount(gitState({ origin: { published: true }, actions: { push: { enabled: false, reason: "Nothing to push." }, createPr: { enabled: false, reason: "No commits to review yet." } } }));
    const primary = screen.getByTestId("git-primary-action");
    expect(primary).toBeDisabled();
    expect(primary.parentElement).toHaveAttribute("title", "Nothing to push.");
    const menu = await openMenu();
    expect(menu.querySelector('[data-testid="git-action-create-pr-reason"]')).toHaveTextContent("No commits to review yet.");
  });

  it("a forge-setup reason offers the link to Settings -> GitHub", async () => {
    await mount(gitState({ actions: { createPr: { enabled: false, code: "forge_unauthenticated", reason: "Sign in to GitHub first." } } }));
    const menu = await openMenu();
    expect(menu.querySelector('[data-testid="git-action-settings-link"]')).not.toBeNull();
  });

  it("an update failure shows the error card", async () => {
    const client = await mount(gitState({ pr: openPr(), actions: { updatePr: { enabled: true } } }));
    fireEvent.click(screen.getByTestId("git-primary-action"));
    await flush();
    client.nth("task.updatePr", 0).reject(new Error("boom"));
    await flush();
    expect(screen.getByTestId("pr-error-card")).toBeInTheDocument();
  });
});
