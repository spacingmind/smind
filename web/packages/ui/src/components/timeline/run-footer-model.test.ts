import { describe, expect, it } from "vitest";

import {
  continuePrompt,
  describeRunFooter,
  formatAbsoluteTime,
  formatDuration,
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

describe("TestModelDisplay_UnknownIDShownRaw", () => {
  it("shows an uncatalogued instance model id raw, suffix and all", () => {
    expect(resolveRunModelLabel(run({ provider: "zai-glm", model: "glm-5.3[1m]" }), {})).toBe("glm-5.3[1m]");
    expect(resolveRunModelLabel(run({ provider: "zai-glm", model: "glm-5.3[1m]" }), undefined)).toBe("glm-5.3[1m]");
  });
  it("does not pick up a sibling model's label for the same instance", () => {
    expect(resolveRunModelLabel(run({ provider: "zai-glm", model: "deepseek-flash" }), { "zai-glm/other": "Other" })).toBe("deepseek-flash");
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

describe("describeRunFooter: the daemon's needs-continue disposition (ADR-0027 D13)", () => {
  it("reads as not finished and offers Continue for the caps it knows", () => {
    const turn = describeRunFooter(freshRun({ stopReason: "max_turn_requests", disposition: "needs-continue" }), { now: NOW, modelLabel: "" });
    expect(turn.sentence).toBe("Hit the tool-turn limit · not finished");
    expect(turn.tone).toBe("warning");
    expect(turn.action).toBe("continue");
    const tokens = describeRunFooter(freshRun({ stopReason: "max_tokens", disposition: "needs-continue" }), { now: NOW, modelLabel: "" });
    expect(tokens.sentence).toBe("Stopped at the output limit · not finished");
  });
  it("trusts the disposition for a cap this client has never heard of", () => {
    const budget = describeRunFooter(freshRun({ stopReason: "budget_exceeded", disposition: "needs-continue" }), { now: NOW, modelLabel: "" });
    expect(budget.sentence).toBe("Stopped at the budget limit · not finished");
    expect(budget.action).toBe("continue");
    // Unlisted reason: still a warning with Continue, never the neutral "Ended: …".
    const other = describeRunFooter(freshRun({ stopReason: "some_future_cap", disposition: "needs-continue" }), { now: NOW, modelLabel: "" });
    expect(other.sentence).toBe("Stopped at a limit · not finished");
    expect(other.tone).toBe("warning");
    expect(other.action).toBe("continue");
  });
  it("is ignored unless the run is done, and an older daemon (no disposition) is unchanged", () => {
    const failed = describeRunFooter(freshRun({ status: "error", stopReason: "max_turn_requests" }), { now: NOW, modelLabel: "" });
    expect(failed.sentence).toMatch(/^Failed/);
    expect(failed.action).toBe("retry");
    const old = describeRunFooter(freshRun({ stopReason: "max_turn_requests" }), { now: NOW, modelLabel: "" });
    expect(old.sentence).toBe("Hit the tool-turn limit");
    expect(old.action).toBe("continue");
    const normal = describeRunFooter(freshRun({ stopReason: "end_turn" }), { now: NOW, modelLabel: "" });
    expect(normal.sentence).toBeUndefined();
  });
});

describe("describeRunFooter: a run the daemon's restart cut off", () => {
  it("a graceful restart (stopped + daemon_restart + needs-continue) reads as interrupted and offers Continue", () => {
    const view = describeRunFooter(
      freshRun({ status: "stopped", stopReason: "daemon_restart", disposition: "needs-continue" }),
      { now: NOW, modelLabel: "Haiku 5.5" },
    );
    expect(view.sentence).toBe("Interrupted when the daemon restarted · not finished");
    expect(view.tone).toBe("warning");
    expect(view.action).toBe("continue");
  });
  it("a crash (interrupted status) reads the same", () => {
    const view = describeRunFooter(
      freshRun({ status: "interrupted", stopReason: "daemon_restart", disposition: "needs-continue" }),
      { now: NOW, modelLabel: "" },
    );
    expect(view.sentence).toBe("Interrupted when the daemon restarted · not finished");
    expect(view.action).toBe("continue");
  });
  it("with no session to resume it says why but offers nothing", () => {
    const view = describeRunFooter(freshRun({ status: "stopped", stopReason: "daemon_restart_fresh" }), { now: NOW, modelLabel: "" });
    expect(view.sentence).toBe("Interrupted when the daemon restarted");
    expect(view.tone).toBe("warning");
    expect(view.action).toBeNull();
  });
  it("a plain Stop with no reason is still just Stopped", () => {
    const view = describeRunFooter(freshRun({ status: "stopped", stopReason: undefined }), { now: NOW, modelLabel: "" });
    expect(view.sentence).toBe("Stopped");
    expect(view.action).toBeNull();
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

// ADR-0041 R3: a provider quota reads as a warning with Retry, not a code failure.
// Local-time fixtures (the sentence is local HH:MM): 2026-10-13 is a Tuesday.
describe("describeRunFooter: a rate-limited run", () => {
  const LOCAL_NOW = new Date(2026, 9, 13, 10, 0, 0);
  const limited = (overrides: Partial<RunFooterRun> = {}): RunFooterRun =>
    run({
      status: "error",
      err: "429 Usage limit reached",
      failureKind: "rate_limited",
      failureWindow: "5h",
      startedAt: new Date(LOCAL_NOW.getTime() - 9_000).toISOString(),
      finishedAt: LOCAL_NOW.toISOString(),
      ...overrides,
    });
  const view = (r: RunFooterRun) => describeRunFooter(r, { now: LOCAL_NOW, modelLabel: "GLM 5.3" });

  it("footer sentence has ≈ for an inferred reset", () => {
    const at = new Date(2026, 9, 13, 17, 35).toISOString();
    expect(view(limited({ failureResetAt: at, failureResetInferred: true })).sentence).toBe("Rate limited (5h window) · resets ≈17:35");
    expect(view(limited({ failureResetAt: at, failureResetInferred: false })).sentence).toBe("Rate limited (5h window) · resets 17:35");
  });

  it("rate limited with a future reset", () => {
    const v = view(limited({ failureResetAt: new Date(2026, 9, 13, 17, 35).toISOString() }));
    expect(v.kind).toBe("abnormal");
    expect(v.sentence).toBe("Rate limited (5h window) · resets 17:35");
    expect(v.tone).toBe("warning");
    expect(v.action).toBe("retry");
  });

  it("rate limited reset today vs another day", () => {
    expect(view(limited({ failureResetAt: new Date(2026, 9, 13, 23, 59).toISOString() })).sentence).toBe(
      "Rate limited (5h window) · resets 23:59",
    );
    expect(view(limited({ failureResetAt: new Date(2026, 9, 15, 9, 5).toISOString() })).sentence).toBe(
      "Rate limited (5h window) · resets Thu 09:05",
    );
    // The window part is omitted when unknown.
    expect(view(limited({ failureWindow: undefined, failureResetAt: new Date(2026, 9, 13, 17, 35).toISOString() })).sentence).toBe(
      "Rate limited · resets 17:35",
    );
  });

  it("rate limited with unknown reset", () => {
    expect(view(limited()).sentence).toBe("Rate limited (5h window) · reset time unknown");
    expect(view(limited({ failureResetAt: "not a date" })).sentence).toBe("Rate limited (5h window) · reset time unknown");
  });

  it("rate limited after the reset passed", () => {
    const v = view(limited({ failureResetAt: new Date(2026, 9, 13, 9, 0).toISOString() }));
    expect(v.sentence).toBe("Rate limited (5h window) · limit has reset");
    expect(v.tone).toBe("warning");
    expect(v.action).toBe("retry");
  });

  it("plain error footer unchanged", () => {
    const plain = describeRunFooter(freshRun({ status: "error", err: "boom" }), { now: NOW, modelLabel: "Sonnet 5.5" });
    expect(plain.sentence).toBe("Failed after 9s");
    expect(plain.tone).toBe("danger");
    expect(plain.duration).toBeNull();
    // A failure kind this client does not know, or a rate_limited kind on a non-error run, changes nothing either.
    expect(describeRunFooter(freshRun({ status: "error", failureKind: "other" }), { now: NOW, modelLabel: "Sonnet 5.5" })).toEqual(plain);
    expect(describeRunFooter(freshRun({ failureKind: "rate_limited" }), { now: NOW, modelLabel: "" }).kind).toBe("done");
  });

  it("aria label carries the rate limit wording", () => {
    const v = view(limited({ failureResetAt: new Date(2026, 9, 13, 17, 35).toISOString() }));
    expect(v.ariaLabel).toContain("Rate limited (5h window) · resets 17:35");
    expect(v.ariaLabel).toContain("with GLM 5.3");
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

describe("formatAbsoluteTime", () => {
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

// J1: the daemon derives needs-continue for a resumable error run (cli_exited,
// or rate_limited past its reset, with a stored session); the footer only obeys it.
describe("describeRunFooter: Continue on an errored run follows the daemon's disposition (J1)", () => {
  const view = (overrides: Partial<RunFooterRun>) => describeRunFooter(freshRun({ status: "error", ...overrides }), { now: NOW, modelLabel: "" });

  it("cli_exited with the disposition offers Continue, keeping the danger sentence", () => {
    const v = view({ failureKind: "cli_exited", disposition: "needs-continue" });
    expect(v.action).toBe("continue");
    expect(v.tone).toBe("danger");
    expect(v.sentence).toBe("Failed after 9s");
  });
  it("cli_exited without the disposition (no stored session) offers Retry only", () => {
    expect(view({ failureKind: "cli_exited" }).action).toBe("retry");
  });
  it.each([
    ["permission denied / timeout (no kind)", undefined],
    ["auth", "auth"],
    ["invalid model", "invalid_model"],
    ["refused", "refused"],
  ])("a %s failure has no disposition and so no Continue", (_name, failureKind) => {
    expect(view({ failureKind }).action).toBe("retry");
  });
  it("rate_limited before its reset shows the reset text and Retry, no Continue", () => {
    const v = view({ failureKind: "rate_limited", failureWindow: "5h", failureResetAt: "2026-10-13T14:00:00Z" });
    expect(v.action).toBe("retry");
    expect(v.sentence).toMatch(/^Rate limited \(5h window\) · resets /);
  });
  it("rate_limited after its reset with the disposition offers Continue", () => {
    const v = view({ failureKind: "rate_limited", failureWindow: "5h", failureResetAt: "2026-10-13T11:00:00Z", disposition: "needs-continue" });
    expect(v.action).toBe("continue");
    expect(v.sentence).toBe("Rate limited (5h window) · limit has reset");
    expect(v.tone).toBe("warning");
  });
  it("a stopped-by-you run never offers Continue", () => {
    const v = describeRunFooter(freshRun({ status: "stopped", stopReason: "cancelled" }), { now: NOW, modelLabel: "" });
    expect(v.action).toBeNull();
  });
});

describe("continuePrompt", () => {
  it("is the plain Continue for a capped or restart-interrupted run", () => {
    expect(continuePrompt({ status: "done" })).toBe("Continue");
    expect(continuePrompt({ status: "stopped" })).toBe("Continue");
  });
  it("is one line naming the failure for an errored run", () => {
    const p = continuePrompt({ status: "error", failureKind: "cli_exited" });
    expect(p).toBe("Continue from where you left off (the previous run ended unexpectedly: the agent CLI exited without a result).");
    expect(p).not.toContain("\n");
    expect(continuePrompt({ status: "error", failureKind: "rate_limited" })).toContain("provider rate limit");
    expect(continuePrompt({ status: "error" })).toContain("an unexpected error");
  });
});
