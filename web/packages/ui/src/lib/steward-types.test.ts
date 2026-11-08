import { describe, expect, it } from "vitest";

import {
  axisLabel,
  axisStatusLabel,
  blockedReasonLabel,
  findingsAsPrompt,
  groupFindingsByAxisThenFile,
  groupFindingsByFile,
  sortGatesFailingFirst,
  verdictLabel,
  type StewardFinding,
  type StewardGate,
  type StewardReview,
} from "@/lib/steward-types";

function review(overrides: Partial<StewardReview> = {}): StewardReview {
  return {
    id: 7,
    taskId: 42,
    triggerKind: "human",
    headSha: "0123456789abcdef",
    validationId: 3,
    ciState: "success",
    status: "done",
    verdict: "CHANGES",
    findings: [],
    gates: [],
    mergeReady: false,
    costUsd: 0.4,
    createdAt: "2026-11-01T00:00:00Z",
    ...overrides,
  };
}

describe("verdictLabel", () => {
  it("verdictLabel unknown value renders as text", () => {
    expect(verdictLabel("ACCEPTED")).toEqual({ text: "Accepted", tone: "success" });
    expect(verdictLabel("CHANGES")).toEqual({ text: "Changes requested", tone: "warning" });
    expect(verdictLabel("BLOCKED")).toEqual({ text: "Blocked", tone: "danger" });
    expect(verdictLabel("SOMETHING_NEW")).toEqual({ text: "SOMETHING_NEW", tone: "neutral" });
    expect(verdictLabel(undefined)).toEqual({ text: "Pending", tone: "neutral" });
  });
});

describe("sortGatesFailingFirst", () => {
  it("gates sort failing first and stable", () => {
    const gates: StewardGate[] = [
      { id: "ci", state: "pass" },
      { id: "not_stacked", state: "fail", detail: "carries 2 commits of another PR" },
      { id: "no_secrets", state: "fail" },
      { id: "frames_fresh", state: "n/a" },
      { id: "peer_claims", state: "unknown" },
    ];
    expect(sortGatesFailingFirst(gates).map((g) => g.id)).toEqual([
      "not_stacked",
      "no_secrets",
      "ci",
      "frames_fresh",
      "peer_claims",
    ]);
    expect(gates.map((g) => g.id)).toEqual(["ci", "not_stacked", "no_secrets", "frames_fresh", "peer_claims"]);
  });
});

describe("groupFindingsByFile", () => {
  it("findings group by file", () => {
    const findings: StewardFinding[] = [
      { file: "b.go", line: 30, why: "w3", check: "c3" },
      { file: "a.go", line: 12, why: "w1", check: "c1" },
      { file: "b.go", line: 4, why: "w2", check: "c2" },
    ];
    expect(groupFindingsByFile(findings)).toEqual([
      { file: "b.go", findings: [
        { file: "b.go", line: 4, why: "w2", check: "c2" },
        { file: "b.go", line: 30, why: "w3", check: "c3" },
      ] },
      { file: "a.go", findings: [{ file: "a.go", line: 12, why: "w1", check: "c1" }] },
    ]);
    expect(groupFindingsByFile([])).toEqual([]);
  });
});

describe("findingsAsPrompt", () => {
  it("findings prompt lists every finding", () => {
    const prompt = findingsAsPrompt(
      review({
        findings: [
          { file: "a.go", line: 12, why: "unquoted input reaches a shell", check: "run the mutation check", severity: "high" },
          { file: "b.ts", line: 3, why: "test asserts nothing", check: "task test" },
        ],
      }),
    );
    const lines = prompt.split("\n");
    expect(lines).toHaveLength(3);
    expect(lines[0]).toBe("Steward review 7 of task 42 at 0123456: Changes requested");
    expect(lines[1]).toBe("- a.go:12 [high] unquoted input reaches a shell (check: run the mutation check)");
    expect(lines[2]).toBe("- b.ts:3 test asserts nothing (check: task test)");
  });

  it("findings prompt says so when there are none", () => {
    expect(findingsAsPrompt(review({ verdict: undefined }))).toBe(
      "Steward review 7 of task 42 at 0123456: Pending\nNo findings.",
    );
  });
});

describe("axis helpers", () => {
  const ax = (status: string, seatVerdict?: string, blockedReason?: string) => ({ axis: "spec", status, seatVerdict, blockedReason, costUsd: 0 });

  it("axisStatusLabel maps every status", () => {
    expect(axisStatusLabel(ax("done", "ACCEPTED"))).toEqual({ text: "Accepted", tone: "success" });
    expect(axisStatusLabel(ax("done", "CHANGES"))).toEqual({ text: "Changes requested", tone: "warning" });
    expect(axisStatusLabel(ax("done", "BLOCKED"))).toEqual({ text: "Blocked", tone: "danger" });
    expect(axisStatusLabel(ax("running"))).toEqual({ text: "Running", tone: "neutral" });
    expect(axisStatusLabel(ax("pending"))).toEqual({ text: "Pending", tone: "neutral" });
    expect(axisStatusLabel(ax("skipped", undefined, "no_spec"))).toEqual({ text: "Skipped · no spec available", tone: "neutral" });
    expect(axisStatusLabel(ax("skipped"))).toEqual({ text: "Skipped", tone: "neutral" });
    expect(axisStatusLabel(ax("blocked", undefined, "timeout"))).toEqual({ text: "Did not complete", tone: "danger" });
  });

  it("blockedReasonLabel falls back to raw text", () => {
    expect(blockedReasonLabel("timeout")).toBe("Timed out");
    expect(blockedReasonLabel("budget")).toBe("Over its budget");
    expect(blockedReasonLabel("crashed")).toBe("Crashed");
    expect(blockedReasonLabel("cancelled")).toBe("Cancelled");
    expect(blockedReasonLabel("scope_violation")).toBe("Scope violation");
    expect(blockedReasonLabel("not_run")).toBe("Not run");
    expect(blockedReasonLabel("no_spec")).toBe("No spec available");
    expect(blockedReasonLabel("exotic")).toBe("exotic");
  });

  const f = (file: string, line: number, axis?: string): StewardFinding => ({ file, line, why: "w", check: "c", axis });

  it("groupFindingsByAxisThenFile orders standards before spec", () => {
    const groups = groupFindingsByAxisThenFile([f("z.go", 1), f("b.go", 9, "spec"), f("a.go", 5, "standards"), f("a.go", 2, "standards")]);
    expect(groups.map((g) => g.axis)).toEqual(["standards", "spec", undefined]);
    expect(groups[0].files[0].findings.map((x) => x.line)).toEqual([2, 5]);
  });

  it("findingsAsPrompt is unchanged without an axis", () => {
    const r = review({ findings: [{ file: "a.go", line: 3, why: "bad", check: "task test", severity: "high", rule: "ignored" }] });
    expect(findingsAsPrompt(r)).toBe("Steward review 7 of task 42 at 0123456: Changes requested\n- a.go:3 [high] bad (check: task test)");
  });

  it("findingsAsPrompt tags a finding with its axis and rule", () => {
    const r = review({ findings: [{ file: "a.go", line: 3, why: "bad", check: "task test", axis: "standards", rule: "no-raw-sql" }] });
    expect(findingsAsPrompt(r)).toContain("- a.go:3 [standards] bad (check: task test) rule: no-raw-sql");
  });

  it("unknown axis and status never throw", () => {
    expect(axisLabel("novel")).toBe("novel");
    expect(axisStatusLabel({ status: "weird" })).toEqual({ text: "weird", tone: "neutral" });
    expect(axisStatusLabel({ status: "done", seatVerdict: "ODD" }).text).toBe("ODD");
    expect(() => groupFindingsByAxisThenFile([f("a", 1, "novel")])).not.toThrow();
  });
});
