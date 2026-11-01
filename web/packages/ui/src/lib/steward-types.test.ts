import { describe, expect, it } from "vitest";

import {
  findingsAsPrompt,
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
