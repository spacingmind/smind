import { describe, expect, it } from "vitest";

import { childrenOf, pillStatusWithSubtree, rateLimitHint, statusSummary, usageChipText, type TaskTreeNode, type TaskTreeCounts, type TreeUsageTotals } from "@/lib/task-lineage";

function counts(partial: Partial<TaskTreeCounts> = {}): TaskTreeCounts {
  return { running: 0, waitingPermission: 0, failed: 0, done: 0, idle: 0, ...partial };
}

function usage(partial: Partial<TreeUsageTotals> = {}): TreeUsageTotals {
  return { runs: 0, inputTokens: null, outputTokens: null, cacheReadTokens: null, cacheWriteTokens: null, reasoningTokens: null, costUsd: null, ...partial };
}

function node(partial: Partial<TaskTreeNode> & { id: number }): TaskTreeNode {
  return {
    parentId: null,
    workspaceId: 1,
    title: `task ${partial.id}`,
    depth: 0,
    taskStatus: "active",
    tier: null,
    childCount: 0,
    run: { state: "idle", runningRunIds: [], pendingPermissions: 0 },
    counts: counts(),
    usage: { own: usage(), subtree: usage(), archivedDescendants: 0 },
    ...partial,
  };
}

describe("statusSummary", () => {
  it("statusSummary lists running, needs you, failed and done in order", () => {
    expect(statusSummary(counts({ running: 2, waitingPermission: 1, failed: 1, done: 3 }))).toBe(
      "2 running · 1 needs you · 1 failed · 3 done",
    );
  });

  it("statusSummary omits zero parts and idle", () => {
    expect(statusSummary(counts({ running: 1, idle: 5 }))).toBe("1 running");
    expect(statusSummary(counts({ done: 2 }))).toBe("2 done");
    expect(statusSummary(counts({ idle: 4 }))).toBe("");
  });
});

describe("usageChipText", () => {
  it("usageChipText is null when nothing is reported and never shows 0 for unreported", () => {
    expect(usageChipText(usage())).toBeNull();
    // Zero runs means "nothing ran", not "0 tokens reported".
    expect(usageChipText(usage({ runs: 0, costUsd: 1 }))).toBeNull();
    // Only a cost reported: tokens stay an em dash, never 0.
    expect(usageChipText(usage({ runs: 2, costUsd: 0.5 }))).toBe("$0.500 · — tok");
  });

  it("usageChipText sums input and output when only one is reported", () => {
    expect(usageChipText(usage({ runs: 1, inputTokens: 1200 }))).toBe("— · 1.2k tok");
    expect(usageChipText(usage({ runs: 3, inputTokens: 700, outputTokens: 300 }))).toBe("— · 1k tok");
  });
});

describe("childrenOf", () => {
  it("childrenOf returns only direct children in order", () => {
    const root = node({ id: 1 });
    const a = node({ id: 2, parentId: 1 });
    const grandchild = node({ id: 4, parentId: 2 });
    const b = node({ id: 3, parentId: 1 });
    expect(childrenOf([root, a, grandchild, b], 1)).toEqual([a, b]);
    expect(childrenOf([root, a, grandchild, b], 2)).toEqual([grandchild]);
    expect(childrenOf([root], 1)).toEqual([]);
  });
});

describe("pillStatusWithSubtree", () => {
  it("rate limited node pill is warning not danger", () => {
    const limited = node({ id: 3, run: { state: "error", runningRunIds: [], pendingPermissions: 0, failureKind: "rate_limited", window: "5h", resetAt: "2030-01-01T17:35:00Z" } });
    expect(pillStatusWithSubtree(limited)).toBe("warning");
    // A plain error, or a failure kind this client does not know, stays danger.
    expect(pillStatusWithSubtree(node({ id: 4, run: { state: "error", runningRunIds: [], pendingPermissions: 0 } }))).toBe("danger");
    expect(pillStatusWithSubtree(node({ id: 5, run: { state: "error", runningRunIds: [], pendingPermissions: 0, failureKind: "other" } }))).toBe("danger");
    // A descendant's failure still outranks it.
    expect(pillStatusWithSubtree({ ...limited, counts: counts({ failed: 1 }) })).toBe("danger");
  });

  it("a parent whose only failed descendant is rate limited is warning, not danger", () => {
    const limitedRun = { state: "error" as const, runningRunIds: [], pendingPermissions: 0, failureKind: "rate_limited" };
    const plainRun = { state: "error" as const, runningRunIds: [], pendingPermissions: 0 };
    const parent = node({ id: 1, counts: counts({ failed: 1 }) });
    const limitedKid = node({ id: 2, parentId: 1, run: limitedRun });
    const plainKid = node({ id: 3, parentId: 1, run: plainRun });
    expect(pillStatusWithSubtree(parent, [parent, limitedKid])).toBe("warning");
    // A plain failure beside it, or no node list to tell them apart, stays danger.
    expect(pillStatusWithSubtree({ ...parent, counts: counts({ failed: 2 }) }, [parent, limitedKid, plainKid])).toBe("danger");
    expect(pillStatusWithSubtree(parent)).toBe("danger");
    // The parent's own plain failure outranks a rate-limited child.
    expect(pillStatusWithSubtree({ ...parent, run: plainRun }, [parent, limitedKid])).toBe("danger");
    // A grandchild counts too.
    const mid = node({ id: 4, parentId: 1 });
    const grandKid = node({ id: 5, parentId: 4, run: limitedRun });
    expect(pillStatusWithSubtree(parent, [parent, mid, grandKid])).toBe("warning");
  });

  it("rateLimitHint words the reset in local time and is null for any other node", () => {
    const now = new Date(2026, 9, 13, 10, 0, 0);
    const run = { state: "error" as const, runningRunIds: [], pendingPermissions: 0, failureKind: "rate_limited" };
    expect(rateLimitHint(node({ id: 3, run: { ...run, resetAt: new Date(2026, 9, 13, 17, 35).toISOString() } }), now)).toBe("Rate limited · resets 17:35");
    expect(rateLimitHint(node({ id: 3, run }), now)).toBe("Rate limited · reset time unknown");
    expect(rateLimitHint(node({ id: 3, run: { ...run, resetAt: new Date(2026, 9, 13, 9, 0).toISOString() } }), now)).toBe("Rate limited · limit has reset");
    expect(rateLimitHint(node({ id: 4, run: { state: "error", runningRunIds: [], pendingPermissions: 0 } }), now)).toBeNull();
  });

  it("pill shows warning when a descendant needs permission", () => {
    expect(pillStatusWithSubtree(node({ id: 3, counts: counts({ waitingPermission: 1 }) }))).toBe("warning");
  });

  it("pill shows danger when a descendant failed", () => {
    expect(
      pillStatusWithSubtree(node({ id: 3, counts: counts({ failed: 1, waitingPermission: 1, running: 2 }) })),
    ).toBe("danger");
  });

  it("pill uses own state for a leaf", () => {
    expect(pillStatusWithSubtree(node({ id: 3, run: { state: "done", runningRunIds: [], pendingPermissions: 0 } }))).toBe("success");
    expect(pillStatusWithSubtree(node({ id: 3, run: { state: "waiting_permission", runningRunIds: [], pendingPermissions: 1 } }))).toBe("warning");
  });

  it("a done leaf cut off at a cap is a warning, not a calm success", () => {
    const capped = node({ id: 3, run: { state: "done", runningRunIds: [], pendingPermissions: 0, disposition: "needs-continue" } });
    expect(pillStatusWithSubtree(capped)).toBe("warning");
    // ...but only when it is done: a failure stays red, a running run stays running.
    expect(pillStatusWithSubtree(node({ id: 4, run: { state: "error", runningRunIds: [], pendingPermissions: 0, disposition: "needs-continue" } }))).toBe("danger");
    expect(pillStatusWithSubtree(node({ id: 5, run: { state: "done", runningRunIds: [], pendingPermissions: 0 } }))).toBe("success");
  });

  it("danger outranks warning outranks running", () => {
    const waiting = node({ id: 3, counts: counts({ waitingPermission: 1 }) });
    expect(pillStatusWithSubtree(waiting)).toBe("warning");
    expect(pillStatusWithSubtree(node({ id: 4, counts: counts({ failed: 1, waitingPermission: 1 }) }))).toBe("danger");
    expect(pillStatusWithSubtree(node({ id: 5, counts: counts({ running: 1 }) }))).toBe("running");
    // An own running run also promotes an otherwise-idle pill.
    expect(pillStatusWithSubtree(node({ id: 6, run: { state: "running", runningRunIds: ["r1"], pendingPermissions: 0 } }))).toBe("running");
  });
});
