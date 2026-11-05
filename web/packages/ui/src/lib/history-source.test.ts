import { describe, expect, it } from "vitest";

import { describeHistorySource } from "@/lib/history-source";

const skeleton = (reason: string, canLoadFull?: boolean) => ({ source: "skeleton" as const, reason, canLoadFull });

describe("describeHistorySource", () => {
  it("renders every skeletonReason the daemon sends with its exact sentence and tone", () => {
    expect(describeHistorySource(skeleton("rebuild_not_requested", true))).toEqual({
      tone: "info",
      message: "Showing a summary: large tool output was trimmed.",
      canLoad: true,
    });
    expect(describeHistorySource(skeleton("provider_cannot_rebuild"))).toEqual({
      tone: "info",
      message: "Showing a summary: this provider does not keep a history smind can read.",
      canLoad: false,
    });
    expect(describeHistorySource(skeleton("native_session_missing"))).toEqual({
      tone: "warning",
      message: "Showing a summary: the provider's session file is no longer there.",
      canLoad: false,
    });
    expect(describeHistorySource(skeleton("native_session_in_use"))).toEqual({
      tone: "info",
      message: "Showing a summary: this chat is running. Try again when it ends.",
      canLoad: false,
    });
    expect(describeHistorySource(skeleton("slice_not_found"))).toEqual({
      tone: "warning",
      message: "Showing a summary: this turn could not be matched in the provider's session.",
      canLoad: false,
    });
    expect(describeHistorySource(skeleton("format_drift"))).toEqual({
      tone: "warning",
      message: "Showing a summary: the provider's session format was not recognised.",
      canLoad: false,
    });
    expect(describeHistorySource(skeleton("rebuild_failed"))).toEqual({
      tone: "warning",
      message: "Showing a summary: the full history could not be loaded (it may be too large or took too long).",
      canLoad: false,
    });
    expect(describeHistorySource(skeleton("rebuild_busy"))).toEqual({
      tone: "info",
      message: "Showing a summary: another history is loading. Try again in a moment.",
      canLoad: false,
    });
  });

  it("an unknown reason falls back to the neutral summary sentence", () => {
    expect(describeHistorySource(skeleton("something_new"))).toEqual({ tone: "info", message: "Showing a summary of this run.", canLoad: false });
  });

  it("rebuild_not_requested without canLoadFull offers no button", () => {
    expect(describeHistorySource(skeleton("rebuild_not_requested", false)).canLoad).toBe(false);
    expect(describeHistorySource(skeleton("rebuild_not_requested")).canLoad).toBe(false);
  });

  it("a rebuilt history states it plainly, with no button", () => {
    expect(describeHistorySource({ source: "rebuilt" })).toEqual({
      tone: "info",
      message: "Full history, rebuilt from the provider's session.",
      canLoad: false,
    });
  });
});
