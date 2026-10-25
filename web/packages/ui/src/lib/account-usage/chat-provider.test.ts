import { describe, expect, it } from "vitest";

import { summaryWindows } from "@/lib/account-usage/chat-provider";
import { claudeAccount, problemAccount } from "@/test/account-usage-fixtures";

describe("summaryWindows", () => {
  it("uses the summary-flagged windows, else the first with a percent, never throws on a reshaped report", () => {
    expect(summaryWindows(claudeAccount()).map((w) => w.id)).toEqual(["five_hour", "weekly"]);
    const account = claudeAccount();
    const windows = account.report!.windows!.map((w) => ({ ...w, summary: false }));
    expect(summaryWindows({ ...account, report: { ...account.report!, windows } }).map((w) => w.id)).toEqual(["five_hour"]);
    expect(summaryWindows(problemAccount("expired"))).toEqual([]);
    expect(summaryWindows({ ...account, report: { ...account.report!, windows: null as never } })).toEqual([]);
  });

  it("keeps_a_percentless_window_only_when_its_status_limits", () => {
    const account = claudeAccount();
    const [five, weekly] = account.report!.windows!;
    const rejected = { id: "obs_a", label: "obs", status: "rejected", source: "observed" };
    const warning = { id: "obs_b", label: "obs", status: "allowed_warning", source: "observed" };
    const allowed = { id: "obs_c", label: "obs", status: "allowed", source: "observed" };
    const windows = [rejected, five, allowed, weekly, warning, null as never];
    const ids = (w: typeof windows) => summaryWindows({ ...account, report: { ...account.report!, windows: w } }).map((x) => x.id);
    expect(ids(windows)).toEqual(["obs_a", "five_hour", "weekly", "obs_b"]);
    // Without summary flags the first percent window stays the only meter, the status ones still show.
    const unflagged = windows.map((w) => (w ? { ...w, summary: false } : w));
    expect(ids(unflagged)).toEqual(["obs_a", "five_hour", "obs_b"]);
  });
});
