import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { PrChip } from "@/components/git/pr-chip";
import type { TaskGitPr } from "@/lib/types";

const NOW = Date.parse("2026-01-01T12:00:00Z");

function pr(overrides: Partial<TaskGitPr> = {}): TaskGitPr {
  return {
    number: 42,
    repo: "owner/repo",
    url: "https://github.com/owner/repo/pull/42",
    title: "A title",
    state: "open",
    draft: false,
    headRef: "feat",
    headSha: "abc",
    baseRef: "main",
    fromFork: false,
    mergeable: "mergeable",
    reviewDecision: "none",
    fetchedAt: new Date(NOW).toISOString(),
    stale: "",
    refreshAvailableAt: "",
    ...overrides,
  };
}

afterEach(() => vi.restoreAllMocks());

describe("PrChip", () => {
  it("renders each state with text", () => {
    const cases: [Partial<TaskGitPr>, string][] = [
      [{}, "#42 · Open"],
      [{ draft: true }, "#42 · Draft"],
      [{ state: "merged" }, "#42 · Merged"],
      [{ state: "closed" }, "#42 · Closed"],
    ];
    for (const [overrides, text] of cases) {
      const { unmount } = render(<PrChip pr={pr(overrides)} nowMs={NOW} />);
      expect(screen.getByTestId("pr-chip")).toHaveTextContent(text);
      expect(screen.getByTestId("pr-chip")).toHaveAccessibleName(`${text} — Open on GitHub`);
      unmount();
    }
  });

  it("colours only failing/blocking", () => {
    const failing = pr({ checks: { state: "failure", passed: 0, failed: 2, pending: 0, total: 2 } });
    const { rerender } = render(<PrChip pr={failing} nowMs={NOW} />);
    expect(screen.getByTestId("pr-chip")).toHaveClass("text-destructive");
    expect(screen.getByTestId("pr-chip")).toHaveTextContent("✗ 2 failing");
    rerender(<PrChip pr={pr({ checks: { state: "success", passed: 1, failed: 0, pending: 0, total: 1 } })} nowMs={NOW} />);
    expect(screen.getByTestId("pr-chip")).not.toHaveClass("text-destructive");
  });

  it("opens the PR url with noopener", () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    render(<PrChip pr={pr()} nowMs={NOW} />);
    fireEvent.click(screen.getByTestId("pr-chip"));
    expect(open).toHaveBeenCalledWith("https://github.com/owner/repo/pull/42", "_blank", "noopener,noreferrer");
  });

  it("is disabled for a non-https url", () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    render(<PrChip pr={pr({ url: "javascript:alert(1)" })} nowMs={NOW} />);
    const chip = screen.getByTestId("pr-chip");
    expect(chip).toBeDisabled();
    fireEvent.click(chip);
    expect(open).not.toHaveBeenCalled();
  });

  it("handles missing pr/checks/unknown state", () => {
    const odd = pr({ checks: undefined, state: "weird" as unknown as TaskGitPr["state"], url: undefined as unknown as string, title: undefined as unknown as string });
    render(<PrChip pr={odd} nowMs={NOW} />);
    expect(screen.getByTestId("pr-chip")).toHaveTextContent("#42 · Open");
    expect(screen.getByTestId("pr-chip")).toBeDisabled();
  });

  it("renders the PR title as text", () => {
    const { container } = render(<PrChip pr={pr({ title: "<img onerror=x>" })} nowMs={NOW} />);
    expect(container.querySelector("img")).toBeNull();
    expect(screen.getByTestId("pr-chip")).toHaveAttribute("title", expect.stringContaining("<img onerror=x>"));
  });
});
