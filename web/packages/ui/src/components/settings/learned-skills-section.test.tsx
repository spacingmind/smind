import { act } from "react";
import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/learned-skills-section";
import { LEARNED_SKILLS_SECTION_ID } from "@/lib/learning-section";
import type { LearnedSkillView, LearningStatus, ProposalView } from "@/lib/learning-types";
import { learningDaemon, learningStatus } from "@/test/learning-test-client";
import { proposal, skill } from "@/test/learning-fixtures";
import { renderWithDaemon } from "@/test/render-with-daemon";
import type { FakeUsageDaemon } from "@/test/usage-test-client";

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 6; i++) await Promise.resolve();
  });
}

const STATUS: LearningStatus = learningStatus({
  workspaces: [
    { id: 1, title: "alpha", learningMode: "propose", pendingProposals: 2, activeSkills: 3, disabledSkills: 1 },
    { id: 2, title: "beta", learningMode: "off", pendingProposals: 0, activeSkills: 0, disabledSkills: 0 },
  ],
});

interface World {
  status?: LearningStatus;
  skills?: Record<string, LearnedSkillView[]>;
  proposals?: ProposalView[];
}

function world(w: World = {}): FakeUsageDaemon {
  return learningDaemon((method, params) => {
    const p = (params ?? {}) as { workspaceId?: number; statuses?: string[] };
    if (method === "learning.status") return w.status ?? STATUS;
    if (method === "learning.skills.list") return (w.skills ?? {})[`${p.workspaceId}:${(p.statuses ?? []).join(",")}`] ?? [];
    if (method === "learning.proposals.list") return p.workspaceId === 1 ? (w.proposals ?? []) : [];
    return null;
  });
}

function renderSection(d: FakeUsageDaemon) {
  const section = listSettingsSections().find((s) => s.id === LEARNED_SKILLS_SECTION_ID)!;
  return renderWithDaemon(<>{section.render({ client: d.client, events: d.events })}</>, d);
}

const ACTIVE = [
  skill({ id: 11, name: "run-focused-tests", probation: true, offeredRuns: 12, loadedRuns: 3, loadRate: 0.25 }),
  skill({ id: 12, name: "never-force-push", pinned: true, source: "curator" }),
  skill({ id: 13, name: "fresh-skill", measured: false, offeredRuns: 2, loadedRuns: 0, loadRate: 0, lastLoadedAt: undefined }),
  skill({ id: 14, name: "muted-skill", status: "disabled" }),
];

describe("registration", () => {
  it("registers Skills (learned) in Agents & providers with a deep-linkable id and a nav badge", () => {
    const section = listSettingsSections().find((s) => s.id === "learned-skills");
    expect(section?.label).toBe("Skills (learned)");
    expect(section?.groupLabel).toBe("Agents & providers");
    expect(section?.badge).toBeTruthy();
  });
});

describe("Settings -> Skills (learned)", () => {
  it("lists the active tab with counters, source and badges", async () => {
    renderSection(world({ skills: { "1:active,disabled": ACTIVE } }));
    await flush();
    fireEvent.mouseDown(screen.getByTestId("learning-tab-active"));
    fireEvent.click(screen.getByTestId("learning-tab-active"));
    await flush();

    const first = screen.getByTestId("learned-skill-row-11");
    expect(within(first).getByText("run-focused-tests")).toBeTruthy();
    expect(within(first).getByTestId("badge-probation")).toBeTruthy();
    expect(screen.getByTestId("learned-skill-counters-11").textContent).toContain("12 offered · 3 loaded · 25%");
    expect(within(screen.getByTestId("learned-skill-row-12")).getByTestId("badge-pinned")).toBeTruthy();
    expect(within(screen.getByTestId("learned-skill-row-12")).getByTestId("learning-source").textContent).toBe("curator");
    expect(within(screen.getByTestId("learned-skill-row-13")).getByTestId("badge-unmeasured")).toBeTruthy();
    expect(within(screen.getByTestId("learned-skill-row-14")).getByTestId("badge-disabled")).toBeTruthy();
    // A measured skill never shows the unmeasured badge.
    expect(within(first).queryByTestId("badge-unmeasured")).toBeNull();
  });

  it("opens on Proposed with the pending count and its rows", async () => {
    renderSection(world({ proposals: [proposal({ id: 31, name: "tidy-imports" }), proposal({ id: 32, name: "merge-docs", kind: "bundle", possibleDuplicateOf: "docs-skill", similarity: 0.72 })] }));
    await flush();
    expect(screen.getByTestId("learning-pending-count").textContent).toBe("2");
    expect(screen.getByTestId("proposal-row-31")).toBeTruthy();
    expect(within(screen.getByTestId("proposal-row-32")).getByTestId("proposal-duplicate").textContent).toContain("72%");
    expect(within(screen.getByTestId("proposal-row-32")).getByTestId("proposal-kind").textContent).toBe("bundle");
  });

  it("shows an empty state per tab", async () => {
    renderSection(world({ status: { ...STATUS, workspaces: [{ ...STATUS.workspaces[0]!, pendingProposals: 0 }, STATUS.workspaces[1]!] } }));
    await flush();
    expect(screen.getByTestId("learning-empty-proposed")).toBeTruthy();
    expect(screen.queryByTestId("learning-pending-count")).toBeNull();
    fireEvent.mouseDown(screen.getByTestId("learning-tab-active"));
    await flush();
    expect(screen.getByTestId("learning-empty-active")).toBeTruthy();
    fireEvent.mouseDown(screen.getByTestId("learning-tab-archived"));
    await flush();
    expect(screen.getByTestId("learning-empty-archived")).toBeTruthy();
  });

  it("the Archived tab asks only for archived skills", async () => {
    const d = world({ skills: { "1:archived": [skill({ id: 21, name: "old-skill", status: "archived" })] } });
    renderSection(d);
    await flush();
    fireEvent.mouseDown(screen.getByTestId("learning-tab-archived"));
    await flush();
    expect(screen.getByTestId("learned-skill-row-21")).toBeTruthy();
    expect(d.calls.some((c) => c.method === "learning.skills.list" && (c.params as { statuses: string[] }).statuses.join() === "archived")).toBe(true);
  });

  it("keeps the list area the same size on every tab (no layout shift)", async () => {
    const d = world({ skills: { "1:active,disabled": ACTIVE } });
    renderSection(d);
    await flush();
    const frameClass = () => document.querySelector('[role="tabpanel"] > div')!.className;
    const proposedFrame = frameClass();
    fireEvent.mouseDown(screen.getByTestId("learning-tab-active"));
    await flush();
    expect(frameClass()).toBe(proposedFrame);
    expect(proposedFrame).toContain("min-h-");
  });

  it("rows are buttons, reachable and openable by keyboard", async () => {
    renderSection(world({ proposals: [proposal({ id: 31, name: "tidy-imports" })] }));
    await flush();
    const row = screen.getByTestId("proposal-row-31");
    expect(row.tagName).toBe("BUTTON");
    row.focus();
    expect(document.activeElement).toBe(row);
  });

  it("opening an active row shows its detail, and Back returns to the same tab", async () => {
    const d = learningDaemon((method, params) => {
      if (method === "learning.status") return STATUS;
      if (method === "learning.skills.list") return ACTIVE;
      if (method === "learning.skills.get") return { skill: ACTIVE[0], versions: [], ledger: [] };
      if (method === "learning.proposals.list") return [];
      void params;
      return null;
    });
    renderSection(d);
    await flush();
    fireEvent.mouseDown(screen.getByTestId("learning-tab-active"));
    await flush();
    fireEvent.click(screen.getByTestId("learned-skill-row-11"));
    await flush();
    expect(screen.getByTestId("learned-skill-detail")).toBeTruthy();
    expect((d.calls.find((c) => c.method === "learning.skills.get")?.params as { id: number }).id).toBe(11);
    fireEvent.click(screen.getByTestId("learning-detail-back"));
    expect(screen.getByTestId("learned-skill-row-11")).toBeTruthy();
  });
});
