import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { SidebarGitGlyph } from "@/components/git/sidebar-git-glyph";
import type { TaskGit, TaskGitPr } from "@/lib/types";

function git(overrides: Partial<TaskGit> = {}): TaskGit {
  return { taskId: 1, untrusted: false, computedAt: "2026-01-01T00:00:00Z", ...overrides };
}

const PR: TaskGitPr = {
  number: 7,
  repo: "o/r",
  url: "https://github.com/o/r/pull/7",
  title: "t",
  state: "open",
  draft: false,
  headRef: "h",
  headSha: "s",
  baseRef: "main",
  fromFork: false,
  mergeable: "mergeable",
  reviewDecision: "none",
  fetchedAt: "",
  stale: "",
  refreshAvailableAt: "",
};

describe("SidebarGitGlyph", () => {
  it("shows nothing for a task with nothing to say", () => {
    const { container } = render(<SidebarGitGlyph git={git()} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("shows at most one glyph and one dot", () => {
    render(
      <SidebarGitGlyph
        git={git({ pr: PR, conflict: { state: "conflicts", files: [], checkedAt: "" }, origin: { published: false }, commits: { sinceFork: 2 } })}
      />,
    );
    expect(screen.getAllByTestId("sidebar-git-glyph")).toHaveLength(1);
    expect(screen.getAllByTestId("sidebar-git-dot")).toHaveLength(1);
    expect(screen.getAllByRole("img")).toHaveLength(2);
  });

  it("labels the glyph and the dot with text", () => {
    render(<SidebarGitGlyph git={git({ pr: { ...PR, draft: true }, origin: { published: false }, commits: { sinceFork: 1 } })} />);
    const glyph = screen.getByLabelText("Pull request #7 draft");
    expect(glyph).toHaveAttribute("title", "Pull request #7 draft");
    expect(glyph).toHaveClass("text-foreground-subtlest");
    const dot = screen.getByTestId("sidebar-git-dot");
    expect(dot).toHaveAccessibleName("Not published");
    expect(dot).toHaveAttribute("title", "Not published");
  });

  it("prefers the conflicts dot over the unpublished dot", () => {
    render(
      <SidebarGitGlyph
        git={git({
          base: { ref: "main", known: true },
          conflict: { state: "conflicts", files: [], checkedAt: "" },
          origin: { published: false },
          commits: { sinceFork: 3 },
        })}
      />,
    );
    const dot = screen.getByTestId("sidebar-git-dot");
    expect(dot).toHaveAccessibleName("Conflicts with main");
    expect(dot).toHaveClass("bg-destructive");
  });
});
