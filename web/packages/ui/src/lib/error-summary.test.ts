import { describe, expect, it } from "vitest";

import { summarizeError } from "@/lib/error-summary";

describe("summarizeError", () => {
  it("drops the gh argv and keeps the tool's own reason as one line", () => {
    const raw =
      "task.createPr: create pr 1: gh pr create: gh [pr create --base develop --head smind/task-x --title Add logging --body Opened from smind task #1 via task.createPr.]: exit status 1: none of the git remotes configured for this repository point to a known GitHub host. To tell gh about a new GitHub host, please use `gh auth login`";
    const { summary, details } = summarizeError(raw);
    expect(summary).toBe("none of the git remotes configured for this repository point to a known GitHub host.");
    expect(summary).not.toContain("--base");
    expect(details).toBe(raw);
  });

  it("uses the first line of a plain message and keeps the rest as details", () => {
    const { summary, details } = summarizeError("push failed\nremote: permission denied\nfatal: unable to access");
    expect(summary).toBe("push failed");
    expect(details).toContain("permission denied");
  });

  it("offers no details when the message is already one short line", () => {
    expect(summarizeError("daemon said no")).toEqual({ summary: "daemon said no", details: null });
  });

  it("truncates a very long single line", () => {
    const { summary } = summarizeError("x".repeat(400));
    expect(summary.length).toBeLessThanOrEqual(160);
    expect(summary.endsWith("…")).toBe(true);
  });
});
