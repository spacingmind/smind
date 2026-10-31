import { describe, expect, it } from "vitest";

import { diffLabels, MAX_HOST_LABELS, rowsFromLabels, validateLabelRows } from "@/lib/fleet-labels";

// The same table as TestFleetLabelsValidate in internal/fleet/registry/labels_test.go: the editor must accept and
// refuse exactly what the daemon does, so a label the editor lets through is not refused on Save for a rule it knew.
const LONG = "a".repeat(33);

describe("fleet labels validation mirrors the daemon", () => {
  const bad: Record<string, { key: string; value: string }[]> = {
    "17 labels": Array.from({ length: 17 }, (_, i) => ({ key: `k${i}`, value: "v" })),
    "upper key": [{ key: "OS", value: "linux" }],
    "digit key": [{ key: "9x", value: "linux" }],
    "long key": [{ key: LONG, value: "linux" }],
    "secret-ish key": [{ key: "api-token", value: "x" }],
    "upper value": [{ key: "os", value: "Windows" }],
    "long value": [{ key: "os", value: LONG }],
    "space value": [{ key: "os", value: "a b" }],
    "empty key": [{ key: "", value: "x" }],
    "empty value": [{ key: "os", value: "" }],
    "duplicate key": [
      { key: "os", value: "linux" },
      { key: "os", value: "windows" },
    ],
  };

  it("labels_validation_matches_daemon_table", () => {
    for (const [name, rows] of Object.entries(bad)) {
      expect(validateLabelRows(rows).ok, name).toBe(false);
    }
    expect(validateLabelRows(Array.from({ length: MAX_HOST_LABELS }, (_, i) => ({ key: `k${i}`, value: "v" }))).ok).toBe(true);
    expect(
      validateLabelRows([
        { key: "os", value: "windows" },
        { key: "arch", value: "arm64" },
        { key: "gpu", value: "cuda" },
        { key: "cuda.ver", value: "12.4+r1" },
      ]).ok,
    ).toBe(true);
    expect(validateLabelRows([]).ok).toBe(true);
  });

  it("names the problem per row and for the list", () => {
    const v = validateLabelRows([
      { key: "os", value: "linux" },
      { key: "os", value: "Windows" },
      { key: "my-token", value: "x" },
    ]);
    expect(v.rows[0]).toEqual({ key: null, value: null });
    expect(v.rows[1].key).toBe("Duplicate key");
    expect(v.rows[1].value).toMatch(/a-z/);
    expect(v.rows[2].key).toMatch(/secret/);
    expect(validateLabelRows(bad["17 labels"]).list).toBe("At most 16 labels");
  });
});

describe("label diff", () => {
  it("labels_diff_sends_only_changes", () => {
    const before = { os: "linux", gpu: "cuda", arch: "x64" };
    expect(diffLabels(before, rowsFromLabels(before))).toEqual({ set: {}, remove: [] });
    expect(
      diffLabels(before, [
        { key: "os", value: "windows" }, // changed
        { key: "gpu", value: "cuda" }, // same
        { key: "zone", value: "a" }, // new
        // arch dropped
      ]),
    ).toEqual({ set: { os: "windows", zone: "a" }, remove: ["arch"] });
    // renaming a key is a remove plus a set
    expect(diffLabels({ os: "linux" }, [{ key: "kind", value: "linux" }])).toEqual({ set: { kind: "linux" }, remove: ["os"] });
    expect(diffLabels(undefined, [{ key: "os", value: "linux" }])).toEqual({ set: { os: "linux" }, remove: [] });
  });

  it("rowsFromLabels sorts by key", () => {
    expect(rowsFromLabels({ b: "2", a: "1" })).toEqual([
      { key: "a", value: "1" },
      { key: "b", value: "2" },
    ]);
    expect(rowsFromLabels(undefined)).toEqual([]);
  });
});
