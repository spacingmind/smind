import { afterEach, describe, expect, it } from "vitest";

import {
  REVIEW_DRAFTS_STORAGE_KEY,
  addReviewDraft,
  buildReviewPrompt,
  clearReviewDrafts,
  getReviewDrafts,
  removeReviewDraft,
  resetReviewDrafts,
} from "@/lib/review-drafts";

afterEach(() => {
  resetReviewDrafts();
  window.localStorage.clear();
});

const DRAFT = { path: "file.txt", line: 2, side: "new" as const, snippet: "line2 added", body: "why?" };

describe("review drafts store", () => {
  it("keeps drafts per task, so one task's review can't be submitted against another's", () => {
    addReviewDraft(1, DRAFT);
    addReviewDraft(2, { ...DRAFT, path: "other.txt", body: "different" });

    expect(getReviewDrafts(1)).toHaveLength(1);
    expect(getReviewDrafts(2)).toHaveLength(1);
    expect(getReviewDrafts(1)[0]!.body).toBe("why?");
    expect(getReviewDrafts(3)).toEqual([]);
  });

  it("returns the identical empty array for a task with no drafts (useSyncExternalStore would loop otherwise)", () => {
    expect(getReviewDrafts(9)).toBe(getReviewDrafts(9));
  });

  it("removes one draft and clears a whole task", () => {
    const a = addReviewDraft(1, DRAFT);
    addReviewDraft(1, { ...DRAFT, line: 5, body: "second" });

    removeReviewDraft(1, a.id);
    expect(getReviewDrafts(1).map((d) => d.body)).toEqual(["second"]);

    clearReviewDrafts(1);
    expect(getReviewDrafts(1)).toEqual([]);
  });

  it("mirrors to localStorage so drafts survive a reload, not just a tab switch", () => {
    addReviewDraft(1, DRAFT);
    const raw = window.localStorage.getItem(REVIEW_DRAFTS_STORAGE_KEY);
    expect(raw).toBeTruthy();
    expect(JSON.parse(raw!)["1"]).toHaveLength(1);
  });
});

describe("buildReviewPrompt", () => {
  it("renders one prompt grouped by file, quoting each commented line", () => {
    const drafts = [
      { id: "a", path: "file.txt", line: 2, side: "new" as const, snippet: "  line2 added", body: "why?" },
      { id: "b", path: "file.txt", line: 7, side: "old" as const, snippet: "gone", body: "restore this" },
      { id: "c", path: "other.txt", line: null, side: "new" as const, snippet: "", body: "whole file comment" },
    ];

    const prompt = buildReviewPrompt(drafts);

    expect(prompt).toContain("Please address the following review comments");
    // Grouped: one heading per file, not one per comment.
    expect(prompt.match(/^### file\.txt$/gm)).toHaveLength(1);
    expect(prompt).toContain("### other.txt");
    expect(prompt).toContain("- file.txt:2");
    expect(prompt).toContain("> line2 added");
    expect(prompt).toContain("restore this");
    // A line-less comment references the file, not "file:null".
    expect(prompt).toContain("- other.txt\n  whole file comment");
    expect(prompt).not.toContain("null");
  });
});
