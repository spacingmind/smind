import { describe, expect, it } from "vitest";

import { prChipModel } from "@/components/git/pr-chip-model";
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

describe("prChipModel", () => {
  it("labels each state", () => {
    expect(prChipModel(pr(), NOW).label).toBe("#42 · Open");
    expect(prChipModel(pr({ draft: true }), NOW).label).toBe("#42 · Draft");
    expect(prChipModel(pr({ state: "merged", checks: { state: "failure", passed: 0, failed: 3, pending: 0, total: 3 } }), NOW).label).toBe("#42 · Merged");
    expect(prChipModel(pr({ state: "closed", checks: { state: "failure", passed: 0, failed: 3, pending: 0, total: 3 } }), NOW).label).toBe("#42 · Closed");
  });

  it("appends exactly one segment by priority", () => {
    const both = pr({
      checks: { state: "failure", passed: 1, failed: 2, pending: 0, total: 3 },
      reviewDecision: "changes_requested",
      mergeable: "conflicting",
    });
    expect(prChipModel(both, NOW).label).toBe("#42 · Open · ✗ 2 failing");
    expect(
      prChipModel(pr({ reviewDecision: "changes_requested", mergeable: "conflicting", checks: { state: "pending", passed: 0, failed: 0, pending: 2, total: 2 } }), NOW).label,
    ).toBe("#42 · Open · Changes requested");
    expect(
      prChipModel(pr({ mergeable: "conflicting", checks: { state: "pending", passed: 0, failed: 0, pending: 2, total: 2 } }), NOW).label,
    ).toBe("#42 · Open · Conflicts");
    expect(prChipModel(pr({ checks: { state: "pending", passed: 0, failed: 0, pending: 2, total: 2 } }), NOW).label).toBe("#42 · Open · checks running");
    expect(prChipModel(pr({ checks: { state: "success", passed: 3, failed: 0, pending: 0, total: 3 } }), NOW).label).toBe("#42 · Open · ✓ checks");
    expect(prChipModel(pr({ checks: { state: "none", passed: 0, failed: 0, pending: 0, total: 0 } }), NOW).label).toBe("#42 · Open");
  });

  it("marks only failing/blocking as danger", () => {
    expect(prChipModel(pr({ checks: { state: "failure", passed: 0, failed: 1, pending: 0, total: 1 } }), NOW).tone).toBe("danger");
    expect(prChipModel(pr({ reviewDecision: "changes_requested" }), NOW).tone).toBe("danger");
    expect(prChipModel(pr({ mergeable: "conflicting" }), NOW).tone).toBe("danger");
    expect(prChipModel(pr({ checks: { state: "pending", passed: 0, failed: 0, pending: 1, total: 1 } }), NOW).tone).toBe("neutral");
    expect(prChipModel(pr({ checks: { state: "success", passed: 1, failed: 0, pending: 0, total: 1 } }), NOW).tone).toBe("neutral");
    // A closed/merged PR never carries a segment, so never danger.
    expect(prChipModel(pr({ state: "closed", checks: { state: "failure", passed: 0, failed: 5, pending: 0, total: 5 } }), NOW).tone).toBe("neutral");
  });

  it("never throws on missing checks or unknown values", () => {
    expect(() => prChipModel(pr({ checks: undefined, reviewDecision: undefined as unknown as TaskGitPr["reviewDecision"], mergeable: undefined as unknown as TaskGitPr["mergeable"] }), NOW)).not.toThrow();
    const m = prChipModel(pr({ state: "weird" as unknown as TaskGitPr["state"], stale: "future" as unknown as TaskGitPr["stale"], fetchedAt: "not a date", title: undefined, repo: undefined }), NOW);
    expect(m.label).toBe("#42 · Open");
    expect(m.detail).toContain(" — ");
  });

  it("explains a stale snapshot", () => {
    const fiveMinAgo = new Date(NOW - 5 * 60 * 1000).toISOString();
    const m = prChipModel(pr({ fetchedAt: fiveMinAgo, stale: "rate_limited" }), NOW);
    expect(m.detail).toContain("updated 5 min ago");
    expect(m.detail).toContain("GitHub is rate-limiting requests; showing the last known state.");
    const fresh = prChipModel(pr({ stale: "backoff" }), NOW);
    expect(fresh.detail).not.toContain("min ago");
    expect(fresh.detail).toContain("GitHub isn't answering; showing the last known state.");
  });

  it("shows the age in the label of a stale snapshot, not only in the tooltip", () => {
    const nineMinAgo = new Date(NOW - 9 * 60 * 1000).toISOString();
    expect(prChipModel(pr({ fetchedAt: nineMinAgo, stale: "budget" }), NOW).label).toBe("#42 · Open · 9 min ago");
    // fresh or merely old-but-not-stale snapshots keep the plain label
    expect(prChipModel(pr({ fetchedAt: nineMinAgo, stale: "" }), NOW).label).toBe("#42 · Open");
  });

  it("clips a long title", () => {
    const m = prChipModel(pr({ title: "x".repeat(300) }), NOW);
    expect(m.detail.startsWith("x".repeat(119) + "…")).toBe(true);
    expect(m.detail.length).toBeLessThan(300);
  });
});
