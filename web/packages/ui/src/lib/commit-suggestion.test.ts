import { describe, expect, it } from "vitest";

import { suggestCommitMessage } from "@/lib/commit-suggestion";
import type { TaskFile } from "@/lib/types";

function file(path: string, status: TaskFile["status"], staged = true): TaskFile {
  return { path, status, staged };
}

describe("suggestCommitMessage", () => {
  it("names the file for a single staged addition", () => {
    expect(suggestCommitMessage([file("src/feature.ts", "added")])).toBe("Add src/feature.ts");
  });

  it("names the file for a single staged modification", () => {
    expect(suggestCommitMessage([file("src/feature.ts", "modified")])).toBe("Update src/feature.ts");
  });

  it("counts without claiming a kind for a mixed multi-file change", () => {
    const files = [
      file("src/a.ts", "added"),
      file("src/b.ts", "modified"),
      file("src/c.ts", "deleted"),
      file("src/d.ts", "modified"),
    ];
    expect(suggestCommitMessage(files)).toBe("Update 4 files");
  });

  it("names the kind for a uniform multi-file change", () => {
    expect(suggestCommitMessage([file("a.ts", "added"), file("b.ts", "added")])).toBe("Add 2 files");
  });

  it("suggests nothing when nothing is staged", () => {
    expect(
      suggestCommitMessage([
        file("a.ts", "modified", false),
        file("b.ts", "added", false),
      ]),
    ).toBeNull();
    expect(suggestCommitMessage([])).toBeNull();
  });

  it("falls back to Update for a status it doesn't know", () => {
    expect(suggestCommitMessage([file("old.ts", "renamed")])).toBe("Update old.ts");
  });
});
