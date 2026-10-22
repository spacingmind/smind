import { fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { flush, gitState, Harness, openPr } from "@/components/git/pr-test-support";
import { RpcError } from "@/lib/ws-client";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import type { TaskGit } from "@/lib/types";

async function mount(git: TaskGit) {
  const client = new FakeWsClient();
  render(<Harness client={client} />);
  client.nth("task.git", 0).resolve(git);
  await flush();
  return client;
}

afterEach(() => vi.restoreAllMocks());

describe("PrAction", () => {
  it("opens the dialog from Create PR…", async () => {
    const client = await mount(gitState());
    const button = screen.getByTestId("create-pr-button");
    expect(button).toHaveTextContent("Create PR…");
    expect(button).toBeEnabled();
    fireEvent.click(button);
    await flush();
    expect(client.nth("task.prPreview", 0).params).toEqual({ taskId: 1 });
  });

  it("swaps to Update/View when a PR exists", async () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    const client = await mount(
      gitState({ pr: openPr(), actions: { createPr: { enabled: false, code: "pr_exists", reason: "x" }, updatePr: { enabled: true } } }),
    );
    expect(screen.queryByTestId("create-pr-button")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("update-pr-button"));
    await flush();
    expect(client.nth("task.updatePr", 0).params).toEqual({ taskId: 1 });
    client.nth("task.updatePr", 0).resolve({ pr: openPr(), pushed: { from: "a", to: "b" } });
    await flush();
    fireEvent.click(screen.getByTestId("view-pr-button"));
    expect(open).toHaveBeenCalledWith("https://github.com/owner/repo/pull/42", "_blank", "noopener,noreferrer");
    // Success leaves no transient message: the chip is the record.
    expect(screen.queryByTestId("pr-error")).not.toBeInTheDocument();
    expect(screen.queryByText(/PR opened/)).not.toBeInTheDocument();
  });

  it("only opens https PR urls", async () => {
    await mount(gitState({ pr: openPr({ url: "javascript:alert(1)" }), actions: { updatePr: { enabled: true } } }));
    expect(screen.getByTestId("view-pr-button")).toBeDisabled();
  });

  it("explains why Create PR is disabled (U7: no origin)", async () => {
    const reason = "Create PR isn't available because this workspace has no `origin` remote, so there's nowhere to open a pull request. Add one with `git remote add origin <url>`.";
    await mount(gitState({ actions: { createPr: { enabled: false, code: "forge_no_remote", reason } } }));
    const button = screen.getByTestId("create-pr-button");
    expect(button).toBeDisabled();
    expect(button.parentElement).toHaveAttribute("title", reason);
    expect(screen.getByTestId("pr-action-reason")).toHaveTextContent(reason);
    expect(button).toHaveAccessibleDescription(reason);
  });

  it("never renders a raw error", async () => {
    const client = await mount(gitState({ pr: openPr(), actions: { updatePr: { enabled: true } } }));
    fireEvent.click(screen.getByTestId("update-pr-button"));
    await flush();
    client.nth("task.updatePr", 0).reject(new Error("task.updatePr: git [push origin x:refs/heads/x --force]: exit status 128\n    at Object.<anonymous> (/x.js:1:1)"));
    await flush();
    const error = screen.getByTestId("pr-error");
    expect(error).toHaveTextContent("Couldn't update the pull request.");
    expect(error.textContent).not.toMatch(/exit status|--force|at Object/);
    expect(screen.queryByTestId("pr-error-details")).not.toBeInTheDocument();
  });

  it("shows a typed update error as one line with copyable details", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    const client = await mount(gitState({ pr: openPr(), actions: { updatePr: { enabled: true } } }));
    fireEvent.click(screen.getByTestId("update-pr-button"));
    await flush();
    client
      .nth("task.updatePr", 0)
      .reject(new RpcError("GitHub rejected the push because the branch there has changes this branch doesn't. Update this branch first.", "push_rejected", { detail: "! [rejected] x -> x (fetch first)" }));
    await flush();
    expect(screen.getByTestId("pr-error").querySelector("p")).toHaveTextContent("GitHub rejected the push");
    expect(screen.getByTestId("pr-error-details")).toHaveTextContent("fetch first");
    fireEvent.click(screen.getByTestId("pr-error-copy"));
    expect(writeText).toHaveBeenCalledWith("! [rejected] x -> x (fetch first)");
  });

  it("shows the update error in an out-of-flow card that can be dismissed (it must not grow the header)", async () => {
    const client = await mount(gitState({ pr: openPr(), actions: { updatePr: { enabled: true } } }));
    fireEvent.click(screen.getByTestId("update-pr-button"));
    await flush();
    client.nth("task.updatePr", 0).reject(new RpcError("GitHub rejected the push. Update this branch first.", "push_rejected", {}));
    await flush();
    const card = screen.getByTestId("pr-error-card");
    expect(card.className).toContain("absolute");
    fireEvent.click(screen.getByTestId("pr-error-dismiss"));
    await flush();
    expect(screen.queryByTestId("pr-error-card")).toBeNull();
  });
});
