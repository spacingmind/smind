import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { flush, gitState, Harness, openPr, preview } from "@/components/git/pr-test-support";
import { GIT_PREFS_STORAGE_KEY, loadDraftPr } from "@/lib/git-prefs";
import type { PrPreview } from "@/lib/types";
import { RpcError } from "@/lib/ws-client";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";

beforeEach(() => window.localStorage.removeItem(GIT_PREFS_STORAGE_KEY));

async function openDialog(pv: PrPreview = preview(), client = new FakeWsClient()) {
  render(<Harness client={client} />);
  client.nth("task.git", 0).resolve(gitState());
  await flush();
  fireEvent.click(screen.getByTestId("create-pr-button"));
  await flush();
  client.nth("task.prPreview", 0).resolve(pv);
  await flush();
  return client;
}

describe("CreatePrDialog", () => {
  it("prefills from prPreview", async () => {
    await openDialog();
    expect(screen.getByTestId("create-pr-title")).toHaveValue("Task X");
    expect((screen.getByTestId("create-pr-body") as HTMLTextAreaElement).value).toContain("## Summary\n- Add the thing");
    expect(screen.getByTestId("create-pr-branches")).toHaveTextContent("smind/task-x");
    expect(screen.getByTestId("create-pr-base")).toHaveValue("develop");
    expect(screen.getByTestId("create-pr-diffstat")).toHaveTextContent("+12 −4");
    expect(screen.getByTestId("create-pr-summary")).toHaveTextContent("2 commits");
    expect(screen.getByTestId("create-pr-title")).toHaveFocus();
  });

  it("creates with the edited text, draft and base, then closes", async () => {
    const client = await openDialog();
    fireEvent.change(screen.getByTestId("create-pr-title"), { target: { value: "My title" } });
    fireEvent.click(screen.getByTestId("create-pr-submit"));
    await flush();
    const call = client.nth("task.createPr", 0);
    expect(call.params).toEqual({ taskId: 1, title: "My title", body: expect.stringContaining("## Summary"), draft: true });
    expect(screen.getByTestId("create-pr-submit")).toBeDisabled();
    call.resolve({ pr: openPr() });
    await flush();
    await waitFor(() => expect(screen.queryByTestId("create-pr-dialog")).not.toBeInTheDocument());
  });

  it("sends a changed base as baseBranch and refetches the preview for it", async () => {
    const client = await openDialog();
    const base = screen.getByTestId("create-pr-base");
    fireEvent.change(base, { target: { value: "release" } });
    fireEvent.blur(base);
    await flush();
    expect(client.nth("task.prPreview", 1).params).toEqual({ taskId: 1, baseBranch: "release" });
    client.nth("task.prPreview", 1).resolve(preview({ base: { ref: "release", sha: "d".repeat(40) } }));
    await flush();
    fireEvent.click(screen.getByTestId("create-pr-submit"));
    await flush();
    expect(client.nth("task.createPr", 0).params).toMatchObject({ baseBranch: "release" });
  });

  it("defaults Draft on and remembers per workspace", async () => {
    const client = await openDialog();
    const draft = screen.getByTestId("create-pr-draft");
    expect(draft).toHaveAttribute("aria-checked", "true");
    fireEvent.click(draft);
    expect(draft).toHaveAttribute("aria-checked", "false");
    expect(loadDraftPr(7)).toBe(false);
    expect(loadDraftPr(8)).toBe(true);
    fireEvent.click(screen.getByTestId("create-pr-submit"));
    await flush();
    expect(client.nth("task.createPr", 0).params).toMatchObject({ draft: false });
  });

  it("shows the uncommitted notice and Commit staged first", async () => {
    const box = document.createElement("textarea");
    box.setAttribute("data-testid", "commit-message");
    document.body.append(box);
    await openDialog(preview({ uncommitted: { staged: 2, unstaged: 1, untracked: 0 } }));
    expect(screen.getByTestId("create-pr-summary")).toHaveTextContent("2 commits · 3 files uncommitted (not included)");
    fireEvent.click(screen.getByTestId("create-pr-commit-first"));
    await waitFor(() => expect(screen.queryByTestId("create-pr-dialog")).not.toBeInTheDocument());
    await waitFor(() => expect(box).toHaveFocus());
    box.remove();
  });

  it("disables with the reason sentence", async () => {
    const reason = "Create PR isn't available yet because this branch has no commits beyond `develop`. Commit your changes first.";
    await openDialog(preview({ commits: [], commitCount: 0, blockers: [{ code: "no_commits", reason }] }));
    expect(screen.getByTestId("create-pr-blocker")).toHaveTextContent(reason);
    expect(screen.getByTestId("create-pr-submit")).toBeDisabled();
  });

  it("shows a typed error in one line with details", async () => {
    const client = await openDialog();
    fireEvent.click(screen.getByTestId("create-pr-submit"));
    await flush();
    client.nth("task.createPr", 0).reject(new RpcError("Git couldn't sign in to push. Check the daemon host's git credentials, or run `gh auth login` there.", "push_auth", { detail: "fatal: Authentication failed" }));
    await flush();
    const error = screen.getByTestId("create-pr-error");
    expect(error.querySelector("p")).toHaveTextContent("Git couldn't sign in to push.");
    expect(screen.getByTestId("create-pr-error-details")).toHaveTextContent("Authentication failed");
    expect(screen.getByTestId("create-pr-dialog")).toBeInTheDocument();
    expect(screen.getByTestId("create-pr-submit")).toBeEnabled();
  });

  it("offers View PR when one already exists", async () => {
    const client = await openDialog();
    fireEvent.click(screen.getByTestId("create-pr-submit"));
    await flush();
    client
      .nth("task.createPr", 0)
      .reject(new RpcError("This branch already has an open pull request.", "pr_exists", { url: "https://github.com/owner/repo/pull/9", number: 9 }));
    await flush();
    const link = screen.getByTestId("create-pr-error-view");
    expect(link).toHaveAttribute("href", "https://github.com/owner/repo/pull/9");
    expect(link).toHaveTextContent("View PR #9");
  });

  it("shows the cherry-pick note", async () => {
    await openDialog(preview({ head: { branch: "smind/pr-task-1", ref: "refs/heads/smind/pr-task-1", strategy: "cherry-pick" } }));
    expect(screen.getByTestId("create-pr-cherry-pick-note")).toHaveTextContent(
      "smind will open this from a rebased copy (smind/pr-task-1); later updates aren't automatic.",
    );
  });

  it("renders PR text as text", async () => {
    await openDialog(preview({ defaults: { title: "<img src=x onerror=alert(1)>", body: "<script>x</script>", draft: true } }));
    expect(document.querySelector("img")).toBeNull();
    expect(screen.getByTestId("create-pr-title")).toHaveValue("<img src=x onerror=alert(1)>");
  });
});
