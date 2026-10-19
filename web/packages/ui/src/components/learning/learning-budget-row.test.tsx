import { act } from "react";
import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/learned-skills-section";
import type { LearningStatus, LearningWorkspaceBudget, LearningWorkspaceStatus } from "@/lib/learning-types";
import { learningDaemon, learningStatus } from "@/test/learning-test-client";
import { renderWithDaemon } from "@/test/render-with-daemon";

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 8; i++) await Promise.resolve();
  });
}

const BUDGET: LearningWorkspaceBudget = { windowHours: 24, usedUsd: 0.04, usedTokens: 18_000, capUsd: 0.25, capTokens: 150_000, unreportedRuns: 0 };

function ws(over: Partial<LearningWorkspaceStatus> = {}): LearningWorkspaceStatus {
  return { id: 1, title: "alpha", learningMode: "propose", pendingProposals: 0, activeSkills: 0, disabledSkills: 0, budget: BUDGET, ...over };
}

function setup(first: LearningStatus) {
  let current = first;
  const d = learningDaemon((method) => (method === "learning.status" ? current : []));
  const section = listSettingsSections().find((s) => s.id === "learned-skills")!;
  renderWithDaemon(<>{section.render({ client: d.client, events: d.events })}</>, d);
  return { d, set: (next: LearningStatus) => (current = next) };
}

const row = () => screen.getByTestId("learning-budget");

describe("learning budget row", () => {
  it("shows the cost and token values with two thin bars", async () => {
    setup(learningStatus({ workspaces: [ws()] }));
    await flush();
    expect(row().textContent).toContain("Learning budget (last 24 h)");
    expect(within(row()).getByTestId("learning-budget-cost").textContent).toBe("$0.04 of $0.25");
    expect(within(row()).getByTestId("learning-budget-tokens").textContent).toBe("18k of 150k tokens");
    const bars = within(row()).getAllByRole("progressbar");
    expect(bars.map((b) => b.getAttribute("aria-valuenow"))).toEqual(["16", "12"]);
    expect(row().getAttribute("data-level")).toBe("ok");
    expect(within(row()).queryByTestId("learning-budget-paused")).toBeNull();
    expect(row().querySelector('[data-slot="usage-meter"]')).not.toBeNull();
  });

  it("warns from 80% with the shared warning tone", async () => {
    setup(learningStatus({ workspaces: [ws({ budget: { ...BUDGET, usedUsd: 0.21, usedTokens: 130_000 } })] }));
    await flush();
    expect(row().getAttribute("data-level")).toBe("warn");
    expect(row().querySelector('[data-tone="warning"]')).not.toBeNull();
    expect(within(row()).queryByTestId("learning-budget-paused")).toBeNull();
  });

  it("exhausted says reflection is paused and that nothing is lost", async () => {
    setup(learningStatus({ workspaces: [ws({ budget: { ...BUDGET, usedUsd: 0.25 } })] }));
    await flush();
    expect(row().getAttribute("data-level")).toBe("exhausted");
    expect(within(row()).getByTestId("learning-budget-paused").textContent).toBe(
      "Reflection is paused until usage drops below the daily limit; nothing is lost, windows are kept for the next run.",
    );
  });

  it("renders nothing when learning is off or the daemon sends no budget", async () => {
    setup(learningStatus({ workspaces: [ws({ learningMode: "off" })] }));
    await flush();
    expect(screen.queryByTestId("learning-budget")).toBeNull();
  });

  it("renders nothing for an older daemon without a budget", async () => {
    setup(learningStatus({ workspaces: [ws({ budget: undefined })] }));
    await flush();
    expect(screen.getByTestId("learning-optin")).toBeTruthy();
    expect(screen.queryByTestId("learning-budget")).toBeNull();
  });

  it("footnotes runs without reported usage, singular and plural", async () => {
    setup(learningStatus({ workspaces: [ws({ budget: { ...BUDGET, unreportedRuns: 3 } })] }));
    await flush();
    expect(within(row()).getByTestId("learning-budget-note").textContent).toBe("3 runs without reported usage are estimated");
  });

  it("singular footnote and none at zero", async () => {
    setup(learningStatus({ workspaces: [ws({ budget: { ...BUDGET, unreportedRuns: 1 } })] }));
    await flush();
    expect(within(row()).getByTestId("learning-budget-note").textContent).toBe("1 run without reported usage is estimated");
  });

  it("no footnote when every run reported usage", async () => {
    setup(learningStatus({ workspaces: [ws()] }));
    await flush();
    expect(screen.queryByTestId("learning-budget-note")).toBeNull();
  });

  it("re-reads the status after a learning.job event and shows the new values", async () => {
    const { d, set } = setup(learningStatus({ workspaces: [ws()] }));
    await flush();
    expect(d.countCalls("learning.status")).toBe(1);
    set(learningStatus({ workspaces: [ws({ budget: { ...BUDGET, usedUsd: 0.25, usedTokens: 90_000 } })] }));
    await act(async () => d.emit("learning.job", { workspaceId: 1, jobId: 9, status: "done" }));
    await flush();
    expect(d.countCalls("learning.status")).toBe(2);
    expect(within(row()).getByTestId("learning-budget-cost").textContent).toBe("$0.25 of $0.25");
    expect(row().getAttribute("data-level")).toBe("exhausted");
  });
});
