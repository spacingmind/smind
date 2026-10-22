import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { GitTokens } from "@/components/git/git-tokens";
import type { TaskGit } from "@/lib/types";

function git(overrides: Partial<TaskGit> = {}): TaskGit {
  return { taskId: 1, untrusted: false, computedAt: "2026-01-01T00:00:00Z", ...overrides };
}

describe("GitTokens", () => {
  it("renders the token line", () => {
    render(
      <GitTokens
        git={git({
          base: { ref: "main", known: true },
          diff: { files: 3, insertions: 12, deletions: 4 },
          commits: { sinceFork: 2, behindBase: 5 },
          origin: { published: true, ahead: 2, behind: 1 },
          conflict: { state: "conflicts", files: [], checkedAt: "" },
        })}
      />,
    );
    const el = screen.getByTestId("git-tokens");
    expect(el).toHaveTextContent("+12 −4");
    expect(el).toHaveTextContent("2 commits");
    expect(el).toHaveTextContent("↑2 ↓1");
    expect(el).toHaveTextContent("↓5 behind main");
    expect(el).toHaveTextContent("⚠ conflicts with main");
  });

  it("tints only conflicts and diff deletions", () => {
    render(
      <GitTokens
        git={git({
          diff: { files: 1, insertions: 1, deletions: 1 },
          commits: { sinceFork: 1, behindBase: 1 },
          origin: { published: false },
          conflict: { state: "conflicts", files: [], checkedAt: "" },
        })}
      />,
    );
    const tinted = Array.from(screen.getByTestId("git-tokens").querySelectorAll(".text-destructive")).map((n) => n.textContent);
    expect(tinted).toEqual(["−1", "⚠ conflicts with base"]);
    expect(screen.getByTestId("git-tokens")).toHaveTextContent("1 commit");
    expect(screen.getByTestId("git-tokens")).toHaveTextContent("not published");
  });

  it("shows nothing when there is nothing to say", () => {
    const { container } = render(
      <GitTokens
        git={git({
          diff: { files: 0, insertions: 0, deletions: 0 },
          commits: { sinceFork: 0, behindBase: 0 },
          origin: { published: true, ahead: 0, behind: 0 },
          conflict: { state: "unknown", files: [], checkedAt: "" },
        })}
      />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("never throws when every field is missing", () => {
    const { container } = render(<GitTokens git={git()} />);
    expect(container).toBeEmptyDOMElement();
  });
});
