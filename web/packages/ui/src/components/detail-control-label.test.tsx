import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

import { DETAIL_LEVEL_LABELS } from "@/components/timeline/detail-level";

const taskDetail = readFileSync(resolve(__dirname, "task-detail.tsx"), "utf8");

describe("transcript detail control", () => {
  it("detail-control-reads-as-the-transcript-detail-level", () => {
    // The header control is a sentence fragment, not a bare word: "Detail: Auto".
    expect(taskDetail).toContain("Detail: {DETAIL_LEVEL_LABELS[detailLevel]}");
    expect(taskDetail).toContain("Icons.chevronDown");
    expect(DETAIL_LEVEL_LABELS).toMatchObject({ auto: "Auto", detailed: "Detailed", overview: "Overview" });
  });
});
