import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { GitBranchLine } from "@/components/git/git-branch-line";

describe("GitBranchLine", () => {
  it("shows (gone) when the base is gone", () => {
    render(<GitBranchLine branch="fix" base="smind/parent" gone testId="line" />);
    expect(screen.getByTestId("line")).toHaveTextContent("fix → smind/parent (gone)");
  });

  it("titles a stacked task with its parent branch", () => {
    render(<GitBranchLine branch="fix" base="smind/parent" stackedOn="smind/parent" testId="line" />);
    expect(screen.getByTestId("line")).toHaveAttribute("title", "fix → smind/parent · stacked on smind/parent");
  });
});
