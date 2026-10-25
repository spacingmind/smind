import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

import { RunFooter } from "@/components/timeline/run-footer";
import type { RunFooterRun } from "@/components/timeline/run-footer-model";

const BASE = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const HEAD = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";

function run(overrides: Partial<RunFooterRun> = {}): RunFooterRun {
  return {
    id: "run-1",
    provider: "claude-native",
    status: "done",
    startedAt: "2024-01-01T00:00:00Z",
    finishedAt: "2024-01-01T00:00:05Z",
    stopReason: "end_turn",
    ...overrides,
  };
}

afterEach(() => cleanup());

describe("RunFooter provenance segment", () => {
  it("mounts the provenance segment when shas are known", () => {
    render(<RunFooter run={run({ baseSha: BASE, headSha: HEAD })} />);
    expect(screen.getByTestId("run-provenance").textContent).toContain("bbbbbbb");
  });

  it("mounts nothing when neither sha is known", () => {
    const { rerender } = render(<RunFooter run={run()} />);
    expect(screen.queryByTestId("run-provenance")).toBeNull();
    // No stray empty segment either: a terminal run without shas has the
    // same run-meta child count as one with a provenance segment minus one.
    rerender(<RunFooter run={run({ baseSha: BASE, headSha: HEAD })} />);
    expect(screen.getByTestId("run-provenance")).toBeTruthy();
  });

  it("renders no separator segment for a sha-less terminal run", () => {
    const { rerender } = render(<RunFooter run={run({ usage: undefined })} />);
    const without = screen.getByTestId("run-meta").childElementCount;
    rerender(<RunFooter run={run({ usage: undefined, baseSha: BASE, headSha: HEAD })} />);
    const withProvenance = screen.getByTestId("run-meta").childElementCount;
    expect(without).toBe(withProvenance - 1);
    rerender(<RunFooter run={run({ usage: undefined })} />);
    expect(screen.getByTestId("run-meta").childElementCount).toBe(without);
    expect(screen.queryByTestId("run-provenance")).toBeNull();
  });
});

describe("RunFooter usage", () => {
  it("run footer renders no usage pill or cost text for a run with usage", () => {
    render(<RunFooter run={run({ usage: { inputTokens: 12345, outputTokens: 1234, costUsd: 0.03, source: "claude_result" } })} />);
    expect(screen.queryByTestId("run-usage-pill")).toBeNull();
    const text = screen.getByTestId("run-footer").textContent ?? "";
    expect(text).not.toContain("$");
    expect(text).not.toContain("tokens");
  });
});
