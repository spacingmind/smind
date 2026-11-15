import { describe, expect, it } from "vitest";

import { gitActionEntries, primaryGitAction } from "@/components/git/git-actions-model";
import { gitState, openPr } from "@/components/git/pr-test-support";

const off = (reason: string, code = "x") => ({ enabled: false, code, reason });
const ids = (g: ReturnType<typeof gitState>, opts = {}) => gitActionEntries(g, opts).map((e) => e.id);
const primary = (g: ReturnType<typeof gitState>) => primaryGitAction(gitActionEntries(g))?.id;

describe("git action entries and the primary action", () => {
  it("lists the actions in flow order and leaves out the ones the daemon does not report", () => {
    const g = gitState({ origin: { published: true }, actions: { push: { enabled: true }, updateFromBase: { enabled: true }, createPr: { enabled: true }, updatePr: off("no PR") } });
    expect(ids(g)).toEqual(["push", "update", "create-pr"]);
    expect(ids(gitState({ actions: { createPr: { enabled: true } } }))).toEqual(["create-pr"]);
    expect(gitActionEntries(undefined)).toEqual([]);
  });

  it("diff-primary-git-action-follows-task-state: the next sensible step", () => {
    // ready for review: Create PR
    expect(primary(gitState({ origin: { published: true }, actions: { push: off("nothing to push"), updateFromBase: off("up to date"), createPr: { enabled: true }, updatePr: off("no PR") } }))).toBe("create-pr");
    // unpublished commits and no PR possible yet: Publish branch
    expect(primary(gitState({ origin: { published: false }, actions: { push: { enabled: true }, updateFromBase: off("up to date"), createPr: off("Publish the branch first"), updatePr: off("no PR") } }))).toBe("push");
    // behind its base and nothing else enabled: Update
    expect(primary(gitState({ origin: { published: true }, actions: { push: off("nothing to push"), updateFromBase: { enabled: true }, createPr: off("no commits"), updatePr: off("no PR") } }))).toBe("update");
    // an open PR: Update PR
    expect(primary(gitState({ pr: openPr(), origin: { published: true }, actions: { push: off("x"), updateFromBase: off("y"), createPr: off("pr exists"), updatePr: { enabled: true } } }))).toBe("update-pr");
    // merged: Archive, and View PR is the only other entry
    const merged = gitState({ pr: openPr({ state: "merged" }), merged: { state: "merged", via: "pr" }, actions: { push: { enabled: true }, createPr: off("x"), updatePr: off("y") } });
    expect(ids(merged)).toEqual(["archive", "view-pr"]);
    expect(primary(merged)).toBe("archive");
    expect(ids(gitState({ merged: { state: "merged", via: "ancestor" } }))).toEqual(["archive"]);
  });

  it("with nothing enabled the primary is the first step in flow order, disabled, never View PR", () => {
    const g = gitState({ pr: openPr(), origin: { published: true }, actions: { push: off("nothing to push"), updateFromBase: off("up to date"), createPr: off("pr exists"), updatePr: off("PR is current") } });
    const entries = gitActionEntries(g);
    const p = primaryGitAction(entries)!;
    expect(p.id).toBe("push");
    expect(p.enabled).toBe(false);
    expect(p.reason).toBe("nothing to push");
  });

  it("labels follow the branch: Publish branch until it has an upstream, then Push; Update from the base's name", () => {
    const base = { push: { enabled: true }, updateFromBase: { enabled: true } };
    expect(gitActionEntries(gitState({ origin: { published: false }, actions: base })).map((e) => e.label)).toEqual(["Publish branch", "Update from develop"]);
    expect(gitActionEntries(gitState({ origin: { published: true }, actions: base })).map((e) => e.label)).toEqual(["Push", "Update from develop"]);
  });

  it("an updating PR disables Update PR and says so; a non-https PR url offers no View PR", () => {
    const g = gitState({ pr: openPr(), actions: { updatePr: { enabled: true } } });
    const e = gitActionEntries(g, { updatingPr: true }).find((x) => x.id === "update-pr")!;
    expect(e.enabled).toBe(false);
    expect(e.label).toBe("Updating…");
    expect(ids(gitState({ pr: openPr({ url: "javascript:alert(1)" }), actions: { updatePr: { enabled: true } } }))).toEqual(["update-pr"]);
  });
});
