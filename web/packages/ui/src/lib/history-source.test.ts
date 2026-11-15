import { describe, expect, it } from "vitest";

import { describeHistorySource, runWasTrimmed } from "@/lib/history-source";
import type { TimelineItem } from "@/hooks/use-run-timeline";

const skeleton = (reason: string, canLoadFull?: boolean) => ({ source: "skeleton" as const, reason, canLoadFull });

const text = (kind: "assistant" | "user" | "thinking", text: string): TimelineItem => ({ kind, id: `${kind}-1`, text });

const tool = (payload: { input?: unknown; result?: unknown }): TimelineItem => ({ kind: "tool_call", id: "tool-1", toolCallId: "t1", status: "success", ...payload });

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
    expect(describeHistorySource(skeleton("excerpt_pruned", true))).toEqual({
      tone: "info",
      message: "Showing a summary: large tool output was removed after the retention period.",
      canLoad: true,
    });
  });

  it("confirm_required is an info row the human can act on", () => {
    expect(describeHistorySource(skeleton("confirm_required", true))).toEqual({
      tone: "info",
      message: "Showing a summary: loading this history needs your confirmation.",
      canLoad: true,
    });
    expect(describeHistorySource(skeleton("confirm_required", false)).canLoad).toBe(false);
  });

  it("an unknown reason falls back to the neutral summary sentence", () => {
    expect(describeHistorySource(skeleton("something_new"))).toEqual({ tone: "info", message: "Showing a summary of this run.", canLoad: false });
  });

  it("rebuild_not_requested without canLoadFull offers no button", () => {
    expect(describeHistorySource(skeleton("rebuild_not_requested", false)).canLoad).toBe(false);
    expect(describeHistorySource(skeleton("rebuild_not_requested")).canLoad).toBe(false);
  });

  it("offers the button again after a failure that may pass (and only when the daemon says it can)", () => {
    for (const reason of ["native_session_in_use", "rebuild_failed", "rebuild_busy", "excerpt_pruned"]) {
      expect(describeHistorySource(skeleton(reason, true)).canLoad, reason).toBe(true);
      expect(describeHistorySource(skeleton(reason, false)).canLoad, reason).toBe(false);
    }
    for (const reason of ["provider_cannot_rebuild", "native_session_missing", "slice_not_found", "format_drift"]) {
      expect(describeHistorySource(skeleton(reason, true)).canLoad, `${reason} is not retryable`).toBe(false);
    }
  });

  it("a rebuilt history states it plainly, with no button", () => {
    expect(describeHistorySource({ source: "rebuilt" })).toEqual({
      tone: "info",
      message: "Full history, rebuilt from the provider's session.",
      canLoad: false,
    });
  });

  it("describe-history-source-thinking-copy", () => {
    expect(describeHistorySource(skeleton("rebuild_not_requested", true), "thinking")).toEqual({
      tone: "info",
      message: "Showing a summary: the agent's reasoning was not kept.",
      canLoad: true,
    });
    expect(describeHistorySource(skeleton("rebuild_not_requested", false), "thinking").canLoad).toBe(false);
    expect(describeHistorySource(skeleton("rebuild_not_requested", true), "output").message).toBe("Showing a summary: large tool output was trimmed.");
    expect(describeHistorySource(skeleton("rebuild_not_requested", true), null).message).toBe("Showing a summary: large tool output was trimmed.");
    expect(describeHistorySource(skeleton("rebuild_not_requested", true)).message).toBe("Showing a summary: large tool output was trimmed.");
    // Only rebuild_not_requested rewords itself: every other reason keeps its exact sentence whatever trim says.
    expect(describeHistorySource(skeleton("excerpt_pruned", true), "thinking").message).toBe("Showing a summary: large tool output was removed after the retention period.");
    expect(describeHistorySource(skeleton("native_session_missing"), "output").message).toBe("Showing a summary: the provider's session file is no longer there.");
  });
});

describe("runWasTrimmed", () => {
  it("run-was-trimmed-table", () => {
    const circular: { self?: unknown } = {};
    circular.self = circular;

    const cases: readonly [string, readonly TimelineItem[] | undefined, "output" | "thinking" | null][] = [
      ["no items at all", undefined, null],
      ["an empty list", [], null],
      ["a plain assistant text", [text("assistant", "the answer")], null],
      ["a tool result string with the omission marker", [tool({ result: "head\n…[8192 bytes omitted]…\ntail" })], "output"],
      ["the marker inside a tool input object", [tool({ input: { command: "cat big.log", stdout: "…[4096 bytes omitted]…" } })], "output"],
      ["the marker in an assistant text", [text("assistant", "…[2048 bytes omitted]…")], "output"],
      ["the marker in a user text", [text("user", "…[2048 bytes omitted]…")], "output"],
      ["a thinking item with no text and nothing else", [text("thinking", "")], "thinking"],
      ["a thinking item with text kept", [text("thinking", "so the plan is")], null],
      ["a marker alongside dropped thinking", [text("thinking", ""), text("assistant", "…[2048 bytes omitted]…")], "output"],
      ["dropped thinking before a later marker", [text("thinking", ""), tool({ result: "…[2048 bytes omitted]…" })], "output"],
      ["a lookalike marker without the ellipses", [text("assistant", "head [100 bytes omitted] tail"), tool({ result: "[100 bytes omitted]" })], null],
    ];
    for (const [name, items, expected] of cases) {
      expect(runWasTrimmed(items), name).toBe(expected);
    }

    // A circular tool result makes JSON.stringify throw; the scan must return, not raise.
    expect(runWasTrimmed([tool({ result: circular })])).toBe(null);
  });
});
