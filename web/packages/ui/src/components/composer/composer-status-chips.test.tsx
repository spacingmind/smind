import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ComposerStatusChips } from "@/components/composer/composer-status-chips";
import { useTaskGit } from "@/hooks/use-task-git";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import type { TaskGit, TaskGitPr } from "@/lib/types";

const PR: TaskGitPr = {
  number: 42,
  repo: "o/r",
  url: "https://github.com/o/r/pull/42",
  title: "t",
  state: "open",
  draft: false,
  headRef: "h",
  headSha: "s",
  baseRef: "develop",
  fromFork: false,
  mergeable: "mergeable",
  reviewDecision: "none",
  fetchedAt: "2026-10-22T00:00:00Z",
  stale: "",
  refreshAvailableAt: "",
};

function taskGit(over: Partial<TaskGit> = {}): TaskGit {
  return { taskId: 1, untrusted: false, computedAt: "2026-10-22T00:00:00Z", ...over };
}

/** The wiring task-detail.tsx does: the shared task git state feeding the chips. */
function Wired({ onOpenDiff }: { client: FakeWsClient; onOpenDiff: () => void }) {
  const git = useTaskGit(1);
  return <ComposerStatusChips running={false} git={git} onOpenDiff={onOpenDiff} />;
}

describe("ComposerStatusChips", () => {
  it("says it is waiting for the user, with a static dot, while a permission is unanswered (U26)", () => {
    const { rerender } = render(<ComposerStatusChips running />);
    expect(screen.getByTestId("composer-working-chip")).toHaveTextContent("Working");
    expect(screen.getByTestId("composer-working-chip").querySelector("span")).toHaveClass("animate-pulse");
    rerender(<ComposerStatusChips running waiting />);
    const chip = screen.getByTestId("composer-working-chip");
    expect(chip).toHaveTextContent("Waiting for you");
    expect(chip).not.toHaveTextContent("Working");
    expect(chip.querySelector("span")).not.toHaveClass("animate-pulse");
    // waiting without a live run shows nothing
    rerender(<ComposerStatusChips running={false} waiting />);
    expect(screen.queryByTestId("composer-working-chip")).not.toBeInTheDocument();
  });

  it("renders the concurrency chip beside Working and calls onJumpToChat", () => {
    const onJumpToChat = vi.fn();
    render(<ComposerStatusChips running otherRunningChat={{ id: 7, title: "Fix login" }} onJumpToChat={onJumpToChat} />);
    expect(screen.getByTestId("composer-working-chip")).toBeInTheDocument();
    const chip = screen.getByTestId("composer-concurrency-chip");
    expect(chip).toHaveAccessibleName("Also running Fix login - another chat is running in this worktree; open it");
    expect(chip).toHaveTextContent("Fix login");
    fireEvent.click(chip);
    expect(onJumpToChat).toHaveBeenCalledWith(7);
  });

  it("renders nothing when nothing applies", () => {
    const { container } = render(<ComposerStatusChips running={false} otherRunningChat={null} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("it refreshes the diff chip when a task.git event arrives", async () => {
    const client = new FakeWsClient();
    render(<Wired client={client} onOpenDiff={() => {}} />);
    await act(async () => client.nth("task.git", 0).resolve(taskGit({ diff: { files: 1, insertions: 3, deletions: 1 } })));
    expect(screen.getByTestId("composer-diff-stat")).toHaveTextContent("+3");

    act(() =>
      client.pushNotification("task.git", { taskId: 1, git: taskGit({ diff: { files: 2, insertions: 12, deletions: 4 } }) }),
    );
    await waitFor(() => expect(screen.getByTestId("composer-diff-stat")).toHaveTextContent("+12"));
    expect(screen.getByTestId("composer-diff-stat")).toHaveTextContent("\u22124");
  });

  it("it shows the PR chip and the conflicts chip", () => {
    const onOpenDiff = vi.fn();
    render(
      <ComposerStatusChips
        running={false}
        onOpenDiff={onOpenDiff}
        git={taskGit({
          pr: PR,
          base: { ref: "develop", known: true },
          conflict: { state: "conflicts", files: ["a.go"], against: "develop", checkedAt: "2026-10-22T00:00:00Z" },
        })}
      />,
    );
    expect(screen.getByTestId("pr-chip")).toHaveTextContent("#42");
    const conflicts = screen.getByTestId("composer-conflicts-chip");
    expect(conflicts).toHaveTextContent("Conflicts with develop");
    expect(conflicts.className).toContain("text-destructive");
    fireEvent.click(conflicts);
    expect(onOpenDiff).toHaveBeenCalledTimes(1);
  });

  it("renders nothing for a git state with no diff, PR or conflicts", () => {
    const { container } = render(<ComposerStatusChips running={false} git={taskGit({ conflict: { state: "none", files: [], checkedAt: "" } })} onOpenDiff={() => {}} />);
    expect(container).toBeEmptyDOMElement();
  });
});
