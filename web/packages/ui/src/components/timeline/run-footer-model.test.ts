import { describe, expect, it } from "vitest";

import {
  describeRunFooter,
  formatAbsoluteTime,
  formatDuration,
  formatRelativeTime,
  resolveRunModelLabel,
  type RunFooterRun,
} from "@/components/timeline/run-footer-model";

const NOW = new Date("2026-10-13T12:00:00Z");

function run(overrides: Partial<RunFooterRun> = {}): RunFooterRun {
  return {
    id: "run-1",
    provider: "claude-native",
    status: "done",
    startedAt: "2026-10-13T11:59:51Z", // 9s before NOW
    finishedAt: "2026-10-13T11:59:60Z".replace("60", "12:00:00Z").slice(0, 0) || "2026-10-13T12:00:00Z",
    items: undefined,
    ...overrides,
  } as RunFooterRun;
}

// A run whose finish is recent enough that no age segment renders (the
// footer only ages runs >= 1 min old).
function freshRun(overrides: Partial<RunFooterRun> = {}): RunFooterRun {
  return run({ startedAt: "2026-10-13T11:59:51Z", finishedAt: "2026-10-13T12:00:00Z", ...overrides });
}

describe("formatDuration renders 9s, 1m 42s, 1h 5m and <1s", () => {
  it("renders whole seconds under a minute", () => {
    expect(formatDuration(9_000)).toBe("9s");
  });
  it("renders minutes and seconds under an hour", () => {
    expect(formatDuration(102_000)).toBe("1m 42s");
  });
  it("renders hours and minutes", () => {
    expect(formatDuration(3_900_000)).toBe("1h 5m");
  });
  it("renders <1s under a second", () => {
    expect(formatDuration(400)).toBe("<1s");
    expect(formatDuration(0)).toBe("<1s");
  });
});

describe("resolveRunModelLabel prefers the requested model, then usage.model, then the provider default, then the provider label", () => {
  const labels = {
    "claude-native/claude-sonnet-4-5": "Sonnet 4.5",
    "claude-native/claude-opus-4-6": "Opus 4.6",
    "claude-native/": "Sonnet 4.5",
    "glm/": "GLM Default",
    glm: "GLM",
  };

  it("uses the requested model's catalog label", () => {
    expect(resolveRunModelLabel(run({ model: "claude-opus-4-6" }), labels)).toBe("Opus 4.6");
  });
  it("falls back to the reported usage.model when none was requested", () => {
    expect(resolveRunModelLabel(run({ usage: { model: "claude-opus-4-6", source: "claude_result" } }), labels)).toBe("Opus 4.6");
  });
  it("falls back to the provider's default-model label", () => {
    expect(resolveRunModelLabel(run(), labels)).toBe("Sonnet 4.5");
  });
  it("falls back to the provider label when nothing else is known", () => {
    expect(resolveRunModelLabel(run({ provider: "glm" }), { "glm/": "" }, "GLM")).toBe("GLM");
  });
});

describe("resolveRunModelLabel humanises an uncatalogued claude id and leaves unknown ids raw", () => {
  it("humanises claude ids", () => {
    expect(resolveRunModelLabel(run({ model: "claude-opus-5-5" }))).toBe("Opus 5.5");
    expect(resolveRunModelLabel(run({ model: "claude-sonnet-4-5" }))).toBe("Sonnet 4.5");
  });
  it("drops a dated suffix and a [1m] context suffix", () => {
    expect(resolveRunModelLabel(run({ model: "claude-sonnet-4-5-20250929" }))).toBe("Sonnet 4.5");
    expect(resolveRunModelLabel(run({ model: "claude-opus-5-5[1m]" }))).toBe("Opus 5.5");
  });
  it("leaves non-claude ids raw", () => {
    expect(resolveRunModelLabel(run({ model: "glm-5.3" }))).toBe("glm-5.3");
    expect(resolveRunModelLabel(run({ model: "o4-mini" }))).toBe("o4-mini");
  });
});

describe("describeRunFooter: a normal finish has no status word and no provider id", () => {
  it("words a done run as a plain finish", () => {
    const view = describeRunFooter(freshRun({ stopReason: "end_turn" }), { now: NOW, modelLabel: "Sonnet 5.5" });
    expect(view.kind).toBe("done");
    expect(view.sentence).toBeUndefined();
    expect(view.tone).toBe("neutral");
    expect(view.action).toBeNull();
    expect(view.duration).toBe("9s");
    expect(view.model).toBe("Sonnet 5.5");
    expect(view.ariaLabel).not.toMatch(/done|claude/i);
  });
  it("treats a missing stop reason the same as end_turn", () => {
    const view = describeRunFooter(freshRun({ stopReason: undefined }), { now: NOW, modelLabel: "" });
    expect(view.kind).toBe("done");
  });
  it("never throws on an invalid startedAt (duration null)", () => {
    const view = describeRunFooter(freshRun({ startedAt: "not a date" }), { now: NOW, modelLabel: "" });
    expect(view.duration).toBeNull();
    expect(view.durationTitle).toBeNull();
  });
});

describe('describeRunFooter: cancelled reads "Stopped by you" with a neutral tone', () => {
  it("words a cancel", () => {
    const view = describeRunFooter(freshRun({ stopReason: "cancelled" }), { now: NOW, modelLabel: "Sonnet 5.5" });
    expect(view.sentence).toBe("Stopped by you");
    expect(view.tone).toBe("neutral");
    expect(view.action).toBeNull();
    expect(view.kind).toBe("abnormal");
  });
});

describe("describeRunFooter: max_tokens and max_turn_requests are warnings with a continue action", () => {
  it("max_tokens", () => {
    const view = describeRunFooter(freshRun({ stopReason: "max_tokens" }), { now: NOW, modelLabel: "" });
    expect(view.sentence).toBe("Stopped at the output limit");
    expect(view.tone).toBe("warning");
    expect(view.action).toBe("continue");
  });
  it("max_turn_requests", () => {
    const view = describeRunFooter(freshRun({ stopReason: "max_turn_requests" }), { now: NOW, modelLabel: "" });
    expect(view.sentence).toBe("Hit the tool-turn limit");
    expect(view.tone).toBe("warning");
    expect(view.action).toBe("continue");
  });
});

describe("describeRunFooter: refusal is a warning with no action", () => {
  it("words a refusal", () => {
    const view = describeRunFooter(freshRun({ stopReason: "refusal" }), { now: NOW, modelLabel: "" });
    expect(view.sentence).toBe("The agent declined to continue");
    expect(view.tone).toBe("warning");
    expect(view.action).toBeNull();
  });
});

describe("describeRunFooter: an errored run offers retry", () => {
  it("words a failure", () => {
    const view = describeRunFooter(freshRun({ status: "error", err: "boom" }), { now: NOW, modelLabel: "Sonnet 5.5" });
    expect(view.sentence).toBe("Failed after 9s");
    expect(view.tone).toBe("danger");
    expect(view.action).toBe("retry");
  });
});

describe('describeRunFooter: an unknown stop reason is shown verbatim as "Ended: <reason>"', () => {
  it("words an unknown reason", () => {
    const view = describeRunFooter(freshRun({ stopReason: "whatever" }), { now: NOW, modelLabel: "" });
    expect(view.sentence).toBe("Ended: whatever");
    expect(view.tone).toBe("neutral");
  });
  it("a stopped status with no reason reads just Stopped", () => {
    const view = describeRunFooter(freshRun({ status: "stopped", stopReason: undefined }), { now: NOW, modelLabel: "" });
    expect(view.sentence).toBe("Stopped");
  });
});

describe("describeRunFooter: the aria label carries duration, model, cost and age", () => {
  it("composes the full detail", () => {
    const view = describeRunFooter(
      run({
        startedAt: "2026-10-13T09:59:51Z",
        finishedAt: "2026-10-13T10:00:00Z",
        usage: { costUsd: 0.032, source: "claude_result" },
      }),
      { now: NOW, modelLabel: "Sonnet 5.5" },
    );
    expect(view.ariaLabel).toBe("Finished in 9.0 s with Sonnet 5.5, cost $0.032, 2h ago");
    expect(view.age).toEqual({ label: "2h ago", title: formatAbsoluteTime(new Date("2026-10-13T10:00:00Z")) });
  });
});

describe("formatRelativeTime and formatAbsoluteTime", () => {
  it("words the ages", () => {
    const now = new Date("2026-10-13T12:00:00Z");
    expect(formatRelativeTime(new Date("2026-10-13T11:59:30Z"), now)).toBe("just now");
    expect(formatRelativeTime(new Date("2026-10-13T11:55:00Z"), now)).toBe("5m ago");
    expect(formatRelativeTime(new Date("2026-10-13T10:00:00Z"), now)).toBe("2h ago");
    expect(formatRelativeTime(new Date("2026-10-10T12:00:00Z"), now)).toBe("3d ago");
    expect(formatRelativeTime(new Date("2026-09-01T12:00:00Z"), now)).toMatch(/^[A-Z][a-z]{2} \d{1,2}$/);
  });
  it("formats the absolute title", () => {
    expect(formatAbsoluteTime(new Date("2026-10-13T10:00:00Z"))).toBe(
      new Date("2026-10-13T10:00:00Z").toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" }),
    );
  });
});

describe("describeRunFooter: running", () => {
  it("words a live run with its elapsed duration", () => {
    const view = describeRunFooter(run({ status: "running", finishedAt: undefined }), { now: NOW, modelLabel: "" });
    expect(view.kind).toBe("running");
    expect(view.duration).toBe("9s");
    expect(view.age).toBeNull();
  });
});
