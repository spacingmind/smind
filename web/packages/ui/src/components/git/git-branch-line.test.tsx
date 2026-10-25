import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { GitBranchLine } from "@/components/git/git-branch-line";

const LONG_OWN = "smind/task-add-refunds-dmdo2u10uxe8";
const LONG_BASE = "smind/task-payments-rework-dmdo2typy7js";

describe("GitBranchLine", () => {
  it("shows (gone) when the base is gone", () => {
    render(<GitBranchLine branch="fix" base="smind/parent" gone testId="line" />);
    expect(screen.getByTestId("line")).toHaveTextContent("fix→ Base: smind/parent (gone)");
  });

  it("titles a stacked task with its parent branch", () => {
    render(<GitBranchLine branch="fix" base="smind/parent" stackedOn="smind/parent" testId="line" />);
    expect(screen.getByTestId("line")).toHaveAttribute("title", "fix → smind/parent · stacked on smind/parent");
  });

  it("labels the base so it differs from the task's own branch", () => {
    render(<GitBranchLine branch="fix" base="develop" testId="line" />);
    expect(screen.getByTestId("line")).toHaveTextContent("→ Base: develop");
  });

  it("keeps the distinguishing tail of long own and base branch names", () => {
    render(<GitBranchLine branch={LONG_OWN} base={LONG_BASE} testId="line" />);
    const line = screen.getByTestId("line");
    const tails = Array.from(line.querySelectorAll('[data-part="tail"]')).map((e) => e.textContent);
    expect(tails).toEqual([LONG_OWN.slice(-14), LONG_BASE.slice(-14)]);
    expect(line.querySelector('[title="' + LONG_BASE + '"]')).not.toBeNull();
  });
});
