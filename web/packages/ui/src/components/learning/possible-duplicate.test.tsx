import { act } from "react";
import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ProposalReview } from "@/components/learning/proposal-review";
import type { ProposalDetail } from "@/lib/learning-types";
import { item, proposal, skill } from "@/test/learning-fixtures";
import { learningDaemon } from "@/test/learning-test-client";
import { renderWithDaemon } from "@/test/render-with-daemon";

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 8; i++) await Promise.resolve();
  });
}

function setup(detail: ProposalDetail) {
  const calls: { method: string; params: unknown }[] = [];
  let backs = 0;
  const d = learningDaemon((method, params) => {
    calls.push({ method, params });
    if (method === "learning.proposals.get") return detail;
    return null;
  });
  renderWithDaemon(<ProposalReview id={9} onBack={() => backs++} />, d);
  return { calls, backs: () => backs };
}

const EXISTING_BODY = "# Focused tests\nRun the package first.\nThen the suite.\n";
const LEARNED: ProposalDetail = {
  proposal: proposal({
    id: 9,
    possibleDuplicateOf: "focused-tests",
    similarity: 0.87,
    items: [item({ name: "run-focused-tests", description: "Run one package first.", body: "# Focused\nRun the package.\n" })],
  }),
  merge: {
    target: skill({ id: 77, name: "focused-tests", description: "Package-first testing.", body: EXISTING_BODY }),
    similarity: 0.87,
  },
};

describe("possible duplicate", () => {
  it("possible duplicate shows both skills and merge action", async () => {
    const v = setup(LEARNED);
    await flush();

    // The banner names the skill and the score.
    expect(screen.getByTestId("duplicate-banner").textContent).toContain("Possible duplicate of focused-tests (87%)");

    // Both skills side by side: description + body each, in one equal-height grid that stacks when narrow.
    const compare = screen.getByTestId("duplicate-compare");
    expect(compare.className).toContain("grid-cols-1");
    expect(compare.className).toContain("md:grid-cols-2");
    expect(compare.className).toContain("items-stretch");
    const proposed = within(compare).getByTestId("duplicate-proposed");
    const existing = within(compare).getByTestId("duplicate-existing");
    expect(proposed.textContent).toContain("Run one package first.");
    expect(within(proposed).getByTestId("duplicate-proposed-body").textContent).toContain("Run the package.");
    expect(existing.textContent).toContain("Package-first testing.");
    expect(within(existing).getByTestId("duplicate-existing-body").textContent).toContain("Then the suite.");

    // A primary-secondary pair: Merge into this skill, Keep as separate skill.
    expect(screen.getByTestId("proposal-merge").textContent).toBe("Merge into this skill");
    expect(screen.getByTestId("proposal-keep-separate").textContent).toBe("Keep as separate skill");

    // Merging opens an editable body pre-filled with the EXISTING body; both skills stay visible.
    fireEvent.click(screen.getByTestId("proposal-merge"));
    const textarea = screen.getByTestId("proposal-merge-body") as HTMLTextAreaElement;
    expect(textarea.value).toBe(EXISTING_BODY);
    expect(screen.getByTestId("duplicate-proposed")).toBeTruthy();
    expect(screen.getByTestId("duplicate-existing")).toBeTruthy();

    fireEvent.change(textarea, { target: { value: "# Focused tests\nRun the package first, always.\n" } });
    fireEvent.click(screen.getByTestId("proposal-approve-merge"));
    await flush();

    // Merge sends the existing skill's NAME (the daemon identifies the target by name) and the edited body.
    expect(v.calls.find((c) => c.method === "learning.proposals.approve")?.params).toEqual({
      id: 9,
      mergeInto: "focused-tests",
      body: "# Focused tests\nRun the package first, always.\n",
    });
    expect(v.backs()).toBe(1);
  });

  it("keep as separate skill approves without mergeInto", async () => {
    const v = setup(LEARNED);
    await flush();
    fireEvent.click(screen.getByTestId("proposal-keep-separate"));
    await flush();
    expect(v.calls.find((c) => c.method === "learning.proposals.approve")?.params).toEqual({ id: 9 });
  });

  it("an invalid merged body cannot be approved", async () => {
    setup(LEARNED);
    await flush();
    fireEvent.click(screen.getByTestId("proposal-merge"));
    fireEvent.change(screen.getByTestId("proposal-merge-body"), { target: { value: "  \n" } });
    expect(screen.getByTestId("proposal-approve-merge")).toBeDisabled();
  });

  it("a human duplicate shows the banner with the skill name and no merge action", async () => {
    const human: ProposalDetail = {
      proposal: proposal({ id: 9, possibleDuplicateOf: "human-skill", similarity: 0.64 }),
    };
    const v = setup(human);
    await flush();
    const banner = screen.getByTestId("duplicate-banner");
    expect(banner.textContent).toContain("Possible duplicate of human-skill (64%)");
    expect(banner.textContent).toContain("smind never edits it");
    expect(screen.queryByTestId("duplicate-compare")).toBeNull();
    expect(screen.queryByTestId("proposal-merge")).toBeNull();
    // The normal decision stays available.
    fireEvent.click(screen.getByTestId("proposal-approve"));
    await flush();
    expect(v.calls.find((c) => c.method === "learning.proposals.approve")?.params).toEqual({ id: 9 });
    expect(screen.getByTestId("proposal-reject")).toBeTruthy();
  });
});
