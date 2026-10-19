import { act } from "react";
import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ProposalReview } from "@/components/learning/proposal-review";
import type { LearnedSkillDetail, ProposalDetail, ProposalView } from "@/lib/learning-types";
import { RpcError } from "@/lib/ws-client";
import { item, proposal, skill } from "@/test/learning-fixtures";
import { learningDaemon } from "@/test/learning-test-client";
import { renderWithDaemon } from "@/test/render-with-daemon";

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 8; i++) await Promise.resolve();
  });
}

interface Setup {
  detail?: ProposalDetail;
  targets?: Record<number, LearnedSkillDetail>;
  fail?: Record<string, RpcError>;
}

function setup(s: Setup = {}) {
  const calls: { method: string; params: unknown }[] = [];
  let backs = 0;
  const d = learningDaemon((method, params) => {
    calls.push({ method, params });
    const failure = s.fail?.[method];
    if (failure) throw failure;
    if (method === "learning.proposals.get") return s.detail ?? { proposal: proposal({ id: 9 }) };
    if (method === "learning.skills.get") return s.targets?.[(params as { id: number }).id] ?? Promise.reject(new RpcError("not found"));
    return null;
  });
  renderWithDaemon(<ProposalReview id={9} onBack={() => backs++} />, d);
  return { calls, backs: () => backs, daemon: d };
}

const BODY = "# Tests\nRun go test.\nThen the suite.\n";
const TARGET: LearnedSkillDetail = {
  skill: skill({ id: 5, name: "run-tests", version: 2 }),
  versions: [
    { version: 1, description: "d", body: "# Tests\nRun go test.\n", actor: "reflector", createdAt: "2026-10-10T10:00:00Z" },
    { version: 2, description: "d", body: BODY, actor: "human", createdAt: "2026-10-12T10:00:00Z" },
  ],
  ledger: [],
};
const PATCH = proposal({
  id: 9,
  kind: "patch",
  name: "run-tests",
  items: [item({ action: "patch", name: "run-tests", oldString: "Then the suite.", newString: "Then the whole suite.", targetSkillId: 5, baseVersion: 2, reason: "clarify" })],
});

describe("proposal review actions", () => {
  it("shows a create proposal in full with its evidence and approves it", async () => {
    const v = setup();
    await flush();
    expect(screen.getByTestId("proposal-reason").textContent).toContain("asked twice");
    expect(screen.getByTestId("proposal-body").textContent).toContain("go test ./pkg/...");
    expect(within(screen.getByTestId("proposal-evidence")).getAllByTestId("evidence-item")).toHaveLength(2);
    fireEvent.click(screen.getByTestId("proposal-approve"));
    await flush();
    expect(v.calls.find((c) => c.method === "learning.proposals.approve")?.params).toEqual({ id: 9 });
    expect(v.backs()).toBe(1);
  });

  it("rejects with an optional note", async () => {
    const v = setup();
    await flush();
    fireEvent.click(screen.getByTestId("proposal-reject"));
    fireEvent.change(screen.getByTestId("proposal-reject-note"), { target: { value: "too specific to this repo" } });
    fireEvent.click(screen.getByTestId("proposal-reject-confirm"));
    await flush();
    expect(v.calls.find((c) => c.method === "learning.proposals.reject")?.params).toEqual({ id: 9, note: "too specific to this repo" });
    expect(v.backs()).toBe(1);
  });

  it("rejects without a note when none is typed", async () => {
    const v = setup();
    await flush();
    fireEvent.click(screen.getByTestId("proposal-reject"));
    fireEvent.click(screen.getByTestId("proposal-reject-confirm"));
    await flush();
    expect(v.calls.find((c) => c.method === "learning.proposals.reject")?.params).toEqual({ id: 9 });
  });

  it("edit-then-approve sends the edited description and body, with live limits", async () => {
    const v = setup();
    await flush();
    fireEvent.click(screen.getByTestId("proposal-edit-start"));
    const desc = screen.getByTestId("proposal-edit-description") as HTMLTextAreaElement;
    expect(desc.value).toBe("Run one package's tests before the whole suite.");
    fireEvent.change(desc, { target: { value: "Run the failing package first." } });
    fireEvent.change(screen.getByTestId("proposal-edit-body"), { target: { value: "# Focused\nRun it.\n" } });
    expect(screen.getByTestId("proposal-edit-body-count").textContent).toBe("2 / 25 lines");
    expect(screen.getByTestId("proposal-edit-description-count").textContent).toBe("30 / 160 chars");
    fireEvent.click(screen.getByTestId("proposal-approve-edited"));
    await flush();
    expect(v.calls.find((c) => c.method === "learning.proposals.approve")?.params).toEqual({
      id: 9,
      description: "Run the failing package first.",
      body: "# Focused\nRun it.\n",
    });
  });

  it("blocks approving edits that break the limits", async () => {
    setup();
    await flush();
    fireEvent.click(screen.getByTestId("proposal-edit-start"));
    fireEvent.change(screen.getByTestId("proposal-edit-body"), { target: { value: Array.from({ length: 26 }, (_, i) => `line ${i}`).join("\n") } });
    expect(screen.getByTestId("proposal-approve-edited")).toBeDisabled();
    expect(screen.getByTestId("proposal-edit-body-count").textContent).toBe("26 / 25 lines");
    fireEvent.change(screen.getByTestId("proposal-edit-body"), { target: { value: "ok" } });
    fireEvent.change(screen.getByTestId("proposal-edit-description"), { target: { value: "x".repeat(161) } });
    expect(screen.getByTestId("proposal-approve-edited")).toBeDisabled();
  });

  it("a gate_rejected approve lists every finding and the proposal stays pending", async () => {
    const v = setup({
      fail: {
        "learning.proposals.approve": new RpcError("gate rejected", "gate_rejected", {
          findings: [
            { reason: "secret", detail: "looks like an API key" },
            { reason: "injection", detail: "tells the agent to ignore instructions" },
          ],
        }),
      },
    });
    await flush();
    fireEvent.click(screen.getByTestId("proposal-approve"));
    await flush();
    const banner = screen.getByTestId("gate-findings");
    expect(banner.textContent).toContain("secret: looks like an API key");
    expect(banner.textContent).toContain("injection: tells the agent to ignore instructions");
    // Still reviewable and actionable, and we did not navigate away.
    expect(v.backs()).toBe(0);
    expect(screen.getByTestId("proposal-approve")).not.toBeDisabled();
  });

  it("a patch shows the diff against the target's current body", async () => {
    setup({ detail: { proposal: PATCH }, targets: { 5: TARGET } });
    await flush();
    const diff = screen.getByTestId("proposal-diff");
    expect(diff.textContent).toContain("Then the whole suite.");
    expect(diff.textContent).toContain("Then the suite.");
    expect(screen.queryByTestId("proposal-stale")).toBeNull();
    expect(screen.getByTestId("proposal-approve")).not.toBeDisabled();
  });

  it("stale: a changed target disables approve and says why", async () => {
    setup({ detail: { proposal: PATCH }, targets: { 5: { ...TARGET, skill: { ...TARGET.skill, version: 3 }, versions: [...TARGET.versions, { version: 3, description: "d", body: BODY + "More.\n", actor: "human", createdAt: "2026-10-13T10:00:00Z" }] } } });
    await flush();
    expect(screen.getByTestId("proposal-stale").textContent).toContain("changed since this was proposed");
    expect(screen.getByTestId("proposal-approve")).toBeDisabled();
    // Rejecting stays possible.
    expect(screen.getByTestId("proposal-reject")).not.toBeDisabled();
  });

  it("stale: a daemon `stale` error on approve disables further approves", async () => {
    setup({
      detail: { proposal: proposal({ id: 9 }) },
      fail: { "learning.proposals.approve": new RpcError("target changed", "stale") },
    });
    await flush();
    fireEvent.click(screen.getByTestId("proposal-approve"));
    await flush();
    expect(screen.getByTestId("proposal-stale").textContent).toContain("target changed");
    expect(screen.getByTestId("proposal-approve")).toBeDisabled();
  });

  it("a bundle shows the umbrella and the skills that will be archived", async () => {
    const bundle: ProposalView = proposal({
      id: 9,
      kind: "bundle",
      source: "curator",
      name: "testing-umbrella",
      items: [
        item({ action: "create", name: "testing-umbrella", description: "How to run tests here.", body: "# Testing\nAll of it.\n" }),
        item({ action: "delete", name: "run-go-tests", absorbedInto: "testing-umbrella", reason: "covered" }),
        item({ action: "delete", name: "run-pkg-tests", absorbedInto: "testing-umbrella" }),
      ],
    });
    setup({ detail: { proposal: bundle } });
    await flush();
    expect(screen.getByTestId("proposal-body").textContent).toContain("All of it.");
    const archived = screen.getByTestId("proposal-archived");
    expect(within(archived).getAllByTestId("proposal-item-delete")).toHaveLength(2);
    expect(archived.textContent).toContain("run-go-tests is archived, merged into testing-umbrella");
  });

  it("a decided proposal is read-only", async () => {
    setup({ detail: { proposal: proposal({ id: 9, status: "rejected", note: "not useful" }) } });
    await flush();
    expect(screen.getByTestId("proposal-status").textContent).toBe("rejected");
    expect(screen.queryByTestId("proposal-actions")).toBeNull();
    expect(screen.getByText("Note: not useful")).toBeTruthy();
  });
});
